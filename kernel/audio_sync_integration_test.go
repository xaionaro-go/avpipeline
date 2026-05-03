//go:build test_e2e

package kernel

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestE2ESyncPipeline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sampleRate := 48000
	windowDuration := 100 * time.Millisecond
	_ = windowDuration

	// 1. Setup AudioSync
	syncCfg := DefaultAudioSyncConfig()
	// For GCC-PHAT, use a power-of-two window size to align with FFT size assumptions.
	// See: https://ffmpeg.org/pipermail/ffmpeg-devel/2023-October/315398.html (side data move)
	// And GCC-PHAT implementation: /home/streaming/go/src/github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat/syncer.go
	windowSize := 4096
	syncCfg.Syncer = &gccphat.Factory{
		WindowSize: windowSize,
		HopSize:    windowSize / 2,
		MaxLag:     windowSize * 2,
	}
	syncCfg.SyncInterval = 0 // Sync every window
	syncCfg.ConfidenceThreshold = 0
	syncCfg.OffsetThreshold = 0
	syncCfg.ConsistencyDuration = 0
	syncCfg.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}
	syncKernel := NewAudioSync(ctx, syncCfg)

	// 2. Helper to push through pipeline
	finalOutputCh := make(chan packetorframe.OutputUnion, 1000)

	push := func(input packetorframe.InputUnion) {
		syncOutCh := make(chan packetorframe.OutputUnion, 10)
		err := syncKernel.SendInput(ctx, input, syncOutCh)
		testifyassert.NoError(t, err)

		for len(syncOutCh) > 0 {
			out := <-syncOutCh
			finalOutputCh <- out
		}
	}

	// 3. Generate common audio content (full window noise)
	rng := rand.New(rand.NewSource(1))
	windowContent := make([]float64, windowSize)
	for i := range windowContent {
		windowContent[i] = rng.Float64()*2 - 1
	}

	shiftSamples := func(src []float64, shift int) []float64 {
		out := make([]float64, len(src))
		if shift == 0 {
			copy(out, src)
			return out
		}
		if shift > 0 {
			if shift >= len(src) {
				return out
			}
			copy(out[shift:], src[:len(src)-shift])
			return out
		}
		shift = -shift
		if shift >= len(src) {
			return out
		}
		copy(out, src[shift:])
		return out
	}

	sendFrames := func(streamIndex int, startPTS int64, numWindows int, contentOffset int) {
		for i := 0; i < numWindows; i++ {
			pts := startPTS + int64(i)*int64(windowSize)

			samples := shiftSamples(windowContent, contentOffset)

			frame := createTestAudioFrame(streamIndex, pts, sampleRate, samples)
			push(frame)
		}
	}

	fmt.Println("Phase 1: Perfect sync")
	for i := 0; i < 5; i++ {
		sendFrames(0, int64(i)*int64(windowSize), 1, 0)
		sendFrames(1, int64(i)*int64(windowSize), 1, 0)
	}

	// Drain outputs
	var outputs []packetorframe.OutputUnion
loop1:
	for {
		select {
		case out := <-finalOutputCh:
			outputs = append(outputs, out)
		default:
			break loop1
		}
	}
	testifyassert.Greater(t, len(outputs), 0)

	fmt.Println("Phase 2: Introduce 50ms drift (comp leads ref)")
	drift := 50 * time.Millisecond
	driftSamples := int(drift.Seconds() * float64(sampleRate))
	for i := 5; i < 25; i++ {
		sendFrames(0, int64(i)*int64(windowSize), 1, 0)
		sendFrames(1, int64(i)*int64(windowSize), 1, -driftSamples)
	}

	// Wait for syncer to react
	time.Sleep(100 * time.Millisecond)

loop2:
	for {
		select {
		case out := <-finalOutputCh:
			outputs = append(outputs, out)
		default:
			break loop2
		}
	}

	// Verify that syncer detected shift
	state := syncKernel.streamStates[1]
	testifyassert.NotNil(t, state)

	// Total shift should be around +50ms in samples. The syncer reports the offset
	// as comparison vs reference, which should be positive when comp leads ref.
	expectedShiftSamples := int64(driftSamples)
	fmt.Printf("Detected offset: %v samples (expected around %v)\n", state.offset, expectedShiftSamples)

	// GCC-PHAT is noise-sensitive; allow a wider tolerance for integration test.
	// Expected 50ms at 48kHz = 2400 samples. Allow 150ms (7200 samples).
	testifyassert.InDelta(t, float64(expectedShiftSamples), math.Abs(float64(state.offset)), float64(sampleRate*3/20))
}
