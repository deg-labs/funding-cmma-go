package funding

import "math"

// Utilization は Funding Rate の利用率を返します。
//
// 既存 Python 実装との互換性のため、負のレートでは負の値を返します。
// これは API 仕様の「0.0〜1.0」とは一致しませんが、移行時に挙動を変えないため
// あえて現行どおりに実装しています。修正する場合は破壊的変更として扱ってください。
//
// cap または floor が nil、あるいは 0 の場合は nil を返します。
// SQLite では NULL との除算および 0 除算が NULL になるため、その挙動に合わせています。
func Utilization(rate float64, cap, floor *float64) *float64 {
	switch {
	case rate > 0:
		if cap == nil || *cap == 0 {
			return nil
		}
		return floatPtr(rate / *cap)
	case rate < 0:
		if floor == nil || *floor == 0 {
			return nil
		}
		return floatPtr(rate / math.Abs(*floor))
	default:
		return floatPtr(0)
	}
}

// RoundHalfEven は Python の round() に近い丸めを行います。
func RoundHalfEven(value float64, digits int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	pow := math.Pow(10, float64(digits))
	return math.RoundToEven(value*pow) / pow
}

func floatPtr(v float64) *float64 {
	return &v
}
