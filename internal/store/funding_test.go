package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/deg-labs/funding-cmma-go/internal/funding"
)

func TestUpsertAndLatestRates(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path, Options{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := EnsureSchema(ctx, db); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	capValue := 0.01
	floorValue := -0.01
	nextFunding := int64(1_700_000_000_000)
	interval := 8

	rows := []UpsertRow{
		{Symbol: "BTCUSDT", Rate: 0.005, Timestamp: 1000},
		{Symbol: "BTCUSDT", Rate: 0.008, Timestamp: 2000, NextFundingTime: &nextFunding, IntervalHours: &interval, Cap: &capValue, Floor: &floorValue},
		{Symbol: "ETHUSDT", Rate: -0.005, Timestamp: 1000, Cap: &capValue, Floor: &floorValue},
		{Symbol: "XRPUSDT", Rate: 0.02, Timestamp: 1000},
		{Symbol: "DOGEUSDT", Rate: 0.0001, Timestamp: 1000},
	}
	if err := UpsertRows(ctx, db, rows); err != nil {
		t.Fatalf("UpsertRows() error = %v", err)
	}

	// 同じ主キーは更新されること。
	if err := UpsertRows(ctx, db, []UpsertRow{{Symbol: "BTCUSDT", Rate: 0.009, Timestamp: 2000}}); err != nil {
		t.Fatalf("UpsertRows() update error = %v", err)
	}

	latest, err := LatestTimestamps(ctx, db)
	if err != nil {
		t.Fatalf("LatestTimestamps() error = %v", err)
	}
	if latest["BTCUSDT"] != 2000 {
		t.Errorf("BTCUSDT latest = %d, want 2000", latest["BTCUSDT"])
	}

	rates, err := LatestRates(ctx, db, LatestQuery{
		Threshold: 0.001,
		Direction: funding.DirectionBoth,
		Sort:      "funding_abs_desc",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("LatestRates() error = %v", err)
	}
	if len(rates) != 3 {
		t.Fatalf("len(rates) = %d, want 3", len(rates))
	}
	if rates[0].Symbol != "XRPUSDT" {
		t.Errorf("first symbol = %s, want XRPUSDT", rates[0].Symbol)
	}

	btc := findRate(t, rates, "BTCUSDT")
	if btc.Rate != 0.009 {
		t.Errorf("BTCUSDT rate = %v, want 0.009", btc.Rate)
	}
	if btc.NextFundingTime != nil {
		t.Errorf("BTCUSDT next funding = %v, want nil after overwrite", *btc.NextFundingTime)
	}

	eth := findRate(t, rates, "ETHUSDT")
	if eth.Utilization == nil || *eth.Utilization != -0.5 {
		t.Errorf("ETHUSDT utilization = %v, want -0.5", eth.Utilization)
	}

	xrp := findRate(t, rates, "XRPUSDT")
	if xrp.Utilization != nil {
		t.Errorf("XRPUSDT utilization = %v, want nil", *xrp.Utilization)
	}
}

func TestLatestRatesFiltersAndSorts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path, Options{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := EnsureSchema(ctx, db); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	capValue := 0.01
	floorValue := -0.01
	rows := []UpsertRow{
		{Symbol: "AAAUSDT", Rate: 0.005, Timestamp: 1000, Cap: &capValue, Floor: &floorValue},
		{Symbol: "BBBUSDT", Rate: -0.008, Timestamp: 1000, Cap: &capValue, Floor: &floorValue},
		{Symbol: "CCCUSDT", Rate: 0.001, Timestamp: 1000, Cap: &capValue, Floor: &floorValue},
	}
	if err := UpsertRows(ctx, db, rows); err != nil {
		t.Fatalf("UpsertRows() error = %v", err)
	}

	positive, err := LatestRates(ctx, db, LatestQuery{
		Threshold: 0.001, Direction: funding.DirectionPositive, Sort: "funding_rate_desc", Limit: 10,
	})
	if err != nil {
		t.Fatalf("LatestRates() error = %v", err)
	}
	if len(positive) != 2 {
		t.Fatalf("len(positive) = %d, want 2", len(positive))
	}

	negative, err := LatestRates(ctx, db, LatestQuery{
		Threshold: 0.001, Direction: funding.DirectionNegative, Sort: "funding_rate_asc", Limit: 10,
	})
	if err != nil {
		t.Fatalf("LatestRates() error = %v", err)
	}
	if len(negative) != 1 || negative[0].Symbol != "BBBUSDT" {
		t.Fatalf("negative = %+v, want BBBUSDT only", negative)
	}

	threshold := 0.5
	utilization, err := LatestRates(ctx, db, LatestQuery{
		Threshold: 0.001, Direction: funding.DirectionBoth, Sort: "utilization_desc",
		Limit: 10, UtilizationGTE: &threshold,
	})
	if err != nil {
		t.Fatalf("LatestRates() error = %v", err)
	}
	if len(utilization) != 1 || utilization[0].Symbol != "AAAUSDT" {
		t.Fatalf("utilization filter = %+v, want AAAUSDT only", utilization)
	}

	limited, err := LatestRates(ctx, db, LatestQuery{
		Threshold: 0.001, Direction: funding.DirectionBoth, Sort: "symbol_asc", Limit: 2,
	})
	if err != nil {
		t.Fatalf("LatestRates() error = %v", err)
	}
	if len(limited) != 2 || limited[0].Symbol != "AAAUSDT" || limited[1].Symbol != "BBBUSDT" {
		t.Fatalf("limited = %+v, want AAAUSDT, BBBUSDT", limited)
	}

	if _, err := LatestRates(ctx, db, LatestQuery{Sort: "rate; DROP TABLE"}); err == nil {
		t.Fatal("LatestRates() with invalid sort should fail")
	}
}

func TestHistory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path, Options{})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := EnsureSchema(ctx, db); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	rows := []UpsertRow{
		{Symbol: "BTCUSDT", Rate: 0.001, Timestamp: 1000},
		{Symbol: "BTCUSDT", Rate: 0.002, Timestamp: 2000},
		{Symbol: "BTCUSDT", Rate: 0.003, Timestamp: 3000},
	}
	if err := UpsertRows(ctx, db, rows); err != nil {
		t.Fatalf("UpsertRows() error = %v", err)
	}

	history, err := History(ctx, db, "BTCUSDT", 2)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("len(history) = %d, want 2", len(history))
	}
	if history[0].Timestamp != 3000 || history[1].Timestamp != 2000 {
		t.Errorf("history order = %d, %d, want 3000, 2000", history[0].Timestamp, history[1].Timestamp)
	}
	if history[0].Utilization != nil {
		t.Errorf("history utilization = %v, want nil", *history[0].Utilization)
	}

	missing, err := History(ctx, db, "UNKNOWN", 10)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("len(missing) = %d, want 0", len(missing))
	}
}

func findRate(t *testing.T, rates []funding.StoredRate, symbol string) funding.StoredRate {
	t.Helper()
	for _, rate := range rates {
		if rate.Symbol == symbol {
			return rate
		}
	}
	t.Fatalf("symbol %s not found", symbol)
	return funding.StoredRate{}
}
