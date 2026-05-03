//go:build test_long

package kernel

import (
	"context"
	"math/rand"
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestAudioSync_LargeDelay(t *testing.T) {
	t.Parallel()
	config := DefaultAudioSyncConfig()
	config.Syncer = &gccphat.Factory{WindowSize: 16384, HopSize: 8192, MaxLag: 150000}
	config.ConfidenceThreshold = 0.1
	config.ConsistencyDuration = 0
	config.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := NewAudioSync(ctx, config)
	outCh := make(chan packetorframe.OutputUnion, 100)
	sampleRate := 44100

	// Create noise
	noise := make([]float64, sampleRate*5)
	r := rand.New(rand.NewSource(42))
	for i := range noise {
		noise[i] = r.Float64()*2 - 1
	}

	// Reference starts at PTS 0
	fRef := createTestAudioFrame(0, 0, sampleRate, noise)
	testifyassert.NoError(t, k.SendInput(ctx, fRef, outCh))

	// Comparison starts at PTS 3s (gap handled by fillGaps)
	fComp := createTestAudioFrame(1, int64(sampleRate*3), sampleRate, noise)
	testifyassert.NoError(t, k.SendInput(ctx, fComp, outCh))

	// Check if sync was detected
	state := k.getStreamState(ctx, 1)
	dataRef, err := fRef.Frame.Data().Bytes(1)
	if err != nil {
		panic(err)
	}
	dataComp, err := fComp.Frame.Data().Bytes(1)
	if err != nil {
		panic(err)
	}
	var nonZeroRef int
	for _, v := range dataRef {
		if v != 0 {
			nonZeroRef++
			break
		}
	}
	var nonZeroComp int
	for _, v := range dataComp {
		if v != 0 {
			nonZeroComp++
			break
		}
	}
	if nonZeroRef == 0 || nonZeroComp == 0 {
		panic("audio frame data is all zeros")
	}
	testifyassert.InDelta(t, -int64(sampleRate*3), state.offset, 1000)
}
