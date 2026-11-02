package exchange

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func newTestBitget(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New("bitget", Options{
		BaseURL:        server.URL,
		ProductType:    "usdt-futures",
		Timeout:        2 * time.Second,
		RetryMax:       1,
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

func TestBitgetSymbolsAndTickers(t *testing.T) {
	t.Parallel()

	client := newTestBitget(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"00000","data":[
			{"symbol":"BTCUSDT","fundingRate":"0.0001","nextUpdate":"1700000000000","fundingRateInterval":"8","maxFundingRate":"0.00375","minFundingRate":"-0.00375"},
			{"symbol":"ETHUSDC","fundingRate":"0.0001"}
		]}`))
	})

	symbols, err := client.Symbols(context.Background())
	if err != nil {
		t.Fatalf("Symbols() error = %v", err)
	}
	if len(symbols) != 1 || symbols[0] != "BTCUSDT" {
		t.Fatalf("Symbols() = %v, want [BTCUSDT]", symbols)
	}

	tickers, err := client.Tickers(context.Background())
	if err != nil {
		t.Fatalf("Tickers() error = %v", err)
	}
	btc := tickers["BTCUSDT"]
	if btc.Cap == nil || *btc.Cap != 0.00375 {
		t.Errorf("Cap = %v", btc.Cap)
	}
	if btc.Floor == nil || *btc.Floor != -0.00375 {
		t.Errorf("Floor = %v", btc.Floor)
	}
	if btc.IntervalHours == nil || *btc.IntervalHours != 8 {
		t.Errorf("IntervalHours = %v", btc.IntervalHours)
	}
}

func TestBitgetFundingHistoryPaginates(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var pages []int
	client := newTestBitget(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/mix/market/history-fund-rate" {
			http.NotFound(w, r)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("pageNo"))
		size, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
		mu.Lock()
		pages = append(pages, page)
		pageCount := len(pages)
		mu.Unlock()

		items := make([]string, 0, size)
		for i := 0; i < size; i++ {
			items = append(items, fmt.Sprintf(
				`{"symbol":"BTCUSDT","fundingRate":"0.0001","fundingTime":"%d"}`,
				int64(1_700_000_000_000)-int64(pageCount*1000+i),
			))
		}
		body := `{"code":"00000","data":[`
		for i, item := range items {
			if i > 0 {
				body += ","
			}
			body += item
		}
		body += `]}`
		_, _ = w.Write([]byte(body))
	})

	records, err := client.FundingHistory(context.Background(), "BTCUSDT", 150)
	if err != nil {
		t.Fatalf("FundingHistory() error = %v", err)
	}
	if len(records) != 150 {
		t.Fatalf("len(records) = %d, want 150", len(records))
	}
	if len(pages) != 2 || pages[0] != 1 || pages[1] != 2 {
		t.Fatalf("pages = %v, want [1 2]", pages)
	}
}

func TestBitgetAPIErrorIsNotRetried(t *testing.T) {
	t.Parallel()

	calls := 0
	client := newTestBitget(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"code":"40001","msg":"invalid symbol","data":[]}`))
	})

	if _, err := client.FundingHistory(context.Background(), "UNKNOWN", 10); err == nil {
		t.Fatal("FundingHistory() should fail")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
