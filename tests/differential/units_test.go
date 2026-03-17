package differential

import (
	"fmt"
	"strings"
	"testing"
)

// ediv implements Euclidean division matching Lean 4's Int.div semantics.
// The remainder is always non-negative: a = q*b + r, 0 <= r < |b|.
func ediv(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	q := a / b
	r := a % b
	if r < 0 {
		if b > 0 {
			q--
		} else {
			q++
		}
	}
	return q
}

func TestDiffUnits(t *testing.T) {
	var input strings.Builder
	type testCase struct {
		op   string
		a, b int64
	}
	var cases []testCase

	// Values for single-arg operations (tob, toB, tobps, toBps).
	singleArgVals := make([]int64, 0, 300)
	for v := int64(-100); v <= 100; v++ {
		singleArgVals = append(singleArgVals, v)
	}
	// Add larger boundary values.
	for _, v := range []int64{-10000, -1000, -255, -256, 255, 256, 1000, 10000, 1000000, -1000000} {
		singleArgVals = append(singleArgVals, v)
	}

	singleOps := []string{"tob", "toB", "tobps", "toBps"}
	for _, op := range singleOps {
		for _, v := range singleArgVals {
			cases = append(cases, testCase{op: op, a: v})
			fmt.Fprintf(&input, "%s %d\n", op, v)
		}
	}

	// Values for two-arg operations.
	// For division ops (bytesToTime, bitsToTime, bytesToRate, bitsToRate),
	// the second arg must be non-zero.
	twoArgVals := make([]int64, 0, 100)
	for v := int64(-30); v <= 30; v++ {
		twoArgVals = append(twoArgVals, v)
	}
	for _, v := range []int64{-1000, -500, -100, 100, 500, 1000, 10000, -10000} {
		twoArgVals = append(twoArgVals, v)
	}

	mulOps := []string{"rateTimeToBytes", "rateTimeToBits"}
	for _, op := range mulOps {
		for _, a := range twoArgVals {
			for _, b := range twoArgVals {
				cases = append(cases, testCase{op: op, a: a, b: b})
				fmt.Fprintf(&input, "%s %d %d\n", op, a, b)
			}
		}
	}

	divOps := []string{"bytesToTime", "bitsToTime", "bytesToRate", "bitsToRate"}
	for _, op := range divOps {
		for _, a := range twoArgVals {
			for _, b := range twoArgVals {
				if b == 0 {
					continue
				}
				cases = append(cases, testCase{op: op, a: a, b: b})
				fmt.Fprintf(&input, "%s %d %d\n", op, a, b)
			}
		}
	}

	t.Logf("Generated %d units test vectors", len(cases))

	output := runDifftest(t, "units", input.String())
	lines := strings.Split(output, "\n")

	if len(lines) != len(cases) {
		t.Fatalf("expected %d output lines, got %d", len(cases), len(lines))
	}

	mismatches := 0
	for i, tc := range cases {
		var goResult int64
		switch tc.op {
		case "tob":
			goResult = tc.a * 8
		case "toB":
			goResult = ediv(tc.a, 8)
		case "tobps":
			goResult = tc.a * 8
		case "toBps":
			goResult = ediv(tc.a, 8)
		case "rateTimeToBytes":
			goResult = tc.a * tc.b
		case "rateTimeToBits":
			goResult = tc.a * tc.b
		case "bytesToTime":
			goResult = ediv(tc.a, tc.b)
		case "bitsToTime":
			goResult = ediv(tc.a, tc.b)
		case "bytesToRate":
			goResult = ediv(tc.a, tc.b)
		case "bitsToRate":
			goResult = ediv(tc.a, tc.b)
		}

		expected := fmt.Sprintf("%d", goResult)
		if lines[i] != expected {
			mismatches++
			t.Errorf("MISMATCH %s(%d, %d): Go=%s Lean=%s",
				tc.op, tc.a, tc.b, expected, lines[i])
			if mismatches >= 20 {
				t.Fatalf("too many mismatches (%d), stopping early", mismatches)
			}
		}
	}

	if mismatches == 0 {
		t.Logf("All %d units test vectors matched", len(cases))
	}
}
