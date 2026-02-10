package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/hazyhaar/horostracker/internal/api"
	"github.com/hazyhaar/horostracker/internal/auth"
	"github.com/hazyhaar/horostracker/internal/config"
	"github.com/hazyhaar/horostracker/internal/db"
	horosmcp "github.com/hazyhaar/horostracker/internal/mcp"
	"github.com/hazyhaar/horostracker/pkg/audit"
	"github.com/hazyhaar/horostracker/pkg/chassis"
	"github.com/hazyhaar/horostracker/pkg/mcprt"
	"github.com/hazyhaar/horostracker/pkg/trace"
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
	fs.Parse(args)

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
		os.Exit(1)
	}
	defer flowsDB.Close()

	// --- Trace store ---
	traceStore := trace.NewStore(sqlDB)
	defer traceStore.Close()

	// --- Audit logger ---
	auditLog := audit.NewSQLiteLogger(sqlDB)
	defer auditLog.Close()

	// --- MCP tool registry (flight control) ---
	registry := mcprt.NewRegistry(sqlDB)
	if err := registry.Init(); err != nil {
		logger.Error("init MCP tool registry", "error", err)
		os.Exit(1)
	}
	horosmcp.SeedDefaultTools(sqlDB)

	// --- Context with signal-based cancellation ---
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Load dynamic tools + start watcher
	if err := registry.LoadTools(ctx); err != nil {
		logger.Warn("loading dynamic MCP tools", "error", err)
	}
	go registry.RunWatcher(ctx)

	// --- MCP server (core tools + dynamic tools) ---
	mcpServer := horosmcp.NewServer(database, auditLog)
	mcprt.Bridge(mcpServer, registry)

	// --- HTTP mux (API + static) ---
	a := auth.New(cfg.Auth.JWTSecret, cfg.Auth.TokenExpiryMin)
	apiHandler := api.New(database, a)

	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	staticFS := http.FileServer(http.Dir("static"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFS))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	// --- QUIC chassis ---
	srv, err := chassis.New(chassis.Config{
		Addr:      cfg.Server.Addr,
		CertFile:  cfg.Server.CertFile,
		KeyFile:   cfg.Server.KeyFile,
		Handler:   mux,
		MCPServer: mcpServer,
		Logger:    logger,
	})
	if err != nil {
		logger.Error("creating chassis", "error", err)
		os.Exit(1)
	}

	logger.Info("horostracker starting",
		"version", version,
		"addr", cfg.Server.Addr,
		"nodes_db", cfg.Database.Path,
		"flows_db", cfg.Database.FlowsPath,
		"federation", cfg.Federation.Enabled,
		"tcp", "HTTP/1.1+HTTP/2 (TLS)",
		"udp", "QUIC (HTTP/3 + MCP)",
	)

	// Start chassis in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Keep references alive for future middleware wiring
	_ = traceStore
	_ = flowsDB

	// Wait for signal or error
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			logger.Error("server error", "error", err)
		}
	}

	// Graceful shutdown
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5e9) // 5s
	defer stopCancel()
	if err := srv.Stop(stopCtx); err != nil {
		logger.Error("shutdown error", "error", err)
	}

	logger.Info("horostracker stopped")
}
