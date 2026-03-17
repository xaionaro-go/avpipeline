package differential

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// bitrateConfig mirrors Lean's LimitBitrateConfig.
type bitrateConfig struct {
	averageBitRate      uint64
	averagingBufferBits uint64
}

// bitrateState mirrors Lean's LimitBitrateState.
type bitrateState struct {
	consumed     uint64
	skippedFrame bool
}

// inputKind mirrors Lean's InputKind.
type inputKind int

const (
	inputKindNonVideo    inputKind = 0
	inputKindVideoFrame  inputKind = 1
	inputKindVideoPacket inputKind = 2
)

// bitrateInput holds one test vector input line.
type bitrateInput struct {
	kind        inputKind
	sizeBits    uint64
	isKeyFrame  bool
	drainAmount uint64
}

// bitrateDrain reimplements Lean's LimitBitrateState.drain.
// Nat subtraction in Lean clamps to 0.
func bitrateDrain(s bitrateState, allowedBits uint64) bitrateState {
	consumed := s.consumed
	if allowedBits >= consumed {
		consumed = 0
	} else {
		consumed -= allowedBits
	}
	return bitrateState{consumed: consumed, skippedFrame: s.skippedFrame}
}

// bitrateProcessVideoPacket reimplements Lean's LimitBitrateState.processVideoPacket.
func bitrateProcessVideoPacket(
	s bitrateState,
	cfg bitrateConfig,
	sizeBits uint64,
	isKeyFrame bool,
) (bool, bitrateState) {
	consumedWithPacket := s.consumed + sizeBits
	overflows := consumedWithPacket > cfg.averagingBufferBits
	notKeyFrameOrBucketNonEmpty := !isKeyFrame || s.consumed != 0
	rejectOverflow := overflows && notKeyFrameOrBucketNonEmpty

	if rejectOverflow {
		return false, bitrateState{consumed: s.consumed, skippedFrame: true}
	}

	if s.skippedFrame && !isKeyFrame {
		return false, s
	}

	return true, bitrateState{consumed: consumedWithPacket, skippedFrame: false}
}

// bitrateStep reimplements Lean's LimitBitrateState.step.
func bitrateStep(
	s bitrateState,
	cfg bitrateConfig,
	inp bitrateInput,
) (bool, bitrateState) {
	if cfg.averageBitRate == 0 {
		return true, s
	}

	switch inp.kind {
	case inputKindNonVideo:
		return true, s
	case inputKindVideoFrame:
		return true, s
	default:
		drained := bitrateDrain(s, inp.drainAmount)
		return bitrateProcessVideoPacket(drained, cfg, inp.sizeBits, inp.isKeyFrame)
	}
}

// bitrateSequence holds a complete test sequence.
type bitrateSequence struct {
	name   string
	cfg    bitrateConfig
	inputs []bitrateInput
}

func generateBitrateSequences() []bitrateSequence {
	var seqs []bitrateSequence

	// Sequence 1: Normal bitrate limiting with small packets that all fit.
	{
		inputs := make([]bitrateInput, 80)
		for i := range inputs {
			inputs[i] = bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    8000, // 1KB
				isKeyFrame:  i%30 == 0,
				drainAmount: 10000, // drain faster than fill
			}
		}
		seqs = append(seqs, bitrateSequence{
			name:   "normal_low_bitrate",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 10000000},
			inputs: inputs,
		})
	}

	// Sequence 2: Overflow — large packets exceeding buffer.
	{
		inputs := make([]bitrateInput, 60)
		for i := range inputs {
			inputs[i] = bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    500000, // 62.5KB — large
				isKeyFrame:  i%10 == 0,
				drainAmount: 100, // slow drain
			}
		}
		seqs = append(seqs, bitrateSequence{
			name:   "overflow_large_packets",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 2000000},
			inputs: inputs,
		})
	}

	// Sequence 3: Keyframe exception — keyframe accepted even on overflow when bucket is empty.
	{
		inputs := []bitrateInput{
			// Start with a huge keyframe on empty bucket — should be accepted
			{kind: inputKindVideoPacket, sizeBits: 5000000, isKeyFrame: true, drainAmount: 0},
			// Non-keyframe after skip — should be rejected (skippedFrame is false here, bucket full)
			{kind: inputKindVideoPacket, sizeBits: 100000, isKeyFrame: false, drainAmount: 100},
			// Drain a lot, then another keyframe
			{kind: inputKindVideoPacket, sizeBits: 100000, isKeyFrame: true, drainAmount: 6000000},
			// Non-keyframe after accepted keyframe
			{kind: inputKindVideoPacket, sizeBits: 100000, isKeyFrame: false, drainAmount: 100},
		}
		seqs = append(seqs, bitrateSequence{
			name:   "keyframe_exception_empty_bucket",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 3000000},
			inputs: inputs,
		})
	}

	// Sequence 4: Skip streak — once a frame is skipped, only keyframes break out.
	{
		inputs := make([]bitrateInput, 50)
		for i := range inputs {
			size := uint64(200000)
			isKey := false
			drain := uint64(50)
			if i == 0 {
				isKey = true
			}
			// At frame 20, insert a keyframe to break the skip streak
			if i == 20 {
				isKey = true
				drain = 5000000 // large drain to empty bucket
			}
			inputs[i] = bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    size,
				isKeyFrame:  isKey,
				drainAmount: drain,
			}
		}
		seqs = append(seqs, bitrateSequence{
			name:   "skip_streak_keyframe_break",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 1000000},
			inputs: inputs,
		})
	}

	// Sequence 5: Mixed input kinds (non-video and video frames always pass).
	{
		inputs := make([]bitrateInput, 60)
		for i := range inputs {
			switch i % 3 {
			case 0:
				inputs[i] = bitrateInput{kind: inputKindNonVideo, sizeBits: 0, drainAmount: 0}
			case 1:
				inputs[i] = bitrateInput{kind: inputKindVideoFrame, sizeBits: 0, drainAmount: 0}
			default:
				inputs[i] = bitrateInput{
					kind:        inputKindVideoPacket,
					sizeBits:    80000,
					isKeyFrame:  i%15 == 0,
					drainAmount: 50000,
				}
			}
		}
		seqs = append(seqs, bitrateSequence{
			name:   "mixed_input_kinds",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 5000000},
			inputs: inputs,
		})
	}

	// Sequence 6: Zero bitrate (disabled — all pass).
	{
		inputs := make([]bitrateInput, 30)
		for i := range inputs {
			inputs[i] = bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    999999,
				isKeyFrame:  false,
				drainAmount: 0,
			}
		}
		seqs = append(seqs, bitrateSequence{
			name:   "zero_bitrate_disabled",
			cfg:    bitrateConfig{averageBitRate: 0, averagingBufferBits: 0},
			inputs: inputs,
		})
	}

	// Sequence 7: Gradual fill then drain cycle.
	{
		inputs := make([]bitrateInput, 100)
		for i := range inputs {
			drain := uint64(0)
			if i > 0 {
				// Simulate time-based drain: each step ~33ms at 1Mbps = ~33000 bits
				drain = 33000
			}
			inputs[i] = bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    40000, // slightly above drain rate
				isKeyFrame:  i%30 == 0,
				drainAmount: drain,
			}
		}
		seqs = append(seqs, bitrateSequence{
			name:   "gradual_fill_drain",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 500000},
			inputs: inputs,
		})
	}

	// Sequence 8: Burst then recovery.
	{
		var inputs []bitrateInput
		// 10 large frames (burst)
		for i := 0; i < 10; i++ {
			inputs = append(inputs, bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    300000,
				isKeyFrame:  i == 0,
				drainAmount: 1000,
			})
		}
		// 40 small frames with large drain (recovery)
		for i := 0; i < 40; i++ {
			inputs = append(inputs, bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    10000,
				isKeyFrame:  i%10 == 0,
				drainAmount: 100000,
			})
		}
		seqs = append(seqs, bitrateSequence{
			name:   "burst_then_recovery",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 2000000},
			inputs: inputs,
		})
	}

	// Sequence 9: All keyframes — no skip streak possible.
	{
		inputs := make([]bitrateInput, 50)
		for i := range inputs {
			inputs[i] = bitrateInput{
				kind:        inputKindVideoPacket,
				sizeBits:    100000,
				isKeyFrame:  true,
				drainAmount: 50000,
			}
		}
		seqs = append(seqs, bitrateSequence{
			name:   "all_keyframes",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 500000},
			inputs: inputs,
		})
	}

	// Sequence 10: Edge — exactly at buffer limit.
	{
		inputs := []bitrateInput{
			{kind: inputKindVideoPacket, sizeBits: 1000000, isKeyFrame: true, drainAmount: 0},
			// consumed = 1000000 = averagingBufferBits → not > → accepted
			{kind: inputKindVideoPacket, sizeBits: 1000000, isKeyFrame: false, drainAmount: 1000000},
			// After drain: consumed=0; consumed+1000000=1000000 = buffer → accepted
			{kind: inputKindVideoPacket, sizeBits: 1, isKeyFrame: false, drainAmount: 0},
			// consumed=1000000+1=1000001 > 1000000 → overflow, not keyframe → rejected
		}
		seqs = append(seqs, bitrateSequence{
			name:   "exact_buffer_boundary",
			cfg:    bitrateConfig{averageBitRate: 1000000, averagingBufferBits: 1000000},
			inputs: inputs,
		})
	}

	return seqs
}

func TestDiffLimitBitrate(t *testing.T) {
	sequences := generateBitrateSequences()
	totalVectors := 0
	totalMismatches := 0

	for _, seq := range sequences {
		t.Run(seq.name, func(t *testing.T) {
			// Build input for Lean binary
			var input strings.Builder
			fmt.Fprintf(&input, "%d %d\n", seq.cfg.averageBitRate, seq.cfg.averagingBufferBits)
			for _, inp := range seq.inputs {
				isKey := 0
				if inp.isKeyFrame {
					isKey = 1
				}
				fmt.Fprintf(&input, "%d %d %d %d\n", inp.kind, inp.sizeBits, isKey, inp.drainAmount)
			}

			// Run Lean
			leanOutput := runDifftest(t, "bitrate", input.String())
			leanLines := strings.Split(leanOutput, "\n")

			if len(leanLines) != len(seq.inputs) {
				t.Fatalf("expected %d output lines, got %d\noutput: %s",
					len(seq.inputs), len(leanLines), leanOutput)
			}

			// Run Go
			state := bitrateState{}
			mismatches := 0
			for i, inp := range seq.inputs {
				accepted, newState := bitrateStep(state, seq.cfg, inp)

				// Parse Lean output: "<accepted> <consumedBits>"
				parts := strings.Fields(leanLines[i])
				if len(parts) != 2 {
					t.Fatalf("input %d: expected 2 fields, got %d: %q", i, len(parts), leanLines[i])
				}

				leanAccepted, err := strconv.Atoi(parts[0])
				if err != nil {
					t.Fatalf("input %d: parse accepted: %v", i, err)
				}
				leanConsumed, err := strconv.ParseUint(parts[1], 10, 64)
				if err != nil {
					t.Fatalf("input %d: parse consumed: %v", i, err)
				}

				goAccepted := 0
				if accepted {
					goAccepted = 1
				}

				if goAccepted != leanAccepted || newState.consumed != leanConsumed {
					mismatches++
					t.Errorf("MISMATCH input %d (kind=%d size=%d key=%v drain=%d): "+
						"Go(acc=%d consumed=%d) vs Lean(acc=%d consumed=%d)",
						i, inp.kind, inp.sizeBits, inp.isKeyFrame, inp.drainAmount,
						goAccepted, newState.consumed, leanAccepted, leanConsumed)
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

	t.Logf("LimitBitrate: %d total test vectors across %d sequences, %d mismatches",
		totalVectors, len(sequences), totalMismatches)
}
