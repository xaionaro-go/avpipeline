package differential

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// graph is an adjacency list: nodeID -> list of neighbor IDs.
// Mirrors Spec.Graph: List (Nat × List Nat).
type graph []graphNode

type graphNode struct {
	id        uint
	neighbors []uint
}

// graphNeighbors returns the neighbors of node n in graph g.
// First match wins, matching Lean's Graph.neighbors.
func graphNeighbors(g graph, n uint) []uint {
	for _, node := range g {
		if node.id == n {
			return node.neighbors
		}
	}
	return nil
}

// graphNextLayer returns deduplicated neighbors for a single node.
// Mirrors Lean's Graph.nextLayer: fold with contains-check accumulator.
func graphNextLayer(g graph, n uint) []uint {
	ns := graphNeighbors(g, n)
	var acc []uint
	for _, x := range ns {
		if !uintSliceContains(acc, x) {
			acc = append(acc, x)
		}
	}
	return acc
}

// graphDFS mirrors Lean's Graph.dfs: DFS with visited set and fuel.
// For each root: skip if visited, mark visited, recurse into nextLayer children,
// then continue with remaining roots.
func graphDFS(g graph, fuel uint, visited []uint, roots []uint) []uint {
	if fuel == 0 {
		return visited
	}
	if len(roots) == 0 {
		return visited
	}

	n := roots[0]
	rest := roots[1:]

	if uintSliceContains(visited, n) {
		return graphDFS(g, fuel-1, visited, rest)
	}

	visited = append(visited, n)
	children := graphNextLayer(g, n)
	visited = graphDFS(g, fuel-1, visited, children)
	return graphDFS(g, fuel-1, visited, rest)
}

// graphTraverse mirrors Lean's Graph.traverse: DFS from roots with empty visited set.
func graphTraverse(g graph, roots []uint) []uint {
	fuel := uint(len(g)*2 + 10)
	return graphDFS(g, fuel, nil, roots)
}

func uintSliceContains(s []uint, v uint) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// formatGraphInput builds the difftest stdin line for "traverse" command.
// Format: traverse <num_nodes> <id1> <num_neighbors> <n1> ... | <root1> <root2> ...
func formatGraphInput(g graph, roots []uint) string {
	var parts []string
	parts = append(parts, "traverse", fmt.Sprintf("%d", len(g)))
	for _, node := range g {
		parts = append(parts, fmt.Sprintf("%d", node.id), fmt.Sprintf("%d", len(node.neighbors)))
		for _, nb := range node.neighbors {
			parts = append(parts, fmt.Sprintf("%d", nb))
		}
	}
	parts = append(parts, "|")
	for _, r := range roots {
		parts = append(parts, fmt.Sprintf("%d", r))
	}
	return strings.Join(parts, " ")
}

// formatUintSlice formats a uint slice as space-separated string (matching Lean output).
func formatUintSlice(xs []uint) string {
	if len(xs) == 0 {
		return ""
	}
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%d", x)
	}
	return strings.Join(parts, " ")
}

func TestDiffGraph(t *testing.T) {
	type testCase struct {
		name  string
		graph graph
		roots []uint
	}

	cases := []testCase{
		{
			name:  "empty graph, no roots",
			graph: graph{},
			roots: nil,
		},
		{
			name:  "single node, no edges, root is the node",
			graph: graph{{id: 0, neighbors: nil}},
			roots: []uint{0},
		},
		{
			name:  "single node, self-loop",
			graph: graph{{id: 0, neighbors: []uint{0}}},
			roots: []uint{0},
		},
		{
			name: "linear chain 0->1->2->3",
			graph: graph{
				{id: 0, neighbors: []uint{1}},
				{id: 1, neighbors: []uint{2}},
				{id: 2, neighbors: []uint{3}},
				{id: 3, neighbors: nil},
			},
			roots: []uint{0},
		},
		{
			name: "linear chain, root at middle",
			graph: graph{
				{id: 0, neighbors: []uint{1}},
				{id: 1, neighbors: []uint{2}},
				{id: 2, neighbors: []uint{3}},
				{id: 3, neighbors: nil},
			},
			roots: []uint{2},
		},
		{
			name: "binary tree depth 2",
			graph: graph{
				{id: 0, neighbors: []uint{1, 2}},
				{id: 1, neighbors: []uint{3, 4}},
				{id: 2, neighbors: []uint{5, 6}},
				{id: 3, neighbors: nil},
				{id: 4, neighbors: nil},
				{id: 5, neighbors: nil},
				{id: 6, neighbors: nil},
			},
			roots: []uint{0},
		},
		{
			name: "DAG with shared child",
			graph: graph{
				{id: 0, neighbors: []uint{1, 2}},
				{id: 1, neighbors: []uint{3}},
				{id: 2, neighbors: []uint{3}},
				{id: 3, neighbors: nil},
			},
			roots: []uint{0},
		},
		{
			name: "cycle: 0->1->2->3->0",
			graph: graph{
				{id: 0, neighbors: []uint{1}},
				{id: 1, neighbors: []uint{2}},
				{id: 2, neighbors: []uint{3}},
				{id: 3, neighbors: []uint{0}},
			},
			roots: []uint{0},
		},
		{
			name: "disconnected components, multiple roots",
			graph: graph{
				{id: 0, neighbors: []uint{1}},
				{id: 1, neighbors: nil},
				{id: 2, neighbors: []uint{3}},
				{id: 3, neighbors: nil},
			},
			roots: []uint{0, 2},
		},
		{
			name: "disconnected, only first component reachable",
			graph: graph{
				{id: 0, neighbors: []uint{1}},
				{id: 1, neighbors: nil},
				{id: 2, neighbors: nil},
				{id: 3, neighbors: nil},
			},
			roots: []uint{0},
		},
		{
			name: "diamond DAG",
			graph: graph{
				{id: 0, neighbors: []uint{1, 2}},
				{id: 1, neighbors: []uint{3}},
				{id: 2, neighbors: []uint{3}},
				{id: 3, neighbors: nil},
			},
			roots: []uint{0},
		},
		{
			name: "node with duplicate neighbors",
			graph: graph{
				{id: 0, neighbors: []uint{1, 1, 2}},
				{id: 1, neighbors: nil},
				{id: 2, neighbors: nil},
			},
			roots: []uint{0},
		},
		{
			name: "multiple roots, some overlap",
			graph: graph{
				{id: 0, neighbors: []uint{2}},
				{id: 1, neighbors: []uint{2}},
				{id: 2, neighbors: nil},
			},
			roots: []uint{0, 1},
		},
		{
			name: "star graph",
			graph: graph{
				{id: 0, neighbors: []uint{1, 2, 3, 4}},
				{id: 1, neighbors: nil},
				{id: 2, neighbors: nil},
				{id: 3, neighbors: nil},
				{id: 4, neighbors: nil},
			},
			roots: []uint{0},
		},
		{
			name: "reverse star (all point to center)",
			graph: graph{
				{id: 0, neighbors: nil},
				{id: 1, neighbors: []uint{0}},
				{id: 2, neighbors: []uint{0}},
				{id: 3, neighbors: []uint{0}},
			},
			roots: []uint{1, 2, 3},
		},
		{
			name: "two-node mutual cycle",
			graph: graph{
				{id: 0, neighbors: []uint{1}},
				{id: 1, neighbors: []uint{0}},
			},
			roots: []uint{0},
		},
		{
			name: "root not in graph",
			graph: graph{
				{id: 0, neighbors: []uint{1}},
				{id: 1, neighbors: nil},
			},
			roots: []uint{5},
		},
		{
			name: "complex DAG with multiple paths",
			graph: graph{
				{id: 0, neighbors: []uint{1, 2}},
				{id: 1, neighbors: []uint{3, 4}},
				{id: 2, neighbors: []uint{4, 5}},
				{id: 3, neighbors: []uint{6}},
				{id: 4, neighbors: []uint{6}},
				{id: 5, neighbors: []uint{6}},
				{id: 6, neighbors: nil},
			},
			roots: []uint{0},
		},
	}

	mismatches := 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goResult := formatUintSlice(graphTraverse(tc.graph, tc.roots))
			input := formatGraphInput(tc.graph, tc.roots)
			leanResult := runDifftest(t, "graph", input+"\n")

			if !assert.Equal(t, goResult, leanResult,
				"Go vs Lean mismatch for %s\ninput: %s", tc.name, input) {
				mismatches++
			}
		})
	}

	require.Zero(t, mismatches, "%d mismatches found", mismatches)
	t.Logf("All %d graph test vectors matched between Go and Lean", len(cases))
}
