package differential

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	types "github.com/xaionaro-go/avpipeline/types"
)

func TestDiffRational(t *testing.T) {
	var input strings.Builder
	type testCase struct {
		op   string
		a, b types.Rational
	}
	var cases []testCase

	// Range for numerators and denominators.
	vals := make([]int, 0, 201)
	for v := -100; v <= 100; v++ {
		vals = append(vals, v)
	}

	// For mul and div: test pairs where both denominators are non-zero,
	// and for div the second rational must also have non-zero numerator.
	// Full cross-product [-100,100]^4 is 201^4 ≈ 1.6B — too large.
	// Use a representative subset: small range exhaustive + sampled large range.

	smallVals := make([]int, 0, 21)
	for v := -10; v <= 10; v++ {
		smallVals = append(smallVals, v)
	}

	// Exhaustive over small range for mul/div/reverse.
	for _, aN := range smallVals {
		for _, aD := range smallVals {
			if aD == 0 {
				continue
			}
			// reverse
			cases = append(cases, testCase{op: "reverse", a: types.Rational{Num: aN, Den: aD}})
			fmt.Fprintf(&input, "reverse %d %d\n", aN, aD)

			for _, bN := range smallVals {
				for _, bD := range smallVals {
					if bD == 0 {
						continue
					}
					// mul
					cases = append(cases, testCase{
						op: "mul",
						a:  types.Rational{Num: aN, Den: aD},
						b:  types.Rational{Num: bN, Den: bD},
					})
					fmt.Fprintf(&input, "mul %d %d %d %d\n", aN, aD, bN, bD)

					// div: second num must be non-zero
					if bN != 0 {
						cases = append(cases, testCase{
							op: "div",
							a:  types.Rational{Num: aN, Den: aD},
							b:  types.Rational{Num: bN, Den: bD},
						})
						fmt.Fprintf(&input, "div %d %d %d %d\n", aN, aD, bN, bD)
					}
				}
			}
		}
	}

	// Boundary cases with larger values.
	boundaryVals := []int{-100, -99, -50, -1, 0, 1, 50, 99, 100}
	for _, aN := range boundaryVals {
		for _, aD := range boundaryVals {
			if aD == 0 {
				continue
			}
			for _, bN := range boundaryVals {
				for _, bD := range boundaryVals {
					if bD == 0 {
						continue
					}
					cases = append(cases, testCase{
						op: "mul",
						a:  types.Rational{Num: aN, Den: aD},
						b:  types.Rational{Num: bN, Den: bD},
					})
					fmt.Fprintf(&input, "mul %d %d %d %d\n", aN, aD, bN, bD)

					if bN != 0 {
						cases = append(cases, testCase{
							op: "div",
							a:  types.Rational{Num: aN, Den: aD},
							b:  types.Rational{Num: bN, Den: bD},
						})
						fmt.Fprintf(&input, "div %d %d %d %d\n", aN, aD, bN, bD)
					}
				}
			}
		}
	}

	t.Logf("Generated %d test vectors", len(cases))

	output := runDifftest(t, "rational", input.String())
	lines := strings.Split(output, "\n")

	if len(lines) != len(cases) {
		t.Fatalf("expected %d output lines, got %d", len(cases), len(lines))
	}

	mismatches := 0
	for i, tc := range cases {
		parts := strings.Fields(lines[i])

		var goNum, goDen int
		switch tc.op {
		case "mul":
			r := tc.a.Mul(tc.b)
			goNum, goDen = r.Num, r.Den
		case "div":
			r := tc.a.Div(tc.b)
			goNum, goDen = r.Num, r.Den
		case "reverse":
			r := tc.a.Reverse()
			goNum, goDen = r.Num, r.Den
		}

		if len(parts) != 2 {
			t.Fatalf("line %d: expected 2 fields, got %d: %q", i, len(parts), lines[i])
		}
		leanNum, err := strconv.Atoi(parts[0])
		if err != nil {
			t.Fatalf("line %d: parse num %q: %v", i, parts[0], err)
		}
		leanDen, err := strconv.Atoi(parts[1])
		if err != nil {
			t.Fatalf("line %d: parse den %q: %v", i, parts[1], err)
		}

		if goNum != leanNum || goDen != leanDen {
			mismatches++
			t.Errorf("MISMATCH %s(%v, %v): Go=%d/%d Lean=%d/%d",
				tc.op, tc.a, tc.b, goNum, goDen, leanNum, leanDen)
			if mismatches >= 20 {
				t.Fatalf("too many mismatches (%d), stopping early", mismatches)
			}
		}
	}

	if mismatches == 0 {
		t.Logf("All %d test vectors matched between Go and Lean", len(cases))
	}
}
