package types

import (
	"testing"
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
		{"~0.33", 1, 3, false},
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
