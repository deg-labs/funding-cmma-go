package main

import (
	"database/sql"
	"log/slog"
)

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type fundingInfo struct {
	Rate        float64  `json:"rate"`
	Direction   string   `json:"direction"`
	Utilization *float64 `json:"utilization"`
}

type constraintsInfo struct {
	IntervalHours *int     `json:"interval_hours"`
	Cap           *float64 `json:"cap"`
	Floor         *float64 `json:"floor"`
}

type fundingRateData struct {
	Symbol        string          `json:"symbol"`
	FundingTS     int64           `json:"funding_ts"`
	NextFundingTS *int64          `json:"next_funding_ts"`
	Funding       fundingInfo     `json:"funding"`
	Constraints   constraintsInfo `json:"constraints"`
}

type fundingRateResponse struct {
	Count int               `json:"count"`
	Data  []fundingRateData `json:"data"`
}

type continuityStats struct {
	ConsecutiveHitRate float64 `json:"consecutive_hit_rate"`
	TotalHitRate       float64 `json:"total_hit_rate"`
	AverageRunLength   float64 `json:"average_run_length"`
}

type continuityResponse struct {
	Symbol    string            `json:"symbol"`
	Threshold float64           `json:"threshold"`
	Lookback  int               `json:"lookback"`
	Stats     continuityStats   `json:"stats"`
	History   []fundingRateData `json:"history"`
}

type apiServer struct {
	db      *sql.DB
	logger  *slog.Logger
	metrics *metricsRegistry
}

var validSorts = []string{
	"funding_abs_desc",
	"funding_rate_desc",
	"funding_rate_asc",
	"utilization_desc",
	"symbol_asc",
}
