package funding

import "testing"

func TestUtilization(t *testing.T) {
	t.Parallel()

	capValue := 0.01
	floorValue := -0.01
	zero := 0.0

	tests := []struct {
		name      string
		rate      float64
		cap       *float64
		floor     *float64
		want      *float64
		wantIsNil bool
	}{
		{name: "positive rate uses cap", rate: 0.005, cap: &capValue, floor: &floorValue, want: floatPointer(0.5)},
		{name: "negative rate keeps the legacy negative sign", rate: -0.005, cap: &capValue, floor: &floorValue, want: floatPointer(-0.5)},
		{name: "zero rate is zero", rate: 0, cap: &capValue, floor: &floorValue, want: floatPointer(0)},
		{name: "missing cap yields null", rate: 0.005, cap: nil, floor: &floorValue, wantIsNil: true},
		{name: "missing floor yields null", rate: -0.005, cap: &capValue, floor: nil, wantIsNil: true},
		{name: "zero cap yields null", rate: 0.005, cap: &zero, floor: &floorValue, wantIsNil: true},
		{name: "zero floor yields null", rate: -0.005, cap: &capValue, floor: &zero, wantIsNil: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := Utilization(test.rate, test.cap, test.floor)
			if test.wantIsNil {
				if got != nil {
					t.Fatalf("Utilization() = %v, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("Utilization() = nil, want %v", *test.want)
			}
			if *got != *test.want {
				t.Errorf("Utilization() = %v, want %v", *got, *test.want)
			}
		})
	}
}

func TestRoundHalfEven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value  float64
		digits int
		want   float64
	}{
		{value: 0.12345678, digits: 7, want: 0.1234568},
		{value: -0.5, digits: 0, want: -0},
		{value: 1.5, digits: 0, want: 2},
		{value: 2.5, digits: 0, want: 2},
	}

	for _, test := range tests {
		got := RoundHalfEven(test.value, test.digits)
		if got != test.want {
			t.Errorf("RoundHalfEven(%v, %d) = %v, want %v", test.value, test.digits, got, test.want)
		}
	}
}

func floatPointer(value float64) *float64 {
	return &value
}
