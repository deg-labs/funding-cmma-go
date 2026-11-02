package exchange

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/deg-labs/funding-cmma-go/internal/funding"
)

const bitgetHistoryPageSize = 100

type bitgetClient struct {
	*requester
	baseURL     string
	productType string
}

func (c *bitgetClient) Name() string { return "bitget" }

type bitgetFundingItem struct {
	Symbol              string     `json:"symbol"`
	FundingRate         flexNumber `json:"fundingRate"`
	NextUpdate          flexNumber `json:"nextUpdate"`
	FundingRateInterval flexNumber `json:"fundingRateInterval"`
	MaxFundingRate      flexNumber `json:"maxFundingRate"`
	MinFundingRate      flexNumber `json:"minFundingRate"`
}

type bitgetCurrentResponse struct {
	Code string              `json:"code"`
	Msg  string              `json:"msg"`
	Data []bitgetFundingItem `json:"data"`
}

func (r *bitgetCurrentResponse) validate() error {
	return bitgetCodeError("current-fund-rate", r.Code, r.Msg)
}

func (c *bitgetClient) currentFunding(ctx context.Context) ([]bitgetFundingItem, error) {
	params := url.Values{}
	params.Set("productType", c.productType)

	var payload bitgetCurrentResponse
	endpoint := c.baseURL + "/api/v2/mix/market/current-fund-rate?" + params.Encode()
	if err := c.getJSON(ctx, endpoint, &payload, payload.validate); err != nil {
		return nil, fmt.Errorf("fetch current funding rates: %w", err)
	}
	return payload.Data, nil
}

func (c *bitgetClient) Symbols(ctx context.Context) ([]string, error) {
	items, err := c.currentFunding(ctx)
	if err != nil {
		return nil, err
	}

	symbols := make([]string, 0, len(items))
	for _, item := range items {
		if item.Symbol != "" && hasSuffix(item.Symbol, "USDT") {
			symbols = append(symbols, item.Symbol)
		}
	}
	return symbols, nil
}

func (c *bitgetClient) Tickers(ctx context.Context) (map[string]funding.Ticker, error) {
	items, err := c.currentFunding(ctx)
	if err != nil {
		return nil, err
	}

	tickers := make(map[string]funding.Ticker, len(items))
	for _, item := range items {
		if item.Symbol == "" {
			continue
		}
		ticker := funding.Ticker{}
		populated := false

		if value, ok := item.NextUpdate.Int64(); ok {
			ticker.NextFundingTime = &value
			populated = true
		}
		if value, ok := item.FundingRateInterval.Int64(); ok {
			hours := int(value)
			ticker.IntervalHours = &hours
			populated = true
		}
		if value, ok := item.MaxFundingRate.Float64(); ok {
			capValue := value
			ticker.Cap = &capValue
			populated = true
		}
		if value, ok := item.MinFundingRate.Float64(); ok {
			floorValue := value
			ticker.Floor = &floorValue
			populated = true
		}

		if populated {
			tickers[item.Symbol] = ticker
		}
	}
	return tickers, nil
}

type bitgetHistoryResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		Symbol      string     `json:"symbol"`
		FundingRate flexNumber `json:"fundingRate"`
		FundingTime flexNumber `json:"fundingTime"`
	} `json:"data"`
}

func (r *bitgetHistoryResponse) validate() error {
	return bitgetCodeError("history-fund-rate", r.Code, r.Msg)
}

func (c *bitgetClient) FundingHistory(ctx context.Context, symbol string, limit int) ([]funding.Record, error) {
	if limit <= 0 {
		limit = 1
	}

	records := make([]funding.Record, 0, limit)
	for page := 1; len(records) < limit; page++ {
		pageSize := limit - len(records)
		if pageSize > bitgetHistoryPageSize {
			pageSize = bitgetHistoryPageSize
		}

		params := url.Values{}
		params.Set("symbol", symbol)
		params.Set("productType", c.productType)
		params.Set("pageSize", strconv.Itoa(pageSize))
		params.Set("pageNo", strconv.Itoa(page))

		var payload bitgetHistoryResponse
		endpoint := c.baseURL + "/api/v2/mix/market/history-fund-rate?" + params.Encode()
		if err := c.getJSON(ctx, endpoint, &payload, payload.validate); err != nil {
			return nil, fmt.Errorf("fetch funding history for %s: %w", symbol, err)
		}

		if len(payload.Data) == 0 {
			break
		}

		for _, item := range payload.Data {
			rate, rateOK := item.FundingRate.Float64()
			ts, tsOK := item.FundingTime.Int64()
			if !rateOK || !tsOK {
				c.logger.Warn("skip malformed bitget funding record",
					"symbol", symbol,
					"rate", item.FundingRate.value,
					"timestamp", item.FundingTime.value,
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
			if len(records) >= limit {
				break
			}
		}

		if len(payload.Data) < pageSize {
			break
		}
	}
	return records, nil
}

func bitgetCodeError(operation, code, message string) error {
	if code == "" || code == "00000" {
		return nil
	}
	err := fmt.Errorf("bitget %s: code=%s msg=%s", operation, code, message)
	// 429 と 40034 はレート制限です。
	if code == "429" || code == "40034" {
		return &retryableError{err: err}
	}
	return err
}

func hasSuffix(value, suffix string) bool {
	return strings.HasSuffix(value, suffix)
}
