package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/pastelocal/pastelocal/internal/auth"
	"github.com/pastelocal/pastelocal/internal/clipboard"
	"github.com/pastelocal/pastelocal/internal/config"
	"github.com/pastelocal/pastelocal/internal/server"
)

// version is set via ldflags at build time.
var version = "dev"

var (
	cfgPath      string
	portOverride int
	verbose      bool
)

var rootCmd = &cobra.Command{
	Use:     "pastelocald",
	Short:   "Pastelocal local clipboard daemon",
	Version: version,
	RunE:    run,
}

func init() {
	rootCmd.Flags().StringVar(&cfgPath, "config", config.ConfigPath(), "config file path")
	rootCmd.Flags().IntVar(&portOverride, "port", 0, "override listen port")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "increase log level")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Load config.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply CLI overrides.
	if portOverride != 0 {
		cfg.Port = portOverride
	}
	if verbose {
		cfg.LogLevel = "debug"
	}

	// Set up logging (slog, JSON, level from config).
	logLevel := parseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// Create clipboard reader (platform-appropriate).
	toolOverride := platformToolOverride(cfg)
	reader := clipboard.NewReader(toolOverride)

	// Create token store (keychain when available, otherwise file).
	tokenStore := auth.NewTokenStore(true, auth.DefaultTokenPath())

	// Create and start HTTP server.
	srv := server.New(cfg, cfgPath, tokenStore, reader, logger)

	pid := os.Getpid()
	logger.Info("pastelocald started",
		"addr", fmt.Sprintf("127.0.0.1:%d", cfg.Port),
		"transport", cfg.Transport,
		"pid", pid,
	)

	// Additional SIGHUP handler: the server already reloads config on SIGHUP,
	// but we also note that the token will be re-read from the store on the
	// next request.
	sighupCh := make(chan os.Signal, 1)
	signal.Notify(sighupCh, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sighupCh:
				logger.Info("received SIGHUP, reloading config and token")
			case <-done:
				signal.Stop(sighupCh)
				return
			}
		}
	}()
	defer close(done)

	// Start blocks until the server is shut down (SIGTERM triggers graceful
	// shutdown with a 5-second drain timeout inside the server).
	if err := srv.Start(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}

	logger.Info("pastelocald stopped")
	return nil
}

// parseLogLevel converts a config log level string to an slog.Level.
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// platformToolOverride returns the platform-specific clipboard tool override
// from the config, or empty string for auto-detection.
func platformToolOverride(cfg *config.Config) string {
	switch runtime.GOOS {
	case "darwin":
		if cfg.MacOS.PngpastePath != "" {
			return cfg.MacOS.PngpastePath
		}
	case "linux":
		if cfg.Linux.ClipboardTool != "" && cfg.Linux.ClipboardTool != "auto" {
			return cfg.Linux.ClipboardTool
		}
	}
	return ""
}
