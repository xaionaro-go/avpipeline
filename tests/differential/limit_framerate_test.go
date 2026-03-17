package differential

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// limitState mirrors Lean's LimitState: per-stream state for the framerate filter.
type limitState struct {
	minPTS       int64
	debt         int64
	durRemainder int64
}

// limitInput mirrors Lean's LimitState.Input.
type limitInput struct {
	pts int64
	dur int64
}

// limitMatchResult mirrors Lean's LimitState.MatchResult.
type limitMatchResult struct {
	accepted    bool
	newState    limitState
	assignedDur int64
}

// limitMatch reimplements the Lean spec's LimitState.match' as a pure function.
// minDurNum/minDurDen represent the rational minDuration (num/den).
// maxFPSNum is the numerator of maxFPS (0 means reject all).
func limitMatch(
	s limitState,
	inp limitInput,
	minDurNum, minDurDen int64,
	maxFPSNum int64,
) limitMatchResult {
	// Step 1: maxFPS.Num == 0 → reject all
	if maxFPSNum == 0 {
		return limitMatchResult{
			accepted:    false,
			newState:    s,
			assignedDur: inp.dur,
		}
	}

	// Step 2: large backward jump
	if inp.pts < s.minPTS-2 && s.minPTS >= 2 {
		return limitMatchResult{
			accepted:    false,
			newState:    s,
			assignedDur: inp.dur,
		}
	}

	// Step 3: debt tracking when pts < minPTS
	if inp.pts < s.minPTS {
		addDebt := s.minPTS - inp.pts
		totalDebt := s.debt + addDebt
		if totalDebt > 3 {
			return limitMatchResult{
				accepted:    false,
				newState:    limitState{minPTS: s.minPTS, debt: totalDebt, durRemainder: s.durRemainder},
				assignedDur: inp.dur,
			}
		}
	}

	// Accepted path (pts >= minPTS, or pts < minPTS with debt <= 3)
	newDebt := s.debt - (inp.pts - s.minPTS)
	debtStored := newDebt
	if newDebt <= 0 {
		debtStored = 0
	}

	effectivePTS := inp.pts
	if s.minPTS > effectivePTS {
		effectivePTS = s.minPTS
	}

	// Lean uses integer division (truncation toward zero for non-negative values).
	// Both effectivePTS * minDurDen and (curFrameID+1) * minDurNum are non-negative
	// when inputs are valid, so Go's integer division matches Lean's Int.div.
	curFrameID := effectivePTS * minDurDen / minDurNum
	nextMinPTS := (curFrameID + 1) * minDurNum / minDurDen

	num := minDurNum + s.durRemainder
	minDurationInt := num / minDurDen
	newDurRemainder := num % minDurDen

	assignedDur := inp.dur
	if inp.dur < minDurationInt+3 {
		assignedDur = minDurationInt
	}

	return limitMatchResult{
		accepted: true,
		newState: limitState{
			minPTS:       nextMinPTS,
			debt:         debtStored,
			durRemainder: newDurRemainder,
		},
		assignedDur: assignedDur,
	}
}

// limitFramerateSequence holds a test sequence config + frames.
type limitFramerateSequence struct {
	name                         string
	maxFPSNum, maxFPSDen         int64
	minDurNum, minDurDen         int64
	frames                       []limitInput
}

func generateLimitFramerateSequences() []limitFramerateSequence {
	var seqs []limitFramerateSequence

	// Helper: generate regularly-spaced frames.
	regularFrames := func(count int, startPTS, spacing int64) []limitInput {
		frames := make([]limitInput, count)
		for i := range frames {
			frames[i] = limitInput{
				pts: startPTS + int64(i)*spacing,
				dur: spacing,
			}
		}
		return frames
	}

	// Helper: generate frames with jitter around a regular spacing.
	jitteredFrames := func(count int, startPTS, spacing int64, jitterPattern []int64) []limitInput {
		frames := make([]limitInput, count)
		for i := range frames {
			jitter := jitterPattern[i%len(jitterPattern)]
			frames[i] = limitInput{
				pts: startPTS + int64(i)*spacing + jitter,
				dur: spacing,
			}
		}
		return frames
	}

	// Sequence 1: 30 fps regular (timeBase 1/30000 → minDuration = 30000/30 = 1000/1)
	// minDuration = timeBase.Reverse().Div(maxFPS) = (30000/1).Div(30/1) = (30000*1)/(1*30) = 30000/30 = 1000/1
	seqs = append(seqs, limitFramerateSequence{
		name:      "30fps_regular",
		maxFPSNum: 30, maxFPSDen: 1,
		minDurNum: 1000, minDurDen: 1,
		frames: regularFrames(80, 0, 1000),
	})

	// Sequence 2: 30 fps with slightly fast input (spacing 999 instead of 1000)
	seqs = append(seqs, limitFramerateSequence{
		name:      "30fps_fast_input",
		maxFPSNum: 30, maxFPSDen: 1,
		minDurNum: 1000, minDurDen: 1,
		frames: regularFrames(80, 0, 999),
	})

	// Sequence 3: 24000/1001 fps (NTSC). timeBase 1/90000.
	// minDuration = (90000/1).Div(24000/1001) = (90000*1001)/(1*24000) = 90090000/24000 = 3753.75 → 3753/1 ... no.
	// Actually the Rational stays as 90090000/24000 unreduced.
	seqs = append(seqs, limitFramerateSequence{
		name:      "24000_1001_ntsc",
		maxFPSNum: 24000, maxFPSDen: 1001,
		minDurNum: 90090000, minDurDen: 24000,
		frames: regularFrames(60, 0, 3754),
	})

	// Sequence 4: 1 fps. minDuration = 30000/1 (for timeBase 1/30000)
	seqs = append(seqs, limitFramerateSequence{
		name:      "1fps_slow",
		maxFPSNum: 1, maxFPSDen: 1,
		minDurNum: 30000, minDurDen: 1,
		frames: regularFrames(50, 0, 1000),
	})

	// Sequence 5: 60 fps. minDuration = 500/1 (for timeBase 1/30000)
	seqs = append(seqs, limitFramerateSequence{
		name:      "60fps_regular",
		maxFPSNum: 60, maxFPSDen: 1,
		minDurNum: 500, minDurDen: 1,
		frames: regularFrames(100, 0, 500),
	})

	// Sequence 6: 30 fps with jitter (rounding errors in encoder output)
	seqs = append(seqs, limitFramerateSequence{
		name:      "30fps_jittered",
		maxFPSNum: 30, maxFPSDen: 1,
		minDurNum: 1000, minDurDen: 1,
		frames: jitteredFrames(80, 0, 1000, []int64{0, -1, 1, -2, 2, 0, 1, -1}),
	})

	// Sequence 7: 30 fps with a backward PTS jump mid-stream
	backwardFrames := regularFrames(40, 0, 1000)
	// Insert a backward jump: after frame 39 at PTS=39000, jump back to PTS=10000
	backwardFrames = append(backwardFrames, regularFrames(40, 10000, 1000)...)
	seqs = append(seqs, limitFramerateSequence{
		name:      "30fps_backward_jump",
		maxFPSNum: 30, maxFPSDen: 1,
		minDurNum: 1000, minDurDen: 1,
		frames: backwardFrames,
	})

	// Sequence 8: maxFPS = 0 (reject all)
	seqs = append(seqs, limitFramerateSequence{
		name:      "zero_fps_reject_all",
		maxFPSNum: 0, maxFPSDen: 1,
		minDurNum: 1000, minDurDen: 1,
		frames: regularFrames(20, 0, 1000),
	})

	// Sequence 9: 60 fps limit on 120 fps input (every other frame rejected)
	seqs = append(seqs, limitFramerateSequence{
		name:      "60fps_limit_120fps_input",
		maxFPSNum: 60, maxFPSDen: 1,
		minDurNum: 500, minDurDen: 1,
		frames: regularFrames(100, 0, 250),
	})

	// Sequence 10: 30 fps with large jitter causing debt accumulation
	seqs = append(seqs, limitFramerateSequence{
		name:      "30fps_large_jitter",
		maxFPSNum: 30, maxFPSDen: 1,
		minDurNum: 1000, minDurDen: 1,
		frames: jitteredFrames(80, 0, 1000, []int64{0, -3, 3, -5, 5, -1, 1, -4}),
	})

	// Sequence 11: Non-unit denominator minDuration (fractional spacing).
	// maxFPS=25/1, timeBase 1/90000 → minDuration = 90000/25 = 3600/1
	seqs = append(seqs, limitFramerateSequence{
		name:      "25fps_regular",
		maxFPSNum: 25, maxFPSDen: 1,
		minDurNum: 3600, minDurDen: 1,
		frames: regularFrames(70, 0, 3600),
	})

	// Sequence 12: 30fps limit, input at exactly 30fps but starting at PTS=5000000
	seqs = append(seqs, limitFramerateSequence{
		name:      "30fps_high_start_pts",
		maxFPSNum: 30, maxFPSDen: 1,
		minDurNum: 1000, minDurDen: 1,
		frames: regularFrames(60, 5000000, 1000),
	})

	return seqs
}

func TestDiffLimitFramerate(t *testing.T) {
	sequences := generateLimitFramerateSequences()
	totalVectors := 0
	totalMismatches := 0

	for _, seq := range sequences {
		t.Run(seq.name, func(t *testing.T) {
			// Build input for Lean binary
			var input strings.Builder
			fmt.Fprintf(&input, "%d %d %d %d\n", seq.maxFPSNum, seq.maxFPSDen, seq.minDurNum, seq.minDurDen)
			for _, f := range seq.frames {
				fmt.Fprintf(&input, "%d %d\n", f.pts, f.dur)
			}

			// Run Lean
			leanOutput := runDifftest(t, "framerate", input.String())
			leanLines := strings.Split(leanOutput, "\n")

			if len(leanLines) != len(seq.frames) {
				t.Fatalf("expected %d output lines, got %d\noutput: %s", len(seq.frames), len(leanLines), leanOutput)
			}

			// Run Go
			state := limitState{}
			mismatches := 0
			for i, f := range seq.frames {
				result := limitMatch(state, f, seq.minDurNum, seq.minDurDen, seq.maxFPSNum)

				// Parse Lean output: "<accepted> <newMinPTS> <newDebt>"
				parts := strings.Fields(leanLines[i])
				if len(parts) != 3 {
					t.Fatalf("frame %d: expected 3 fields in Lean output, got %d: %q", i, len(parts), leanLines[i])
				}

				leanAccepted, err := strconv.Atoi(parts[0])
				if err != nil {
					t.Fatalf("frame %d: parse accepted: %v", i, err)
				}
				leanMinPTS, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					t.Fatalf("frame %d: parse minPTS: %v", i, err)
				}
				leanDebt, err := strconv.ParseInt(parts[2], 10, 64)
				if err != nil {
					t.Fatalf("frame %d: parse debt: %v", i, err)
				}

				goAccepted := int64(0)
				if result.accepted {
					goAccepted = 1
				}

				if goAccepted != int64(leanAccepted) ||
					result.newState.minPTS != leanMinPTS ||
					result.newState.debt != leanDebt {
					mismatches++
					t.Errorf("MISMATCH frame %d (pts=%d dur=%d): "+
						"Go(acc=%d minPTS=%d debt=%d) vs Lean(acc=%d minPTS=%d debt=%d)",
						i, f.pts, f.dur,
						goAccepted, result.newState.minPTS, result.newState.debt,
						leanAccepted, leanMinPTS, leanDebt)
					if mismatches >= 10 {
						t.Fatalf("too many mismatches (%d), stopping", mismatches)
					}
				}

				state = result.newState
				totalVectors++
			}
			totalMismatches += mismatches
		})
	}

	t.Logf("LimitFramerate: %d total test vectors across %d sequences, %d mismatches",
		totalVectors, len(sequences), totalMismatches)
}
