package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-openapi/runtime/middleware"

	"github.com/deg-labs/funding-cmma-go/internal/config"
	"github.com/deg-labs/funding-cmma-go/internal/logging"
	"github.com/deg-labs/funding-cmma-go/internal/store"
)

func main() {
	cfg, err := config.LoadAPI()
	if err != nil {
		slog.New(logging.New(slog.LevelError, "json")).Error("invalid configuration", "error", err.Error())
		os.Exit(1)
	}

	logger := slog.New(logging.New(cfg.LogLevel, cfg.LogFormat))
	slog.SetDefault(logger)

	db, err := store.Open(cfg.DBPath, store.Options{
		ReadOnly:     true,
		BusyTimeout:  5 * time.Second,
		MaxOpenConns: cfg.MaxOpenConns,
	})
	if err != nil {
		logger.Error("database open failed", "path", cfg.DBPath, "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	server := &apiServer{
		db:      db,
		logger:  logger,
		metrics: newMetricsRegistry(),
	}

	openAPISpec, err := buildOpenAPISpec()
	if err != nil {
		logger.Error("openapi spec build failed", "error", err.Error())
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.rootHandler)
	mux.HandleFunc("/funding-rates", server.fundingRatesHandler)
	mux.HandleFunc("/funding-rates/extreme-continuity", server.extremeContinuityHandler)
	mux.HandleFunc("/healthz", server.healthHandler)
	mux.HandleFunc("/readyz", server.readyHandler)
	mux.HandleFunc("/metrics", server.metricsHandler)

	var handler http.Handler = mux
	handler = middleware.SwaggerUI(middleware.SwaggerUIOpts{
		BasePath: "/",
		Path:     "funding-rates/docs",
		SpecURL:  "/funding-rates/openapi.json",
		Title:    "CMMA Funding Rate API",
	}, handler)
	handler = middleware.Spec("/", openAPISpec, handler,
		middleware.WithSpecPath("funding-rates"),
		middleware.WithSpecDocument("openapi.json"),
	)
	handler = withMiddleware(handler, server.metrics, logger)

	httpServer := &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api started", "addr", cfg.Addr, "db", cfg.DBPath)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "error", err.Error())
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("api shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("api stopped")
}

func withMiddleware(next http.Handler, metrics *metricsRegistry, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}

		if shouldSetJSONContentType(r.URL.Path) {
			recorder.Header().Set("Content-Type", "application/json; charset=utf-8")
		}

		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.observe(r.URL.Path, status)

		if status >= http.StatusInternalServerError {
			logger.Error("request failed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration", time.Since(started).String(),
			)
			return
		}
		logger.Info("request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration", time.Since(started).String(),
		)
	})
}

func shouldSetJSONContentType(path string) bool {
	switch path {
	case "/metrics":
		return false
	}
	if path == "/funding-rates/docs" || path == "/funding-rates/docs/" || path == "/funding-rates/openapi.json" {
		return false
	}
	return true
}
