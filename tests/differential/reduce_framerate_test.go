package differential

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const difftestBin = "/home/claude/src/avpipeline/proofs/.lake/build/bin/difftest"

func runDifftest(t *testing.T, component string, input string) string {
	t.Helper()
	cmd := exec.Command(difftestBin, component)
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("difftest %s failed: %v\nstderr: %s", component, err, ee.Stderr)
		}
		t.Fatalf("difftest %s failed: %v", component, err)
	}
	return strings.TrimSpace(string(out))
}

// shouldPassGo mirrors the Bresenham-style acceptance criterion from
// reduce_framerate_fraction.go: (frameID % den) * num % den < num.
func shouldPassGo(num, den, frameID uint64) bool {
	if num == 0 {
		return false
	}
	r := frameID % den
	return (r*num)%den < num
}

func TestDiffReduceFramerate(t *testing.T) {
	var input strings.Builder
	type testCase struct {
		num, den, frameID uint64
	}
	var cases []testCase

	// Generate all (num, den, frameID) for den ∈ [1,30], num ∈ [0,den], frameID ∈ [0, 3*den).
	// The 3*den range tests multi-period behavior (periodicity).
	for den := uint64(1); den <= 30; den++ {
		for num := uint64(0); num <= den; num++ {
			for frameID := uint64(0); frameID < 3*den; frameID++ {
				fmt.Fprintf(&input, "%d %d %d\n", num, den, frameID)
				cases = append(cases, testCase{num, den, frameID})
			}
		}
	}

	t.Logf("Generated %d test vectors", len(cases))

	output := runDifftest(t, "reduceframerate", input.String())
	lines := strings.Split(output, "\n")

	if len(lines) != len(cases) {
		t.Fatalf("expected %d output lines, got %d", len(cases), len(lines))
	}

	mismatches := 0
	for i, tc := range cases {
		goResult := shouldPassGo(tc.num, tc.den, tc.frameID)
		leanVal, err := strconv.Atoi(lines[i])
		if err != nil {
			t.Fatalf("line %d: failed to parse Lean output %q: %v", i, lines[i], err)
		}
		leanResult := leanVal == 1

		if goResult != leanResult {
			mismatches++
			t.Errorf("MISMATCH num=%d den=%d frameID=%d: Go=%t Lean=%d",
				tc.num, tc.den, tc.frameID, goResult, leanVal)
			if mismatches >= 20 {
				t.Fatalf("too many mismatches (%d), stopping early", mismatches)
			}
		}
	}

	if mismatches == 0 {
		t.Logf("All %d test vectors matched between Go and Lean", len(cases))
	}
}
