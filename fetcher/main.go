package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/deg-labs/funding-cmma-go/internal/config"
	"github.com/deg-labs/funding-cmma-go/internal/exchange"
	"github.com/deg-labs/funding-cmma-go/internal/logging"
	"github.com/deg-labs/funding-cmma-go/internal/store"
)

func main() {
	cfg, err := config.LoadFetcher()
	if err != nil {
		slog.New(logging.New(slog.LevelError, "json")).Error("invalid configuration", "error", err.Error())
		os.Exit(1)
	}

	logger := slog.New(logging.New(cfg.LogLevel, cfg.LogFormat))
	slog.SetDefault(logger)

	db, err := store.Open(cfg.DBPath, store.Options{ReadOnly: false})
	if err != nil {
		logger.Error("database open failed", "path", cfg.DBPath, "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()

	if err := store.EnsureSchema(context.Background(), db); err != nil {
		logger.Error("database schema setup failed", "error", err.Error())
		os.Exit(1)
	}

	client, err := exchange.New(cfg.Exchange, exchange.Options{
		BaseURL:     cfg.BaseURL,
		ProductType: cfg.BitgetProductType,
		Timeout:     cfg.RequestTimeout,
		RetryMax:    cfg.RetryMax,
		RPS:         cfg.RateLimitRPS,
		Burst:       cfg.RateLimitBurst,
		Logger:      logger,
	})
	if err != nil {
		logger.Error("exchange client setup failed", "error", err.Error())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("fetcher started",
		"exchange", cfg.Exchange,
		"base_url", cfg.BaseURL,
		"interval", cfg.FetchInterval.String(),
		"concurrency", cfg.ConcurrencyLimit,
		"history_limit", cfg.HistoryLimit,
	)

	service := NewService(client, db, cfg, logger)
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("fetcher stopped with error", "error", err.Error())
		os.Exit(1)
	}

	logger.Info("fetcher stopped")
}
