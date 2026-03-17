package differential

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// monotonicState mirrors Lean's MonotonicPTSState.
type monotonicState struct {
	latestPTS      int64
	sourcePTSShift map[uint64]int64
	shouldCorrect  bool
}

func monotonicInit(shouldCorrect bool) monotonicState {
	return monotonicState{
		latestPTS:      0,
		sourcePTSShift: make(map[uint64]int64),
		shouldCorrect:  shouldCorrect,
	}
}

// monotonicFrameInput mirrors Lean's MonotonicPTSState.FrameInput.
type monotonicFrameInput struct {
	pts         int64
	dts         int64
	streamIndex uint64
	sourceKey   uint64
	dtsIsNoPTS  bool
}

// monotonicMatchResult mirrors Lean's MonotonicPTSState.MatchResult.
type monotonicMatchResult struct {
	accepted    bool
	adjustedPTS int64 // only meaningful when accepted
}

func monotonicShiftedPTS(s monotonicState, inp monotonicFrameInput) int64 {
	return inp.pts + s.sourcePTSShift[inp.sourceKey]
}

func monotonicPtsLtDts(inp monotonicFrameInput) bool {
	return inp.pts < inp.dts && !inp.dtsIsNoPTS
}

func monotonicIsForward(s monotonicState, inp monotonicFrameInput, tolerance int64) bool {
	return monotonicShiftedPTS(s, inp)+tolerance > s.latestPTS
}

// monotonicMatch reimplements Lean's matchResult + matchNextState combined.
func monotonicMatch(
	s monotonicState,
	inp monotonicFrameInput,
	tolerance int64,
) (monotonicMatchResult, monotonicState) {
	// ptsLtDts check
	if monotonicPtsLtDts(inp) {
		return monotonicMatchResult{accepted: false}, s
	}

	// Non-first stream: accepted with shifted PTS, no state change
	if inp.streamIndex != 0 {
		return monotonicMatchResult{
			accepted:    true,
			adjustedPTS: monotonicShiftedPTS(s, inp),
		}, s
	}

	// Forward check
	if monotonicIsForward(s, inp, tolerance) {
		shPTS := monotonicShiftedPTS(s, inp)
		newState := monotonicState{
			latestPTS:      shPTS,
			sourcePTSShift: copyShiftMap(s.sourcePTSShift),
			shouldCorrect:  s.shouldCorrect,
		}
		return monotonicMatchResult{
			accepted:    true,
			adjustedPTS: shPTS,
		}, newState
	}

	// Not forward, no correction
	if !s.shouldCorrect {
		return monotonicMatchResult{accepted: false}, s
	}

	// Correction: use latestPTS + 1
	correctedPTS := s.latestPTS + 1
	newShift := copyShiftMap(s.sourcePTSShift)
	newShift[inp.sourceKey] = correctedPTS - inp.pts
	newState := monotonicState{
		latestPTS:      correctedPTS,
		sourcePTSShift: newShift,
		shouldCorrect:  s.shouldCorrect,
	}
	return monotonicMatchResult{
		accepted:    true,
		adjustedPTS: correctedPTS,
	}, newState
}

func copyShiftMap(m map[uint64]int64) map[uint64]int64 {
	out := make(map[uint64]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// monotonicTestInput holds one line's worth of input for differential testing.
type monotonicTestInput struct {
	frame         monotonicFrameInput
	shouldCorrect bool
	tolerance     int64
}

// monotonicSequence holds a complete test sequence.
type monotonicSequence struct {
	name   string
	inputs []monotonicTestInput
}

func generateMonotonicSequences() []monotonicSequence {
	var seqs []monotonicSequence

	// Sequence 1: Monotonically increasing PTS, single source, with correction.
	{
		inputs := make([]monotonicTestInput, 80)
		for i := range inputs {
			inputs[i] = monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 3000,
					dts:         int64(i)*3000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			}
		}
		seqs = append(seqs, monotonicSequence{name: "monotonic_increasing", inputs: inputs})
	}

	// Sequence 2: PTS goes backward — triggers correction.
	{
		var inputs []monotonicTestInput
		// First 20 frames: normal
		for i := 0; i < 20; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		// Jump backward
		for i := 0; i < 30; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000, // restarts from 0
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "backward_jump_corrected", inputs: inputs})
	}

	// Sequence 3: PTS goes backward, shouldCorrect=false — frames rejected.
	{
		var inputs []monotonicTestInput
		for i := 0; i < 20; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: false,
				tolerance:      10,
			})
		}
		// Jump backward — should be rejected without correction
		for i := 0; i < 20; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: false,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "backward_no_correction", inputs: inputs})
	}

	// Sequence 4: PTS < DTS (invalid) — always rejected.
	{
		var inputs []monotonicTestInput
		for i := 0; i < 30; i++ {
			pts := int64(i) * 1000
			dts := pts + 100 // dts > pts → rejected
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         pts,
					dts:         dts,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "pts_lt_dts_rejected", inputs: inputs})
	}

	// Sequence 5: Non-first stream (streamIndex != 0) — always accepted, no state change.
	{
		inputs := make([]monotonicTestInput, 50)
		for i := range inputs {
			// Even backward PTS on non-first stream should pass
			inputs[i] = monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(50-i) * 1000, // decreasing
					dts:         int64(50-i)*1000 - 1,
					streamIndex: 1,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			}
		}
		seqs = append(seqs, monotonicSequence{name: "non_first_stream", inputs: inputs})
	}

	// Sequence 6: Multiple sources with different shifts.
	{
		var inputs []monotonicTestInput
		// Source 1: normal forward
		for i := 0; i < 20; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		// Switch to source 2 with overlapping PTS (starts at 0 again)
		for i := 0; i < 30; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   2,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "multi_source_overlap", inputs: inputs})
	}

	// Sequence 7: Within tolerance — forward but close to latestPTS.
	{
		var inputs []monotonicTestInput
		// Large tolerance = 100
		for i := 0; i < 50; i++ {
			pts := int64(i) * 1000
			// Every 5th frame is slightly backward but within tolerance
			if i%5 == 4 && i > 0 {
				pts -= 50
			}
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         pts,
					dts:         pts - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      100,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "within_tolerance", inputs: inputs})
	}

	// Sequence 8: dtsIsNoPTS flag — PTS < DTS should NOT reject when dtsIsNoPTS.
	{
		var inputs []monotonicTestInput
		for i := 0; i < 30; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 + 100, // dts > pts
					streamIndex: 0,
					sourceKey:   1,
					dtsIsNoPTS:  true, // but dtsIsNoPTS → skip the reject check
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "dts_is_no_pts", inputs: inputs})
	}

	// Sequence 9: Interleaved streams — only stream 0 affects latestPTS.
	{
		var inputs []monotonicTestInput
		for i := 0; i < 60; i++ {
			streamIdx := uint64(i % 3)
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 500,
					dts:         int64(i)*500 - 1,
					streamIndex: streamIdx,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "interleaved_streams", inputs: inputs})
	}

	// Sequence 10: Correction chain — repeated backward jumps force repeated corrections.
	{
		var inputs []monotonicTestInput
		// Build up latestPTS to 10000
		for i := 0; i < 11; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		// Now repeatedly send PTS=0 (way backward) — each gets corrected to latestPTS+1
		for i := 0; i < 20; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         0,
					dts:         0,
					streamIndex: 0,
					sourceKey:   1,
					dtsIsNoPTS:  true,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "repeated_correction_chain", inputs: inputs})
	}

	// Sequence 11: Zero tolerance — strict monotonicity.
	{
		var inputs []monotonicTestInput
		for i := 0; i < 50; i++ {
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(i) * 1000,
					dts:         int64(i)*1000 - 1,
					streamIndex: 0,
					sourceKey:   1,
				},
				shouldCorrect: false,
				tolerance:      0,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "zero_tolerance", inputs: inputs})
	}

	// Sequence 12: Multiple sources alternating with backward PTS on each switch.
	{
		var inputs []monotonicTestInput
		for i := 0; i < 60; i++ {
			// Alternate between source 1 and source 2 every 10 frames
			sourceKey := uint64(1)
			if (i/10)%2 == 1 {
				sourceKey = 2
			}
			// Each source has its own PTS counter (restarting within its window)
			localIdx := i % 10
			inputs = append(inputs, monotonicTestInput{
				frame: monotonicFrameInput{
					pts:         int64(localIdx) * 1000,
					dts:         int64(localIdx)*1000 - 1,
					streamIndex: 0,
					sourceKey:   sourceKey,
				},
				shouldCorrect: true,
				tolerance:      10,
			})
		}
		seqs = append(seqs, monotonicSequence{name: "alternating_sources", inputs: inputs})
	}

	return seqs
}

func TestDiffMonotonicPTS(t *testing.T) {
	sequences := generateMonotonicSequences()
	totalVectors := 0
	totalMismatches := 0

	for _, seq := range sequences {
		t.Run(seq.name, func(t *testing.T) {
			// Build input for Lean binary
			var input strings.Builder
			for _, inp := range seq.inputs {
				shouldCorrect := 0
				if inp.shouldCorrect {
					shouldCorrect = 1
				}
				dtsIsNoPTS := 0
				if inp.frame.dtsIsNoPTS {
					dtsIsNoPTS = 1
				}
				fmt.Fprintf(&input, "%d %d %d %d %d %d %d\n",
					inp.frame.pts, inp.frame.dts,
					inp.frame.streamIndex, inp.frame.sourceKey,
					shouldCorrect, inp.tolerance,
					dtsIsNoPTS)
			}

			// Run Lean
			leanOutput := runDifftest(t, "monotonic", input.String())
			leanLines := strings.Split(leanOutput, "\n")

			if len(leanLines) != len(seq.inputs) {
				t.Fatalf("expected %d output lines, got %d\noutput: %s",
					len(seq.inputs), len(leanLines), leanOutput)
			}

			// Run Go — the Lean driver initializes with shouldCorrect=false, then
			// updates shouldCorrect from each input line before processing.
			state := monotonicInit(false)
			mismatches := 0
			for i, inp := range seq.inputs {
				state.shouldCorrect = inp.shouldCorrect
				result, newState := monotonicMatch(state, inp.frame, inp.tolerance)

				// Parse Lean output: "<accepted> <adjustedPTS> <latestPTS>"
				parts := strings.Fields(leanLines[i])
				if len(parts) != 3 {
					t.Fatalf("input %d: expected 3 fields, got %d: %q", i, len(parts), leanLines[i])
				}

				leanAccepted, err := strconv.Atoi(parts[0])
				if err != nil {
					t.Fatalf("input %d: parse accepted: %v", i, err)
				}
				leanAdjustedPTS, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					t.Fatalf("input %d: parse adjustedPTS: %v", i, err)
				}
				leanLatestPTS, err := strconv.ParseInt(parts[2], 10, 64)
				if err != nil {
					t.Fatalf("input %d: parse latestPTS: %v", i, err)
				}

				goAccepted := 0
				if result.accepted {
					goAccepted = 1
				}

				if goAccepted != leanAccepted ||
					result.adjustedPTS != leanAdjustedPTS ||
					newState.latestPTS != leanLatestPTS {
					mismatches++
					t.Errorf("MISMATCH input %d (pts=%d dts=%d stream=%d src=%d correct=%v tol=%d): "+
						"Go(acc=%d adjPTS=%d latPTS=%d) vs Lean(acc=%d adjPTS=%d latPTS=%d)",
						i, inp.frame.pts, inp.frame.dts, inp.frame.streamIndex,
						inp.frame.sourceKey, inp.shouldCorrect, inp.tolerance,
						goAccepted, result.adjustedPTS, newState.latestPTS,
						leanAccepted, leanAdjustedPTS, leanLatestPTS)
					if mismatches >= 10 {
						t.Fatalf("too many mismatches (%d), stopping", mismatches)
					}
				}

				state = newState
				totalVectors++
			}
			totalMismatches += mismatches
		})
	}

	t.Logf("MonotonicPTS: %d total test vectors across %d sequences, %d mismatches",
		totalVectors, len(sequences), totalMismatches)
}
