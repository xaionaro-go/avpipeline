package differential

import (
	"fmt"
	"strings"
	"testing"
)

// nodeState mirrors Lean's NodeState.
type nodeState struct {
	flushed bool
	drained bool
	blocked bool
}

func (n nodeState) String() string {
	return fmt.Sprintf("%s %s %s", boolStr(n.flushed), boolStr(n.drained), boolStr(n.blocked))
}

// goIsDrained mirrors Lean's Drain.isDrained: all nodes drained. Empty = true.
func goIsDrained(nodes []nodeState) bool {
	for _, n := range nodes {
		if !n.drained {
			return false
		}
	}
	return true
}

// goDrainNode mirrors Lean's Drain.drainNode: optionally set block, flush, set drained=true.
func goDrainNode(setBlock *bool, n nodeState) nodeState {
	if setBlock != nil {
		n.blocked = *setBlock
	}
	n.flushed = true
	n.drained = true
	return n
}

// goDrain mirrors Lean's Drain.drain: map drainNode over all nodes.
func goDrain(setBlock *bool, nodes []nodeState) []nodeState {
	result := make([]nodeState, len(nodes))
	for i, n := range nodes {
		result[i] = goDrainNode(setBlock, n)
	}
	return result
}

func nodesToInput(nodes []nodeState) string {
	parts := make([]string, 0, len(nodes)*3)
	for _, n := range nodes {
		parts = append(parts, boolStr(n.flushed), boolStr(n.drained), boolStr(n.blocked))
	}
	return strings.Join(parts, " ")
}

func nodesToOutput(nodes []nodeState) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = n.String()
	}
	return strings.Join(parts, " ")
}

// allNodeCombos generates all possible nodeState lists of a given length.
// Each node has 3 booleans = 8 states, so length n gives 8^n combos.
func allNodeCombos(length int) [][]nodeState {
	if length == 0 {
		return [][]nodeState{{}}
	}
	total := 1
	for i := 0; i < length; i++ {
		total *= 8
	}
	result := make([][]nodeState, total)
	for i := 0; i < total; i++ {
		nodes := make([]nodeState, length)
		v := i
		for j := 0; j < length; j++ {
			nodes[j] = nodeState{
				flushed: v&1 == 1,
				drained: (v>>1)&1 == 1,
				blocked: (v>>2)&1 == 1,
			}
			v >>= 3
		}
		result[i] = nodes
	}
	return result
}

func TestDiffDrain(t *testing.T) {
	var input strings.Builder
	var expected []string

	boolTrue := true
	boolFalse := false

	// isDrained: all combos for 0, 1, 2, 3 nodes
	for length := 0; length <= 3; length++ {
		for _, nodes := range allNodeCombos(length) {
			line := "isDrained " + nodesToInput(nodes)
			if length == 0 {
				line = "isDrained"
			}
			fmt.Fprintln(&input, line)
			expected = append(expected, boolStr(goIsDrained(nodes)))
		}
	}

	// drain: all combos for 0, 1, 2 nodes, with setBlock = none/true/false
	setBlockOptions := []*bool{nil, &boolTrue, &boolFalse}
	setBlockNames := []string{"none", "1", "0"}

	for sbIdx, sb := range setBlockOptions {
		sbStr := setBlockNames[sbIdx]
		for length := 0; length <= 2; length++ {
			for _, nodes := range allNodeCombos(length) {
				nodeStr := nodesToInput(nodes)
				line := "drain " + sbStr
				if nodeStr != "" {
					line += " " + nodeStr
				}
				fmt.Fprintln(&input, line)
				result := goDrain(sb, nodes)
				expected = append(expected, nodesToOutput(result))
			}
		}
	}

	// drain with 3 nodes: specific interesting cases (all combos would be 8^3*3 = 1536)
	for _, sb := range setBlockOptions {
		sbStr := setBlockNames[0]
		if sb != nil {
			if *sb {
				sbStr = "1"
			} else {
				sbStr = "0"
			}
		}
		interestingNodes := [][]nodeState{
			{{false, false, false}, {false, false, false}, {false, false, false}},
			{{true, true, true}, {true, true, true}, {true, true, true}},
			{{false, false, false}, {true, true, true}, {false, true, false}},
			{{true, false, true}, {false, true, false}, {true, false, false}},
		}
		for _, nodes := range interestingNodes {
			line := "drain " + sbStr + " " + nodesToInput(nodes)
			fmt.Fprintln(&input, line)
			result := goDrain(sb, nodes)
			expected = append(expected, nodesToOutput(result))
		}
	}

	t.Logf("Generated %d drain test vectors", len(expected))

	leanOutput := runDifftest(t, "drain", input.String())
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
		t.Logf("All %d drain test vectors matched between Go and Lean", len(expected))
	}
}
