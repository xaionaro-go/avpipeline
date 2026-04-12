// node_kernel.go defines the NodeKernel, which handles the actual packet/frame processing within a route.

package router

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type SourceInfo struct {
	TimeShift time.Duration
}

type NodeKernel struct {
	*closuresignaler.ClosureSignaler
	Locker            xsync.Mutex
	Config            nodeKernelConfig
	PreviousSource    map[int]packet.Source // map[streamID]Source
	SourceInfo        map[packet.Source]*SourceInfo
	FormatContext     *astiav.FormatContext
	OutputStreams     map[int]*astiav.Stream
	LatestPTS         time.Duration
	audioTimestampDetected bool
	audioSampleRate        int64 // non-zero when rate correction is needed
	audioTimeBaseDen       int64
	audioEpochOffset       time.Duration
	audioEpochComputed     bool
}

var _ kernel.Abstract = (*NodeKernel)(nil)

func NewNodeKernel(
	ctx context.Context,
	opts ...NodeKernelOption,
) (_ *NodeKernel, _err error) {
	logger.Tracef(ctx, "NewNodeKernel")
	defer func() { logger.Tracef(ctx, "/NewNodeKernel: %v", _err) }()
	k := &NodeKernel{
		ClosureSignaler: closuresignaler.New(),
		Config:          NodeKernelOptions(opts).config(),
		PreviousSource:  map[int]packet.Source{},
		SourceInfo:      map[packet.Source]*SourceInfo{},
		FormatContext:   astiav.AllocFormatContext(),
		OutputStreams:   map[int]*astiav.Stream{},
	}
	setFinalizerFree(ctx, k.FormatContext)
	return k, nil
}

func (k *NodeKernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "SendInput()")
	defer func() { logger.Tracef(ctx, "/SendInput(): %v", _err) }()

	switch {
	case input.Packet != nil:
		return xsync.DoA3R1(ctx, &k.Locker, k.sendPacket, ctx, *input.Packet, outputCh)
	case input.Frame != nil:
		return xsync.DoA3R1(ctx, &k.Locker, k.sendFrame, ctx, *input.Frame, outputCh)
	default:
		return kerneltypes.ErrUnexpectedInputType{}
	}
}

func (k *NodeKernel) sendPacket(
	ctx context.Context,
	input packet.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "sendPacket()")
	defer func() { logger.Tracef(ctx, "/sendPacket(): %v", _err) }()

	inputSource := input.Source.(packet.Source)
	isNewSource := inputSource != k.PreviousSource[input.GetStreamIndex()]
	if isNewSource {
		logger.Tracef(ctx, "New source for stream %d: %s", input.GetStreamIndex(), inputSource)
		k.PreviousSource[input.GetStreamIndex()] = inputSource
	}
	if err := k.makeTimeMoveOnlyForward(ctx, &input, inputSource, isNewSource); err != nil {
		if errors.Is(err, errSkip{}) {
			logger.Tracef(ctx, "skipping packet for stream %v due to errSkip", input.GetStreamIndex())
			return nil
		}
		return fmt.Errorf("unable to handle corrections for stream %v: %w", input.GetStreamIndex(), err)
	}

	outPkt := packet.BuildOutput(
		packet.CloneAsReferenced(input.Packet),
		input.StreamInfo,
	)
	select {
	case outputCh <- packetorframe.OutputUnion{Packet: &outPkt}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (k *NodeKernel) sendFrame(
	ctx context.Context,
	input frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "NodeKernel.sendFrame()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.sendFrame(): %v", _err) }()

	if err := k.makeTimeMoveOnlyForward(ctx, &input, nil, input.GetDTS() == 0); err != nil {
		if errors.Is(err, errSkip{}) {
			logger.Tracef(ctx, "skipping frame due to errSkip")
			return nil
		}
		return fmt.Errorf("unable to handle corrections for stream %d: %w", input.GetStreamIndex(), err)
	}

	outFrame := frame.BuildOutput(
		frame.CloneAsReferenced(input.Frame),
		input.StreamInfo,
	)
	select {
	case outputCh <- packetorframe.OutputUnion{Frame: &outFrame}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

type errSkip struct{}

func (errSkip) Error() string {
	return "skip"
}

func (k *NodeKernel) makeTimeMoveOnlyForward(
	ctx context.Context,
	input packetorframe.Abstract,
	packetSource packet.Source,
	setNewTimeShift bool,
) (_err error) {
	logger.Tracef(ctx, "makeTimeMoveOnlyForward")
	defer func() { logger.Tracef(ctx, "/makeTimeMoveOnlyForward: %v", _err) }()

	switch input.GetMediaType() {
	case astiav.MediaTypeAudio:
		k.detectAudioTimestampMismatch(ctx, input)

		// Step 1: Rate correction. Phone may send audio timestamps in
		// sample-count units (e.g. 44100 Hz) instead of the declared
		// timebase (1/1000). Rescale: corrected = raw * tbDen / sampleRate.
		if k.audioSampleRate > 0 {
			input.SetDTS(input.GetDTS() * k.audioTimeBaseDen / k.audioSampleRate)
			input.SetPTS(input.GetPTS() * k.audioTimeBaseDen / k.audioSampleRate)
		}

		// Step 2: Epoch alignment. After rate correction, audio may still
		// be offset from video (phone starts audio capture before video).
		// Compute the offset once and subtract from all audio timestamps.
		timeBase := input.GetTimeBase()
		if !k.audioEpochComputed && k.audioTimestampDetected && k.LatestPTS > 0 {
			correctedDTS := avconv.Duration(input.GetDTS(), timeBase)
			k.audioEpochOffset = correctedDTS - k.LatestPTS
			k.audioEpochComputed = true
			logger.Debugf(ctx, "Audio epoch offset: %v (correctedDTS=%v, videoPTS=%v)",
				k.audioEpochOffset, correctedDTS, k.LatestPTS)
		}
		if k.audioEpochOffset != 0 {
			offset := avconv.FromDuration(k.audioEpochOffset, timeBase)
			input.SetDTS(input.GetDTS() - offset)
			input.SetPTS(input.GetPTS() - offset)
		}
		return nil
	case astiav.MediaTypeVideo:
		// Handled below.
	default:
		// Subtitle/data streams may use different clock sources and
		// should not receive video-derived shifts.
		return nil
	}

	if input.GetDTS() > input.GetPTS() {
		logger.Errorf(ctx, "DTS (%d) is greater than PTS (%d) for source %v; fixing...", input.GetDTS(), input.GetPTS(), packetSource)
		input.SetDTS(input.GetPTS())
	}

	sourceInfo := k.SourceInfo[packetSource]
	if sourceInfo == nil {
		sourceInfo = &SourceInfo{}
		k.SourceInfo[packetSource] = sourceInfo
	}

	timeBase := input.GetTimeBase()
	defer func() {
		if _err == nil {
			k.LatestPTS = avconv.Duration(input.GetPTS(), timeBase)
			logger.Tracef(ctx, "Updated the latest PTS to %v [%p]", k.LatestPTS, k)
		}
	}()

	if !setNewTimeShift {
		timeShift := avconv.FromDuration(sourceInfo.TimeShift, timeBase)
		logger.Tracef(ctx, "Applying PTS offset %v (%d) to source %v", sourceInfo.TimeShift, timeShift, packetSource)
		input.SetDTS(input.GetDTS() + timeShift)
		input.SetPTS(input.GetPTS() + timeShift)
		return nil
	}

	if k.LatestPTS == -1 {
		return nil
	}

	previousPTSBased := avconv.FromDuration(k.LatestPTS, timeBase)
	newPTS := previousPTSBased + 2 // +1 to ensure PTS is always increasing and +1 for any rounding errors (due to invoking float64)
	ptsOffset := newPTS - input.GetPTS()
	newTimeShift := avconv.Duration(ptsOffset, timeBase)
	logger.Tracef(ctx, "Calculating a new time shift for source %v: ~: %v-%v (time_base:%v): %v [%p]", packetSource, k.LatestPTS, avconv.Duration(input.GetPTS(), timeBase), timeBase.Float64(), newTimeShift, k)
	if newTimeShift < sourceInfo.TimeShift {
		logger.Tracef(ctx, "New time shift %v is less than the previous one %v, keeping the previous one", newTimeShift, sourceInfo.TimeShift)
		return nil
	}
	sourceInfo.TimeShift = newTimeShift
	if !k.Config.ShouldFixPTS {
		logger.Debugf(ctx, "PTS fixing is disabled, not applying the new time shift (%v) to video; recorded for audio use", newTimeShift)
		return errSkip{}
	}
	logger.Tracef(ctx, "Setting PTS to %d (offset %d) from %d for source %v", newPTS, ptsOffset, input.GetPTS(), packetSource)

	input.SetPTS(newPTS)
	input.SetDTS(input.GetDTS() + ptsOffset)
	return nil
}

func (k *NodeKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *NodeKernel) String() string {
	return "RoutingNode"
}

func (k *NodeKernel) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Close()")
	defer func() { logger.Tracef(ctx, "/Close(): %v", _err) }()
	k.ClosureSignaler.Close(ctx)
	return nil
}

// detectAudioTimestampMismatch checks whether audio timestamps are in
// sample-count units rather than the declared timebase (e.g. phone sends
// 44100 Hz sample counts as RTMP millisecond timestamps, making audio DTS
// grow ~44x faster than video). Once confirmed, audioSampleRate and
// audioTimeBaseDen are stored so the caller can rescale precisely:
// corrected = raw * timeBaseDen / sampleRate.
func (k *NodeKernel) detectAudioTimestampMismatch(
	ctx context.Context,
	input packetorframe.Abstract,
) {
	if k.audioTimestampDetected {
		return
	}

	// Wait until we have a video reference to compare against.
	if k.LatestPTS <= 0 {
		return
	}

	timeBase := input.GetTimeBase()
	audioDTS := avconv.Duration(input.GetDTS(), timeBase)

	// Need a meaningful divergence before we can confirm.
	const detectionThreshold = 2 * time.Second
	if audioDTS-k.LatestPTS < detectionThreshold {
		return
	}

	codecParams := input.GetCodecParameters()
	if codecParams == nil {
		return
	}
	sampleRate := int64(codecParams.SampleRate())
	tbDen := int64(timeBase.Den())
	if sampleRate <= 0 || tbDen <= 0 {
		return
	}
	expectedRatio := sampleRate / tbDen
	if expectedRatio <= 1 {
		return
	}

	// Verify the actual divergence matches the expected sample-rate ratio.
	actualRatio := int64(audioDTS) / int64(k.LatestPTS)
	if actualRatio < expectedRatio/2 || actualRatio > expectedRatio*2 {
		logger.Debugf(ctx, "Audio DTS divergence (actual ratio %d) does not match sample-rate ratio %d; not correcting",
			actualRatio, expectedRatio)
		k.audioTimestampDetected = true
		return
	}

	k.audioTimestampDetected = true
	k.audioSampleRate = sampleRate
	k.audioTimeBaseDen = tbDen
	logger.Debugf(ctx, "Detected audio sample-rate timestamps: rescaling by %d/%d (audioDTS=%v, videoPTS=%v)",
		tbDen, sampleRate, audioDTS, k.LatestPTS)
}

func (k *NodeKernel) CloseChan() <-chan struct{} {
	return k.ClosureSignaler.CloseChan()
}

func (k *NodeKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "NodeKernel.Generate()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.Generate(): %v", _err) }()
	return nil
}

func (k *NodeKernel) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	k.Locker.Do(ctx, func() {
		callback(k.FormatContext)
	})
}

func (k *NodeKernel) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	k.Locker.Do(ctx, func() {
		callback(k.FormatContext)
	})
}

func (k *NodeKernel) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_ret error) {
	logger.Debugf(ctx, "NotifyAboutPacketSource(ctx, %T)", source)
	defer func() { logger.Debugf(ctx, "/NotifyAboutPacketSource(ctx, %T): %v", source, _ret) }()

	var errs []error
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		k.Locker.Do(ctx, func() {
			for _, inputStream := range fmtCtx.Streams() {
				outputStream, err := k.getOutputStreamForPacketByIndex(
					ctx,
					inputStream.Index(),
					inputStream.CodecParameters(),
					inputStream.TimeBase(),
				)
				if outputStream != nil {
					logger.Debugf(ctx, "made sure stream #%d (<-%d) is initialized", outputStream.Index(), inputStream.Index())
				} else {
					logger.Debugf(ctx, "no output stream for stream <-%d", inputStream.Index())
				}
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to initialize an output stream #%d for input stream %d from source %s: %w", inputStream.Index(), inputStream.Index(), source, err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func (k *NodeKernel) getOutputStreamForPacketByIndex(
	ctx context.Context,
	outputStreamIndex int,
	codecParameters *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	outputStream := k.OutputStreams[outputStreamIndex]
	if outputStream != nil {
		return outputStream, nil
	}

	outputStream, err := k.newOutputStream(
		ctx,
		outputStreamIndex,
		codecParameters, timeBase,
	)
	if err != nil {
		return nil, err
	}
	k.OutputStreams[outputStreamIndex] = outputStream
	return outputStream, nil
}

func (k *NodeKernel) newOutputStream(
	ctx context.Context,
	outputStreamIndex int,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*astiav.Stream, error) {
	outputStream := k.FormatContext.NewStream(nil)
	codecParams.Copy(outputStream.CodecParameters())
	outputStream.SetTimeBase(timeBase)
	outputStream.SetIndex(outputStreamIndex)
	logger.Debugf(
		ctx,
		"new output stream %d: %s: %s: %s: %s: %s",
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)
	return outputStream, nil
}
