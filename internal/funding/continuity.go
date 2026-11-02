package funding

// ContinuityStats は極端な Funding Rate の継続性指標です。
type ContinuityStats struct {
	ConsecutiveHitRate float64
	TotalHitRate       float64
	AverageRunLength   float64
}

// ComputeContinuity は履歴から継続性指標を計算します。
//
// rates は古い順（昇順）で渡してください。
//
// 既存 Python 実装と同じ結果になるよう、次の点を再現しています。
//   - total_hit_rate と consecutive_hit_rate は正負を区別しない
//   - direction=both の average_run_length は正のランと負のランを別々に数える
//   - 分母は要求した lookback ではなく、実際に取得できた件数
func ComputeContinuity(rates []float64, threshold float64, direction Direction) ContinuityStats {
	n := len(rates)
	if n == 0 {
		return ContinuityStats{}
	}

	var isExtreme []bool
	var runs []int
	totalHits := 0

	switch direction {
	case DirectionPositive:
		isExtreme = make([]bool, n)
		for i, rate := range rates {
			isExtreme[i] = rate >= threshold
		}
		runs = trueRuns(isExtreme)
		totalHits = countTrue(isExtreme)
	case DirectionNegative:
		isExtreme = make([]bool, n)
		for i, rate := range rates {
			isExtreme[i] = rate <= -threshold
		}
		runs = trueRuns(isExtreme)
		totalHits = countTrue(isExtreme)
	default:
		positive := make([]bool, n)
		negative := make([]bool, n)
		isExtreme = make([]bool, n)
		for i, rate := range rates {
			positive[i] = rate >= threshold
			negative[i] = rate <= -threshold
			isExtreme[i] = positive[i] || negative[i]
		}
		totalHits = countTrue(isExtreme)
		runs = append(trueRuns(positive), trueRuns(negative)...)
	}

	consecutiveHits := 0
	for i := n - 1; i >= 0; i-- {
		if !isExtreme[i] {
			break
		}
		consecutiveHits++
	}

	stats := ContinuityStats{
		TotalHitRate:       float64(totalHits) / float64(n),
		ConsecutiveHitRate: float64(consecutiveHits) / float64(n),
	}
	if len(runs) > 0 {
		sum := 0
		for _, run := range runs {
			sum += run
		}
		stats.AverageRunLength = float64(sum) / float64(len(runs))
	}
	return stats
}

func trueRuns(flags []bool) []int {
	runs := make([]int, 0, len(flags))
	current := 0
	for _, flag := range flags {
		if flag {
			current++
			continue
		}
		if current > 0 {
			runs = append(runs, current)
			current = 0
		}
	}
	if current > 0 {
		runs = append(runs, current)
	}
	return runs
}

func countTrue(flags []bool) int {
	count := 0
	for _, flag := range flags {
		if flag {
			count++
		}
	}
	return count
}
