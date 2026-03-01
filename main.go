// CLAUDE:SUMMARY Entry point for horostracker server — CLI dispatch (serve, version, help), HTTP server startup with graceful shutdown
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/hazyhaar/horostracker/internal/api"
	"github.com/hazyhaar/horostracker/internal/auth"
	"github.com/hazyhaar/horostracker/internal/config"
	"github.com/hazyhaar/horostracker/internal/db"
	"github.com/hazyhaar/horostracker/internal/llm"
	horosmcp "github.com/hazyhaar/horostracker/internal/mcp"
	"github.com/hazyhaar/pkg/audit"
	"github.com/hazyhaar/pkg/chassis"
	"github.com/hazyhaar/pkg/feedback"
	"github.com/hazyhaar/pkg/mcprt"
	"github.com/hazyhaar/pkg/trace"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "version":
		fmt.Printf("horostracker %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`horostracker — proof-tree search engine

Usage:
  horostracker serve [--config config.toml] [--addr :8080]
  horostracker version
  horostracker help

Commands:
  serve     Start the server (TCP/HTTP2 + QUIC/HTTP3 + MCP)
  version   Print version
  help      Show this help`)
}

func cmdServe(args []string) {
	logger := slog.Default()

	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.toml")
	addr := fs.String("addr", "", "listen address (overrides config)")
	plainHTTP := fs.Bool("http", false, "plain HTTP mode (no TLS, no QUIC) for local dev")
	_ = fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("loading config", "error", err)
		os.Exit(1)
	}
	if *addr != "" {
		cfg.Server.Addr = *addr
	}

	// --- Databases ---
	database, err := db.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("opening database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	sqlDB := database.DB // underlying *sql.DB

	flowsDB, err := db.OpenFlows(cfg.Database.FlowsPath)
	if err != nil {
		logger.Error("opening flows database", "error", err)
		os.Exit(1) //nolint:gocritic // exitAfterDefer acceptable in main()
	}
	defer flowsDB.Close()

	metricsDB, err := db.OpenMetrics(cfg.Database.MetricsPath)
	if err != nil {
		logger.Error("opening metrics database", "error", err)
		os.Exit(1)
	}
	defer metricsDB.Close()

	// --- Trace store ---
	traceStore := trace.NewStore(sqlDB)
	defer traceStore.Close()

	// --- Audit logger ---
	auditLog := audit.NewSQLiteLogger(sqlDB)
	defer auditLog.Close()

	// --- MCP tool registry (flight control) ---
	registry := mcprt.NewRegistry(sqlDB)
	if initErr := registry.Init(); initErr != nil {
		logger.Error("init MCP tool registry", "error", initErr)
		os.Exit(1)
	}
	horosmcp.SeedDefaultTools(sqlDB)

	// --- Context with signal-based cancellation ---
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Load dynamic tools + start watcher
	if loadErr := registry.LoadTools(ctx); loadErr != nil {
		logger.Warn("loading dynamic MCP tools", "error", loadErr)
	}
	go registry.RunWatcher(ctx)

	// --- LLM client + flow engine + resolution + challenges + replay ---
	llmClient := llm.NewFromConfig(cfg.LLM)
	flowEngine := llm.NewFlowEngine(llmClient, flowsDB, logger)
	resEngine := llm.NewResolutionEngine(llmClient, flowsDB, logger)
	challengeRunner := llm.NewChallengeRunner(flowEngine, database, logger)
	replayEngine := llm.NewReplayEngine(llmClient, flowsDB, logger)
	workflowEngine := llm.NewWorkflowEngine(llmClient, flowsDB, logger)
	modelDiscovery := llm.NewModelDiscovery(flowsDB, llmClient, logger)

	providerCount := len(llmClient.Providers())
	if providerCount > 0 {
		logger.Info("LLM providers configured", "count", providerCount, "providers", llmClient.Providers())
	} else {
		logger.Info("no LLM providers configured — human-only mode")
	}

	// --- MCP server (core tools + dynamic tools) ---
	mcpServer := horosmcp.NewServer(database, auditLog)
	mcprt.Bridge(mcpServer, registry)

	// --- Bot user (auto-create @horostracker) ---
	var botUserID string
	if cfg.Bot.Enabled {
		botHash, _ := auth.New(cfg.Auth.JWTSecret, 0).HashPassword("bot-internal-" + cfg.Auth.JWTSecret)
		botID, botErr := database.EnsureBotUser(cfg.Bot.Handle, botHash)
		if botErr != nil {
			logger.Error("creating bot user", "error", botErr)
		} else {
			botUserID = botID
			// Ensure daily credit allowance
			_ = database.AddCredits(botID, cfg.Bot.CreditPerDay, "daily_allowance", "system", "startup")
			logger.Info("bot user ready", "handle", cfg.Bot.Handle, "id", botID)
		}
	}

	// --- Seed core workflows + discover models ---
	if botUserID != "" {
		llm.SeedCoreWorkflows(flowsDB, botUserID, logger)
	}
	go modelDiscovery.DiscoverAll(ctx)

	// --- HTTP mux (API + static) ---
	a := auth.New(cfg.Auth.JWTSecret, cfg.Auth.TokenExpiryMin)
	apiHandler := api.New(database, a)
	apiHandler.SetResolutionEngine(resEngine)
	apiHandler.SetChallengeRunner(challengeRunner)
	apiHandler.SetReplayEngine(replayEngine)
	apiHandler.SetWorkflowEngine(workflowEngine)
	apiHandler.SetModelDiscovery(modelDiscovery)
	apiHandler.SetFlowsDB(flowsDB, cfg.Database.FlowsPath)
	apiHandler.SetMetricsDB(metricsDB, cfg.Database.MetricsPath)
	apiHandler.SetLLMClient(llmClient)
	apiHandler.SetBotUserID(botUserID)
	apiHandler.SetFederationConfig(cfg.Federation, cfg.Instance)

	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	// --- Feedback widget ---
	fbWidget, err := feedback.New(feedback.Config{
		DB:      sqlDB,
		AppName: "horostracker",
		UserIDFn: func(r *http.Request) string {
			if claims := a.ExtractClaims(r); claims != nil {
				return claims.UserID
			}
			return ""
		},
	})
	if err != nil {
		logger.Error("feedback widget", "error", err)
	} else {
		fbWidget.RegisterMux(mux, "/feedback")
	}

	staticFS := http.FileServer(http.Dir("static"))
	mux.Handle("GET /static/", api.NoCacheStatic(http.StripPrefix("/static/", staticFS)))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	// Count nodes for startup display
	var nodeCount int
	_ = database.QueryRow("SELECT COUNT(*) FROM nodes").Scan(&nodeCount)
	var flowStepCount int
	_ = flowsDB.QueryRow("SELECT COUNT(*) FROM flow_steps").Scan(&flowStepCount)
	workflowCount := flowsDB.CountWorkflows()

	handler := api.SecurityHeaders(mux)
	errCh := make(chan error, 1)
	var shutdownFn func()

	if *plainHTTP {
		// --- Plain HTTP mode (local dev) ---
		httpSrv := &http.Server{
			Addr:    cfg.Server.Addr,
			Handler: handler,
		}

		logger.Info("horostracker starting (plain HTTP)",
			"version", version,
			"addr", cfg.Server.Addr,
			"node_count", nodeCount,
			"flow_steps", flowStepCount,
			"workflows", workflowCount,
			"providers", providerCount,
			"bot", cfg.Bot.Enabled,
		)

		shutdownFn = func() {
			sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer sCancel()
			_ = httpSrv.Shutdown(sCtx)
		}

		go func() {
			errCh <- httpSrv.ListenAndServe()
		}()
	} else {
		// --- QUIC chassis (TLS) ---
		srv, err := chassis.New(chassis.Config{
			Addr:      cfg.Server.Addr,
			CertFile:  cfg.Server.CertFile,
			KeyFile:   cfg.Server.KeyFile,
			Handler:   handler,
			MCPServer: mcpServer,
			Logger:    logger,
		})
		if err != nil {
			logger.Error("creating chassis", "error", err)
			os.Exit(1)
		}

		logger.Info("horostracker starting",
			"version", version,
			"binary_hash", api.BinaryHash(),
			"go_version", runtime.Version(),
			"addr", cfg.Server.Addr,
			"nodes_db", cfg.Database.Path,
			"flows_db", cfg.Database.FlowsPath,
			"metrics_db", cfg.Database.MetricsPath,
			"node_count", nodeCount,
			"flow_steps", flowStepCount,
			"workflows", workflowCount,
			"providers", providerCount,
			"bot", cfg.Bot.Enabled,
			"federation", cfg.Federation.Enabled,
			"tcp", "HTTP/1.1+HTTP/2 (TLS)",
			"udp", "QUIC (HTTP/3 + MCP)",
		)

		shutdownFn = func() {
			sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer sCancel()
			_ = srv.Stop(sCtx)
		}

		go func() {
			errCh <- srv.Start(ctx)
		}()
	}

	// Keep references alive for future middleware wiring
	_ = traceStore

	// Wait for signal or error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			logger.Error("server error", "error", err)
		}
	}

	shutdownFn()
	logger.Info("horostracker stopped")
}
