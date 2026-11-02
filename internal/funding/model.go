// Package funding は Funding Rate のドメインモデルと計算処理を提供します。
package funding

// Record は取引所から取得した 1 件の Funding Rate 履歴です。
// Timestamp は Unix ミリ秒です。
type Record struct {
	Symbol    string
	Rate      float64
	Timestamp int64
}

// Ticker は取引所が公開している Funding Rate の付帯情報です。
// 値が存在しない場合は nil を保持し、0 で補完しません。
type Ticker struct {
	NextFundingTime *int64
	IntervalHours   *int
	Cap             *float64
	Floor           *float64
}

// StoredRate は funding_rates テーブルの 1 行です。
// Utilization は最新レート取得時のみ計算され、履歴取得時は nil です。
type StoredRate struct {
	Symbol          string
	Rate            float64
	Timestamp       int64
	NextFundingTime *int64
	IntervalHours   *int
	Cap             *float64
	Floor           *float64
	Utilization     *float64
}

// Direction は Funding Rate の方向フィルタです。
type Direction string

const (
	DirectionPositive Direction = "positive"
	DirectionNegative Direction = "negative"
	DirectionBoth     Direction = "both"
)

// ParseDirection は文字列を Direction に変換します。
func ParseDirection(raw string) (Direction, bool) {
	switch Direction(raw) {
	case DirectionPositive, DirectionNegative, DirectionBoth:
		return Direction(raw), true
	default:
		return "", false
	}
}
