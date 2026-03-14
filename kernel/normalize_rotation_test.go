package kernel

import (
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
)

func TestNormalizeRotation(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{name: "zero", input: 0, expected: 0},
		{name: "90", input: 90, expected: 90},
		{name: "180", input: 180, expected: 180},
		{name: "270", input: 270, expected: 270},
		{name: "360_wraps_to_0", input: 360, expected: 0},
		{name: "negative_90", input: -90, expected: 270},
		{name: "negative_180", input: -180, expected: 180},
		{name: "negative_270", input: -270, expected: 90},
		{name: "negative_360", input: -360, expected: 0},
		{name: "450_wraps_to_90", input: 450, expected: 90},
		{name: "720_wraps_to_0", input: 720, expected: 0},
		{name: "negative_450", input: -450, expected: 270},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testifyassert.Equal(t, tt.expected, normalizeRotation(tt.input))
		})
	}
}
