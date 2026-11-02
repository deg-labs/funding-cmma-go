package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/deg-labs/funding-cmma-go/internal/config"
	"github.com/deg-labs/funding-cmma-go/internal/funding"
	"github.com/deg-labs/funding-cmma-go/internal/store"
)

type fakeClient struct {
	symbols []string
	tickers map[string]funding.Ticker
	history map[string][]funding.Record
}

func (f *fakeClient) Name() string { return "fake" }

func (f *fakeClient) Symbols(context.Context) ([]string, error) {
	return f.symbols, nil
}

func (f *fakeClient) Tickers(context.Context) (map[string]funding.Ticker, error) {
	return f.tickers, nil
}

func (f *fakeClient) FundingHistory(_ context.Context, symbol string, limit int) ([]funding.Record, error) {
	records := f.history[symbol]
	if len(records) > limit {
		records = records[:limit]
	}
	return append([]funding.Record(nil), records...), nil
}

func TestServiceCyclePersistsRates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path, store.Options{})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := store.EnsureSchema(ctx, db); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	capValue := 0.01
	floorValue := -0.01
	nextFunding := int64(1_700_000_000_000)
	interval := 8

	client := &fakeClient{
		symbols: []string{"BTCUSDT"},
		tickers: map[string]funding.Ticker{
			"BTCUSDT": {
				NextFundingTime: &nextFunding,
				IntervalHours:   &interval,
				Cap:             &capValue,
				Floor:           &floorValue,
			},
		},
		history: map[string][]funding.Record{
			"BTCUSDT": {
				{Symbol: "BTCUSDT", Rate: 0.005, Timestamp: 3000},
				{Symbol: "BTCUSDT", Rate: 0.004, Timestamp: 2000},
				{Symbol: "BTCUSDT", Rate: 0.003, Timestamp: 1000},
			},
		},
	}

	cfg := config.Fetcher{
		ConcurrencyLimit:        2,
		IncrementalHistoryLimit: 2,
		HistoryLimit:            5,
		FundingRateThreshold:    0.001,
		FetchInterval:           time.Minute,
	}
	service := NewService(client, db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := service.Cycle(ctx); err != nil {
		t.Fatalf("Cycle() error = %v", err)
	}

	rates, err := store.LatestRates(ctx, db, store.LatestQuery{
		Threshold: 0.001,
		Direction: funding.DirectionBoth,
		Sort:      "funding_abs_desc",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("LatestRates() error = %v", err)
	}
	if len(rates) != 1 {
		t.Fatalf("len(rates) = %d, want 1", len(rates))
	}
	if rates[0].Rate != 0.005 {
		t.Errorf("rate = %v, want 0.005", rates[0].Rate)
	}
	if rates[0].NextFundingTime == nil || *rates[0].NextFundingTime != nextFunding {
		t.Errorf("next funding = %v", rates[0].NextFundingTime)
	}

	history, err := store.History(ctx, db, "BTCUSDT", 10)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("len(history) = %d, want 3", len(history))
	}
	if history[1].NextFundingTime != nil || history[2].NextFundingTime != nil {
		t.Error("older records should not carry ticker metadata")
	}
}

func TestNeedsBackfill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		records       []funding.Record
		lastTimestamp int64
		limit         int
		want          bool
	}{
		{
			name: "gap when the oldest fetched record is newer than the stored one",
			records: []funding.Record{
				{Timestamp: 5000}, {Timestamp: 4000}, {Timestamp: 3000},
			},
			lastTimestamp: 1000,
			limit:         3,
			want:          true,
		},
		{
			name: "no gap when records overlap",
			records: []funding.Record{
				{Timestamp: 5000}, {Timestamp: 4000}, {Timestamp: 1000},
			},
			lastTimestamp: 1000,
			limit:         3,
			want:          false,
		},
		{
			name: "no gap when the exchange returned fewer records than requested",
			records: []funding.Record{
				{Timestamp: 5000}, {Timestamp: 4000},
			},
			lastTimestamp: 1000,
			limit:         3,
			want:          false,
		},
		{
			name:          "empty records",
			records:       nil,
			lastTimestamp: 1000,
			limit:         3,
			want:          false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := needsBackfill(test.records, test.lastTimestamp, test.limit)
			if got != test.want {
				t.Errorf("needsBackfill() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBuildRowsAppliesTickerOnlyToLatest(t *testing.T) {
	t.Parallel()

	capValue := 0.01
	nextFunding := int64(1_700_000_000_000)

	result := fetchResult{
		symbol: "BTCUSDT",
		records: []funding.Record{
			{Symbol: "BTCUSDT", Rate: 0.005, Timestamp: 2000},
			{Symbol: "BTCUSDT", Rate: 0.004, Timestamp: 1000},
		},
	}
	tickers := map[string]funding.Ticker{
		"BTCUSDT": {NextFundingTime: &nextFunding, Cap: &capValue},
	}

	rows := buildRows(result, tickers, 0.001, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].NextFundingTime == nil || *rows[0].NextFundingTime != nextFunding {
		t.Errorf("latest row next funding = %v", rows[0].NextFundingTime)
	}
	if rows[1].NextFundingTime != nil || rows[1].Cap != nil {
		t.Error("older rows should not carry ticker metadata")
	}
}
