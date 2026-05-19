package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
	"github.com/pastelocal/pastelocal/internal/crypto"
	"github.com/pastelocal/pastelocal/internal/proto"
	"github.com/pastelocal/pastelocal/internal/relay"
)

// BinaryVersion is set at build time via -ldflags.
var BinaryVersion = "0.1.0"

// Server is the HTTP daemon that serves clipboard data to authorized remote hosts.
type Server struct {
	cfg                 *config.Config
	configPath          string
	tokenStore          *auth.TokenStore
	reader              clipboard.Reader
	writer              clipboard.Writer
	logger              *slog.Logger
	sem                 chan struct{} // semaphore for max_in_flight
	mu                  sync.Mutex    // serializes clipboard reads
	lastRead            time.Time
	lastReadSize        int64
	lastReadFormat      string
	lastClipboardChange time.Time
	watchEnabled        atomic.Bool
	rateLimiter         *RateLimiter
	history             *HistoryBuffer
	redaction           *RedactionEngine
	processors          *ProcessorPipeline
	watchHub            *WatchHub
	watcherStop         chan struct{} // for graceful Shutdown of the always-running watcher
	httpServer          *http.Server
	relayClient         *relay.Client
	relayKeyPair        *crypto.KeyPair
}

// New creates a new Server. The Server will listen on the port specified in cfg,
// bind to 127.0.0.1 only, and enforce the configured concurrency and rate limits.
func New(cfg *config.Config, configPath string, tokenStore *auth.TokenStore, reader clipboard.Reader, logger *slog.Logger) *Server {
	s := &Server{
		cfg:         cfg,
		configPath:  configPath,
		tokenStore:  tokenStore,
		reader:      reader,
		writer:      clipboard.NewWriter(""),
		logger:      logger,
		sem:         make(chan struct{}, cfg.MaxInFlight),
		rateLimiter: NewRateLimiter(cfg.RateLimitPerMinute),
		redaction:   NewRedactionEngine(cfg),
		processors:  NewProcessorPipeline(cfg, logger),
		watchHub:    NewWatchHub(logger),
	}

	s.watchEnabled.Store(cfg.Watch.Enabled)
	s.watcherStop = make(chan struct{})

	// Initialize history buffer if enabled.
	if cfg.History.Enabled {
		token, _ := tokenStore.Retrieve()
		s.history = NewHistoryBuffer(cfg.History.Size, cfg.History.TTL, token)
	}

	// Initialize relay client if enabled.
	if cfg.Relay.Enabled {
		// Load device keypair.
		keyData, err := os.ReadFile(expandPath(cfg.Relay.DeviceKeyPath))
		if err == nil {
			kp, err := crypto.LoadKeyPairFromBase64(string(keyData))
			if err == nil {
				// Load auth token.
				tokenData, err := os.ReadFile(expandPath(cfg.Relay.AuthTokenPath))
				if err == nil {
					s.relayKeyPair = kp
					s.relayClient = relay.NewClient(cfg.Relay.RelayURL, kp.DeviceID(), kp, string(tokenData))
					logger.Info("relay client initialized", "device_id", kp.DeviceID())
				}
			}
		}
		if s.relayClient == nil {
			logger.Warn("relay enabled but failed to initialize (run 'pastelocal relay pair' first)")
		}
	}

	// Always start the watcher goroutine (it is a cheap no-op when disabled via the early continue).
	// This ensures that SIGHUP/reload enabling the feature takes effect without daemon restart.
	go s.startClipboardWatcher()

	// Pre-fill the semaphore so all slots are available.
	for i := 0; i < cfg.MaxInFlight; i++ {
		s.sem <- struct{}{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/clipboard", s.handleClipboard)
	mux.HandleFunc("/clipboard/history", s.handleClipboardHistory)
	mux.HandleFunc("/clipboard/history/", s.handleClipboardHistory)
	mux.HandleFunc("/clipboard/watch", s.handleWatch)
	mux.HandleFunc("/snippets", s.handleSnippets)
	mux.HandleFunc("/snippets/", s.handleSnippet)
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
	// Signal the always-running watcher goroutine to exit (ticker will stop on return).
	if s.watcherStop != nil {
		close(s.watcherStop)
	}
	return s.httpServer.Shutdown(shutdownCtx)
}

// LastRead returns the timestamp, size, and format of the most recent clipboard read.
func (s *Server) LastRead() (time.Time, int64, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRead, s.lastReadSize, s.lastReadFormat
}

// WatchStatus returns whether the clipboard watcher is enabled and the
// timestamp of the most recent OS clipboard change detected by the watcher.
func (s *Server) WatchStatus() (bool, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.watchEnabled.Load(), s.lastClipboardChange
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
	s.redaction = NewRedactionEngine(cfg)
	s.processors = NewProcessorPipeline(cfg, s.logger)
	s.watchEnabled.Store(cfg.Watch.Enabled)
	s.watcherStop = make(chan struct{})
	if cfg.History.Enabled && s.history == nil {
		token, _ := s.tokenStore.Retrieve()
		s.history = NewHistoryBuffer(cfg.History.Size, cfg.History.TTL, token)
	}
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

// expandPath replaces a leading ~ with the user's home directory.
func expandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// Handler returns the HTTP handler for the server. Useful for testing.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// HandleClipboard returns the handler function for /clipboard.
func (s *Server) HandleClipboard() http.HandlerFunc {
	return s.handleClipboard
}

// HandleClipboardHistory returns the handler function for /clipboard/history.
func (s *Server) HandleClipboardHistory() http.HandlerFunc {
	return s.handleClipboardHistory
}

// HandleHealth returns the handler function for /health.
func (s *Server) HandleHealth() http.HandlerFunc {
	return s.handleHealth
}

// HandleVersion returns the handler function for /version.
func (s *Server) HandleVersion() http.HandlerFunc {
	return s.handleVersion
}

// startClipboardWatcher runs in a background goroutine when watch.enabled=true.
// It polls the OS clipboard at a low frequency, using cheap AvailableFormats +
// lightweight text checks most of the time, and occasional full image reads only
// when an image is on the clipboard (to catch new screenshots). Changes are
// debounced, filtered for insignificant noise, logged, used to update internal
// last* state (for TUI/status), and pushed to watchHub subscribers so that
// pastelocal-remote --watch (and future clients) can react without polling the
// read endpoint.
func (s *Server) startClipboardWatcher() {
	const (
		watchPollInterval  = 2 * time.Second
		watchCtxTimeout    = 1200 * time.Millisecond
		watchImageThrottle = 2200 * time.Millisecond
		watchDebounce      = 600 * time.Millisecond
		watchTinyTextMax   = 6
	)

	s.logger.Debug("clipboard watcher goroutine running", "poll_interval", "2s")
	ticker := time.NewTicker(watchPollInterval)
	defer ticker.Stop()

	var (
		lastFmtSig   string
		lastTextHash string
		lastImgHash  string
		lastImgCheck time.Time
		lastDetected time.Time
	)

	for range ticker.C {
		// Always check for shutdown first (before the early "disabled" continue).
		// This guarantees the stop signal is observable even when watch is disabled (the default).
		select {
		case <-s.watcherStop:
			return
		default:
		}

		if !s.watchEnabled.Load() {
			// Reset memo state on disable so re-enable starts fresh (no stale debounce hashes)
			lastFmtSig = ""
			lastTextHash = ""
			lastImgHash = ""
			lastImgCheck = time.Time{}
			lastDetected = time.Time{}
			continue
		}

		// Use short ctx for each poll to avoid hanging on slow clipboard tools.
		ctx, cancel := context.WithTimeout(context.Background(), watchCtxTimeout)

		formats, fErr := s.reader.AvailableFormats(ctx)
		if fErr != nil {
			s.logger.Debug("clipboard watcher: read formats failed, ignoring tick", "err", fErr)
			cancel()
			continue
		}

		// Normalize for stable sig (platform tools may return formats in non-deterministic order)
		sort.Strings(formats)
		sig := strings.Join(formats, ",")
		now := time.Now().UTC()

		// Concealed/sensitive filter (core of the feature).
		// If the clipboard item carries the macOS ConcealedType (or equiv),
		// skip entirely: do not read bytes, do not update state, do not push to relay.
		// This is what allows safe use of watch + relay with password managers.
		if s.cfg.Watch.Sensitive.FilterConcealed {
			concealed, cErr := s.reader.IsConcealed(ctx)
			if cErr != nil {
				// Detector failure: log the full error (which includes captured
				// stderr) at the level controlled by log_filtered_items. We still
				// fail open (non-concealed) per the spec's "false negatives worse"
				// guidance, but the log makes the risk visible and actionable.
				if s.cfg.Watch.Sensitive.LogFilteredItems {
					s.logger.Info("clipboard watcher: concealed detection failed (fail-open)", "err", cErr)
				} else {
					s.logger.Debug("clipboard watcher: concealed detection failed (fail-open)", "err", cErr)
				}
			} else if concealed {
				lastFmtSig = sig // absorb so we don't re-evaluate the same item constantly
				if s.cfg.Watch.Sensitive.LogFilteredItems {
					s.logger.Info("clipboard watcher: filtered concealed clipboard item (ConcealedType/password manager secret)")
				} else {
					s.logger.Debug("clipboard watcher: filtered concealed item")
				}
				cancel()
				continue
			}
		}

		// Global debounce: ignore bursts.
		if now.Sub(lastDetected) < watchDebounce {
			lastFmtSig = sig
			s.logger.Debug("clipboard watcher: debounce active, ignoring", "since_last_ms", now.Sub(lastDetected).Milliseconds())
			cancel()
			continue
		}

		potential := (sig != lastFmtSig)
		lastFmtSig = sig

		hasImg := false
		hasTxt := false
		for _, f := range formats {
			lf := strings.ToLower(f)
			if strings.Contains(lf, "png") || strings.Contains(lf, "image") || strings.Contains(lf, "jpeg") {
				hasImg = true
			}
			if strings.Contains(lf, "text") || strings.Contains(lf, "utf") || strings.Contains(lf, "plain") {
				hasTxt = true
			}
		}

		var content *clipboard.Content
		heavy := false

		if potential {
			c, e := s.reader.ReadContent(ctx)
			if e == nil {
				content = c
				heavy = true
			}
		} else if hasTxt {
			// Cheap text change detection for rapid typing / copies.
			txt, te := s.reader.ReadText(ctx)
			if te != nil {
				s.logger.Debug("clipboard watcher: ReadText failed during watch", "err", te)
			} else if len(txt) >= 4 {
				h := fmt.Sprintf("%x", sha256.Sum256([]byte(txt)))[:12]
				if h != lastTextHash {
					potential = true
					lastTextHash = h
					content = &clipboard.Content{Data: []byte(txt), Format: "text"}
				}
			}
		}

		// Separate (non-else) check for images so mixed text+image clipboards (common for rich content/screenshots) still detect new images when the text path did not trigger a change.
		if !potential && hasImg && now.Sub(lastImgCheck) > watchImageThrottle {
			// Throttled image read: catches new screenshots without constant full loads.
			lastImgCheck = now
			img, ie := s.reader.ReadImage(ctx)
			if ie != nil {
				s.logger.Debug("clipboard watcher: ReadImage failed during watch", "err", ie)
			} else if len(img) > 100 {
				h := fmt.Sprintf("%x", sha256.Sum256(img))[:8]
				if h != lastImgHash {
					potential = true
					lastImgHash = h
					content = &clipboard.Content{Data: img, Format: "png"}
					heavy = true
				}
			}
		}

		cancel()

		if !potential || content == nil {
			continue
		}

		// Final filter: drop tiny insignificant clipboard spam (e.g. single char).
		if content.Format == "text" {
			if len(content.Data) < watchTinyTextMax && !strings.ContainsAny(string(content.Data), " \n\t") {
				s.logger.Debug("clipboard watcher: tiny text filtered", "len", len(content.Data))
				continue
			}
		}

		lastDetected = now

		// Update internal state for TUI, status, and LastRead reuse.
		s.mu.Lock()
		s.lastClipboardChange = now
		s.lastRead = now
		s.lastReadSize = int64(len(content.Data))
		s.lastReadFormat = content.Format
		s.mu.Unlock()

		s.logger.Info("clipboard watcher: meaningful change detected",
			"format", content.Format,
			"byte_count", len(content.Data),
			"heavy_read", heavy,
		)

		// Push notification to any connected --watch clients via WS.
		s.watchHub.Notify(proto.WatchNotification{
			Event:      "clipboard_changed",
			CapturedAt: now.Format(time.RFC3339),
			Format:     content.Format,
		})

		// Also push to relay peers (if configured). Non-blocking inside.
		if s.relayClient != nil && s.cfg.Relay.AutoUpload {
			go s.pushToRelayPeers(content.Format, content.Data)
		}
	}
}
