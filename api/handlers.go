package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/deg-labs/funding-cmma-go/internal/funding"
	"github.com/deg-labs/funding-cmma-go/internal/store"
)

func (s *apiServer) rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Welcome to CMMA Funding Rate API v3. See /docs for details.",
	})
}

func (s *apiServer) fundingRatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	query := r.URL.Query()

	thresholdRaw := strings.TrimSpace(query.Get("threshold"))
	if thresholdRaw == "" {
		writeValidationError(w, "query->threshold", "Field required")
		return
	}
	threshold, err := parsePositiveFloat(thresholdRaw)
	if err != nil {
		writeValidationError(w, "query->threshold", "Input should be greater than 0")
		return
	}

	direction := strings.TrimSpace(query.Get("direction"))
	if direction == "" {
		direction = string(funding.DirectionBoth)
	}
	parsedDirection, ok := funding.ParseDirection(direction)
	if !ok {
		writeValidationError(w, "query->direction", "Input should be 'positive', 'negative' or 'both'")
		return
	}

	sortKey := strings.TrimSpace(query.Get("sort"))
	if sortKey == "" {
		sortKey = "funding_abs_desc"
	}
	if !contains(validSorts, sortKey) {
		writeValidationError(w, "query->sort", "Input should be one of "+strings.Join(validSorts, ", "))
		return
	}

	limit := 100
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err = parseIntInRange(raw, 1, 500)
		if err != nil {
			writeValidationError(w, "query->limit", "Input should be between 1 and 500")
			return
		}
	}

	var utilizationGTE *float64
	if raw := strings.TrimSpace(query.Get("utilization_gte")); raw != "" {
		value, parseErr := parseNonNegativeFloat(raw)
		if parseErr != nil {
			writeValidationError(w, "query->utilization_gte", "Input should be greater than or equal to 0")
			return
		}
		utilizationGTE = &value
	}

	rates, err := store.LatestRates(r.Context(), s.db, store.LatestQuery{
		Threshold:      threshold,
		Direction:      parsedDirection,
		Sort:           sortKey,
		Limit:          limit,
		UtilizationGTE: utilizationGTE,
	})
	if err != nil {
		s.logger.Error("query funding rates failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to query funding rates")
		return
	}

	data := make([]fundingRateData, 0, len(rates))
	for _, rate := range rates {
		data = append(data, toFundingRateData(rate, true))
	}

	writeJSON(w, http.StatusOK, fundingRateResponse{
		Count: len(data),
		Data:  data,
	})
}

func (s *apiServer) extremeContinuityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
		return
	}

	query := r.URL.Query()

	if _, ok := query["symbol"]; !ok {
		writeValidationError(w, "query->symbol", "Field required")
		return
	}
	symbol := query.Get("symbol")

	thresholdRaw := strings.TrimSpace(query.Get("threshold"))
	if thresholdRaw == "" {
		writeValidationError(w, "query->threshold", "Field required")
		return
	}
	threshold, err := parsePositiveFloat(thresholdRaw)
	if err != nil {
		writeValidationError(w, "query->threshold", "Input should be greater than 0")
		return
	}

	lookbackRaw := strings.TrimSpace(query.Get("lookback"))
	if lookbackRaw == "" {
		writeValidationError(w, "query->lookback", "Field required")
		return
	}
	lookback, err := parseIntInRange(lookbackRaw, 2, 200)
	if err != nil {
		writeValidationError(w, "query->lookback", "Input should be between 2 and 200")
		return
	}

	direction := strings.TrimSpace(query.Get("direction"))
	if direction == "" {
		direction = string(funding.DirectionBoth)
	}
	parsedDirection, ok := funding.ParseDirection(direction)
	if !ok {
		writeValidationError(w, "query->direction", "Input should be 'positive', 'negative' or 'both'")
		return
	}

	history, err := store.History(r.Context(), s.db, symbol, lookback)
	if err != nil {
		s.logger.Error("query funding history failed", "symbol", symbol, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to query funding history")
		return
	}
	if len(history) == 0 {
		writeError(w, http.StatusNotFound, "HTTP_EXCEPTION",
			fmt.Sprintf("No funding rate history found for symbol '%s' with lookback=%d.", symbol, lookback))
		return
	}

	rates := make([]float64, len(history))
	for index := range history {
		rates[len(history)-1-index] = history[index].Rate
	}
	stats := funding.ComputeContinuity(rates, threshold, parsedDirection)

	data := make([]fundingRateData, 0, len(history))
	for _, rate := range history {
		data = append(data, toFundingRateData(rate, false))
	}

	writeJSON(w, http.StatusOK, continuityResponse{
		Symbol:    symbol,
		Threshold: threshold,
		Lookback:  lookback,
		Stats: continuityStats{
			ConsecutiveHitRate: stats.ConsecutiveHitRate,
			TotalHitRate:       stats.TotalHitRate,
			AverageRunLength:   stats.AverageRunLength,
		},
		History: data,
	})
}

func (s *apiServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *apiServer) readyHandler(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "database is not reachable")
		return
	}
	exists, err := store.SchemaExists(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "database schema check failed")
		return
	}
	if !exists {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "funding_rates table is missing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func toFundingRateData(rate funding.StoredRate, withUtilization bool) fundingRateData {
	var utilization *float64
	if withUtilization && rate.Utilization != nil {
		rounded := funding.RoundHalfEven(*rate.Utilization, 7)
		utilization = &rounded
	}

	return fundingRateData{
		Symbol:        rate.Symbol,
		FundingTS:     rate.Timestamp,
		NextFundingTS: rate.NextFundingTime,
		Funding: fundingInfo{
			Rate:        rate.Rate,
			Direction:   rateDirection(rate.Rate),
			Utilization: utilization,
		},
		Constraints: constraintsInfo{
			IntervalHours: rate.IntervalHours,
			Cap:           rate.Cap,
			Floor:         rate.Floor,
		},
	}
}
