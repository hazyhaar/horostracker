package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/hazyhaar/horostracker/internal/api"
	"github.com/hazyhaar/horostracker/internal/auth"
	"github.com/hazyhaar/horostracker/internal/config"
	"github.com/hazyhaar/horostracker/internal/db"
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
  serve     Start the HTTP server
  version   Print version
  help      Show this help`)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.toml")
	addr := fs.String("addr", "", "listen address (overrides config)")
	fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	if *addr != "" {
		cfg.Server.Addr = *addr
	}

	database, err := openDB(cfg)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer database.Close()

	a := auth.New(cfg.Auth.JWTSecret, cfg.Auth.TokenExpiryMin)
	apiHandler := api.New(database, a)

	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)

	// Serve static files
	staticFS := http.FileServer(http.Dir("static"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFS))

	// SPA: serve index.html for all non-API, non-static routes
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/index.html")
	})

	log.Printf("horostracker %s listening on %s", version, cfg.Server.Addr)
	log.Printf("database: %s", cfg.Database.Path)
	if cfg.Federation.Enabled {
		log.Printf("federation: enabled")
	} else {
		log.Printf("federation: disabled (mononode)")
	}

	if err := http.ListenAndServe(cfg.Server.Addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func openDB(cfg *config.Config) (*db.DB, error) {
	return db.Open(cfg.Database.Path)
}
