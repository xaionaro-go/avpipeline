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
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/xsync"
)

type SourceInfo struct {
	TimeShift time.Duration
}

type NodeKernel struct {
	*closuresignaler.ClosureSignaler
	Locker         xsync.Mutex
	Config         nodeKernelConfig
	PreviousSource map[int]packet.Source // map[streamID]Source
	SourceInfo     map[packet.Source]*SourceInfo
	FormatContext  *astiav.FormatContext
	OutputStreams  map[int]*astiav.Stream
	LatestPTS      time.Duration
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

func (k *NodeKernel) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket()")
	defer func() { logger.Tracef(ctx, "/SendInputPacket(): %v", _err) }()
	return xsync.DoA3R1(ctx, &k.Locker, k.sendInputPacket, ctx, input, outputPacketsCh)
}

func (k *NodeKernel) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
) (_err error) {
	logger.Tracef(ctx, "sendInputPacket()")
	defer func() { logger.Tracef(ctx, "/sendInputPacket(): %v", _err) }()

	isNewSource := input.Source != k.PreviousSource[input.StreamIndex()]
	if isNewSource {
		logger.Tracef(ctx, "New source for stream %d: %s", input.Stream, input.Source)
		k.PreviousSource[input.StreamIndex()] = input.Source
	}
	if err := k.makeTimeMoveOnlyForward(ctx, &input, input.Source, isNewSource); err != nil {
		if errors.Is(err, errSkip{}) {
			logger.Tracef(ctx, "skipping packet for stream %v due to errSkip", input.Stream)
			return nil
		}
		return fmt.Errorf("unable to handle corrections for stream %v: %w", input.Stream, err)
	}

	outPkt := packet.BuildOutput(
		packet.CloneAsReferenced(input.Packet),
		input.StreamInfo,
	)
	select {
	case outputPacketsCh <- outPkt:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (k *NodeKernel) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	_ chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "NodeKernel.SendInputFrame()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.SendInputFrame(): %v", _err) }()
	return xsync.DoA3R1(ctx, &k.Locker, k.sendInputFrame, ctx, input, outputFramesCh)
}

func (k *NodeKernel) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "NodeKernel.sendInputFrame()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.sendInputFrame(): %v", _err) }()

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
	case outputFramesCh <- outFrame:
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

	if input.GetMediaType() != astiav.MediaTypeVideo {
		// TODO: investigate this
		//
		// This is actually weird, I'd expect audio clock to be much more stable than video clock.
		// However if I calculate shifts using audio clock I for some reason get a huge jump in PTS
		// when I switch from av1_nvenc to copy in a streammux-powered client.
		//
		// Keep this as is for now... It works at least for me. But if there are issues with audio sync,
		// feel free to create ticket on GitHub or send me an email: xaionaro@gmail.com, opensource@dx.center.
		logger.Tracef(ctx, "Not a video stream, skipping")
		return nil
	}

	if input.GetDTS() > input.GetPTS() {
		return fmt.Errorf("DTS (%d) is greater than PTS (%d) for source %v", input.GetDTS(), input.GetPTS(), packetSource)
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
	if !k.Config.ShouldFixPTS {
		logger.Errorf(ctx, "PTS fixing is disabled, not applying the new time shift (%v)", newTimeShift)
		return errSkip{}
	}
	logger.Tracef(ctx, "Setting PTS to %d (offset %d) from %d for source %v", newPTS, ptsOffset, input.GetPTS(), packetSource)
	sourceInfo.TimeShift = newTimeShift

	input.SetPTS(newPTS)
	input.SetDTS(input.GetDTS() + ptsOffset)
	return nil
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

func (k *NodeKernel) CloseChan() <-chan struct{} {
	return k.ClosureSignaler.CloseChan()
}

func (k *NodeKernel) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
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
