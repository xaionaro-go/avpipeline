package kernel

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestE2ESyncPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sampleRate := 48000
	windowDuration := 100 * time.Millisecond
	windowSamples := int(float64(sampleRate) * windowDuration.Seconds())

	// 1. Setup AudioSync
	syncCfg := DefaultAudioSyncConfig()
	syncCfg.Syncer = &gccphat.Factory{
		WindowSize: windowSamples,
		HopSize:    windowSamples / 2,
		MaxLag:     windowSamples * 2,
	}
	syncCfg.SyncInterval = 0 // Sync every window
	syncCfg.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}
	syncKernel := NewAudioSync(ctx, syncCfg)

	// 2. Setup GapFiller
	gapCfg := DefaultGapFillerConfig()
	gapCfg.OverlapStrategyAudio = OverlapStrategyAudioDrop
	gapCfg.GapsStrategyAudio = GapsStrategyAudioAddSilence
	gapKernel := NewGapFiller(ctx, &gapCfg)

	// 3. Helper to push through pipeline
	finalOutputCh := make(chan packetorframe.OutputUnion, 1000)

	push := func(input packetorframe.InputUnion) {
		syncOutCh := make(chan packetorframe.OutputUnion, 10)
		err := syncKernel.SendInput(ctx, input, syncOutCh)
		testifyassert.NoError(t, err)

		for len(syncOutCh) > 0 {
			out := <-syncOutCh
			in := out.ToInput()
			err = gapKernel.SendInput(ctx, in, finalOutputCh)
			testifyassert.NoError(t, err)
		}
	}

	// 4. Generate common audio content (noise burst)
	burstLen := 20 * time.Millisecond
	burstSamples := int(float64(sampleRate) * burstLen.Seconds())
	commonContent := make([]float64, burstSamples)
	for i := range commonContent {
		commonContent[i] = rand.Float64()*2 - 1
	}

	sendFrames := func(streamIndex int, startPTS int64, numWindows int, contentOffset int, jitter time.Duration) {
		for i := 0; i < numWindows; i++ {
			pts := startPTS + int64(i)*int64(windowSamples)
			pts += int64(jitter.Seconds() * float64(sampleRate))

			samples := make([]float64, windowSamples)
			// Place content at some offset
			copyStart := windowSamples/4 + contentOffset
			for j := 0; j < burstSamples && (copyStart+j) < windowSamples; j++ {
				samples[copyStart+j] = commonContent[j]
			}

			frame := createTestAudioFrame(streamIndex, pts, sampleRate, samples)
			push(frame)
		}
	}

	fmt.Println("Phase 1: Perfect sync")
	for i := 0; i < 5; i++ {
		sendFrames(0, int64(i)*int64(windowSamples), 1, 0, 0)
		sendFrames(1, int64(i)*int64(windowSamples), 1, 0, 0)
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
	for i := 5; i < 25; i++ {
		sendFrames(0, int64(i)*int64(windowSamples), 1, 0, 0)
		sendFrames(1, int64(i)*int64(windowSamples), 1, 0, drift)
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

	// Total shift should be around -50ms in samples
	expectedShiftSamples := -int64(drift.Seconds() * float64(sampleRate))
	fmt.Printf("Detected offset: %v samples (expected around %v)\n", state.offset, expectedShiftSamples)

	testifyassert.InDelta(t, float64(expectedShiftSamples), float64(state.offset), float64(sampleRate/100))
}
