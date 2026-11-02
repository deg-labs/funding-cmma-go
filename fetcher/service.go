package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/deg-labs/funding-cmma-go/internal/config"
	"github.com/deg-labs/funding-cmma-go/internal/exchange"
	"github.com/deg-labs/funding-cmma-go/internal/funding"
	"github.com/deg-labs/funding-cmma-go/internal/store"
)

// Service は取引所からの取得と SQLite への保存を繰り返します。
type Service struct {
	client exchange.Client
	db     *sql.DB
	cfg    config.Fetcher
	logger *slog.Logger
}

// NewService は Service を生成します。
func NewService(client exchange.Client, db *sql.DB, cfg config.Fetcher, logger *slog.Logger) *Service {
	return &Service{client: client, db: db, cfg: cfg, logger: logger}
}

// Run は停止シグナルを受け取るまで収集サイクルを繰り返します。
func (s *Service) Run(ctx context.Context) error {
	for {
		if err := s.Cycle(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.logger.Error("collection cycle failed", "error", err.Error())
		}

		s.logger.Info("waiting for next cycle", "interval", s.cfg.FetchInterval.String())
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.cfg.FetchInterval):
		}
	}
}

type fetchResult struct {
	symbol  string
	records []funding.Record
	err     error
}

// Cycle は 1 回分の収集と保存を実行します。
func (s *Service) Cycle(ctx context.Context) error {
	started := time.Now()
	s.logger.Info("collection cycle started")

	symbols, err := s.client.Symbols(ctx)
	if err != nil {
		return fmt.Errorf("fetch symbols: %w", err)
	}
	if len(symbols) == 0 {
		return fmt.Errorf("no symbols returned from %s", s.client.Name())
	}
	s.logger.Info("symbols fetched", "exchange", s.client.Name(), "count", len(symbols))

	tickers, tickerErr := s.client.Tickers(ctx)
	if tickerErr != nil {
		s.logger.Warn("ticker metadata unavailable", "error", tickerErr.Error())
		tickers = map[string]funding.Ticker{}
	} else {
		s.logger.Info("tickers fetched", "count", len(tickers))
	}

	latest, err := store.LatestTimestamps(ctx, s.db)
	if err != nil {
		return fmt.Errorf("load latest timestamps: %w", err)
	}

	results := s.fetchAll(ctx, symbols, latest)

	rows := make([]store.UpsertRow, 0, len(symbols)*s.cfg.IncrementalHistoryLimit)
	failed := 0
	empty := 0
	for _, result := range results {
		if result.err != nil {
			failed++
			s.logger.Warn("funding history fetch failed",
				"symbol", result.symbol,
				"error", result.err.Error(),
			)
			continue
		}
		if len(result.records) == 0 {
			empty++
			continue
		}
		rows = append(rows, buildRows(result, tickers, s.cfg.FundingRateThreshold, s.logger)...)
	}

	if err := store.UpsertRows(ctx, s.db, rows); err != nil {
		return fmt.Errorf("persist funding rates: %w", err)
	}

	s.logger.Info("collection cycle completed",
		"symbols", len(symbols),
		"rows", len(rows),
		"failed_symbols", failed,
		"empty_symbols", empty,
		"duration", time.Since(started).String(),
	)
	return nil
}

func (s *Service) fetchAll(ctx context.Context, symbols []string, latest map[string]int64) []fetchResult {
	results := make([]fetchResult, len(symbols))
	sem := make(chan struct{}, s.cfg.ConcurrencyLimit)
	var wg sync.WaitGroup

	for index, symbol := range symbols {
		wg.Add(1)
		go func(index int, symbol string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[index] = fetchResult{symbol: symbol, err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			results[index] = s.fetchSymbol(ctx, symbol, latest)
		}(index, symbol)
	}

	wg.Wait()
	return results
}

func (s *Service) fetchSymbol(ctx context.Context, symbol string, latest map[string]int64) fetchResult {
	lastTimestamp, hasLast := latest[symbol]

	limit := s.cfg.IncrementalHistoryLimit
	if !hasLast {
		limit = s.cfg.HistoryLimit
	}

	records, err := s.client.FundingHistory(ctx, symbol, limit)
	if err != nil {
		return fetchResult{symbol: symbol, err: err}
	}
	sortRecordsDesc(records)

	if hasLast && needsBackfill(records, lastTimestamp, limit) {
		s.logger.Info("gap detected, backfilling funding history",
			"symbol", symbol,
			"last_timestamp", lastTimestamp,
			"limit", s.cfg.HistoryLimit,
		)
		records, err = s.client.FundingHistory(ctx, symbol, s.cfg.HistoryLimit)
		if err != nil {
			return fetchResult{symbol: symbol, err: err}
		}
		sortRecordsDesc(records)
	}

	return fetchResult{symbol: symbol, records: records}
}

// needsBackfill は取得済みの範囲と DB の最新時刻の間に欠損があるかを判定します。
func needsBackfill(records []funding.Record, lastTimestamp int64, limit int) bool {
	if len(records) == 0 || len(records) < limit {
		return false
	}
	oldest := records[len(records)-1].Timestamp
	return oldest > lastTimestamp
}

func buildRows(result fetchResult, tickers map[string]funding.Ticker, threshold float64, logger *slog.Logger) []store.UpsertRow {
	if len(result.records) == 0 {
		return nil
	}

	symbol := result.records[0].Symbol
	if symbol == "" {
		symbol = result.symbol
	}
	ticker := tickers[symbol]

	latestRecord := result.records[0]
	if abs(latestRecord.Rate) >= threshold {
		logger.Info("extreme funding rate candidate detected",
			"symbol", symbol,
			"rate", latestRecord.Rate,
			"threshold", threshold,
		)
	}

	rows := make([]store.UpsertRow, 0, len(result.records))
	for index, record := range result.records {
		row := store.UpsertRow{
			Symbol:    record.Symbol,
			Rate:      record.Rate,
			Timestamp: record.Timestamp,
		}
		if row.Symbol == "" {
			row.Symbol = symbol
		}

		// 最新の確定レートにだけ、取引所が公開している次回時刻などを付与します。
		if index == 0 {
			row.NextFundingTime = ticker.NextFundingTime
			row.IntervalHours = ticker.IntervalHours
			row.Cap = ticker.Cap
			row.Floor = ticker.Floor
		}
		rows = append(rows, row)
	}
	return rows
}

func sortRecordsDesc(records []funding.Record) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp > records[j].Timestamp
	})
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
