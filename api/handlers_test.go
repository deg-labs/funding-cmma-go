package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deg-labs/funding-cmma-go/internal/store"
)

func newTestServer(t *testing.T, rows []store.UpsertRow) *apiServer {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(path, store.Options{})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ctx := context.Background()
	if err := store.EnsureSchema(ctx, db); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	if err := store.UpsertRows(ctx, db, rows); err != nil {
		t.Fatalf("UpsertRows() error = %v", err)
	}

	return &apiServer{
		db:      db,
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		metrics: newMetricsRegistry(),
	}
}

func fixtureRows() []store.UpsertRow {
	capValue := 0.01
	floorValue := -0.01
	nextFunding := int64(1_700_000_000_000)
	interval := 8

	return []store.UpsertRow{
		{Symbol: "BTCUSDT", Rate: 0.005, Timestamp: 1000, NextFundingTime: &nextFunding, IntervalHours: &interval, Cap: &capValue, Floor: &floorValue},
		{Symbol: "ETHUSDT", Rate: -0.008, Timestamp: 1000, Cap: &capValue, Floor: &floorValue},
		{Symbol: "XRPUSDT", Rate: 0.02, Timestamp: 1000},
		{Symbol: "DOGEUSDT", Rate: 0.0001, Timestamp: 1000, Cap: &capValue, Floor: &floorValue},
	}
}

func doRequest(t *testing.T, handler http.HandlerFunc, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestFundingRatesHandler(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, fixtureRows())
	recorder := doRequest(t, server.fundingRatesHandler, "/funding-rates?threshold=0.001")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var response fundingRateResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if response.Count != 3 || len(response.Data) != 3 {
		t.Fatalf("count = %d, data = %d, want 3", response.Count, len(response.Data))
	}
	if response.Data[0].Symbol != "XRPUSDT" {
		t.Errorf("first symbol = %s, want XRPUSDT", response.Data[0].Symbol)
	}

	btc := findData(t, response.Data, "BTCUSDT")
	if btc.Funding.Utilization == nil || *btc.Funding.Utilization != 0.5 {
		t.Errorf("BTC utilization = %v, want 0.5", btc.Funding.Utilization)
	}
	if btc.Funding.Direction != "positive" {
		t.Errorf("BTC direction = %s, want positive", btc.Funding.Direction)
	}
	if btc.NextFundingTS == nil || *btc.NextFundingTS != 1_700_000_000_000 {
		t.Errorf("BTC next_funding_ts = %v", btc.NextFundingTS)
	}
	if btc.Constraints.IntervalHours == nil || *btc.Constraints.IntervalHours != 8 {
		t.Errorf("BTC interval_hours = %v", btc.Constraints.IntervalHours)
	}

	eth := findData(t, response.Data, "ETHUSDT")
	if eth.Funding.Utilization == nil || *eth.Funding.Utilization != -0.8 {
		t.Errorf("ETH utilization = %v, want -0.8", eth.Funding.Utilization)
	}

	xrp := findData(t, response.Data, "XRPUSDT")
	if xrp.Funding.Utilization != nil {
		t.Errorf("XRP utilization = %v, want null", *xrp.Funding.Utilization)
	}
	if xrp.Constraints.Cap != nil || xrp.Constraints.Floor != nil || xrp.Constraints.IntervalHours != nil {
		t.Error("XRP constraints should be null")
	}
}

func TestFundingRatesFilters(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, fixtureRows())

	positive := doRequest(t, server.fundingRatesHandler, "/funding-rates?threshold=0.001&direction=positive")
	var positiveResponse fundingRateResponse
	if err := json.NewDecoder(positive.Body).Decode(&positiveResponse); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if len(positiveResponse.Data) != 2 {
		t.Fatalf("positive count = %d, want 2", len(positiveResponse.Data))
	}

	limited := doRequest(t, server.fundingRatesHandler, "/funding-rates?threshold=0.001&limit=1&sort=symbol_asc")
	var limitedResponse fundingRateResponse
	if err := json.NewDecoder(limited.Body).Decode(&limitedResponse); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if len(limitedResponse.Data) != 1 || limitedResponse.Data[0].Symbol != "BTCUSDT" {
		t.Fatalf("limited = %+v, want BTCUSDT", limitedResponse.Data)
	}
}

func TestFundingRatesValidation(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, fixtureRows())

	tests := []struct {
		name   string
		target string
		field  string
	}{
		{name: "missing threshold", target: "/funding-rates", field: "query->threshold"},
		{name: "zero threshold", target: "/funding-rates?threshold=0", field: "query->threshold"},
		{name: "non numeric threshold", target: "/funding-rates?threshold=abc", field: "query->threshold"},
		{name: "invalid direction", target: "/funding-rates?threshold=0.001&direction=up", field: "query->direction"},
		{name: "invalid sort", target: "/funding-rates?threshold=0.001&sort=unknown", field: "query->sort"},
		{name: "limit too large", target: "/funding-rates?threshold=0.001&limit=501", field: "query->limit"},
		{name: "limit zero", target: "/funding-rates?threshold=0.001&limit=0", field: "query->limit"},
		{name: "negative utilization", target: "/funding-rates?threshold=0.001&utilization_gte=-1", field: "query->utilization_gte"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := doRequest(t, server.fundingRatesHandler, test.target)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", recorder.Code)
			}
			var response errorResponse
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("decode error = %v", err)
			}
			if response.Error.Code != "INVALID_INPUT" {
				t.Errorf("code = %s, want INVALID_INPUT", response.Error.Code)
			}
			if !strings.Contains(response.Error.Message, test.field) {
				t.Errorf("message = %q, want field %q", response.Error.Message, test.field)
			}
		})
	}
}

func TestFundingRatesEmptyResultIsArray(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, nil)
	recorder := doRequest(t, server.fundingRatesHandler, "/funding-rates?threshold=0.001")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"data":[]`) {
		t.Fatalf("body = %s, want empty array", body)
	}
	if !strings.Contains(body, `"count":0`) {
		t.Fatalf("body = %s, want count 0", body)
	}
}

func TestExtremeContinuityHandler(t *testing.T) {
	t.Parallel()

	rates := []float64{0.002, 0.003, 0.0001, -0.002, -0.003}
	rows := make([]store.UpsertRow, 0, len(rates))
	for index, rate := range rates {
		rows = append(rows, store.UpsertRow{
			Symbol:    "BTCUSDT",
			Rate:      rate,
			Timestamp: int64(index + 1),
		})
	}
	server := newTestServer(t, rows)

	recorder := doRequest(t, server.extremeContinuityHandler,
		"/funding-rates/extreme-continuity?symbol=BTCUSDT&threshold=0.001&lookback=5&direction=both")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}

	var response continuityResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if response.Symbol != "BTCUSDT" || response.Lookback != 5 {
		t.Errorf("response = %+v", response)
	}
	if !almostEqual(response.Stats.TotalHitRate, 0.8) {
		t.Errorf("total_hit_rate = %v, want 0.8", response.Stats.TotalHitRate)
	}
	if !almostEqual(response.Stats.ConsecutiveHitRate, 0.4) {
		t.Errorf("consecutive_hit_rate = %v, want 0.4", response.Stats.ConsecutiveHitRate)
	}
	if !almostEqual(response.Stats.AverageRunLength, 2.0) {
		t.Errorf("average_run_length = %v, want 2.0", response.Stats.AverageRunLength)
	}
	if len(response.History) != 5 {
		t.Fatalf("len(history) = %d, want 5", len(response.History))
	}
	if response.History[0].FundingTS != 5 {
		t.Errorf("history should be newest first, got ts=%d", response.History[0].FundingTS)
	}
	if response.History[0].Funding.Utilization != nil {
		t.Error("history utilization should be null")
	}
}

func TestExtremeContinuityNotFound(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, fixtureRows())
	recorder := doRequest(t, server.extremeContinuityHandler,
		"/funding-rates/extreme-continuity?symbol=UNKNOWN&threshold=0.001&lookback=10")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	var response errorResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if response.Error.Code != "HTTP_EXCEPTION" {
		t.Errorf("code = %s, want HTTP_EXCEPTION", response.Error.Code)
	}
	if !strings.Contains(response.Error.Message, "No funding rate history found") {
		t.Errorf("message = %q", response.Error.Message)
	}
}

func TestExtremeContinuityValidation(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, fixtureRows())

	tests := []struct {
		name   string
		target string
	}{
		{name: "missing symbol", target: "/funding-rates/extreme-continuity?threshold=0.001&lookback=10"},
		{name: "lookback too small", target: "/funding-rates/extreme-continuity?symbol=BTCUSDT&threshold=0.001&lookback=1"},
		{name: "lookback too large", target: "/funding-rates/extreme-continuity?symbol=BTCUSDT&threshold=0.001&lookback=201"},
		{name: "missing threshold", target: "/funding-rates/extreme-continuity?symbol=BTCUSDT&lookback=10"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := doRequest(t, server.extremeContinuityHandler, test.target)
			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", recorder.Code)
			}
		})
	}
}

func TestRootHandler(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	server.rootHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "CMMA Funding Rate API v3") {
		t.Errorf("body = %s", recorder.Body.String())
	}
}

func TestReadyHandler(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, nil)
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	server.readyHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

func findData(t *testing.T, data []fundingRateData, symbol string) fundingRateData {
	t.Helper()
	for _, item := range data {
		if item.Symbol == symbol {
			return item
		}
	}
	t.Fatalf("symbol %s not found", symbol)
	return fundingRateData{}
}

func almostEqual(a, b float64) bool {
	difference := a - b
	return difference < 1e-9 && difference > -1e-9
}
