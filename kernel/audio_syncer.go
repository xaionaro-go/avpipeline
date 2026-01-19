package kernel

import (
	"context"
	"math"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/audio/pkg/audio"
	"github.com/xaionaro-go/audio/pkg/syncerstream"
	streamgccphat "github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/indicator"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/xsync"
)

type AudioSyncStreamState struct {
	streamIndex  int
	sampleRate   int
	timeBase     astiav.Rational
	lastPts      int64
	lastSyncTime time.Time

	// Current applied offset in samples.
	offset int64

	// Filtering and smoothing
	movingAverage      *indicator.MAMA[int64]
	directionStartTime time.Time
	lastDesiredDir     int // -1, 0, 1
}

type AudioSync struct {
	ctx    context.Context
	config *AudioSyncConfig
	locker xsync.Mutex

	streamStates map[int]*AudioSyncStreamState
	syncers      map[int]syncerstream.SyncerStream // ReferenceStreamIndex -> Syncer
}

func NewAudioSync(ctx context.Context, config *AudioSyncConfig) *AudioSync {
	if config == nil {
		config = DefaultAudioSyncConfig()
	}
	return &AudioSync{
		ctx:          ctx,
		config:       config,
		streamStates: make(map[int]*AudioSyncStreamState),
		syncers:      make(map[int]syncerstream.SyncerStream),
	}
}

func (s *AudioSync) getStreamState(ctx context.Context, idx int) *AudioSyncStreamState {
	s.locker.ManualLock(ctx)
	defer s.locker.ManualUnlock(ctx)

	return s.getStreamStateNoLock(idx)
}

func (s *AudioSync) getStreamStateNoLock(idx int) *AudioSyncStreamState {
	state, ok := s.streamStates[idx]
	if !ok {
		state = &AudioSyncStreamState{
			streamIndex: idx,
		}
		// Initialize MAMA if requested
		if trackCfg, ok := s.config.Tracks[idx]; ok && trackCfg.MovingAverageCount > 0 {
			state.movingAverage = indicator.NewMAMA[int64](trackCfg.MovingAverageCount, 0.5, 0.05)
		} else {
			state.movingAverage = indicator.NewMAMA[int64](10, 0.5, 0.05)
		}
		s.streamStates[idx] = state
	}
	return state
}

func (s *AudioSync) getSyncerNoLock(ctx context.Context, refIdx int, f *astiav.Frame) syncerstream.SyncerStream {
	if syncer, ok := s.syncers[refIdx]; ok {
		return syncer
	}

	var pcmFormat audio.PCMFormat
	if f.SampleFormat() == astiav.SampleFormatFlt || f.SampleFormat() == astiav.SampleFormatFltp {
		pcmFormat = audio.PCMFormatFloat32LE
	} else {
		pcmFormat = audio.PCMFormatFromString(f.SampleFormat().String())
	}

	encoding := audio.EncodingPCM{
		PCMFormat:  pcmFormat,
		SampleRate: audio.SampleRate(f.SampleRate()),
	}

	channels := audio.Channel(f.ChannelLayout().Channels())

	var syncer syncerstream.SyncerStream
	if s.config.Syncer != nil {
		syncer = s.config.Syncer.NewSyncer(encoding, channels)
	} else {
		syncer = streamgccphat.NewSyncer(encoding, channels, 0, 0, 0)
	}
	s.syncers[refIdx] = syncer
	return syncer
}

func (s *AudioSync) SendInput(ctx context.Context, input packetorframe.InputUnion, outCh chan<- packetorframe.OutputUnion) error {
	s.locker.ManualLock(ctx)
	defer s.locker.ManualUnlock(ctx)

	switch {
	case input.Packet != nil:
		outCh <- packetorframe.OutputUnion{Packet: (*packet.Output)(input.Packet)}
		return nil
	case input.Frame == nil:
		return nil
	}

	streamIdx := input.Frame.StreamInfo.StreamIndex
	state := s.getStreamStateNoLock(streamIdx)

	// Update state with frame info
	state.sampleRate = input.Frame.Frame.SampleRate()
	state.timeBase = input.Frame.StreamInfo.TimeBase
	state.lastPts = input.Frame.Frame.Pts()

	// 1. Buffer audio for synchronization
	samplesAsBytes, err := s.extractBytes(input.Frame.Frame)
	if err == nil {
		trackCfg, hasTrackCfg := s.config.Tracks[streamIdx]

		// If this stream is a reference for others, push to their syncers
		for tid, tcfg := range s.config.Tracks {
			if tcfg.ReferenceStreamIndex == streamIdx && tcfg.ReferenceStreamIndex != tid {
				syncer := s.getSyncerNoLock(ctx, streamIdx, input.Frame.Frame)
				syncer.PushReference(ctx, samplesAsBytes)
				break // Only need to push once per syncer instance
			}
		}

		// If this stream has a reference, push to its own syncer
		if hasTrackCfg && trackCfg.ReferenceStreamIndex != streamIdx {
			syncer := s.getSyncerNoLock(ctx, trackCfg.ReferenceStreamIndex, input.Frame.Frame)
			results, err := syncer.PushComparison(ctx, streamIdx, samplesAsBytes)
			if err == nil {
				for _, res := range results {
					s.applySyncResult(state, res.Shift, res.Confidence)
				}
			}
		}
	}

	// 2. Apply the current offset to the frame PTS
	if state.offset != 0 {
		input.Frame.Frame.SetPts(state.lastPts + state.offset)
	}

	outCh <- packetorframe.OutputUnion{Frame: (*frame.Output)(input.Frame)}
	return nil
}

func (s *AudioSync) applySyncResult(state *AudioSyncStreamState, shift float64, confidence float64) {
	if confidence < s.config.ConfidenceThreshold {
		logger.Warnf(s.ctx, "syncer: low confidence result (%v < %v) for stream %d, ignoring", confidence, s.config.ConfidenceThreshold, state.streamIndex)
		state.lastSyncTime = time.Now()
		return
	}

	desiredOffsetSamples := int64(shift)

	// Apply Threshold: Do not change if the shift is too small
	thresholdSamples := int64(s.config.OffsetThreshold.Seconds() * float64(state.sampleRate))
	if math.Abs(float64(desiredOffsetSamples-state.offset)) < float64(thresholdSamples) {
		state.lastSyncTime = time.Now()
		return
	}

	// Consistency Check: Ensure the shift is in the same direction for X duration
	var currentDir int
	switch {
	case desiredOffsetSamples > state.offset:
		currentDir = 1
	case desiredOffsetSamples < state.offset:
		currentDir = -1
	default:
		currentDir = 0
	}

	if currentDir != state.lastDesiredDir {
		state.lastDesiredDir = currentDir
		state.directionStartTime = time.Now()
	}

	if time.Since(state.directionStartTime) < s.config.ConsistencyDuration {
		state.lastSyncTime = time.Now()
		return
	}

	// Smooth using Moving Average
	if state.movingAverage != nil {
		state.offset = state.movingAverage.Update(desiredOffsetSamples)
	} else {
		state.offset = desiredOffsetSamples
	}

	state.lastSyncTime = time.Now()
}

func (s *AudioSync) extractBytes(f *astiav.Frame) ([]byte, error) {
	// For simplicity, just return the raw bytes from the first plane
	// In a real implementation, we should handle multi-plane (planar) audio.
	return f.Data().Bytes(1)
}

// Implement the Kernel interface
func (s *AudioSync) Reset(ctx context.Context) error {
	s.locker.ManualLock(ctx)
	defer s.locker.ManualUnlock(ctx)
	s.streamStates = make(map[int]*AudioSyncStreamState)
	return nil
}
