package kernel

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/audio/pkg/audio"
	"github.com/xaionaro-go/audio/pkg/syncerstream"
	streamgccphat "github.com/xaionaro-go/audio/pkg/syncerstream/implementations/gccphat"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/indicator"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

var _ kerneltypes.Abstract = (*AudioSync)(nil)

type AudioSyncStreamState struct {
	streamIndex  int
	sampleRate   int
	timeBase     astiav.Rational
	lastPts      int64
	lastSyncTime time.Time

	// Expected PTS of the next sample to maintain continuity in the syncer
	expectedNextPts int64

	// Current applied offset in samples.
	offset int64

	// Filtering and smoothing
	movingAverage      *indicator.MAMA[int64]
	directionStartTime time.Time
	lastDesiredDir     int // -1, 0, 1
}

// AudioSync is a kernel component that synchronizes multiple audio streams.
// It works by designating one stream as a Reference and others as Comparison tracks.
// It uses a syncerstream implementation (like GCC-PHAT) to calculate the drift
// and applies it by adjusting the PTS (Presentation Time Stamp) of the comparison frames.
type AudioSync struct {
	*closuresignaler.ClosureSignaler
	ctx    context.Context
	config *AudioSyncConfig
	locker xsync.Mutex

	// streamStates maps each stream index to its synchronization state.
	streamStates map[int]*AudioSyncStreamState
	// syncers maps a Reference stream index to the syncer instance that processes signals relative to it.
	syncers map[int]syncerstream.SyncerStream
	// globalFirstPts and globalFirstPtsSet are used to align the start of silence injection.
	globalFirstPts    int64
	globalFirstPtsSet bool
}

func NewAudioSync(ctx context.Context, config *AudioSyncConfig) *AudioSync {
	if config == nil {
		config = DefaultAudioSyncConfig()
	}
	return &AudioSync{
		ClosureSignaler: closuresignaler.New(),
		ctx:             ctx,
		config:          config,
		streamStates:    make(map[int]*AudioSyncStreamState),
		syncers:         make(map[int]syncerstream.SyncerStream),
	}
}

func (s *AudioSync) getStreamState(ctx context.Context, idx int) *AudioSyncStreamState {
	s.locker.ManualLock(ctx)
	defer s.locker.ManualUnlock(ctx)

	return s.getStreamStateByIdNoLock(idx)
}

func (s *AudioSync) getStreamStateByIdNoLock(idx int) *AudioSyncStreamState {
	return s.streamStates[idx]
}

func (s *AudioSync) getStreamStateNoLock(ctx context.Context, f *frame.Input) *AudioSyncStreamState {
	idx := f.StreamIndex
	state, ok := s.streamStates[idx]
	if !ok {
		state = &AudioSyncStreamState{
			streamIndex: idx,
		}

		// Check configuration
		trackCfg, ok := s.config.Tracks[idx]
		if !ok {
			return nil
		}

		if trackCfg.MovingAverageCount > 0 {
			state.movingAverage = indicator.NewMAMA[int64](trackCfg.MovingAverageCount, 0.5, 0.05)
		} else {
			state.movingAverage = indicator.NewMAMA[int64](10, 0.5, 0.05)
		}
		s.streamStates[idx] = state
	}
	return state
}

func (s *AudioSync) AddTrackConfig(ctx context.Context, idx int, cfg AudioSyncTrackConfig) {
	s.locker.ManualLock(ctx)
	defer s.locker.ManualUnlock(ctx)
	if s.config.Tracks == nil {
		s.config.Tracks = make(map[int]AudioSyncTrackConfig)
	}
	s.config.Tracks[idx] = cfg
}

func (s *AudioSync) getSyncerNoLock(_ context.Context, refIdx int, f *astiav.Frame) (syncerstream.SyncerStream, error) {
	if syncer, ok := s.syncers[refIdx]; ok {
		return syncer, nil
	}

	var pcmFormat audio.PCMFormat
	switch f.SampleFormat() {
	case astiav.SampleFormatS16, astiav.SampleFormatS16P:
		pcmFormat = audio.PCMFormatS16LE
	case astiav.SampleFormatFlt, astiav.SampleFormatFltp:
		pcmFormat = audio.PCMFormatFloat32LE
	case astiav.SampleFormatS32, astiav.SampleFormatS32P:
		pcmFormat = audio.PCMFormatS32LE
	case astiav.SampleFormatDbl, astiav.SampleFormatDblp:
		pcmFormat = audio.PCMFormatFloat64LE
	default:
		pcmFormat = audio.PCMFormatFromString(f.SampleFormat().String())
	}

	encoding := audio.EncodingPCM{
		PCMFormat:  pcmFormat,
		SampleRate: audio.SampleRate(f.SampleRate()),
	}

	channels := audio.Channel(f.ChannelLayout().Channels())

	var syncer syncerstream.SyncerStream
	var err error
	if s.config.Syncer != nil {
		syncer, err = s.config.Syncer.NewSyncer(encoding, channels)
	} else {
		// Using 0 values will trigger defaults in NewSyncer
		syncer, err = streamgccphat.NewSyncer(encoding, channels, 0, 0, 0, 0, 0)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create syncer for stream %d: %w", refIdx, err)
	}
	s.syncers[refIdx] = syncer
	return syncer, nil
}

func (s *AudioSync) SendInput(ctx context.Context, input packetorframe.InputUnion, outCh chan<- packetorframe.OutputUnion) error {
	s.locker.ManualLock(ctx)
	defer s.locker.ManualUnlock(ctx)

	switch {
	case input.Packet != nil:
		return fmt.Errorf("audio syncer: packet input is not supported")
	case input.Frame == nil:
		return nil
	}

	if input.Frame.GetMediaType() != astiav.MediaTypeAudio {
		outCh <- input.CloneAsReferencedOutput()
		return nil
	}

	state := s.getStreamStateNoLock(ctx, input.Frame)
	streamIdx := input.Frame.StreamInfo.StreamIndex

	// Check if this stream is relevant for synchronization.
	// It is relevant if it has a configuration (is a comparison track)
	// or if it's used as a reference by any other track.
	isReference := false
	if state == nil {
		for tid, tcfg := range s.config.Tracks {
			if tcfg.ReferenceStreamIndex == streamIdx && tcfg.ReferenceStreamIndex != tid {
				isReference = true
				break
			}
		}
		if !isReference {
			outCh <- input.CloneAsReferencedOutput()
			return nil
		}
	}

	// 1. Buffer audio for synchronization
	samplesAsBytes, err := s.extractBytes(input.Frame.Frame)
	if err == nil {
		if isReference && !s.globalFirstPtsSet {
			s.globalFirstPts = input.Frame.Frame.Pts()
			s.globalFirstPtsSet = true
		}
		if state != nil {
			// Update state with frame info
			state.sampleRate = input.Frame.Frame.SampleRate()
			state.timeBase = input.Frame.StreamInfo.TimeBase
			state.lastPts = input.Frame.Frame.Pts()
			if !s.globalFirstPtsSet {
				s.globalFirstPts = input.Frame.Frame.Pts()
				s.globalFirstPtsSet = true
			}
			s.fillGaps(ctx, state, input.Frame.Frame.Pts(), input.Frame.Frame)
		}
		if err := s.pushData(ctx, streamIdx, samplesAsBytes, input.Frame.Frame); err != nil {
			return err
		}
		if state != nil {
			if state.expectedNextPts == 0 {
				state.expectedNextPts = input.Frame.Frame.Pts()
			}
			state.expectedNextPts += int64(input.Frame.Frame.NbSamples())
		}
	}

	output := input.CloneAsReferencedOutput()

	if state == nil {
		outCh <- output
		return nil
	}

	// 2. Apply the current offset to the frame PTS.
	// The offset is calculated by the syncer and represents the number of samples
	// the comparison stream needs to be shifted to align with the reference.
	if state.offset != 0 {
		output.SetPTS(state.lastPts + state.offset)
	}

	outCh <- output
	return nil
}

func (s *AudioSync) fillGaps(ctx context.Context, state *AudioSyncStreamState, currentPts int64, f *astiav.Frame) {
	if !s.globalFirstPtsSet {
		s.globalFirstPts = currentPts
		s.globalFirstPtsSet = true
	}

	if state.expectedNextPts == 0 {
		state.expectedNextPts = s.globalFirstPts
	}

	if f == nil || f.NbSamples() <= 0 || f.SampleRate() <= 0 {
		return
	}
	gap := currentPts - state.expectedNextPts
	if gap <= 0 {
		return
	}
	if s.config != nil && s.config.WindowSize > 0 {
		gapNs := astiav.RescaleQ(
			gap,
			state.timeBase,
			astiav.NewRational(1, int(time.Second.Nanoseconds())),
		)
		if gapNs < s.config.WindowSize.Nanoseconds() {
			return
		}
	}
	// Convert gap in PTS units to samples
	gapSamples := int64(float64(gap) * float64(state.timeBase.Num()) / float64(state.timeBase.Den()) * float64(state.sampleRate))
	if gapSamples <= 0 {
		return
	}

	bytesPerSample := f.SampleFormat().BytesPerSample()
	if bytesPerSample <= 0 {
		bytesPerSample = 2
	}
	channels := f.ChannelLayout().Channels()
	if channels <= 0 {
		channels = 1
	}
	frameSize := f.NbSamples()
	if frameSize <= 0 {
		return
	}

	remaining := gapSamples
	for remaining > 0 {
		chunkSamples := int64(frameSize)
		if remaining < chunkSamples {
			chunkSamples = remaining
		}
		chunkBytes := int(chunkSamples) * channels * bytesPerSample
		silence := make([]byte, chunkBytes)
		s.pushData(ctx, state.streamIndex, silence, f)
		remaining -= chunkSamples
	}
}

// pushData routes incoming audio data to the appropriate syncers.
// If the current stream is a reference for others, it pushes to their sync buffers.
// If the current stream has a reference track, it pushes to its own syncer and
// processes any synchronization results.
func (s *AudioSync) pushData(ctx context.Context, streamIdx int, data []byte, f *astiav.Frame) error {
	trackCfg, hasTrackCfg := s.config.Tracks[streamIdx]

	// If this stream is a reference for others, push to their syncers.
	// We iterate through all tracks to find any track that uses this stream as its Reference.
	for tid, tcfg := range s.config.Tracks {
		if tcfg.ReferenceStreamIndex == streamIdx && tcfg.ReferenceStreamIndex != tid {
			syncer, err := s.getSyncerNoLock(ctx, streamIdx, f)
			if err != nil {
				return err
			}
			syncer.PushReference(ctx, data)
			// One syncer instance handles all tracking for a specific reference stream.
			break
		}
	}

	// If this stream has a reference, push to its own syncer (which belongs to the Reference tracks).
	if hasTrackCfg && trackCfg.ReferenceStreamIndex != streamIdx {
		syncer, err := s.getSyncerNoLock(ctx, trackCfg.ReferenceStreamIndex, f)
		if err != nil {
			return err
		}
		results, err := syncer.PushComparison(ctx, streamIdx, data)
		if err != nil {
			return err
		}
		// Each result corresponds to an analyzed window.
		for _, res := range results {
			s.applySyncResult(s.getStreamStateByIdNoLock(streamIdx), res.Shift, res.Confidence)
		}
	}
	return nil
}

// applySyncResult takes a raw shift from the syncer and decides whether to apply it to the PTS offset.
// It incorporates validation layers:
// 1. Confidence Threshold: Ignores results that are likely just noise.
// 2. Offset Threshold: Ignores minor jitter to avoid oscillating PTS corrections.
// 3. Consistency Check: Only applies a correction if the drift direction is consistent over time.
// 4. Smoothing: Applies a Moving Average (MAMA) to the offset for stable playback.
func (s *AudioSync) applySyncResult(state *AudioSyncStreamState, shift float64, confidence float64) {
	if math.Abs(shift) < 1 || math.IsNaN(shift) || math.IsInf(shift, 0) {
		state.lastSyncTime = time.Now()
		return
	}

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
	return f.Data().Bytes(0)
}

func (s *AudioSync) String() string {
	return "AudioSync"
}

func (s *AudioSync) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(s)
}

func (s *AudioSync) Close(ctx context.Context) error {
	return nil
}

func (s *AudioSync) Generate(ctx context.Context, outCh chan<- packetorframe.OutputUnion) error {
	return nil
}

// Implement the Kernel interface
func (s *AudioSync) Reset(ctx context.Context) error {
	s.locker.ManualLock(ctx)
	defer s.locker.ManualUnlock(ctx)
	s.streamStates = make(map[int]*AudioSyncStreamState)
	return nil
}
