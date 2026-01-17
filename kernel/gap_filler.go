// gap_filler.go implements a kernel that fills gaps in video and audio streams.

package kernel

import (
	"context"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/audio/pkg/interpolation"
	"github.com/xaionaro-go/audio/pkg/interpolation/fourier"
	avpaudio "github.com/xaionaro-go/avpipeline/audio"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

const (
	// videoInterpolationFlushFrames is the number of additional frames pushed to the filter graph
	// to satisfy the lookahead requirements of motion-compensated interpolation filters (like minterpolate).
	// The value 15 is chosen because FFmpeg's minterpolate filter typically requires a lookahead
	// of 16 frames (including the current one) to finalize motion vector estimation and release
	// the intermediate interpolated frames from its internal buffer.
	videoInterpolationFlushFrames = 15
	// videoInterpolationSearchParam is the search parameter for motion estimation in MCI.
	// It defines the radius of the search for motion vectors. The value 1024 provides a wide
	// search area suitable for high-resolution video (like 1080p and 4K) where objects can
	// move significant pixel distances between frames, though it comes at a high CPU cost.
	videoInterpolationSearchParam = 1024
)

// GapsStrategyVideo defines the method used to fill gaps in video streams.
type GapsStrategyVideo int

const (
	// GapsStrategyVideoUndefined indicates that no strategy has been set.
	GapsStrategyVideoUndefined GapsStrategyVideo = iota
	// GapsStrategyVideoNone means gaps are ignored and no frames are inserted.
	GapsStrategyVideoNone
	// GapsStrategyVideoAddBlankFrames fills gaps with black frames.
	GapsStrategyVideoAddBlankFrames
	// GapsStrategyVideoExtendNextFrame increases the duration of the current frame to cover the gap.
	GapsStrategyVideoExtendNextFrame
	// GapsStrategyVideoDuplicateNextFrame inserts copies of the current frame to fill the gap.
	GapsStrategyVideoDuplicateNextFrame
	// GapsStrategyVideoInterpolate uses motion-compensated or linear interpolation to generate smooth transitional frames.
	GapsStrategyVideoInterpolate
)

func (vgs GapsStrategyVideo) String() string {
	switch vgs {
	case GapsStrategyVideoUndefined:
		return "<undefined>"
	case GapsStrategyVideoNone:
		return "none"
	case GapsStrategyVideoAddBlankFrames:
		return "add_blank_frames"
	case GapsStrategyVideoExtendNextFrame:
		return "extend_next_frame"
	case GapsStrategyVideoDuplicateNextFrame:
		return "duplicate_next_frame"
	case GapsStrategyVideoInterpolate:
		return "interpolate"
	default:
		return fmt.Sprintf("<unknown:%d>", int(vgs))
	}
}

// GapsStrategyAudio defines the method used to fill gaps in audio streams.
type GapsStrategyAudio int

const (
	// GapsStrategyAudioUndefined indicates that no strategy has been set.
	GapsStrategyAudioUndefined GapsStrategyAudio = iota
	// GapsStrategyAudioNone means gaps are ignored and no samples are inserted.
	GapsStrategyAudioNone
	// GapsStrategyAudioAddSilence fills gaps with silent audio samples.
	GapsStrategyAudioAddSilence
	// GapsStrategyAudioInterpolate uses signal interpolation to generate transitional audio samples.
	GapsStrategyAudioInterpolate
)

func (ags GapsStrategyAudio) String() string {
	switch ags {
	case GapsStrategyAudioUndefined:
		return "<undefined>"
	case GapsStrategyAudioNone:
		return "none"
	case GapsStrategyAudioAddSilence:
		return "add_silence"
	case GapsStrategyAudioInterpolate:
		return "interpolate"
	default:
		return fmt.Sprintf("<unknown:%d>", int(ags))
	}
}

type GapFillerConfig struct {
	GapsStrategyAudio      GapsStrategyAudio
	AudioMaxGapDuration    time.Duration
	GapsStrategyVideo      GapsStrategyVideo
	VideoMaxGapDuration    time.Duration
	VideoInterpolationMode string
}

func (cfg *GapFillerConfig) String() string {
	if cfg == nil {
		return "<nil>"
	}
	return spew.Sdump(*cfg)
}

func DefaultGapFillerConfig() GapFillerConfig {
	return GapFillerConfig{
		GapsStrategyAudio:      GapsStrategyAudioAddSilence,
		AudioMaxGapDuration:    500 * time.Millisecond,
		GapsStrategyVideo:      GapsStrategyVideoExtendNextFrame,
		VideoMaxGapDuration:    2 * time.Second,
		VideoInterpolationMode: "mci",
	}
}

type gapFillerStreamState struct {
	NextExpectedPTS    int64
	NextExpectedPTSSet bool
	LastAudioFrame     *astiav.Frame
	LastVideoFrame     *astiav.Frame

	// For video interpolation
	videoFilterGraph     *astiav.FilterGraph
	videoSrcContext      *astiav.BuffersrcFilterContext
	videoSnkContext      *astiav.BuffersinkFilterContext
	lastFilterArgs       string
	videoVirtualPTS      int64
	videoLastWidth       int
	videoLastHeight      int
	videoLastPixelFormat astiav.PixelFormat
}

type GapFiller struct {
	*closuresignaler.ClosureSignaler
	Config GapFillerConfig
	Locker xsync.Mutex

	streams      map[int]*gapFillerStreamState
	interpolator interpolation.Interpolator
}

var _ Abstract = (*GapFiller)(nil)

func NewGapFiller(
	ctx context.Context,
	config *GapFillerConfig,
) *GapFiller {
	if config == nil {
		config = ptr(DefaultGapFillerConfig())
	}
	return &GapFiller{
		ClosureSignaler: closuresignaler.New(),
		Config:          *config,
		streams:         make(map[int]*gapFillerStreamState),
		interpolator:    fourier.New(),
	}
}

func (k *GapFiller) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *GapFiller) String() string {
	return fmt.Sprintf("GapFiller(%s)", k.Config.String())
}

func (k *GapFiller) Close(ctx context.Context) error {
	return xsync.DoA1R1(ctx, &k.Locker, k.closeLocked, ctx)
}

func (k *GapFiller) closeLocked(ctx context.Context) error {
	for _, s := range k.streams {
		if s.LastAudioFrame != nil {
			frame.Pool.Put(s.LastAudioFrame)
			s.LastAudioFrame = nil
		}
		if s.LastVideoFrame != nil {
			frame.Pool.Put(s.LastVideoFrame)
			s.LastVideoFrame = nil
		}
		if s.videoFilterGraph != nil {
			s.videoFilterGraph.Free()
			s.videoFilterGraph = nil
		}
	}
	k.ClosureSignaler.Close(ctx)
	return nil
}

func (k *GapFiller) SendInput(
	ctx context.Context,
	inputUnion packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "SendInput")
	defer func() { logger.Tracef(ctx, "/SendInput: %v", _err) }()

	input := inputUnion.Frame
	if input == nil {
		// Passthrough packets if any, though GapFiller expect frames
		output := inputUnion.CloneAsReferencedOutput()
		if output.Get() == nil {
			return kerneltypes.ErrUnexpectedInputType{}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case outputCh <- output:
		}
		return nil
	}

	return xsync.DoA3R1(ctx, &k.Locker, k.processFrameLocked, ctx, input, outputCh)
}

func (k *GapFiller) processFrameLocked(
	ctx context.Context,
	input *frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	streamIndex := input.StreamIndex
	state, ok := k.streams[streamIndex]
	if !ok {
		state = &gapFillerStreamState{}
		k.streams[streamIndex] = state
	}

	frames := []*astiav.Frame{input.Frame}

	var err error
	switch input.GetMediaType() {
	case astiav.MediaTypeVideo:
		frames, err = k.fixVideoGapIfNeeded(ctx, state, input.StreamInfo, frames)
	case astiav.MediaTypeAudio:
		frames, err = k.fixAudioGapIfNeeded(ctx, state, input.StreamInfo, frames)
	}
	if err != nil {
		return fmt.Errorf("unable to fix gap: %w", err)
	}

	for _, f := range frames {
		outputFrame := frame.BuildOutput(f, input.StreamInfo)
		output := packetorframe.OutputUnion{Frame: &outputFrame}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case outputCh <- output:
		}
	}

	return nil
}

func (k *GapFiller) Generate(
	ctx context.Context,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (k *GapFiller) getGapStart(
	ctx context.Context,
	state *gapFillerStreamState,
	si *frame.StreamInfo,
	input *astiav.Frame,
	maxGapDuration time.Duration,
	strategy fmt.Stringer,
) *int64 {
	defer func() {
		state.NextExpectedPTS = input.Pts() + input.Duration()
		state.NextExpectedPTSSet = true
	}()

	if !state.NextExpectedPTSSet {
		return nil
	}

	currentPTS := input.Pts()
	expectedPTS := state.NextExpectedPTS
	if currentPTS <= expectedPTS {
		return nil
	}

	gap := currentPTS - expectedPTS
	if maxGapDuration > 0 {
		maxGapPTSUnits := astiav.RescaleQ(
			maxGapDuration.Nanoseconds(),
			astiav.NewRational(1, int(time.Second.Nanoseconds())),
			si.TimeBase,
		)
		if gap > maxGapPTSUnits {
			logger.Warnf(ctx, "GapFiller: detected PTS gap too large to fix: gap=%d, maxGap=%d (strategy: %s); resetting the expected PTS", gap, maxGapPTSUnits, strategy)
			state.NextExpectedPTSSet = false
			return nil
		}
	}

	logger.Warnf(ctx, "GapFiller: detected PTS gap: lastPTS=%d, currentPTS=%d, expectedPTS=%d, fixing using strategy: %s", expectedPTS, currentPTS, expectedPTS, strategy)
	return ptr(expectedPTS)
}

func (k *GapFiller) fixVideoGapIfNeeded(
	ctx context.Context,
	state *gapFillerStreamState,
	si *frame.StreamInfo,
	input []*astiav.Frame,
) ([]*astiav.Frame, error) {
	var _output []*astiav.Frame
	for _, frame := range input {
		res, err := k.fixVideoGapIfNeededForOneFrame(ctx, state, si, frame)
		if err != nil {
			return nil, err
		}
		_output = append(_output, res...)
	}
	return _output, nil
}

func (k *GapFiller) updateLastVideoFrame(state *gapFillerStreamState, input *astiav.Frame) {
	if state.LastVideoFrame != nil {
		frame.Pool.Put(state.LastVideoFrame)
	}
	state.LastVideoFrame = frame.CloneAsReferenced(input)
}

func (k *GapFiller) updateLastAudioFrame(state *gapFillerStreamState, input *astiav.Frame) {
	if state.LastAudioFrame != nil {
		state.LastAudioFrame.Free()
	}
	state.LastAudioFrame = input.Clone()
}

func (k *GapFiller) fixVideoGapIfNeededForOneFrame(
	ctx context.Context,
	state *gapFillerStreamState,
	si *frame.StreamInfo,
	input *astiav.Frame,
) ([]*astiav.Frame, error) {
	strategy := k.Config.GapsStrategyVideo
	if strategy == GapsStrategyVideoNone || strategy == GapsStrategyVideoUndefined {
		return []*astiav.Frame{input}, nil
	}

	gapStart := k.getGapStart(ctx, state, si, input, k.Config.VideoMaxGapDuration, strategy)
	if gapStart == nil {
		if strategy == GapsStrategyVideoInterpolate {
			k.updateLastVideoFrame(state, input)
		}
		return []*astiav.Frame{input}, nil
	}

	var res []*astiav.Frame
	var err error
	switch strategy {
	case GapsStrategyVideoExtendNextFrame:
		res = k.fixVideoGapExtendNextFrame(ctx, input, *gapStart)
	case GapsStrategyVideoDuplicateNextFrame:
		res = k.fixVideoGapDuplicateNextFrame(ctx, input, *gapStart)
	case GapsStrategyVideoAddBlankFrames:
		res = k.fixVideoGapAddBlankFrames(ctx, input, *gapStart)
	case GapsStrategyVideoInterpolate:
		res, err = k.fixVideoGapInterpolate(ctx, state, si, input, *gapStart)
	default:
		logger.Errorf(ctx, "unknown GapsStrategyVideo: %s", strategy)
		res = []*astiav.Frame{input}
	}

	if err != nil {
		return nil, fmt.Errorf("unable to fix video gap: %w", err)
	}

	if strategy == GapsStrategyVideoInterpolate {
		k.updateLastVideoFrame(state, input)
	}

	return res, nil
}

func (k *GapFiller) fixVideoGapAddBlankFrames(
	ctx context.Context,
	input *astiav.Frame,
	gapStart int64,
) []*astiav.Frame {
	targetPTS := input.Pts()
	gapDuration := targetPTS - gapStart
	if gapDuration <= 0 {
		return []*astiav.Frame{input}
	}

	f := frame.Pool.Get()
	f.SetWidth(input.Width())
	f.SetHeight(input.Height())
	f.SetPixelFormat(input.PixelFormat())
	f.SetSampleAspectRatio(input.SampleAspectRatio())
	f.SetColorRange(input.ColorRange())
	f.SetColorSpace(input.ColorSpace())
	if err := f.AllocBuffer(0); err != nil {
		logger.Errorf(ctx, "unable to allocate frame buffer for a blank frame: %v", err)
		frame.Pool.Put(f)
		return []*astiav.Frame{input}
	}
	if err := f.ImageFillBlack(); err != nil {
		logger.Errorf(ctx, "unable to fill frame with black color: %v", err)
	}

	f.SetPts(gapStart)
	f.SetDuration(gapDuration)

	return []*astiav.Frame{f, input}
}

func (k *GapFiller) fixVideoGapExtendNextFrame(
	_ context.Context,
	input *astiav.Frame,
	gapStart int64,
) []*astiav.Frame {
	gap := input.Pts() - gapStart
	newDuration := input.Duration() + gap
	input.SetPts(gapStart)
	input.SetDuration(newDuration)
	return []*astiav.Frame{input}
}

func (k *GapFiller) fixVideoGapDuplicateNextFrame(
	_ context.Context,
	input *astiav.Frame,
	gapStart int64,
) []*astiav.Frame {
	targetPTS := input.Pts()
	gapDuration := targetPTS - gapStart
	if gapDuration <= 0 {
		return []*astiav.Frame{input}
	}

	dupFrame := frame.CloneAsReferenced(input)
	dupFrame.SetPts(gapStart)
	dupFrame.SetDuration(gapDuration)

	return []*astiav.Frame{dupFrame, input}
}

func (k *GapFiller) fixVideoGapInterpolate(
	ctx context.Context,
	state *gapFillerStreamState,
	_ *frame.StreamInfo,
	input *astiav.Frame,
	gapStart int64,
) ([]*astiav.Frame, error) {
	if state.LastVideoFrame == nil {
		return k.fixVideoGapDuplicateNextFrame(ctx, input, gapStart), nil
	}

	lastFrame := state.LastVideoFrame

	// Calculate target FPS for interpolation
	timeBase := input.TimeBase()
	if timeBase.Den() == 0 {
		timeBase = lastFrame.TimeBase()
	}
	if timeBase.Den() == 0 {
		return nil, fmt.Errorf("unknown timebase")
	}
	duration := input.Duration()
	if duration <= 0 {
		return nil, fmt.Errorf("unknown duration")
	}
	fps := float64(timeBase.Den()) / (float64(timeBase.Num()) * float64(duration))

	miMode := k.Config.VideoInterpolationMode
	if miMode == "" {
		miMode = "blend"
	}
	filterArgs := fmt.Sprintf("%f:%s", fps, miMode)

	if state.videoFilterGraph != nil && (state.videoLastWidth != lastFrame.Width() ||
		state.videoLastHeight != lastFrame.Height() ||
		state.videoLastPixelFormat != lastFrame.PixelFormat() ||
		state.lastFilterArgs != filterArgs) {
		state.videoFilterGraph.Free()
		state.videoFilterGraph = nil
	}

	if state.videoFilterGraph == nil {
		fg, srcCtx, snkCtx, err := k.createTransientVideoFilterGraph(ctx, lastFrame, timeBase, fps, miMode)
		if err != nil {
			logger.Errorf(ctx, "unable to create video filter graph for interpolation: %v", err)
			return k.fixVideoGapDuplicateNextFrame(ctx, input, gapStart), nil
		}
		state.videoFilterGraph = fg
		state.videoSrcContext = srcCtx
		state.videoSnkContext = snkCtx
		state.videoLastWidth = lastFrame.Width()
		state.videoLastHeight = lastFrame.Height()
		state.videoLastPixelFormat = lastFrame.PixelFormat()
		state.lastFilterArgs = filterArgs
	}

	srcCtx := state.videoSrcContext
	snkCtx := state.videoSnkContext

	numFrames := (input.Pts() - lastFrame.Pts()) / duration
	if numFrames <= 1 {
		return []*astiav.Frame{input}, nil
	}

	// Push the last frame and the current frame to the filter graph with virtual PTS
	f1 := frame.CloneAsReferenced(lastFrame)
	f1.SetPts(state.videoVirtualPTS * duration)
	if err := srcCtx.AddFrame(f1, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		logger.Errorf(ctx, "unable to push last frame to interpolation filter: %v", err)
	}
	f1.Free()

	f2 := frame.CloneAsReferenced(input)
	f2.SetPts((state.videoVirtualPTS + numFrames) * duration)
	if err := srcCtx.AddFrame(f2, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
		logger.Errorf(ctx, "unable to push current frame to interpolation filter: %v", err)
	}

	// Flush lookahead (minterpolate needs frames to output results)
	for i := int64(1); i <= videoInterpolationFlushFrames; i++ {
		f2.SetPts((state.videoVirtualPTS + numFrames + i) * duration)
		srcCtx.AddFrame(f2, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef))
	}
	f2.Free()

	var result []*astiav.Frame
	// Pull interpolated frames
	for {
		outFrame := astiav.AllocFrame()
		err := snkCtx.GetFrame(outFrame, astiav.NewBuffersinkFlags())
		if err == astiav.ErrEagain {
			outFrame.Free()
			break
		}
		if err != nil {
			logger.Errorf(ctx, "error pulling interpolated frame: %v", err)
			outFrame.Free()
			break
		}

		if outFrame.Pts() <= state.videoVirtualPTS*duration || outFrame.Pts() >= (state.videoVirtualPTS+numFrames)*duration {
			outFrame.Free()
			continue
		}

		offset := outFrame.Pts() - state.videoVirtualPTS*duration
		outFrame.SetPts(lastFrame.Pts() + offset)
		result = append(result, outFrame)
	}

	state.videoVirtualPTS += numFrames + videoInterpolationFlushFrames + 1

	if len(result) == 0 {
		return k.fixVideoGapDuplicateNextFrame(ctx, input, gapStart), nil
	}

	result = append(result, input)
	return result, nil
}

func (k *GapFiller) createTransientVideoFilterGraph(
	_ context.Context,
	f *astiav.Frame,
	timeBase astiav.Rational,
	fps float64,
	miMode string,
) (*astiav.FilterGraph, *astiav.BuffersrcFilterContext, *astiav.BuffersinkFilterContext, error) {
	fg := astiav.AllocFilterGraph()
	if fg == nil {
		return nil, nil, nil, fmt.Errorf("unable to allocate filter graph")
	}

	srcFilter := astiav.FindFilterByName("buffer")
	sinkFilter := astiav.FindFilterByName("buffersink")
	if srcFilter == nil || sinkFilter == nil {
		fg.Free()
		return nil, nil, nil, fmt.Errorf("unable to find buffer or buffersink filters")
	}

	srcCtx, err := fg.NewBuffersrcFilterContext(srcFilter, "in")
	if err != nil {
		fg.Free()
		return nil, nil, nil, fmt.Errorf("unable to create buffersrc context: %w", err)
	}

	sinkCtx, err := fg.NewBuffersinkFilterContext(sinkFilter, "out")
	if err != nil {
		fg.Free()
		return nil, nil, nil, fmt.Errorf("unable to create buffersink context: %w", err)
	}

	params := astiav.AllocBuffersrcFilterContextParameters()
	defer params.Free()
	params.SetWidth(f.Width())
	params.SetHeight(f.Height())
	params.SetPixelFormat(f.PixelFormat())
	params.SetTimeBase(timeBase)
	params.SetSampleAspectRatio(f.SampleAspectRatio())

	if err := srcCtx.SetParameters(params); err != nil {
		fg.Free()
		return nil, nil, nil, fmt.Errorf("unable to set buffersrc parameters: %w", err)
	}

	if err := srcCtx.Initialize(nil); err != nil {
		fg.Free()
		return nil, nil, nil, fmt.Errorf("unable to initialize buffersrc: %w", err)
	}

	outputs := astiav.AllocFilterInOut()
	defer outputs.Free()
	outputs.SetName("in")
	outputs.SetFilterContext(srcCtx.FilterContext())
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)

	inputs := astiav.AllocFilterInOut()
	defer inputs.Free()
	inputs.SetName("out")
	inputs.SetFilterContext(sinkCtx.FilterContext())
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)

	filterString := fmt.Sprintf("[in]minterpolate=mi_mode=%%s:fps=%%f:mc_mode=aobmc:me_mode=bidir:me=esa:search_param=%d:vsbmc=1:scd=fdiff[out]", videoInterpolationSearchParam)
	if miMode != "mci" {
		filterString = "[in]minterpolate=mi_mode=%s:fps=%f[out]"
	}
	if err := fg.Parse(fmt.Sprintf(filterString, miMode, fps), inputs, outputs); err != nil {
		fg.Free()
		return nil, nil, nil, fmt.Errorf("unable to parse filter string %q: %w", filterString, err)
	}

	if err := fg.Configure(); err != nil {
		fg.Free()
		return nil, nil, nil, fmt.Errorf("unable to configure filter graph: %w", err)
	}

	return fg, srcCtx, sinkCtx, nil
}

func (k *GapFiller) fixAudioGapIfNeeded(
	ctx context.Context,
	state *gapFillerStreamState,
	si *frame.StreamInfo,
	input []*astiav.Frame,
) ([]*astiav.Frame, error) {
	var _output []*astiav.Frame
	for _, f := range input {
		res, err := k.fixAudioGapIfNeededForOneFrame(ctx, state, si, f)
		if err != nil {
			return nil, err
		}
		_output = append(_output, res...)
	}
	return _output, nil
}

func (k *GapFiller) fixAudioGapIfNeededForOneFrame(
	ctx context.Context,
	state *gapFillerStreamState,
	si *frame.StreamInfo,
	input *astiav.Frame,
) ([]*astiav.Frame, error) {
	strategy := k.Config.GapsStrategyAudio
	if strategy == GapsStrategyAudioNone || strategy == GapsStrategyAudioUndefined {
		return []*astiav.Frame{input}, nil
	}

	gapStart := k.getGapStart(ctx, state, si, input, k.Config.AudioMaxGapDuration, strategy)
	if gapStart == nil {
		if strategy == GapsStrategyAudioInterpolate {
			k.updateLastAudioFrame(state, input)
		}
		return []*astiav.Frame{input}, nil
	}

	res := []*astiav.Frame{input}
	switch strategy {
	case GapsStrategyAudioAddSilence:
		res = k.fixAudioGapAddSilence(ctx, input, *gapStart)
	case GapsStrategyAudioInterpolate:
		res = k.fixAudioGapInterpolate(ctx, state, si, input, *gapStart)
	default:
		logger.Errorf(ctx, "unknown GapsStrategyAudio: %s", strategy)
	}

	if strategy == GapsStrategyAudioInterpolate {
		k.updateLastAudioFrame(state, input)
	}

	return res, nil
}

func (k *GapFiller) fixAudioGapAddSilence(
	ctx context.Context,
	input *astiav.Frame,
	gapStart int64,
) []*astiav.Frame {
	targetPTS := input.Pts()
	gapDuration := targetPTS - gapStart
	if gapDuration <= 0 {
		return []*astiav.Frame{input}
	}

	nbSamples := input.NbSamples()
	originalDuration := input.Duration()
	if nbSamples <= 0 || originalDuration <= 0 {
		return []*astiav.Frame{input}
	}

	totalSamplesToFill := int(gapDuration * int64(nbSamples) / originalDuration)
	if totalSamplesToFill <= 0 {
		return []*astiav.Frame{input}
	}

	f := frame.Pool.Get()
	f.SetSampleFormat(input.SampleFormat())
	f.SetSampleRate(input.SampleRate())
	f.SetChannelLayout(input.ChannelLayout())
	f.SetNbSamples(totalSamplesToFill)

	if err := f.AllocBuffer(0); err != nil {
		logger.Errorf(ctx, "unable to allocate frame buffer for a silent frame: %v", err)
		frame.Pool.Put(f)
		return []*astiav.Frame{input}
	}
	if err := f.SamplesFillSilence(); err != nil {
		logger.Errorf(ctx, "unable to fill frame with silence: %v", err)
	}

	f.SetPts(gapStart)
	f.SetDuration(gapDuration)

	return []*astiav.Frame{f, input}
}

func (k *GapFiller) fixAudioGapInterpolate(
	ctx context.Context,
	state *gapFillerStreamState,
	si *frame.StreamInfo,
	input *astiav.Frame,
	gapStart int64,
) []*astiav.Frame {
	if state.LastAudioFrame == nil {
		return k.fixAudioGapAddSilence(ctx, input, gapStart)
	}

	targetPTS := input.Pts()
	gapDuration := targetPTS - gapStart
	if gapDuration <= 0 {
		return []*astiav.Frame{input}
	}

	sampleRate := input.SampleRate()
	// The frame PTS is in codec timebase.
	timeBase := si.TimeBase

	gapSamples := int(gapDuration * int64(sampleRate) * int64(timeBase.Num()) / int64(timeBase.Den()))
	if gapSamples <= 0 {
		return []*astiav.Frame{input}
	}

	numChannels := input.ChannelLayout().Channels()
	interpolated := make([][]float64, numChannels)
	for c := range numChannels {
		before, err := avpaudio.ExtractSamples(state.LastAudioFrame, c)
		if err != nil {
			logger.Errorf(ctx, "unable to extract samples from the last frame: %v", err)
			return []*astiav.Frame{input}
		}
		after, err := avpaudio.ExtractSamples(input, c)
		if err != nil {
			logger.Errorf(ctx, "unable to extract samples from the current frame: %v", err)
			return []*astiav.Frame{input}
		}
		interpolated[c] = k.interpolator.Interpolate(before, after, gapSamples)
	}

	// Create a new frame for the interpolated samples
	f := frame.Pool.Get()
	f.SetSampleFormat(input.SampleFormat())
	f.SetSampleRate(input.SampleRate())
	f.SetChannelLayout(input.ChannelLayout())
	f.SetNbSamples(gapSamples)
	if err := f.AllocBuffer(0); err != nil {
		logger.Errorf(ctx, "unable to allocate frame buffer for interpolated frame: %v", err)
		frame.Pool.Put(f)
		return []*astiav.Frame{input}
	}

	for c := range numChannels {
		if err := avpaudio.FillSamples(f, c, interpolated[c]); err != nil {
			logger.Errorf(ctx, "unable to fill samples for channel %d: %v", c, err)
		}
	}

	f.SetPts(gapStart)
	f.SetDuration(gapDuration)

	return []*astiav.Frame{f, input}
}
