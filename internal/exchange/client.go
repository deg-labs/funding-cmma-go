// Package exchange は取引所ごとの API 差異を吸収します。
package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"

	"github.com/deg-labs/funding-cmma-go/internal/funding"
)

// Client は取引所クライアントの共通インターフェースです。
type Client interface {
	// Name は取引所名を返します。
	Name() string
	// Symbols は対象となる USDT 無期限契約のシンボル一覧を返します。
	Symbols(ctx context.Context) ([]string, error)
	// Tickers はシンボルごとの Funding Rate 付帯情報を返します。
	Tickers(ctx context.Context) (map[string]funding.Ticker, error)
	// FundingHistory は指定銘柄の履歴を新しい順に返します。
	FundingHistory(ctx context.Context, symbol string, limit int) ([]funding.Record, error)
}

// Options はクライアントの共通設定です。
type Options struct {
	BaseURL        string
	ProductType    string
	Timeout        time.Duration
	RetryMax       int
	RetryBaseDelay time.Duration
	RPS            float64
	Burst          int
	Logger         *slog.Logger
}

// New は取引所名に対応するクライアントを返します。
func New(name string, opts Options) (Client, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.RetryMax < 0 {
		opts.RetryMax = 0
	}
	if opts.RPS <= 0 {
		opts.RPS = 10
	}
	if opts.Burst <= 0 {
		opts.Burst = 20
	}
	if opts.RetryBaseDelay <= 0 {
		opts.RetryBaseDelay = time.Second
	}

	req := &requester{
		httpClient: &http.Client{Timeout: opts.Timeout},
		limiter:    rate.NewLimiter(rate.Limit(opts.RPS), opts.Burst),
		retryMax:   opts.RetryMax,
		baseDelay:  opts.RetryBaseDelay,
		maxDelay:   30 * time.Second,
		logger:     opts.Logger,
	}

	switch name {
	case "bybit":
		return &bybitClient{requester: req, baseURL: opts.BaseURL}, nil
	case "bitget":
		return &bitgetClient{
			requester:   req,
			baseURL:     opts.BaseURL,
			productType: opts.ProductType,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported exchange: %s", name)
	}
}

type requester struct {
	httpClient *http.Client
	limiter    *rate.Limiter
	retryMax   int
	baseDelay  time.Duration
	maxDelay   time.Duration
	logger     *slog.Logger
}

type retryableError struct {
	err        error
	retryAfter time.Duration
}

func (e *retryableError) Error() string { return e.err.Error() }
func (e *retryableError) Unwrap() error { return e.err }

// getJSON は GET リクエストを送信し、JSON を out へデコードします。
// 429、5xx、接続エラー、不正 JSON、validate が返す再試行可能エラーのみ再試行します。
func (r *requester) getJSON(ctx context.Context, rawURL string, out any, validate func() error) error {
	attempts := r.retryMax + 1
	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		if err := r.limiter.Wait(ctx); err != nil {
			return err
		}

		err := r.doOnce(ctx, rawURL, out, validate)
		if err == nil {
			return nil
		}
		lastErr = err

		var retryable *retryableError
		if !errors.As(err, &retryable) {
			return err
		}
		if attempt == attempts {
			break
		}

		wait := retryable.retryAfter
		if wait <= 0 {
			wait = r.backoff(attempt)
		}
		r.logger.Warn("exchange request failed, retrying",
			"url", rawURL,
			"attempt", attempt,
			"wait", wait.String(),
			"error", err.Error(),
		)

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return fmt.Errorf("request failed after %d attempts: %w", attempts, lastErr)
}

func (r *requester) doOnce(ctx context.Context, rawURL string, out any, validate func() error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &retryableError{err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return &retryableError{
			err:        fmt.Errorf("unexpected status: %d", resp.StatusCode),
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("unexpected status: %d body=%s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return &retryableError{err: fmt.Errorf("decode response: %w", err)}
	}
	if validate != nil {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (r *requester) backoff(attempt int) time.Duration {
	delay := r.baseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= r.maxDelay {
			delay = r.maxDelay
			break
		}
	}
	if delay <= 0 {
		delay = r.baseDelay
	}
	// 同時再送を避けるため 0〜50% のジッターを加えます。
	jitter := time.Duration(rand.Int64N(int64(delay/2) + 1))
	return delay + jitter
}

func parseRetryAfter(raw string) time.Duration {
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// flexNumber は文字列と数値の両方を受け付ける JSON 値です。
type flexNumber struct {
	value string
	valid bool
}

func (f *flexNumber) UnmarshalJSON(data []byte) error {
	raw := string(data)
	if raw == "null" {
		return nil
	}
	raw = trimQuotes(raw)
	if raw == "" {
		return nil
	}
	f.value = raw
	f.valid = true
	return nil
}

func (f flexNumber) Float64() (float64, bool) {
	if !f.valid {
		return 0, false
	}
	value, err := strconv.ParseFloat(f.value, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func (f flexNumber) Int64() (int64, bool) {
	if !f.valid {
		return 0, false
	}
	value, err := strconv.ParseInt(f.value, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func trimQuotes(raw string) string {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}
