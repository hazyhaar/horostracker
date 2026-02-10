// Package chassis provides a unified QUIC server that multiplexes HTTP/3 and
// MCP-over-QUIC on a single UDP port via ALPN routing.
//
// ALPN "h3"            → HTTP/3 handler (API + static files)
// ALPN "horos-mcp-v1"  → MCP JSON-RPC over QUIC stream
//
// In development mode, a self-signed ECDSA P-256 cert is generated automatically.
// In production, supply cert/key files via config.
package chassis

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"

	"github.com/hazyhaar/horostracker/pkg/mcpquic"
	"github.com/mark3labs/mcp-go/server"
)

// Server is the unified QUIC chassis. It runs HTTP/3 for REST API and
// MCP-over-QUIC for tool access on the same port.
type Server struct {
	addr       string
	logger     *slog.Logger
	tlsCfg     *tls.Config
	httpHandler http.Handler
	mcpServer  *server.MCPServer
	mcpListener *mcpquic.Listener
	h3Server   *http3.Server
	mu         sync.Mutex
}

// Config holds configuration for the chassis server.
type Config struct {
	Addr      string           // UDP listen address (e.g. ":8443")
	TLS       *tls.Config      // nil = auto-generate self-signed
	CertFile  string           // production cert path
	KeyFile   string           // production key path
	Handler   http.Handler     // HTTP/3 handler (mux with API + static)
	MCPServer *server.MCPServer // MCP server (nil = MCP disabled)
	Logger    *slog.Logger
}

func New(cfg Config) (*Server, error) {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	tlsCfg := cfg.TLS
	if tlsCfg == nil {
		if cfg.CertFile != "" && cfg.KeyFile != "" {
			var err error
			tlsCfg, err = ProductionTLSConfig(cfg.CertFile, cfg.KeyFile)
			if err != nil {
				return nil, fmt.Errorf("load TLS cert: %w", err)
			}
			cfg.Logger.Info("TLS: production certs loaded")
		} else {
			var err error
			tlsCfg, err = DevelopmentTLSConfig()
			if err != nil {
				return nil, fmt.Errorf("generate dev TLS: %w", err)
			}
			cfg.Logger.Info("TLS: self-signed dev cert generated")
		}
	}

	return &Server{
		addr:        cfg.Addr,
		logger:      cfg.Logger,
		tlsCfg:      tlsCfg,
		httpHandler: cfg.Handler,
		mcpServer:   cfg.MCPServer,
	}, nil
}

// Start launches the QUIC listener. HTTP/3 is served via quic-go/http3.
// If an MCPServer is configured, MCP-over-QUIC runs on the same port via ALPN routing.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()

	qCfg := &quic.Config{
		MaxStreamReceiveWindow:     10 * 1024 * 1024,
		MaxConnectionReceiveWindow: 50 * 1024 * 1024,
		MaxIdleTimeout:             mcpquic.DefaultIdleTimeout,
		KeepAlivePeriod:            mcpquic.DefaultKeepAlive,
	}

	// HTTP/3 server
	s.h3Server = &http3.Server{
		Addr:       s.addr,
		Handler:    s.httpHandler,
		TLSConfig:  s.tlsCfg,
		QUICConfig: qCfg,
	}

	// MCP listener (separate QUIC listener on same or different port)
	if s.mcpServer != nil {
		mcpTLS := s.tlsCfg.Clone()
		mcpTLS.NextProtos = []string{mcpquic.ALPNProtocolMCP}

		listener, err := mcpquic.NewListener(s.addr, mcpTLS, s.mcpServer, s.logger)
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("MCP listener: %w", err)
		}
		s.mcpListener = listener

		go func() {
			if err := listener.Serve(ctx); err != nil && ctx.Err() == nil {
				s.logger.Error("MCP listener error", "error", err)
			}
		}()
		s.logger.Info("MCP over QUIC enabled", "addr", s.addr, "alpn", mcpquic.ALPNProtocolMCP)
	}

	s.mu.Unlock()

	s.logger.Info("chassis started",
		"addr", s.addr,
		"http3", true,
		"mcp", s.mcpServer != nil,
	)

	// HTTP/3 blocks
	if err := s.h3Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP/3: %w", err)
	}
	return nil
}

// Stop gracefully shuts down both HTTP/3 and MCP listeners.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("chassis stopping")

	var firstErr error
	if s.mcpListener != nil {
		if err := s.mcpListener.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if s.h3Server != nil {
		if err := s.h3Server.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	s.logger.Info("chassis stopped")
	return firstErr
}
