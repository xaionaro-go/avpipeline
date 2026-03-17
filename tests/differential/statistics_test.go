package differential

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xaionaro-go/avpipeline/types"
)

func TestDiffStatistics(t *testing.T) {
	var input strings.Builder
	type increment struct {
		leanName string
		goType   types.MediaType
		msgSize  uint64
	}
	type testCase struct {
		increments []increment
	}
	var cases []testCase

	// Build a set of (leanName, goMediaType) pairs for test generation.
	// Lean recognizes: video, audio, other, unknown.
	// Go routing: video→video, audio→audio, default→other.
	// Both Go and Lean route "other" and "unknown" to the other counter via Get/increment.
	type mediaMapping struct {
		leanName string
		goType   types.MediaType
	}
	mappings := []mediaMapping{
		{"video", types.MediaTypeVideo},
		{"audio", types.MediaTypeAudio},
		{"other", types.MediaTypeData},     // Go: any non-video/non-audio → other
		{"unknown", types.MediaTypeUnknown}, // Go: unknown → other via Get()
	}

	msgSizes := []uint64{0, 1, 8, 100, 1000, 65535, 1000000}

	// Single-increment tests: each media type with each message size.
	for _, m := range mappings {
		for _, sz := range msgSizes {
			tc := testCase{increments: []increment{{m.leanName, m.goType, sz}}}
			cases = append(cases, tc)
		}
	}

	// Multi-increment tests: sequences of 2-5 increments with mixed types.
	// Exhaustive over small sequences.
	for _, m1 := range mappings {
		for _, m2 := range mappings {
			for _, sz1 := range []uint64{0, 100, 65535} {
				for _, sz2 := range []uint64{0, 100, 65535} {
					tc := testCase{increments: []increment{
						{m1.leanName, m1.goType, sz1},
						{m2.leanName, m2.goType, sz2},
					}}
					cases = append(cases, tc)
				}
			}
		}
	}

	// Three-increment: exhaustive over media types, sampled sizes.
	for _, m1 := range mappings {
		for _, m2 := range mappings {
			for _, m3 := range mappings {
				for _, sz := range []uint64{0, 42, 1000} {
					tc := testCase{increments: []increment{
						{m1.leanName, m1.goType, sz},
						{m2.leanName, m2.goType, sz * 2},
						{m3.leanName, m3.goType, sz * 3},
					}}
					cases = append(cases, tc)
				}
			}
		}
	}

	// Longer sequences (4-8 increments) with cycling patterns.
	for _, length := range []int{4, 5, 6, 7, 8} {
		for startIdx := range mappings {
			for _, sz := range []uint64{1, 256, 50000} {
				var incs []increment
				for i := 0; i < length; i++ {
					m := mappings[(startIdx+i)%len(mappings)]
					incs = append(incs, increment{m.leanName, m.goType, sz * uint64(i+1)})
				}
				cases = append(cases, testCase{increments: incs})
			}
		}
	}

	// Stress test: many increments of a single type.
	for _, m := range mappings {
		var incs []increment
		for i := 0; i < 100; i++ {
			incs = append(incs, increment{m.leanName, m.goType, uint64(i * 10)})
		}
		cases = append(cases, testCase{increments: incs})
	}

	// Empty case (0 increments).
	cases = append(cases, testCase{increments: nil})

	// Build input lines.
	for _, tc := range cases {
		fmt.Fprintf(&input, "%d", len(tc.increments))
		for _, inc := range tc.increments {
			fmt.Fprintf(&input, " %s %d", inc.leanName, inc.msgSize)
		}
		input.WriteByte('\n')
	}

	t.Logf("Generated %d statistics test vectors", len(cases))

	output := runDifftest(t, "statistics", input.String())
	lines := strings.Split(output, "\n")

	if len(lines) != len(cases) {
		t.Fatalf("expected %d output lines, got %d", len(cases), len(lines))
	}

	mismatches := 0
	for i, tc := range cases {
		// Compute Go-side result using the real CountersSubSection.
		css := types.NewCountersSubSection()
		for _, inc := range tc.increments {
			css.Increment(inc.goType, inc.msgSize)
		}

		goResult := fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d",
			css.Video.Count.Load(), css.Video.Bytes.Load(),
			css.Audio.Count.Load(), css.Audio.Bytes.Load(),
			css.Other.Count.Load(), css.Other.Bytes.Load(),
			css.Unknown.Count.Load(), css.Unknown.Bytes.Load(),
			css.TotalCount(), css.TotalBytes(),
		)

		if lines[i] != goResult {
			mismatches++
			t.Errorf("MISMATCH case %d (n=%d): Go=%q Lean=%q",
				i, len(tc.increments), goResult, lines[i])
			if mismatches >= 20 {
				t.Fatalf("too many mismatches (%d), stopping early", mismatches)
			}
		}
	}

	if mismatches == 0 {
		t.Logf("All %d statistics test vectors matched", len(cases))
	}
}
