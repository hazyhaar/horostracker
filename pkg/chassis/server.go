// Package chassis provides a unified QUIC server that multiplexes HTTP/3 and
// MCP-over-QUIC on a single UDP port via ALPN routing.
//
// ALPN "h3"            → HTTP/3 handler (API + static files)
// ALPN "horos-mcp-v1"  → MCP JSON-RPC over QUIC stream
//
// A single quic.Listener accepts all connections. Each connection is routed
// by its negotiated ALPN protocol: h3 → http3.Server.ServeQUICConn,
// horos-mcp-v1 → mcpquic.Handler.ServeConn.
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
// MCP-over-QUIC for tool access on the same UDP port, demuxed by ALPN.
type Server struct {
	addr        string
	logger      *slog.Logger
	tlsCfg      *tls.Config
	httpHandler http.Handler
	mcpServer   *server.MCPServer
	mcpHandler  *mcpquic.Handler
	h3Server    *http3.Server
	listener    *quic.Listener
	mu          sync.Mutex
}

// Config holds configuration for the chassis server.
type Config struct {
	Addr      string            // UDP listen address (e.g. ":8443")
	TLS       *tls.Config       // nil = auto-generate self-signed
	CertFile  string            // production cert path
	KeyFile   string            // production key path
	Handler   http.Handler      // HTTP/3 handler (mux with API + static)
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

	s := &Server{
		addr:        cfg.Addr,
		logger:      cfg.Logger,
		tlsCfg:      tlsCfg,
		httpHandler: cfg.Handler,
		mcpServer:   cfg.MCPServer,
	}

	if cfg.MCPServer != nil {
		s.mcpHandler = mcpquic.NewHandler(cfg.MCPServer, cfg.Logger)
	}

	return s, nil
}

// Start opens a single QUIC listener and demuxes connections by ALPN.
// HTTP/3 connections (ALPN "h3") are served by quic-go/http3.
// MCP connections (ALPN "horos-mcp-v1") are served by mcpquic.Handler.
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()

	qCfg := &quic.Config{
		MaxStreamReceiveWindow:     10 * 1024 * 1024,
		MaxConnectionReceiveWindow: 50 * 1024 * 1024,
		MaxIdleTimeout:             mcpquic.DefaultIdleTimeout,
		KeepAlivePeriod:            mcpquic.DefaultKeepAlive,
	}

	// Single QUIC listener with both ALPNs
	ln, err := quic.ListenAddr(s.addr, s.tlsCfg, qCfg)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("QUIC listen: %w", err)
	}
	s.listener = ln

	// HTTP/3 server (used via ServeQUICConn, not its own listener)
	s.h3Server = &http3.Server{
		Handler: s.httpHandler,
	}

	s.mu.Unlock()

	s.logger.Info("chassis started",
		"addr", s.addr,
		"http3", true,
		"mcp", s.mcpHandler != nil,
	)

	// Accept loop: demux by ALPN
	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return fmt.Errorf("QUIC accept: %w", err)
		}

		alpn := conn.ConnectionState().TLS.NegotiatedProtocol
		switch alpn {
		case "h3":
			go func() {
				if err := s.h3Server.ServeQUICConn(conn); err != nil {
					s.logger.Debug("HTTP/3 conn done", "remote", conn.RemoteAddr(), "error", err)
				}
			}()
		case mcpquic.ALPNProtocolMCP:
			if s.mcpHandler != nil {
				go s.mcpHandler.ServeConn(ctx, conn)
			} else {
				conn.CloseWithError(quic.ApplicationErrorCode(0x10), "MCP not enabled")
			}
		default:
			s.logger.Warn("unknown ALPN, closing", "alpn", alpn, "remote", conn.RemoteAddr())
			conn.CloseWithError(quic.ApplicationErrorCode(0x11), "unsupported ALPN: "+alpn)
		}
	}
}

// Stop gracefully shuts down the chassis.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.logger.Info("chassis stopping")

	var firstErr error
	if s.listener != nil {
		if err := s.listener.Close(); err != nil && firstErr == nil {
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
