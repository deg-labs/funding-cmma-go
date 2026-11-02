package exchange

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestBybit(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New("bybit", Options{
		BaseURL:        server.URL,
		Timeout:        2 * time.Second,
		RetryMax:       2,
		RetryBaseDelay: time.Millisecond,
		RPS:            1000,
		Burst:          100,
		Logger:         testLogger(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

func TestBybitSymbolsPaginates(t *testing.T) {
	t.Parallel()

	client := newTestBybit(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/market/instruments-info" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("cursor") == "" {
			_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[{"symbol":"BTCUSDT"},{"symbol":"BTCUSDC"}],"nextPageCursor":"page2"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[{"symbol":"ETHUSDT"}],"nextPageCursor":""}}`))
	})

	symbols, err := client.Symbols(context.Background())
	if err != nil {
		t.Fatalf("Symbols() error = %v", err)
	}
	if len(symbols) != 2 || symbols[0] != "BTCUSDT" || symbols[1] != "ETHUSDT" {
		t.Fatalf("Symbols() = %v, want [BTCUSDT ETHUSDT]", symbols)
	}
}

func TestBybitTickers(t *testing.T) {
	t.Parallel()

	client := newTestBybit(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[
			{"symbol":"BTCUSDT","nextFundingTime":"1700000000000","fundingIntervalHour":"8","fundingCap":"0.01"},
			{"symbol":"ETHUSDT"}
		]}}`))
	})

	tickers, err := client.Tickers(context.Background())
	if err != nil {
		t.Fatalf("Tickers() error = %v", err)
	}
	btc, ok := tickers["BTCUSDT"]
	if !ok {
		t.Fatal("BTCUSDT ticker is missing")
	}
	if btc.NextFundingTime == nil || *btc.NextFundingTime != 1_700_000_000_000 {
		t.Errorf("NextFundingTime = %v", btc.NextFundingTime)
	}
	if btc.IntervalHours == nil || *btc.IntervalHours != 8 {
		t.Errorf("IntervalHours = %v", btc.IntervalHours)
	}
	if btc.Cap == nil || *btc.Cap != 0.01 {
		t.Errorf("Cap = %v", btc.Cap)
	}
	if btc.Floor == nil || *btc.Floor != -0.01 {
		t.Errorf("Floor = %v", btc.Floor)
	}
	if _, ok := tickers["ETHUSDT"]; ok {
		t.Error("ETHUSDT should be omitted because it has no metadata")
	}
}

func TestBybitFundingHistory(t *testing.T) {
	t.Parallel()

	client := newTestBybit(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[
			{"symbol":"BTCUSDT","fundingRate":"0.0001","fundingRateTimestamp":"1700000000000"},
			{"symbol":"BTCUSDT","fundingRate":"not-a-number","fundingRateTimestamp":"1699990000000"},
			{"symbol":"BTCUSDT","fundingRate":"-0.0002","fundingRateTimestamp":"1699980000000"}
		]}}`))
	})

	records, err := client.FundingHistory(context.Background(), "BTCUSDT", 200)
	if err != nil {
		t.Fatalf("FundingHistory() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].Rate != 0.0001 || records[0].Timestamp != 1_700_000_000_000 {
		t.Errorf("records[0] = %+v", records[0])
	}
}

func TestBybitRetriesOnRateLimit(t *testing.T) {
	t.Parallel()

	calls := 0
	client := newTestBybit(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[]}}`))
	})

	if _, err := client.FundingHistory(context.Background(), "BTCUSDT", 10); err != nil {
		t.Fatalf("FundingHistory() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestBybitRetCodeRateLimitIsRetried(t *testing.T) {
	t.Parallel()

	calls := 0
	client := newTestBybit(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"retCode":10006,"retMsg":"Too many visits"}`))
			return
		}
		_, _ = w.Write([]byte(`{"retCode":0,"result":{"list":[]}}`))
	})

	if _, err := client.FundingHistory(context.Background(), "BTCUSDT", 10); err != nil {
		t.Fatalf("FundingHistory() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}
