package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
)

// BinaryVersion is set at build time via -ldflags.
var BinaryVersion = "0.1.0"

// Server is the HTTP daemon that serves clipboard data to authorized remote hosts.
type Server struct {
	cfg          *config.Config
	configPath   string
	tokenStore   *auth.TokenStore
	reader       clipboard.Reader
	logger       *slog.Logger
	sem          chan struct{} // semaphore for max_in_flight
	mu           sync.Mutex   // serializes clipboard reads
	lastRead     time.Time
	lastReadSize int64
	rateLimiter  *RateLimiter
	httpServer   *http.Server
}

// New creates a new Server. The Server will listen on the port specified in cfg,
// bind to 127.0.0.1 only, and enforce the configured concurrency and rate limits.
func New(cfg *config.Config, configPath string, tokenStore *auth.TokenStore, reader clipboard.Reader, logger *slog.Logger) *Server {
	s := &Server{
		cfg:         cfg,
		configPath:  configPath,
		tokenStore:  tokenStore,
		reader:      reader,
		logger:      logger,
		sem:         make(chan struct{}, cfg.MaxInFlight),
		rateLimiter: NewRateLimiter(cfg.RateLimitPerMinute),
	}

	// Pre-fill the semaphore so all slots are available.
	for i := 0; i < cfg.MaxInFlight; i++ {
		s.sem <- struct{}{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/clipboard", s.handleClipboard)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/version", s.handleVersion)

	s.httpServer = &http.Server{
		Handler: mux,
		// The listener is bound to 127.0.0.1; this is a defense-in-depth
		// check to reject any non-loopback connection at accept time.
		ConnContext: s.rejectNonLoopbackConnCtx,
	}

	return s
}

// Start begins listening on the configured port, loopback only. It also
// installs signal handlers for SIGHUP (config reload) and SIGTERM (graceful shutdown).
func (s *Server) Start() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server: listen on %s: %w", addr, err)
	}

	s.logger.Info("server listening", "addr", addr)

	// Install signal handlers.
	go s.handleSignals()

	s.httpServer.Serve(listener)
	return nil
}

// Shutdown gracefully shuts down the HTTP server, draining in-flight requests
// for up to 5 seconds.
func (s *Server) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(shutdownCtx)
}

// LastRead returns the timestamp and size of the most recent clipboard read.
func (s *Server) LastRead() (time.Time, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRead, s.lastReadSize
}

// handleSignals responds to OS signals.
func (s *Server) handleSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGTERM)
	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			s.reloadConfig()
		case syscall.SIGTERM:
			s.logger.Info("received SIGTERM, shutting down")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.httpServer.Shutdown(ctx); err != nil {
				s.logger.Error("shutdown error", "err", err)
			}
			return
		}
	}
}

// reloadConfig re-reads the configuration file and applies relevant changes.
func (s *Server) reloadConfig() {
	s.logger.Info("reloading config", "path", s.configPath)
	cfg, err := config.Load(s.configPath)
	if err != nil {
		s.logger.Error("config reload failed", "err", err)
		return
	}
	s.cfg = cfg
	s.rateLimiter = NewRateLimiter(cfg.RateLimitPerMinute)
	s.logger.Info("config reloaded")
}

// rejectNonLoopbackConnCtx is set as ConnContext on the HTTP server. It
// inspects the remote address of each new connection and closes it immediately
// if the remote address is not a loopback address.
func (s *Server) rejectNonLoopbackConnCtx(ctx context.Context, c net.Conn) context.Context {
	if c == nil {
		return ctx
	}
	addr := c.RemoteAddr()
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		s.logger.Warn("rejecting connection: cannot parse remote addr", "addr", addr.String())
		c.Close()
		return ctx
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		s.logger.Warn("rejecting non-loopback connection", "addr", addr.String())
		c.Close()
		return ctx
	}
	return ctx
}
