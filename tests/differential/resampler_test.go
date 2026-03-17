package differential

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// goExpectedOutputSamples mirrors the Lean spec's expectedOutputSamples,
// which models resampler.go lines 213-229 for the case where
// inputSamples > 0 and inRate > 0 (the caller supplies the resolved inRate).
func goExpectedOutputSamples(inputSamples, inRate, outRate, chunkSize int) int {
	// ceilDiv(a, b) = (a + b - 1) / b for b > 0
	a := inputSamples * outRate
	outSamples := (a + inRate - 1) / inRate
	if outSamples < chunkSize {
		outSamples = chunkSize
	}
	return outSamples + chunkSize
}

func TestDiffResamplerExpectedOutputSamples(t *testing.T) {
	var input strings.Builder
	type testCase struct {
		inputSamples, inRate, outRate, chunkSize int
	}
	var cases []testCase

	// Common sample rates in audio processing.
	sampleRates := []int{8000, 11025, 16000, 22050, 32000, 44100, 48000, 88200, 96000}
	chunkSizes := []int{64, 128, 256, 512, 1024, 2048, 4096}
	inputSampleCounts := []int{1, 2, 10, 64, 128, 256, 441, 480, 512, 1024, 1152, 2048, 4096, 8192}

	for _, inRate := range sampleRates {
		for _, outRate := range sampleRates {
			for _, chunkSize := range chunkSizes {
				for _, inputSamples := range inputSampleCounts {
					cases = append(cases, testCase{inputSamples, inRate, outRate, chunkSize})
					fmt.Fprintf(&input, "expectedOutputSamples %d %d %d %d\n",
						inputSamples, inRate, outRate, chunkSize)
				}
			}
		}
	}

	// Edge cases: inRate == outRate (no resampling), small values.
	for inRate := 1; inRate <= 20; inRate++ {
		for outRate := 1; outRate <= 20; outRate++ {
			for inputSamples := 1; inputSamples <= 10; inputSamples++ {
				for _, chunkSize := range []int{1, 2, 5, 10} {
					cases = append(cases, testCase{inputSamples, inRate, outRate, chunkSize})
					fmt.Fprintf(&input, "expectedOutputSamples %d %d %d %d\n",
						inputSamples, inRate, outRate, chunkSize)
				}
			}
		}
	}

	t.Logf("Generated %d expectedOutputSamples test vectors", len(cases))

	output := runDifftest(t, "resampler", input.String())
	lines := strings.Split(output, "\n")

	if len(lines) != len(cases) {
		t.Fatalf("expected %d output lines, got %d", len(cases), len(lines))
	}

	mismatches := 0
	for i, tc := range cases {
		goResult := goExpectedOutputSamples(tc.inputSamples, tc.inRate, tc.outRate, tc.chunkSize)
		leanResult, err := strconv.Atoi(lines[i])
		if err != nil {
			t.Fatalf("line %d: parse Lean output %q: %v", i, lines[i], err)
		}
		if goResult != leanResult {
			mismatches++
			t.Errorf("MISMATCH expectedOutputSamples(%d, %d, %d, %d): Go=%d Lean=%d",
				tc.inputSamples, tc.inRate, tc.outRate, tc.chunkSize, goResult, leanResult)
			if mismatches >= 20 {
				t.Fatalf("too many mismatches (%d), stopping early", mismatches)
			}
		}
	}

	if mismatches == 0 {
		t.Logf("All %d expectedOutputSamples test vectors matched", len(cases))
	}
}

func TestDiffResamplerFifo(t *testing.T) {
	var input strings.Builder
	type testCase struct {
		writeCount, readCount int
	}
	var cases []testCase

	// Exhaustive for small values.
	for w := 0; w <= 50; w++ {
		for r := 0; r <= 50; r++ {
			cases = append(cases, testCase{w, r})
			fmt.Fprintf(&input, "fifo %d %d\n", w, r)
		}
	}

	// Larger values.
	largeVals := []int{100, 200, 500, 1000, 2000, 5000, 10000}
	for _, w := range largeVals {
		for _, r := range append(largeVals, 0, 1, w-1, w, w+1) {
			if r < 0 {
				continue
			}
			cases = append(cases, testCase{w, r})
			fmt.Fprintf(&input, "fifo %d %d\n", w, r)
		}
	}

	t.Logf("Generated %d FIFO test vectors", len(cases))

	output := runDifftest(t, "resampler", input.String())
	lines := strings.Split(output, "\n")

	if len(lines) != len(cases) {
		t.Fatalf("expected %d output lines, got %d", len(cases), len(lines))
	}

	mismatches := 0
	for i, tc := range cases {
		// Go FIFO model: write tc.writeCount, then read tc.readCount.
		sizeAfterWrite := tc.writeCount
		itemsRead := tc.readCount
		if itemsRead > sizeAfterWrite {
			itemsRead = sizeAfterWrite
		}
		remaining := sizeAfterWrite - itemsRead

		expected := fmt.Sprintf("%d %d %d", sizeAfterWrite, itemsRead, remaining)
		if lines[i] != expected {
			mismatches++
			t.Errorf("MISMATCH fifo(%d, %d): Go=%q Lean=%q",
				tc.writeCount, tc.readCount, expected, lines[i])
			if mismatches >= 20 {
				t.Fatalf("too many mismatches (%d), stopping early", mismatches)
			}
		}
	}

	if mismatches == 0 {
		t.Logf("All %d FIFO test vectors matched", len(cases))
	}
}
