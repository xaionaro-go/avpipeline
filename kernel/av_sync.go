// av_sync.go implements the AVSync passthrough kernel that observes
// audio/video PTS, exposes the audio-minus-video delta, and applies
// a per-mediatype PTS+DTS offset to packets in transit.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

const (
	// avSyncNoiseFloor is the absolute delta below which sync is considered
	// "in sync" for the purposes of event-driven log classification.
	avSyncNoiseFloor = 10 * time.Millisecond

	// avSyncPeriodic forces a "periodic" log line at most this often even
	// when sync is stable.
	avSyncPeriodic = time.Minute

	// avSyncRatio is the magnitude-change threshold for a "magnitude" log
	// line: emit when the new abs delta is >= ratio*prev or <= prev/ratio.
	avSyncRatio = 2
)

// ErrAVSyncNotObserved is returned by AutoTune when the AV sync delta
// is not yet measurable (one or both media types have not been seen).
var ErrAVSyncNotObserved = errors.New("av_sync: audio or video not yet observed")

// ErrAVSyncVideoLeadsAudio is returned by AutoTune when the observed
// delta is negative — video is ahead of audio. Auto-tune shifts video
// forward only ("video syncs to audio"); it never shifts video backward
// (would violate downstream DTS monotonicity) and never shifts audio
// backward. The operator must intervene manually in that case.
var ErrAVSyncVideoLeadsAudio = errors.New("av_sync: delta negative (video ahead of audio); auto-tune cannot zero it without backward shift")

// avSyncSnapshot is an internal observation: per-mediatype committed PTS
// plus presence flags.
type avSyncSnapshot struct {
	audioPTS time.Duration
	videoPTS time.Duration
	hasAudio bool
	hasVideo bool
}

func (s avSyncSnapshot) delta() time.Duration { return s.audioPTS - s.videoPTS }
func (s avSyncSnapshot) observed() bool       { return s.hasAudio && s.hasVideo }

// avSyncLogState records the previous emitted snapshot and when it was
// emitted, so shouldLogAVSync can reason about transitions.
type avSyncLogState struct {
	prev avSyncSnapshot
	at   time.Time
	set  bool
}

// shouldLogAVSync classifies whether a new snapshot warrants an emitted
// log line, given the previous emitted state, and what reason to attach.
//
// Reasons (priority order):
//   - "initial":   first observation
//   - "emerged":   prev abs delta below floor, current above
//   - "resolved":  prev abs delta above floor, current below
//   - "flip":      sign change while both ends above floor
//   - "magnitude": both above floor and magnitude changed by >= avSyncRatio
//   - "periodic":  no qualifying transition but >= avSyncPeriodic since last emit
func shouldLogAVSync(
	prev avSyncLogState,
	cur avSyncSnapshot,
	now time.Time,
) (emit bool, reason string) {
	if !cur.observed() {
		return false, ""
	}
	if !prev.set {
		return true, "initial"
	}
	prevDelta, curDelta := prev.prev.delta(), cur.delta()
	prevAbs, curAbs := absDuration(prevDelta), absDuration(curDelta)
	prevSig, curSig := prevAbs >= avSyncNoiseFloor, curAbs >= avSyncNoiseFloor

	switch {
	case !prevSig && curSig:
		return true, "emerged"
	case prevSig && !curSig:
		return true, "resolved"
	case prevSig && curSig && signOf(prevDelta) != signOf(curDelta):
		return true, "flip"
	case prevSig && curSig &&
		(curAbs >= time.Duration(avSyncRatio)*prevAbs ||
			prevAbs >= time.Duration(avSyncRatio)*curAbs):
		return true, "magnitude"
	case now.Sub(prev.at) >= avSyncPeriodic:
		return true, "periodic"
	}
	return false, ""
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func signOf(d time.Duration) int {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// AVSync is a passthrough kernel that observes audio/video PTS and
// applies per-mediatype PTS+DTS offsets to packets in transit.
//
// Inserted just before an Output kernel so the observation reflects
// what reaches the wire. The "delta" is audioPTS minus videoPTS,
// post-offset (i.e., what the player would actually see).
//
// Auto-tune (per "video syncs to audio" hard rule): always shifts
// video forward by the observed positive delta. Never shifts audio.
// Negative-delta case errors out (would require backward shift).
type AVSync struct {
	*closuresignaler.ClosureSignaler

	mu          sync.Mutex
	audioOffset time.Duration // applied to audio packet PTS+DTS as they pass through
	videoOffset time.Duration // applied to video packet PTS+DTS as they pass through
	audioPTS    time.Duration // max post-offset audio PTS observed
	videoPTS    time.Duration // max post-offset video PTS observed
	hasAudio    bool
	hasVideo    bool
	logState    avSyncLogState
}

var _ kerneltypes.Abstract = (*AVSync)(nil)
var _ kerneltypes.Resetter = (*AVSync)(nil)
var _ kerneltypes.Resetter = (*AudioSync)(nil)

func NewAVSync(_ context.Context) *AVSync {
	return &AVSync{
		ClosureSignaler: closuresignaler.New(),
	}
}

func (s *AVSync) String() string { return "AVSync" }

func (s *AVSync) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(s)
}

func (s *AVSync) Close(ctx context.Context) error {
	s.ClosureSignaler.Close(ctx)
	return nil
}

func (s *AVSync) Generate(
	ctx context.Context,
	_ chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "Generate")
	defer func() { logger.Tracef(ctx, "/Generate: %v", _err) }()
	return nil
}

// SendInput applies the configured offset to the packet (if any) for
// its media type, observes the post-offset PTS for delta tracking, and
// forwards the packet onward. Frames pass through unchanged without
// observation or offset (AVSync operates on packet PTS only).
func (s *AVSync) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "SendInput")
	defer func() { logger.Tracef(ctx, "/SendInput: %v", _err) }()

	output := input.CloneAsReferencedOutput()
	if output.Get() == nil {
		return kerneltypes.ErrUnexpectedInputType{}
	}

	// Only packets are observed/adjusted. Frames pass through.
	if output.Packet != nil {
		s.observeAndApply(ctx, &output)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputCh <- output:
	}
	return nil
}

// ApplyAndObserve mutates the abstract's PTS+DTS by the configured
// offset for its media type, then updates the per-mediatype max-PTS
// state and emits a debug log when warranted.
//
// Production wires AVSync exclusively as a PushTo Condition through
// avsynccondition.Condition, which calls ApplyAndObserve directly. The
// kernel.Abstract conformance (SendInput, below) is preserved for
// type-compat with FilterKernelFactory paths and is exercised by the
// av_sync_test.go harness; no production code path inserts AVSync as a
// chain node today. Both call sites delegate to observeAndApply so the
// observation+mutation behavior stays identical across the two paths.
//
// abs must be a non-nil, non-empty packetorframe.Abstract — callers
// must filter out the empty-union and nil cases first.
func (s *AVSync) ApplyAndObserve(
	ctx context.Context,
	abs packetorframe.Abstract,
) {
	s.observeAndApply(ctx, abs)
}

// observeAndApply is the shared implementation of SendInput and
// ApplyAndObserve. It runs the timebase + media-type guards, then
// delegates the critical section to applyAndObserveLocked.
func (s *AVSync) observeAndApply(
	ctx context.Context,
	abs packetorframe.Abstract,
) {
	tb := abs.GetTimeBase()
	if tb.Num() == 0 || tb.Den() == 0 {
		return
	}
	mediaType := abs.GetMediaType()
	if mediaType != astiav.MediaTypeAudio && mediaType != astiav.MediaTypeVideo {
		return
	}

	debugEnabled := logger.FromCtx(ctx).Level() >= logger.LevelDebug

	snap, audioOff, videoOff, emit, reason := s.applyAndObserveLocked(mediaType, tb, abs, debugEnabled)
	if emit {
		logger.Debugf(ctx,
			"av_sync audio_pts=%v video_pts=%v delta=%v reason=%s audio_off=%v video_off=%v",
			snap.audioPTS, snap.videoPTS, snap.delta(), reason, audioOff, videoOff)
	}
}

// applyAndObserveLocked runs the entire critical section of
// observeAndApply — reads the per-mediatype offset, mutates the
// abstract's PTS+DTS in place, commits the post-offset observation, and
// computes whether to emit a debug log. Returns the snapshot + offsets
// + emit decision so the caller can emit the log without holding the
// mutex.
func (s *AVSync) applyAndObserveLocked(
	mediaType astiav.MediaType,
	tb astiav.Rational,
	abs packetorframe.Abstract,
	debugEnabled bool,
) (snap avSyncSnapshot, audioOff, videoOff time.Duration, emit bool, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	off := s.audioOffset
	if mediaType == astiav.MediaTypeVideo {
		off = s.videoOffset
	}
	if off != 0 {
		offUnits := avconv.FromDuration(off, tb)
		if rawPTS := abs.GetPTS(); rawPTS != astiav.NoPtsValue {
			abs.SetPTS(rawPTS + offUnits)
		}
		if rawDTS := abs.GetDTS(); rawDTS != astiav.NoPtsValue {
			abs.SetDTS(rawDTS + offUnits)
		}
	}

	audioOff, videoOff = s.audioOffset, s.videoOffset

	rawPTS := abs.GetPTS()
	if rawPTS == astiav.NoPtsValue {
		return
	}
	ts := avconv.Duration(rawPTS, tb)
	switch mediaType {
	case astiav.MediaTypeAudio:
		if !s.hasAudio || ts > s.audioPTS {
			s.audioPTS = ts
			s.hasAudio = true
		}
	case astiav.MediaTypeVideo:
		if !s.hasVideo || ts > s.videoPTS {
			s.videoPTS = ts
			s.hasVideo = true
		}
	}
	snap = avSyncSnapshot{
		audioPTS: s.audioPTS,
		videoPTS: s.videoPTS,
		hasAudio: s.hasAudio,
		hasVideo: s.hasVideo,
	}
	if debugEnabled {
		now := time.Now()
		emit, reason = shouldLogAVSync(s.logState, snap, now)
		if emit {
			s.logState = avSyncLogState{prev: snap, at: now, set: true}
		}
	}
	return
}

// GetDelta returns the latest observed audioPTS - videoPTS (post-
// offset) and whether both audio and video have been observed.
func (s *AVSync) GetDelta(_ context.Context) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasAudio || !s.hasVideo {
		return 0, false
	}
	return s.audioPTS - s.videoPTS, true
}

// SetOffset configures the PTS+DTS shift applied to packets of the
// given media type as they pass through. Only Audio and Video media
// types are supported.
func (s *AVSync) SetOffset(
	_ context.Context,
	mediaType astiav.MediaType,
	off time.Duration,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch mediaType {
	case astiav.MediaTypeAudio:
		s.audioOffset = off
	case astiav.MediaTypeVideo:
		s.videoOffset = off
	default:
		return fmt.Errorf("av_sync: unsupported media type %v (only Audio/Video)", mediaType)
	}
	return nil
}

// GetOffset returns the configured PTS+DTS shift for the given media
// type. Only Audio and Video are supported; other types return an error.
func (s *AVSync) GetOffset(
	_ context.Context,
	mediaType astiav.MediaType,
) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch mediaType {
	case astiav.MediaTypeAudio:
		return s.audioOffset, nil
	case astiav.MediaTypeVideo:
		return s.videoOffset, nil
	default:
		return 0, fmt.Errorf("av_sync: unsupported media type %v (only Audio/Video)", mediaType)
	}
}

// Reset clears per-stream observation state (audioPTS/videoPTS/has*
// and the log-emission memory). Operator-configured offsets are
// preserved — those are configuration, not observation.
//
// Intended to be called by chain-restart/teardown paths after the old
// chain drains and before any new chain delivers its first packet. The
// max-PTS retention in observeAndApply would otherwise hold values
// from a chain that no longer exists, masking lower-PTS observations
// from the freshly-started chain.
func (s *AVSync) Reset(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audioPTS = 0
	s.videoPTS = 0
	s.hasAudio = false
	s.hasVideo = false
	s.logState = avSyncLogState{}
	return nil
}

// AutoTune reads the current delta and applies an offset to VIDEO so
// subsequent video packets shift forward by the delta — closing the
// audio-leads-video gap. Per the "video syncs to audio" rule, this
// never modifies the audio offset.
//
// Returns ErrAVSyncNotObserved if either media type has not been seen.
// Returns ErrAVSyncVideoLeadsAudio if delta < 0 (video ahead of audio,
// would require a backward shift). A zero delta is treated as a no-op
// and returns (0, nil) — there is nothing to shift.
func (s *AVSync) AutoTune(_ context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasAudio || !s.hasVideo {
		return 0, ErrAVSyncNotObserved
	}
	delta := s.audioPTS - s.videoPTS
	switch {
	case delta == 0:
		return 0, nil
	case delta < 0:
		return delta, ErrAVSyncVideoLeadsAudio
	}
	s.videoOffset += delta
	return delta, nil
}
