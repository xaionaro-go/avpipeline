// smoothing_test.go exercises the Go updateWithInertialValue function and
// compares it against the Lean Smoothing spec.  (agent-generated test)
package difftestgo

import (
	"bufio"
	"fmt"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goUpdateWithInertialValue mirrors the Go implementation in stream_mux.go:1314.
// It takes integer inputs and returns uint64, matching the Go code exactly.
func goUpdateWithInertialValue(oldValue, newValue uint64, inertia float64, count uint64) uint64 {
	effectiveInertia := inertia * (float64(count) / float64(count+3))
	return uint64(float64(oldValue)*effectiveInertia + float64(newValue)*(1-effectiveInertia))
}

// smoothingTestVector holds one smoothing test case.
type smoothingTestVector struct {
	name       string
	oldValue   uint64
	newValue   uint64
	inertiaNum uint64 // e.g. 9 for 0.9
	inertiaDen uint64 // e.g. 10 for 0.9
	count      uint64
}

// goSmoothedResult returns the Go result for a smoothing vector.
func goSmoothedResult(tv smoothingTestVector) uint64 {
	inertia := float64(tv.inertiaNum) / float64(tv.inertiaDen)
	return goUpdateWithInertialValue(tv.oldValue, tv.newValue, inertia, tv.count)
}

// formatSmoothingInput produces the DiffTest protocol input line.
func formatSmoothingInput(tv smoothingTestVector) string {
	return fmt.Sprintf("smoothing %d %d %d %d %d",
		tv.oldValue, tv.newValue, tv.inertiaNum, tv.inertiaDen, tv.count)
}

// TestSmoothingBasic tests basic smoothing cases.
func TestSmoothingBasic(t *testing.T) {
	vectors := []smoothingTestVector{
		{"count_zero", 100, 200, 9, 10, 0},
		{"count_one", 100, 200, 9, 10, 1},
		{"count_large", 100, 200, 9, 10, 100},
		{"same_value", 500, 500, 9, 10, 50},
		{"inertia_zero", 100, 200, 0, 10, 10},
		{"inertia_full", 100, 200, 10, 10, 10},
		{"large_values", 50_000_000, 40_000_000, 9, 10, 20},
	}

	for _, tv := range vectors {
		t.Run(tv.name, func(t *testing.T) {
			goResult := goSmoothedResult(tv)
			t.Logf("Go result: %d", goResult)

			// Lean: smoothedNum / smoothedDen
			// smoothedNum = old * inertiaNum * count + new * (inertiaDen * (count+3) - inertiaNum * count)
			// smoothedDen = inertiaDen * (count+3)
			den := tv.inertiaDen * (tv.count + 3)
			weightOld := tv.inertiaNum * tv.count
			var weightNew uint64
			if den >= weightOld {
				weightNew = den - weightOld
			}
			num := tv.oldValue*weightOld + tv.newValue*weightNew
			var leanResult uint64
			if den > 0 {
				leanResult = num / den
			}
			t.Logf("Lean result: %d (num=%d, den=%d)", leanResult, num, den)

			// Allow difference of 1 due to float64 vs integer rounding.
			diff := int64(goResult) - int64(leanResult)
			if diff < 0 {
				diff = -diff
			}
			assert.LessOrEqual(t, diff, int64(1),
				"Go=%d, Lean=%d, diff=%d", goResult, leanResult, diff)
		})
	}
}

// TestSmoothingMassRandom runs 1000 random smoothing test vectors through
// both Go and Lean and diffs.  (agent-generated test)
func TestSmoothingMassRandom(t *testing.T) {
	difftestBin := findDifftestBin(t)

	rng := rand.New(rand.NewSource(43))
	const n = 1000

	vectors := make([]smoothingTestVector, n)
	goResults := make([]uint64, n)

	for i := range vectors {
		// Go always uses inertia < 1 (e.g. 0.9), so inertiaNum < inertiaDen.
		inertiaDen := uint64(rng.Int63n(10) + 1) // 1..10
		inertiaNum := uint64(rng.Int63n(int64(inertiaDen)))  // 0..inertiaDen-1
		vectors[i] = smoothingTestVector{
			name:       fmt.Sprintf("rand_%04d", i),
			oldValue:   uint64(rng.Int63n(100_000_000)),
			newValue:   uint64(rng.Int63n(100_000_000)),
			inertiaNum: inertiaNum,
			inertiaDen: inertiaDen,
			count:      uint64(rng.Int63n(200)),
		}
		goResults[i] = goSmoothedResult(vectors[i])
	}

	// Build Lean input.
	var leanInput strings.Builder
	for _, tv := range vectors {
		leanInput.WriteString(formatSmoothingInput(tv))
		leanInput.WriteString("\n")
	}

	// Run Lean.
	cmd := exec.Command(difftestBin, "streammux-smoothing")
	cmd.Stdin = strings.NewReader(leanInput.String())
	var leanOutput strings.Builder
	cmd.Stdout = &leanOutput
	var leanStderr strings.Builder
	cmd.Stderr = &leanStderr
	err := cmd.Run()
	if err != nil {
		t.Logf("Lean stderr: %s", leanStderr.String())
	}
	require.NoError(t, err, "Lean difftest failed")

	// Parse Lean results.
	scanner := bufio.NewScanner(strings.NewReader(leanOutput.String()))
	leanResults := make([]uint64, 0, n)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "lean_smoothing") {
			continue
		}
		parts := strings.Fields(line)
		require.Len(t, parts, 4, "expected 4 fields in lean_smoothing line")
		result, parseErr := strconv.ParseUint(parts[3], 10, 64)
		require.NoError(t, parseErr)
		leanResults = append(leanResults, result)
	}
	require.Equal(t, n, len(leanResults), "expected %d Lean results", n)

	// Diff.
	divergences := 0
	significantDivergences := 0
	for i := 0; i < n; i++ {
		diff := int64(goResults[i]) - int64(leanResults[i])
		if diff < 0 {
			diff = -diff
		}
		if diff != 0 {
			divergences++
			if diff > 1 {
				significantDivergences++
				t.Logf("SIGNIFICANT DIVERGENCE [%s]: Go=%d, Lean=%d, diff=%d",
					vectors[i].name, goResults[i], leanResults[i], diff)
				t.Logf("  input: %s", formatSmoothingInput(vectors[i]))
			}
		}
	}

	t.Logf("Total: %d/%d divergences, %d significant (|diff|>1)", divergences, n, significantDivergences)
	assert.Zero(t, significantDivergences,
		"found %d significant divergences between Go and Lean smoothing", significantDivergences)
}

// TestFPSPackRoundtrip tests FPS fraction packing roundtrip.
func TestFPSPackRoundtrip(t *testing.T) {
	difftestBin := findDifftestBin(t)

	rng := rand.New(rand.NewSource(44))
	const n = 100

	type fpsTestVector struct {
		num, den uint64
	}
	vectors := make([]fpsTestVector, n)
	for i := range vectors {
		vectors[i] = fpsTestVector{
			num: uint64(rng.Int63n(1 << 32)),
			den: uint64(rng.Int63n(1 << 32)),
		}
	}

	// Build Lean input.
	var leanInput strings.Builder
	for _, tv := range vectors {
		fmt.Fprintf(&leanInput, "packfps %d %d\n", tv.num, tv.den)
	}

	// Run Lean.
	cmd := exec.Command(difftestBin, "streammux-smoothing")
	cmd.Stdin = strings.NewReader(leanInput.String())
	var leanOutput strings.Builder
	cmd.Stdout = &leanOutput
	err := cmd.Run()
	require.NoError(t, err)

	// Parse and verify roundtrip.
	scanner := bufio.NewScanner(strings.NewReader(leanOutput.String()))
	idx := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "lean_packfps") {
			continue
		}
		parts := strings.Fields(line)
		require.Len(t, parts, 4)
		unum, _ := strconv.ParseUint(parts[2], 10, 64)
		uden, _ := strconv.ParseUint(parts[3], 10, 64)
		assert.Equal(t, vectors[idx].num, unum, "num roundtrip failed for idx %d", idx)
		assert.Equal(t, vectors[idx].den, uden, "den roundtrip failed for idx %d", idx)
		idx++
	}
	assert.Equal(t, n, idx, "expected %d roundtrip results", n)
}
