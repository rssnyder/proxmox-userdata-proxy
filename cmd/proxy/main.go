package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rileysndr/proxmox-userdata-proxy/internal/config"
	"github.com/rileysndr/proxmox-userdata-proxy/internal/proxmox"
	"github.com/rileysndr/proxmox-userdata-proxy/internal/proxy"
	"github.com/rileysndr/proxmox-userdata-proxy/internal/snippet"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// Set up logger
	logLevel := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Create Proxmox client
	client, err := proxmox.NewClient(
		cfg.ProxmoxURL,
		cfg.ProxmoxInsecure,
	)
	if err != nil {
		logger.Error("failed to create Proxmox client", slog.Any("error", err))
		os.Exit(1)
	}

	// Create snippet writer
	writer := snippet.NewWriter(cfg.StorageMap)

	// Create handler
	handler := proxy.NewHandler(client, writer, logger)

	// Build middleware chain
	var h http.Handler = handler
	h = proxy.LoggingMiddleware(logger)(h)
	h = proxy.RequestIDMiddleware(h)

	// Create server
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      h,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 5 * time.Minute, // VM operations can be slow
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("starting proxy server",
			slog.String("addr", cfg.ListenAddr),
			slog.String("proxmox_url", cfg.ProxmoxURL),
			slog.Bool("tls_enabled", cfg.TLSEnabled()),
		)

		var err error
		if cfg.TLSEnabled() {
			err = server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			err = server.ListenAndServe()
		}

		if err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("server stopped")
}
