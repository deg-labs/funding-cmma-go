package main

import "github.com/go-openapi/spec"

func buildOpenAPISpec() ([]byte, error) {
	swagger := spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Swagger:  "2.0",
			BasePath: "/",
			Info: &spec.Info{InfoProps: spec.InfoProps{
				Title:       "CMMA Funding Rate API",
				Description: "取引所の異常な Funding Rate を取得する API",
				Version:     "3.0.0-go",
			}},
			Consumes: []string{"application/json"},
			Produces: []string{"application/json"},
			Paths: &spec.Paths{Paths: map[string]spec.PathItem{
				"/": {
					PathItemProps: spec.PathItemProps{
						Get: spec.NewOperation("root").
							WithSummary("Root endpoint").
							WithDescription("Service root endpoint").
							RespondsWith(200, schemaResponse("Root response", "#/definitions/RootResponse")),
					},
				},
				"/funding-rates": {
					PathItemProps: spec.PathItemProps{Get: fundingRatesOperation()},
				},
				"/funding-rates/extreme-continuity": {
					PathItemProps: spec.PathItemProps{Get: extremeContinuityOperation()},
				},
			}},
			Definitions: apiDefinitions(),
		},
	}

	return swagger.MarshalJSON()
}

func fundingRatesOperation() *spec.Operation {
	threshold := spec.QueryParam("threshold").Typed("number", "double").
		WithDescription("Funding Rate の閾値（絶対値）。この値以上の |rate| を持つ銘柄が対象です。")
	threshold.Required = true
	threshold.Minimum = float64Ptr(0)
	threshold.ExclusiveMinimum = true

	direction := spec.QueryParam("direction").Typed("string", "").
		WithDescription("Funding Rate の方向でフィルタします。")
	direction.Default = "both"
	direction.Enum = []any{"positive", "negative", "both"}

	sortParam := spec.QueryParam("sort").Typed("string", "").
		WithDescription("結果のソート順。")
	sortParam.Default = "funding_abs_desc"
	sortParam.Enum = toAnySlice(validSorts)

	limit := spec.QueryParam("limit").Typed("integer", "int32").
		WithDescription("取得する最大件数。")
	limit.Default = 100
	limit.Minimum = float64Ptr(1)
	limit.Maximum = float64Ptr(500)

	utilization := spec.QueryParam("utilization_gte").Typed("number", "double").
		WithDescription("Funding Rate の利用率（派生値）の最小値。")
	utilization.Minimum = float64Ptr(0)

	operation := spec.NewOperation("getFundingRates").
		WithSummary("条件に基づき資金調達率データを取得").
		WithDescription("大きさと方向で Funding Rate をフィルタリングし、銘柄ごとの最新レコードを返します。").
		WithTags("funding-rates")
	operation.Parameters = []spec.Parameter{
		*threshold, *direction, *sortParam, *limit, *utilization,
	}
	operation.Responses = &spec.Responses{ResponsesProps: spec.ResponsesProps{StatusCodeResponses: map[int]spec.Response{
		200: *schemaResponse("成功", "#/definitions/FundingRateResponse"),
		422: *schemaResponse("入力検証エラー", "#/definitions/ErrorResponse"),
		500: *schemaResponse("サーバーエラー", "#/definitions/ErrorResponse"),
	}}}
	return operation
}

func extremeContinuityOperation() *spec.Operation {
	symbol := spec.QueryParam("symbol").Typed("string", "").
		WithDescription("分析対象の銘柄シンボル（例: BTCUSDT）。")
	symbol.Required = true

	threshold := spec.QueryParam("threshold").Typed("number", "double").
		WithDescription("「極端」と判断する Funding Rate の閾値（絶対値）。")
	threshold.Required = true
	threshold.Minimum = float64Ptr(0)
	threshold.ExclusiveMinimum = true

	lookback := spec.QueryParam("lookback").Typed("integer", "int32").
		WithDescription("分析対象とする最新の履歴件数。")
	lookback.Required = true
	lookback.Minimum = float64Ptr(2)
	lookback.Maximum = float64Ptr(200)

	direction := spec.QueryParam("direction").Typed("string", "").
		WithDescription("分析対象とする Funding Rate の方向。")
	direction.Default = "both"
	direction.Enum = []any{"positive", "negative", "both"}

	operation := spec.NewOperation("getExtremeContinuity").
		WithSummary("極端な資金調達率の継続性を分析").
		WithDescription("指定した銘柄の Funding Rate が閾値を超えた状態の継続性を返します。").
		WithTags("funding-rates")
	operation.Parameters = []spec.Parameter{*symbol, *threshold, *lookback, *direction}
	operation.Responses = &spec.Responses{ResponsesProps: spec.ResponsesProps{StatusCodeResponses: map[int]spec.Response{
		200: *schemaResponse("成功", "#/definitions/ExtremeContinuityResponse"),
		404: *schemaResponse("履歴なし", "#/definitions/ErrorResponse"),
		422: *schemaResponse("入力検証エラー", "#/definitions/ErrorResponse"),
		500: *schemaResponse("サーバーエラー", "#/definitions/ErrorResponse"),
	}}}
	return operation
}

func apiDefinitions() spec.Definitions {
	return spec.Definitions{
		"RootResponse": objectSchema(map[string]spec.Schema{
			"message": schemaWithDescription(*spec.StringProperty(), "ルートメッセージ"),
		}, "message"),
		"ErrorDetail": objectSchema(map[string]spec.Schema{
			"code":    schemaWithDescription(*spec.StringProperty(), "エラーコード"),
			"message": schemaWithDescription(*spec.StringProperty(), "エラーメッセージ"),
		}, "code", "message"),
		"ErrorResponse": objectSchema(map[string]spec.Schema{
			"error": schemaWithDescription(*spec.RefSchema("#/definitions/ErrorDetail"), "エラー詳細"),
		}, "error"),
		"FundingInfo": objectSchema(map[string]spec.Schema{
			"rate":        schemaWithDescription(*spec.Float64Property(), "資金調達率"),
			"direction":   schemaWithDescription(*spec.StringProperty(), "方向 (positive, negative, neutral)"),
			"utilization": schemaWithDescription(*spec.Float64Property(), "利用率。算出できない場合は null"),
		}, "rate", "direction", "utilization"),
		"ConstraintsInfo": objectSchema(map[string]spec.Schema{
			"interval_hours": schemaWithDescription(*spec.Int64Property(), "資金調達間隔（時間）"),
			"cap":            schemaWithDescription(*spec.Float64Property(), "資金調達率の上限"),
			"floor":          schemaWithDescription(*spec.Float64Property(), "資金調達率の下限"),
		}, "interval_hours", "cap", "floor"),
		"FundingRateData": objectSchema(map[string]spec.Schema{
			"symbol":          schemaWithDescription(*spec.StringProperty(), "銘柄シンボル"),
			"funding_ts":      schemaWithDescription(*spec.Int64Property(), "資金調達率のタイムスタンプ（ミリ秒）"),
			"next_funding_ts": schemaWithDescription(*spec.Int64Property(), "次の資金調達時刻（ミリ秒）"),
			"funding":         schemaWithDescription(*spec.RefSchema("#/definitions/FundingInfo"), "資金調達率"),
			"constraints":     schemaWithDescription(*spec.RefSchema("#/definitions/ConstraintsInfo"), "制約情報"),
		}, "symbol", "funding_ts", "next_funding_ts", "funding", "constraints"),
		"FundingRateResponse": objectSchema(map[string]spec.Schema{
			"count": schemaWithDescription(*spec.Int64Property(), "返却件数"),
			"data":  schemaWithDescription(*spec.ArrayProperty(spec.RefSchema("#/definitions/FundingRateData")), "Funding Rate データ"),
		}, "count", "data"),
		"ExtremeContinuityStats": objectSchema(map[string]spec.Schema{
			"consecutive_hit_rate": schemaWithDescription(*spec.Float64Property(), "連続ヒット継続率"),
			"total_hit_rate":       schemaWithDescription(*spec.Float64Property(), "有効区間内継続率"),
			"average_run_length":   schemaWithDescription(*spec.Float64Property(), "平均ラン長"),
		}, "consecutive_hit_rate", "total_hit_rate", "average_run_length"),
		"ExtremeContinuityResponse": objectSchema(map[string]spec.Schema{
			"symbol":    schemaWithDescription(*spec.StringProperty(), "分析対象の銘柄シンボル"),
			"threshold": schemaWithDescription(*spec.Float64Property(), "分析に使用した閾値"),
			"lookback":  schemaWithDescription(*spec.Int64Property(), "分析対象の履歴件数"),
			"stats":     schemaWithDescription(*spec.RefSchema("#/definitions/ExtremeContinuityStats"), "統計指標"),
			"history":   schemaWithDescription(*spec.ArrayProperty(spec.RefSchema("#/definitions/FundingRateData")), "履歴データ"),
		}, "symbol", "threshold", "lookback", "stats", "history"),
	}
}

func objectSchema(props map[string]spec.Schema, required ...string) spec.Schema {
	return spec.Schema{SchemaProps: spec.SchemaProps{Type: []string{"object"}, Properties: props, Required: required}}
}

func schemaWithDescription(s spec.Schema, description string) spec.Schema {
	s.Description = description
	return s
}

func schemaResponse(description, schemaRef string) *spec.Response {
	return spec.NewResponse().WithDescription(description).WithSchema(spec.RefSchema(schemaRef))
}

func toAnySlice(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func float64Ptr(value float64) *float64 {
	return &value
}
