package funding

import (
	"math"
	"testing"
)

func TestComputeContinuity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rates     []float64
		threshold float64
		direction Direction
		want      ContinuityStats
	}{
		{
			name:      "both counts positive and negative runs separately",
			rates:     []float64{0.002, 0.003, 0.0001, -0.002, -0.003},
			threshold: 0.001,
			direction: DirectionBoth,
			want: ContinuityStats{
				ConsecutiveHitRate: 0.4,
				TotalHitRate:       0.8,
				AverageRunLength:   2.0,
			},
		},
		{
			name:      "positive only",
			rates:     []float64{0.002, 0.003, 0.0001, -0.002, -0.003},
			threshold: 0.001,
			direction: DirectionPositive,
			want: ContinuityStats{
				ConsecutiveHitRate: 0,
				TotalHitRate:       0.4,
				AverageRunLength:   2.0,
			},
		},
		{
			name:      "negative only",
			rates:     []float64{0.002, 0.003, 0.0001, -0.002, -0.003},
			threshold: 0.001,
			direction: DirectionNegative,
			want: ContinuityStats{
				ConsecutiveHitRate: 0.4,
				TotalHitRate:       0.4,
				AverageRunLength:   2.0,
			},
		},
		{
			name:      "alternating directions have run length one",
			rates:     []float64{0.002, -0.002, 0.002, -0.002},
			threshold: 0.001,
			direction: DirectionBoth,
			want: ContinuityStats{
				ConsecutiveHitRate: 1,
				TotalHitRate:       1,
				AverageRunLength:   1,
			},
		},
		{
			name:      "no hits",
			rates:     []float64{0.0001, -0.0002},
			threshold: 0.001,
			direction: DirectionBoth,
			want:      ContinuityStats{},
		},
		{
			name:      "single hit at the threshold boundary",
			rates:     []float64{0.001},
			threshold: 0.001,
			direction: DirectionPositive,
			want: ContinuityStats{
				ConsecutiveHitRate: 1,
				TotalHitRate:       1,
				AverageRunLength:   1,
			},
		},
		{
			name:      "empty history",
			rates:     nil,
			threshold: 0.001,
			direction: DirectionBoth,
			want:      ContinuityStats{},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeContinuity(test.rates, test.threshold, test.direction)
			if !almostEqual(got.TotalHitRate, test.want.TotalHitRate) {
				t.Errorf("TotalHitRate = %v, want %v", got.TotalHitRate, test.want.TotalHitRate)
			}
			if !almostEqual(got.ConsecutiveHitRate, test.want.ConsecutiveHitRate) {
				t.Errorf("ConsecutiveHitRate = %v, want %v", got.ConsecutiveHitRate, test.want.ConsecutiveHitRate)
			}
			if !almostEqual(got.AverageRunLength, test.want.AverageRunLength) {
				t.Errorf("AverageRunLength = %v, want %v", got.AverageRunLength, test.want.AverageRunLength)
			}
		})
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
