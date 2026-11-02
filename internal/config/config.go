// Package config は環境変数から設定を読み込みます。
//
// 不正な値は無言で既定値へ戻さず、起動時にエラーとして返します。
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/deg-labs/funding-cmma-go/internal/logging"
)

// Fetcher はデータ収集プロセスの設定です。
type Fetcher struct {
	Exchange                string
	BaseURL                 string
	BitgetProductType       string
	FetchInterval           time.Duration
	ConcurrencyLimit        int
	FundingRateThreshold    float64
	HistoryLimit            int
	IncrementalHistoryLimit int
	RequestTimeout          time.Duration
	RetryMax                int
	RateLimitRPS            float64
	RateLimitBurst          int
	DBPath                  string
	LogLevel                slog.Level
	LogFormat               string
}

// API は HTTP API プロセスの設定です。
type API struct {
	DBPath       string
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxOpenConns int
	LogLevel     slog.Level
	LogFormat    string
}

// LoadFetcher は fetcher 用の設定を読み込みます。
func LoadFetcher() (Fetcher, error) {
	exchange := strings.ToLower(getEnv("EXCHANGE", "bybit"))
	if exchange != "bybit" && exchange != "bitget" {
		return Fetcher{}, fmt.Errorf("EXCHANGE must be either 'bybit' or 'bitget': %q", exchange)
	}

	level, err := logging.ParseLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		return Fetcher{}, err
	}
	format := strings.ToLower(getEnv("LOG_FORMAT", "json"))
	if format != "json" && format != "text" {
		return Fetcher{}, fmt.Errorf("LOG_FORMAT must be 'json' or 'text': %q", format)
	}

	fetchInterval, err := intEnv("FETCH_INTERVAL_SECONDS", 300, 1)
	if err != nil {
		return Fetcher{}, err
	}
	concurrency, err := intEnv("CONCURRENCY_LIMIT", 10, 1)
	if err != nil {
		return Fetcher{}, err
	}
	historyLimit, err := intEnv("FUNDING_RATE_HISTORY_LIMIT", 200, 1)
	if err != nil {
		return Fetcher{}, err
	}
	incremental, err := intEnv("INCREMENTAL_HISTORY_LIMIT", 10, 1)
	if err != nil {
		return Fetcher{}, err
	}
	timeout, err := intEnv("REQUEST_TIMEOUT_SECONDS", 10, 1)
	if err != nil {
		return Fetcher{}, err
	}
	retryMax, err := intEnv("REQUEST_RETRY_MAX", 3, 0)
	if err != nil {
		return Fetcher{}, err
	}
	burst, err := intEnv("RATE_LIMIT_BURST", 20, 1)
	if err != nil {
		return Fetcher{}, err
	}

	rpsRaw := getEnv("RATE_LIMIT_RPS", "10")
	rps, err := strconv.ParseFloat(rpsRaw, 64)
	if err != nil || rps <= 0 {
		return Fetcher{}, fmt.Errorf("RATE_LIMIT_RPS must be a positive number: %q", rpsRaw)
	}

	thresholdRaw := getEnv("FUNDING_RATE_THRESHOLD", "0.001")
	threshold, err := strconv.ParseFloat(thresholdRaw, 64)
	if err != nil || threshold <= 0 {
		return Fetcher{}, fmt.Errorf("FUNDING_RATE_THRESHOLD must be a positive number: %q", thresholdRaw)
	}

	baseURL := ""
	switch exchange {
	case "bybit":
		baseURL = getEnv("BYBIT_BASE_URL", "https://api.bybit.com")
	case "bitget":
		baseURL = getEnv("BITGET_BASE_URL", "https://api.bitget.com")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	return Fetcher{
		Exchange:                exchange,
		BaseURL:                 baseURL,
		BitgetProductType:       getEnv("BITGET_PRODUCT_TYPE", "usdt-futures"),
		FetchInterval:           time.Duration(fetchInterval) * time.Second,
		ConcurrencyLimit:        concurrency,
		FundingRateThreshold:    threshold,
		HistoryLimit:            historyLimit,
		IncrementalHistoryLimit: incremental,
		RequestTimeout:          time.Duration(timeout) * time.Second,
		RetryMax:                retryMax,
		RateLimitRPS:            rps,
		RateLimitBurst:          burst,
		DBPath:                  getEnv("DB_PATH", "/app/data/cmma.db"),
		LogLevel:                level,
		LogFormat:               format,
	}, nil
}

// LoadAPI は API 用の設定を読み込みます。
func LoadAPI() (API, error) {
	level, err := logging.ParseLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		return API{}, err
	}
	format := strings.ToLower(getEnv("LOG_FORMAT", "json"))
	if format != "json" && format != "text" {
		return API{}, fmt.Errorf("LOG_FORMAT must be 'json' or 'text': %q", format)
	}

	readTimeout, err := intEnv("HTTP_READ_TIMEOUT_SECONDS", 10, 1)
	if err != nil {
		return API{}, err
	}
	writeTimeout, err := intEnv("HTTP_WRITE_TIMEOUT_SECONDS", 10, 1)
	if err != nil {
		return API{}, err
	}
	maxOpen, err := intEnv("DB_MAX_OPEN_CONNS", 4, 1)
	if err != nil {
		return API{}, err
	}

	addr := getEnv("HTTP_ADDR", ":8000")
	if !strings.Contains(addr, ":") {
		return API{}, fmt.Errorf("HTTP_ADDR must contain a port: %q", addr)
	}

	return API{
		DBPath:       getEnv("DB_PATH", "/app/data/cmma.db"),
		Addr:         addr,
		ReadTimeout:  time.Duration(readTimeout) * time.Second,
		WriteTimeout: time.Duration(writeTimeout) * time.Second,
		MaxOpenConns: maxOpen,
		LogLevel:     level,
		LogFormat:    format,
	}, nil
}

func getEnv(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func intEnv(key string, fallback, minimum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %q", key, raw)
	}
	if value < minimum {
		return 0, fmt.Errorf("%s must be >= %d: %d", key, minimum, value)
	}
	return value, nil
}
