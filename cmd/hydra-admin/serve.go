package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hydra-admin/assets"
	"hydra-admin/internal/audit"
	"hydra-admin/internal/config"
	"hydra-admin/internal/httpserver"
	"hydra-admin/internal/observability"
)

func runServe(configPath string, stderr io.Writer) int {
	web, err := fs.Sub(assets.Dist, "dist")
	if err != nil {
		fmt.Fprintln(stderr, "web assets are absent: build the web UI before starting the service")
		return 1
	}
	if _, err := fs.Stat(web, "index.html"); err != nil {
		fmt.Fprintln(stderr, "web assets are absent: build the web UI before starting the service")
		return 1
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "configuration invalid: %v\n", err)
		return 1
	}
	if err := cfg.CheckWritablePaths(); err != nil {
		fmt.Fprintf(stderr, "local file check failed: %v\n", err)
		return 1
	}
	auditWriter, err := audit.NewRotatingWriter(cfg.Audit.Path, cfg.Audit.MaxFileBytes, cfg.Audit.MaxArchives, 0600)
	if err != nil {
		fmt.Fprintf(stderr, "audit log unavailable: %v\n", err)
		return 1
	}
	appWriterRaw, err := audit.NewRotatingWriter(cfg.Log.Path, cfg.Log.MaxFileBytes, cfg.Log.MaxArchives, 0600)
	if err != nil {
		auditWriter.Close()
		fmt.Fprintf(stderr, "application log unavailable: %v\n", err)
		return 1
	}
	appWriter := audit.NewAppLogWriter(appWriterRaw, stderr)
	logger := makeLogger(appWriter, cfg.Log.Format, cfg.Log.Level)
	s, err := httpserver.New(cfg, web, audit.NewRecorder(auditWriter), logger)
	if err != nil {
		auditWriter.Close()
		appWriter.Close()
		fmt.Fprintf(stderr, "service setup failed: %v\n", err)
		return 1
	}
	adapter, err := newHydraAdapter(cfg)
	if err != nil {
		auditWriter.Close()
		appWriter.Close()
		fmt.Fprintln(stderr, "Hydra probe configuration invalid")
		return 1
	}
	probeCtx, stopProbe := context.WithTimeout(context.Background(), cfg.Hydra.Timeout.Value())
	s.HydraReport = adapter.Report(probeCtx)
	s.HydraAdapter = adapter
	stopProbe()
	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Server.BindAddress, fmt.Sprint(cfg.Server.Port)))
	if err != nil {
		auditWriter.Close()
		appWriter.Close()
		fmt.Fprintf(stderr, "listen failed: %v\n", err)
		return 1
	}
	httpService := &http.Server{Handler: s.Handler(), ReadTimeout: cfg.Server.ReadTimeout.Value(), ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.Value(), WriteTimeout: cfg.Server.WriteTimeout.Value(), IdleTimeout: cfg.Server.IdleTimeout.Value()}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	cleanupDone := s.StartCleanup(ctx)
	shutdownDone := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		shutdownDone <- httpService.Shutdown(shutdownCtx)
	}()
	logger.Info("HTTP service listening", "bind_address", cfg.Server.BindAddress, "port", cfg.Server.Port)
	serveErr := httpService.Serve(listener)
	cancel()
	<-cleanupDone
	shutdownErr := <-shutdownDone
	auditErr := auditWriter.Close()
	appErr := appWriter.Close()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		fmt.Fprintf(stderr, "HTTP service failed: %v\n", serveErr)
		return 1
	}
	if shutdownErr != nil || auditErr != nil || appErr != nil || !appWriter.Healthy() {
		fmt.Fprintln(stderr, "service shutdown or log durability failed")
		return 1
	}
	return 0
}

func makeLogger(w io.Writer, format, level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	options := &slog.HandlerOptions{Level: l}
	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(w, options)
	} else {
		handler = slog.NewJSONHandler(w, options)
	}
	return slog.New(observability.NewRedactingHandler(handler))
}
