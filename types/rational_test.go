package types

import (
	"fmt"
	"math"
	"testing"

	"github.com/asticode/go-astiav"
)

func TestRationalFromString(t *testing.T) {
	tests := []struct {
		input          string
		expectedNum    int
		expectedDen    int
		expectingError bool
	}{
		{"30", 30, 1, false},
		{"30/1", 30, 1, false},
		{"30000/1001", 30000, 1001, false}, // NTSC
		{"~23.976", 24000, 1001, false},    // NTSC
		{"~23.98", 24000, 1001, false},     // NTSC
		{"~29.93", 2993, 100, false},       // non-NTSC
		{"~29.97", 30000, 1001, false},     // NTSC
		{"~25", 25, 1, false},
		{"~47.952", 48000, 1001, false},
		{"~119.88", 120000, 1001, false},
		{"~60", 60, 1, false},
		{"~0.3", 3, 10, false},
		{"~0.33", 33, 100, false},
		{"0.33333", 33333, 100000, false},
		{"0/1", 0, 1, false},
		{"1/0", 0, 0, true},
		{"invalid", 0, 0, true},
		{"10/invalid", 0, 0, true},
	}

	for _, test := range tests {
		rational, err := RationalFromString(test.input)
		if test.expectingError {
			if err == nil {
				t.Errorf("Expected error for input %q, but got none", test.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("Unexpected error for input %q: %v", test.input, err)
			continue
		}
		if rational.Num != test.expectedNum || rational.Den != test.expectedDen {
			t.Errorf("For input %q, expected (%d/%d), but got (%d/%d)", test.input, test.expectedNum, test.expectedDen, rational.Num, rational.Den)
		}
	}
}

func TestRationalFromApproxFloat64(t *testing.T) {
	tests := []float64{
		0.3,
		0.33,
		24.0,
		23.976,
		23.98,
		25.0,
		29.671843,
		29.6868165522599696259931079111993312835693359375,
		29.6868165522599696259931079111993312835693359376,
		29.6868165522599696259931079111993312835693359377,
		29.6868165522599696259931079111993312835693359378,
		29.6868165522599696259931079111993312835693359379,
		29.6868165522599696269931079111993312835693359375,
		29.97,
		30.0,
		47.952,
		60.0,
		119.88,
	}

	for _, test := range tests {
		actual := RationalFromApproxFloat64(test)
		if actual.Num <= 0 || actual.Den <= 0 {
			t.Errorf("For input %f, got negative num or/and den: %d/%d", test, actual.Num, actual.Den)
		}
		if math.Abs(1-(actual.Float64()/test)) > 2e-2 {
			t.Errorf("For input %f, the approximated rational %d/%d gives %f which is not close enough", test, actual.Num, actual.Den, actual.Float64())
		}
		rationalLibAv := astiav.NewRational(actual.Num, actual.Den)
		if math.Abs(1-(rationalLibAv.Float64()/test)) > 2e-2 {
			t.Errorf("For input %f, the approximated rational %d/%d gives %f which is not close enough (libav)", test, actual.Num, actual.Den, rationalLibAv.Float64())
		}
	}
}

func BenchmarkRationalFromApproxFloat64(b *testing.B) {
	tests := []float64{
		0.3,
		0.33,
		24.0,
		23.976,
		23.98,
		25.0,
		29.5,
		29.6868165522599696259931079111993312835693359375,
		29.6868165522599696259931079111993312835693359376,
		29.6868165522599696259931079111993312835693359377,
		29.6868165522599696259931079111993312835693359378,
		29.6868165522599696259931079111993312835693359379,
		29.6868165522599696269931079111993312835693359375,
		29.97,
		30.0,
		47.952,
		60.0,
		119.88,
	}

	for _, test := range tests {
		b.Run(fmt.Sprintf("%f", test), func(b *testing.B) {
			for b.Loop() {
				_ = RationalFromApproxFloat64(test)
			}
		})
	}
}
