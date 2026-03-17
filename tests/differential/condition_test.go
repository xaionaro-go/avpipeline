package differential

import (
	"fmt"
	"strings"
	"testing"
)

// goAndMatch mirrors Lean's Condition.andMatch: all true. Empty = true.
func goAndMatch(bools []bool) bool {
	for _, b := range bools {
		if !b {
			return false
		}
	}
	return true
}

// goOrMatch mirrors Lean's Condition.orMatch: any true. Empty = false.
func goOrMatch(bools []bool) bool {
	for _, b := range bools {
		if b {
			return true
		}
	}
	return false
}

// goNotMatch mirrors Lean's Condition.notMatch: !(andMatch conds).
func goNotMatch(bools []bool) bool {
	return !goAndMatch(bools)
}

// goInSet mirrors Lean's Condition.inSet: target in set.
func goInSet(set []int, target int) bool {
	for _, v := range set {
		if v == target {
			return true
		}
	}
	return false
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func boolsToArgs(bs []bool) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = boolStr(b)
	}
	return strings.Join(parts, " ")
}

func TestDiffCondition(t *testing.T) {
	var input strings.Builder
	var expected []string

	// AND: all combinations up to length 4, plus empty
	for bits := 0; bits < (1 << 4); bits++ {
		for length := 0; length <= 4; length++ {
			if bits >= (1 << length) {
				continue
			}
			bools := make([]bool, length)
			for i := 0; i < length; i++ {
				bools[i] = (bits>>i)&1 == 1
			}
			line := "and " + boolsToArgs(bools)
			if length == 0 {
				line = "and"
			}
			fmt.Fprintln(&input, line)
			expected = append(expected, boolStr(goAndMatch(bools)))
		}
	}

	// OR: same combinations
	for bits := 0; bits < (1 << 4); bits++ {
		for length := 0; length <= 4; length++ {
			if bits >= (1 << length) {
				continue
			}
			bools := make([]bool, length)
			for i := 0; i < length; i++ {
				bools[i] = (bits>>i)&1 == 1
			}
			line := "or " + boolsToArgs(bools)
			if length == 0 {
				line = "or"
			}
			fmt.Fprintln(&input, line)
			expected = append(expected, boolStr(goOrMatch(bools)))
		}
	}

	// NOT_AND: same combinations
	for bits := 0; bits < (1 << 4); bits++ {
		for length := 0; length <= 4; length++ {
			if bits >= (1 << length) {
				continue
			}
			bools := make([]bool, length)
			for i := 0; i < length; i++ {
				bools[i] = (bits>>i)&1 == 1
			}
			line := "not_and " + boolsToArgs(bools)
			if length == 0 {
				line = "not_and"
			}
			fmt.Fprintln(&input, line)
			expected = append(expected, boolStr(goNotMatch(bools)))
		}
	}

	// IN: test target membership in various sets
	for target := 0; target <= 5; target++ {
		// Empty set
		fmt.Fprintf(&input, "in %d\n", target)
		expected = append(expected, boolStr(goInSet(nil, target)))

		// Singleton sets
		for elem := 0; elem <= 5; elem++ {
			fmt.Fprintf(&input, "in %d %d\n", target, elem)
			expected = append(expected, boolStr(goInSet([]int{elem}, target)))
		}

		// Multi-element sets
		sets := [][]int{
			{1, 2, 3},
			{0, 5},
			{2, 4},
			{0, 1, 2, 3, 4, 5},
		}
		for _, set := range sets {
			parts := make([]string, len(set))
			for i, v := range set {
				parts[i] = fmt.Sprintf("%d", v)
			}
			fmt.Fprintf(&input, "in %d %s\n", target, strings.Join(parts, " "))
			expected = append(expected, boolStr(goInSet(set, target)))
		}
	}

	// STATIC: both values
	for _, b := range []bool{false, true} {
		fmt.Fprintf(&input, "static %s\n", boolStr(b))
		expected = append(expected, boolStr(b))
	}

	t.Logf("Generated %d condition test vectors", len(expected))

	leanOutput := runDifftest(t, "condition", input.String())
	leanLines := strings.Split(leanOutput, "\n")

	if len(leanLines) != len(expected) {
		t.Fatalf("expected %d output lines, got %d", len(expected), len(leanLines))
	}

	mismatches := 0
	inputLines := strings.Split(strings.TrimSpace(input.String()), "\n")
	for i := range expected {
		if expected[i] != leanLines[i] {
			mismatches++
			t.Errorf("MISMATCH line %d: input=%q Go=%q Lean=%q",
				i, inputLines[i], expected[i], leanLines[i])
			if mismatches >= 20 {
				t.Fatalf("too many mismatches (%d), stopping early", mismatches)
			}
		}
	}

	if mismatches == 0 {
		t.Logf("All %d condition test vectors matched between Go and Lean", len(expected))
	}
}
