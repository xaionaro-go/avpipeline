package kernel

import (
	"context"
	"encoding/binary"
	"math/rand"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/audio/pkg/syncerstream"
	"github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func createTestAudioFrame(streamIndex int, pts int64, sampleRate int, samples []float64) packetorframe.InputUnion {
	f := astiav.AllocFrame()
	f.SetNbSamples(len(samples))
	f.SetSampleRate(sampleRate)
	f.SetSampleFormat(astiav.SampleFormatS16)
	f.SetChannelLayout(astiav.ChannelLayoutMono)
	f.SetPts(pts)
	f.SetDuration(int64(len(samples)))
	if err := f.AllocBuffer(1); err != nil {
		panic(err)
	}

	bufferSize, err := f.SamplesBufferSize(1)
	if err != nil {
		panic(err)
	}
	if bufferSize == 0 {
		panic("invalid audio buffer size")
	}
	b := make([]byte, bufferSize)
	for i, v := range samples {
		idx := i * 2
		if idx+1 >= len(b) {
			break
		}
		binary.LittleEndian.PutUint16(b[idx:], uint16(int16(v*32767)))
	}

	if err := f.Data().SetBytes(b, 1); err != nil {
		// If SetBytes fails, try to fallback to direct copy if possible
		// but usually it means the frame was not allocated correctly.
		panic(err)
	}

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

func createNoiseBurst(sampleRate int, duration time.Duration, burstStart time.Duration, burstDuration time.Duration) []float64 {
	length := int(float64(sampleRate) * duration.Seconds())
	samples := make([]float64, length)
	startIdx := int(float64(sampleRate) * burstStart.Seconds())
	burstLen := int(float64(sampleRate) * burstDuration.Seconds())

	for i := 0; i < burstLen && (startIdx+i) < length; i++ {
		samples[startIdx+i] = rand.Float64()*2 - 1
	}
	return samples
}

func TestAudioSync_Basic(t *testing.T) {
	config := DefaultAudioSyncConfig()
	config.Syncer = &gccphat.Factory{WindowSize: 1024, HopSize: 512, MaxLag: 1024}
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
	config.Syncer = &gccphat.Factory{WindowSize: 4096, HopSize: 2048, MaxLag: 8192}
	config.SyncInterval = 0
	config.WindowSize = 200 * time.Millisecond
	config.OffsetThreshold = 50 * time.Millisecond
	config.ConfidenceThreshold = 0.05 // Lowered for noise in zero-padded window
	config.ConsistencyDuration = 0
	config.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := NewAudioSync(ctx, config)
	outCh := make(chan packetorframe.OutputUnion, 100)

	sampleRate := 44100
	baseSignal := createNoiseBurst(sampleRate, 1*time.Second, 0, 1*time.Second)

	// 1. Small shift: 10ms (less than 50ms threshold)
	refSamples := baseSignal[int(0.2*float64(sampleRate)):int(0.7*float64(sampleRate))]
	compSamples := baseSignal[int(0.19*float64(sampleRate)):int(0.69*float64(sampleRate))] // comp 10ms ahead

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	state := k.getStreamState(ctx, 1)
	testifyassert.Equal(t, int64(0), state.offset)

	// 2. Large shift: 60ms (more than 50ms threshold)
	k.locker.ManualLock(ctx)
	k.syncers = make(map[int]syncerstream.SyncerStream)
	if s0, ok := k.streamStates[0]; ok {
		s0.lastSyncTime = time.Time{}
	}
	if s1, ok := k.streamStates[1]; ok {
		s1.lastSyncTime = time.Time{}
	}
	k.locker.ManualUnlock(ctx)

	refSamples = baseSignal[int(0.2*float64(sampleRate)):int(0.7*float64(sampleRate))]
	compSamples = baseSignal[int(0.26*float64(sampleRate)):int(0.76*float64(sampleRate))] // comp 60ms ahead

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	testifyassert.True(t, state.offset > 0, "offset should be positive")
	testifyassert.InDelta(t, int64(2646), state.offset, 100)
}

func TestAudioSync_Consistency(t *testing.T) {
	config := DefaultAudioSyncConfig()
	config.Syncer = &gccphat.Factory{WindowSize: 4096, HopSize: 2048, MaxLag: 8192}
	config.SyncInterval = 0
	config.WindowSize = 200 * time.Millisecond
	config.OffsetThreshold = 0
	config.ConfidenceThreshold = 0.05
	config.ConsistencyDuration = 100 * time.Millisecond
	config.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := NewAudioSync(ctx, config)
	outCh := make(chan packetorframe.OutputUnion, 100)
	sampleRate := 44100
	baseSignal := createNoiseBurst(sampleRate, 1*time.Second, 0, 1*time.Second)

	refSamples := baseSignal[int(0.2*float64(sampleRate)):int(0.7*float64(sampleRate))]
	compSamples := baseSignal[int(0.26*float64(sampleRate)):int(0.76*float64(sampleRate))] // comp 60ms ahead

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	state := k.getStreamState(ctx, 1)
	testifyassert.Equal(t, int64(0), state.offset)
	testifyassert.False(t, state.directionStartTime.IsZero())

	// Deterministic substitute for the prior `time.Sleep(150ms)` that
	// was waiting for time.Since(directionStartTime) to exceed the
	// 100ms ConsistencyDuration gate at audio_sync.go. Pushing
	// directionStartTime backwards by ConsistencyDuration+1ms makes
	// the next sync pass the gate without depending on real time
	// elapsing, eliminating the test's flakiness under load and
	// removing the deadlock-shaped ordering window the prior critic
	// flagged (sleep-then-ManualLock can race against the audio_sync
	// internal locker on slow CI under -race).
	k.locker.ManualLock(ctx)
	k.syncers = make(map[int]syncerstream.SyncerStream)
	if s0, ok := k.streamStates[0]; ok {
		s0.lastSyncTime = time.Time{}
	}
	if s1, ok := k.streamStates[1]; ok {
		s1.lastSyncTime = time.Time{}
		s1.directionStartTime = time.Now().Add(-config.ConsistencyDuration - time.Millisecond)
	}
	k.locker.ManualUnlock(ctx)

	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(0, 0, sampleRate, refSamples), outCh))
	testifyassert.NoError(t, k.SendInput(ctx, createTestAudioFrame(1, 0, sampleRate, compSamples), outCh))

	testifyassert.NotEqual(t, int64(0), state.offset)
}

func BenchmarkAudioSync_Adaptive(b *testing.B) {
	config := DefaultAudioSyncConfig()
	// Large search range: 5 seconds (220500 samples)
	// Window size: 16384 samples (~370ms)
	config.Syncer = &gccphat.Factory{WindowSize: 16384, HopSize: 8192, MaxLag: 220500}
	config.ConfidenceThreshold = 0.1
	config.ConsistencyDuration = 0
	config.Tracks[1] = AudioSyncTrackConfig{ReferenceStreamIndex: 0}

	ctx := context.Background()
	sampleRate := 44100

	// Create noise for 10 seconds
	noise := make([]float64, sampleRate*10)
	r := rand.New(rand.NewSource(42))
	for i := range noise {
		noise[i] = r.Float64()*2 - 1
	}

	b.Run("Search", func(b *testing.B) {
		// Measures time to acquire the first sync (includes full MaxLag search)
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			k := NewAudioSync(ctx, config)
			outCh := make(chan packetorframe.OutputUnion, 1000)

			// Reference at PTS 0
			// Comparison at PTS 3s
			// We push enough samples to trigger the first few analyses (1s of data)
			fRef := createTestAudioFrame(0, 0, sampleRate, noise[:sampleRate])
			fComp := createTestAudioFrame(1, int64(sampleRate*3), sampleRate, noise[sampleRate*3:sampleRate*4])

			b.StartTimer()
			_ = k.SendInput(ctx, fRef, outCh)
			_ = k.SendInput(ctx, fComp, outCh)
		}
	})

	b.Run("Track", func(b *testing.B) {
		// Measures time for steady-state tracking (uses localized search window)
		k := NewAudioSync(ctx, config)
		outCh := make(chan packetorframe.OutputUnion, 1000)

		// 1. Initial sync to put the syncer into "Tracking" mode
		fRefInit := createTestAudioFrame(0, 0, sampleRate, noise[:sampleRate*2])
		fCompInit := createTestAudioFrame(1, int64(sampleRate*3), sampleRate, noise[sampleRate*3:sampleRate*5])
		_ = k.SendInput(ctx, fRefInit, outCh)
		_ = k.SendInput(ctx, fCompInit, outCh)

		// 2. Measure tracking performance for new incoming data
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Use modulo to stay within noise buffer limits
			startRef := (sampleRate*2 + i*8192) % (len(noise) - 8192)
			startComp := (sampleRate*5 + i*8192) % (len(noise) - 8192)

			fRef := createTestAudioFrame(0, int64(sampleRate*2+i*8192), sampleRate, noise[startRef:startRef+8192])
			fComp := createTestAudioFrame(1, int64(sampleRate*5+i*8192), sampleRate, noise[startComp:startComp+8192])
			_ = k.SendInput(ctx, fRef, outCh)
			_ = k.SendInput(ctx, fComp, outCh)
		}
	})
}
