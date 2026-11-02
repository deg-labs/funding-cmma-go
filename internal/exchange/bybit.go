package exchange

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/deg-labs/funding-cmma-go/internal/funding"
)

const bybitHistoryLimit = 200

type bybitClient struct {
	*requester
	baseURL string
}

func (c *bybitClient) Name() string { return "bybit" }

type bybitInstrumentsResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []struct {
			Symbol string `json:"symbol"`
		} `json:"list"`
		NextPageCursor string `json:"nextPageCursor"`
	} `json:"result"`
}

func (r *bybitInstrumentsResponse) validate() error {
	return bybitRetCodeError("instruments-info", r.RetCode, r.RetMsg)
}

func (c *bybitClient) Symbols(ctx context.Context) ([]string, error) {
	symbols := make([]string, 0, 800)
	cursor := ""

	for {
		params := url.Values{}
		params.Set("category", "linear")
		params.Set("status", "Trading")
		params.Set("limit", "1000")
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		var payload bybitInstrumentsResponse
		endpoint := c.baseURL + "/v5/market/instruments-info?" + params.Encode()
		if err := c.getJSON(ctx, endpoint, &payload, payload.validate); err != nil {
			return nil, fmt.Errorf("fetch instruments: %w", err)
		}

		for _, item := range payload.Result.List {
			if hasSuffix(item.Symbol, "USDT") {
				symbols = append(symbols, item.Symbol)
			}
		}

		cursor = payload.Result.NextPageCursor
		if cursor == "" {
			break
		}
	}
	return symbols, nil
}

type bybitTickersResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []bybitTickerItem `json:"list"`
	} `json:"result"`
}

type bybitTickerItem struct {
	Symbol              string     `json:"symbol"`
	NextFundingTime     flexNumber `json:"nextFundingTime"`
	FundingIntervalHour flexNumber `json:"fundingIntervalHour"`
	FundingCap          flexNumber `json:"fundingCap"`
}

func (r *bybitTickersResponse) validate() error {
	return bybitRetCodeError("tickers", r.RetCode, r.RetMsg)
}

func (c *bybitClient) Tickers(ctx context.Context) (map[string]funding.Ticker, error) {
	params := url.Values{}
	params.Set("category", "linear")

	var payload bybitTickersResponse
	endpoint := c.baseURL + "/v5/market/tickers?" + params.Encode()
	if err := c.getJSON(ctx, endpoint, &payload, payload.validate); err != nil {
		return nil, fmt.Errorf("fetch tickers: %w", err)
	}

	tickers := make(map[string]funding.Ticker, len(payload.Result.List))
	for _, item := range payload.Result.List {
		if item.Symbol == "" {
			continue
		}
		ticker := funding.Ticker{}
		populated := false

		if value, ok := item.NextFundingTime.Int64(); ok {
			ticker.NextFundingTime = &value
			populated = true
		}
		if value, ok := item.FundingIntervalHour.Int64(); ok {
			hours := int(value)
			ticker.IntervalHours = &hours
			populated = true
		}
		if value, ok := item.FundingCap.Float64(); ok {
			capValue := value
			floorValue := -value
			ticker.Cap = &capValue
			ticker.Floor = &floorValue
			populated = true
		}

		if populated {
			tickers[item.Symbol] = ticker
		}
	}
	return tickers, nil
}

type bybitFundingResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []struct {
			Symbol               string     `json:"symbol"`
			FundingRate          flexNumber `json:"fundingRate"`
			FundingRateTimestamp flexNumber `json:"fundingRateTimestamp"`
		} `json:"list"`
	} `json:"result"`
}

func (r *bybitFundingResponse) validate() error {
	return bybitRetCodeError("funding/history", r.RetCode, r.RetMsg)
}

func (c *bybitClient) FundingHistory(ctx context.Context, symbol string, limit int) ([]funding.Record, error) {
	if limit <= 0 {
		limit = 1
	}
	if limit > bybitHistoryLimit {
		limit = bybitHistoryLimit
	}

	params := url.Values{}
	params.Set("category", "linear")
	params.Set("symbol", symbol)
	params.Set("limit", strconv.Itoa(limit))

	var payload bybitFundingResponse
	endpoint := c.baseURL + "/v5/market/funding/history?" + params.Encode()
	if err := c.getJSON(ctx, endpoint, &payload, payload.validate); err != nil {
		return nil, fmt.Errorf("fetch funding history for %s: %w", symbol, err)
	}

	records := make([]funding.Record, 0, len(payload.Result.List))
	for _, item := range payload.Result.List {
		rate, rateOK := item.FundingRate.Float64()
		ts, tsOK := item.FundingRateTimestamp.Int64()
		if !rateOK || !tsOK {
			c.logger.Warn("skip malformed bybit funding record",
				"symbol", symbol,
				"rate", item.FundingRate.value,
				"timestamp", item.FundingRateTimestamp.value,
			)
			continue
		}
		recordSymbol := item.Symbol
		if recordSymbol == "" {
			recordSymbol = symbol
		}
		records = append(records, funding.Record{
			Symbol:    recordSymbol,
			Rate:      rate,
			Timestamp: ts,
		})
	}
	return records, nil
}

func bybitRetCodeError(operation string, retCode int, retMsg string) error {
	if retCode == 0 {
		return nil
	}
	err := fmt.Errorf("bybit %s: retCode=%d retMsg=%s", operation, retCode, retMsg)
	// 10006 はレート制限です。
	if retCode == 10006 {
		return &retryableError{err: err}
	}
	return err
}
