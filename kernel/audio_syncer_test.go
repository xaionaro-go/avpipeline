package kernel

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func createTestAudioFrame(streamIndex int, pts int64, sampleRate int, samples []float64) packetorframe.InputUnion {
	f := astiav.AllocFrame()
	f.SetNbSamples(len(samples))
	f.SetSampleRate(sampleRate)
	f.SetSampleFormat(astiav.SampleFormatFlt)
	f.SetChannelLayout(astiav.ChannelLayoutMono)
	if err := f.AllocBuffer(0); err != nil {
		panic(err)
	}

	b := make([]byte, f.NbSamples()*4)
	for i, v := range samples {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(float32(v)))
	}

	// Copy data to frame
	if err := f.Data().SetBytes(b, 1); err != nil {
		panic(err)
	}

	f.SetPts(pts)

	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetSampleRate(sampleRate)
	cp.SetChannelLayout(astiav.ChannelLayoutMono)

	return packetorframe.InputUnion{
		Frame: &frame.Input{
			Frame: f,
			StreamInfo: &frame.StreamInfo{
				StreamIndex:     streamIndex,
				TimeBase:        astiav.NewRational(1, sampleRate),
				CodecParameters: cp,
			},
		},
	}
}

func createSineBurst(sampleRate int, duration time.Duration, burstStart time.Duration, burstDuration time.Duration) []float64 {
	length := int(float64(sampleRate) * duration.Seconds())
	samples := make([]float64, length)
	startIdx := int(float64(sampleRate) * burstStart.Seconds())
	burstLen := int(float64(sampleRate) * burstDuration.Seconds())

	for i := 0; i < burstLen && (startIdx+i) < length; i++ {
		t := float64(i) / float64(sampleRate)
		samples[startIdx+i] = math.Sin(2 * math.Pi * 1000 * t)
	}
	return samples
}

func TestAudioSync_Basic(t *testing.T) {
	config := DefaultAudioSyncConfig()
	config.Syncer = &gccphat.Factory{WindowSize: 1024, HopSize: 512}
	config.SyncInterval = 10 * time.Millisecond
	config.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := NewAudioSync(ctx, config)

	// Create ref frame (Stream 0)
	refFrame := createTestAudioFrame(0, 0, 44100, make([]float64, 1024))

	outCh := make(chan packetorframe.OutputUnion, 10)
	err := k.SendInput(ctx, refFrame, outCh)
	testifyassert.NoError(t, err)

	// Create comp frame (Stream 1)
	compFrame := createTestAudioFrame(1, 0, 44100, make([]float64, 1024))
	err = k.SendInput(ctx, compFrame, outCh)
	testifyassert.NoError(t, err)

	testifyassert.Len(t, outCh, 2)
}

func TestAudioSync_Threshold(t *testing.T) {
	config := DefaultAudioSyncConfig()
	config.Syncer = &gccphat.Factory{WindowSize: 1024, HopSize: 512}
	config.SyncInterval = 0
	config.WindowSize = 200 * time.Millisecond
	config.OffsetThreshold = 50 * time.Millisecond
	config.ConsistencyDuration = 0
	config.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := NewAudioSync(ctx, config)
	outCh := make(chan packetorframe.OutputUnion, 100)

	sampleRate := 44100

	// 1. Small shift: 10ms (less than 50ms threshold)
	refSamples := createSineBurst(sampleRate, 200*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond)
	compSamples := createSineBurst(sampleRate, 200*time.Millisecond, 90*time.Millisecond, 10*time.Millisecond) // comp 10ms ahead

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	state := k.getStreamState(ctx, 1)
	testifyassert.Equal(t, int64(0), state.offset)

	// 2. Large shift: 60ms (more than 50ms threshold)
	k.locker.ManualLock(ctx)
	k.streamStates[0].syncerStream = nil
	k.streamStates[1].syncerStream = nil
	k.streamStates[0].lastSyncTime = time.Time{}
	k.streamStates[1].lastSyncTime = time.Time{}
	k.locker.ManualUnlock(ctx)

	refSamples = createSineBurst(sampleRate, 200*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond)
	compSamples = createSineBurst(sampleRate, 200*time.Millisecond, 40*time.Millisecond, 10*time.Millisecond)

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	testifyassert.True(t, state.offset > 0, "offset should be positive")
	testifyassert.InDelta(t, int64(2646), state.offset, 100)
}

func TestAudioSync_Consistency(t *testing.T) {
	config := DefaultAudioSyncConfig()
	config.Syncer = &gccphat.Factory{WindowSize: 1024, HopSize: 512}
	config.SyncInterval = 0
	config.WindowSize = 200 * time.Millisecond
	config.OffsetThreshold = 0
	config.ConsistencyDuration = 100 * time.Millisecond
	config.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := NewAudioSync(ctx, config)
	outCh := make(chan packetorframe.OutputUnion, 100)
	sampleRate := 44100

	refSamples := createSineBurst(sampleRate, 200*time.Millisecond, 100*time.Millisecond, 10*time.Millisecond)
	compSamples := createSineBurst(sampleRate, 200*time.Millisecond, 40*time.Millisecond, 10*time.Millisecond)

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	state := k.getStreamState(ctx, 1)
	testifyassert.Equal(t, int64(0), state.offset)
	testifyassert.False(t, state.directionStartTime.IsZero())

	time.Sleep(150 * time.Millisecond)

	k.locker.ManualLock(ctx)
	k.streamStates[0].syncerStream = nil
	k.streamStates[1].syncerStream = nil
	k.streamStates[0].lastSyncTime = time.Time{}
	k.streamStates[1].lastSyncTime = time.Time{}
	k.locker.ManualUnlock(ctx)

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	testifyassert.NotEqual(t, int64(0), state.offset)
}
