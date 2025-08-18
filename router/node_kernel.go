package router

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/rpn"
	rpntypes "github.com/xaionaro-go/rpn/types"
	"github.com/xaionaro-go/xsync"
)

type StreamInfo struct {
	PreviousPTS      int64 // previous PTS of the stream
	CurrentPTSOffset int64 // offset to apply to the PTS
}

type NodeKernel struct {
	*closuresignaler.ClosureSignaler
	PTSInitFunc    rpn.Expr[*StreamInfo, int64]
	Locker         xsync.Mutex
	PreviousSource map[int]packet.Source // map[streamID]Source
	StreamInfo     map[int]*StreamInfo   // map[streamID]StreamInfo
	FormatContext  *astiav.FormatContext
	OutputStreams  map[int]*astiav.Stream
}

var _ kernel.Abstract = (*NodeKernel)(nil)

const (
	DefaultPTSInitFunc = "pts_latest 1 +" // which means: "pts_latest + 1"
)

func NewNodeKernel(
	ctx context.Context,
	opts ...NodeKernelOption,
) (_ *NodeKernel, _err error) {
	logger.Tracef(ctx, "NewNodeKernel")
	defer func() { logger.Tracef(ctx, "/NewNodeKernel: %v", _err) }()
	cfg := NodeKernelOptions(opts).config()
	k := &NodeKernel{
		ClosureSignaler: closuresignaler.New(),
		PreviousSource:  map[int]packet.Source{},
		StreamInfo:      map[int]*StreamInfo{},
		FormatContext:   astiav.AllocFormatContext(),
		OutputStreams:   map[int]*astiav.Stream{},
	}
	setFinalizerFree(ctx, k.FormatContext)

	var err error
	k.PTSInitFunc, err = rpn.Parse[*StreamInfo, int64](cfg.PTSInitFunc, streamInfoSymResolver{})
	if err != nil {
		return nil, fmt.Errorf("unable to parse PTSInitFunc expression '%s': %w", cfg.PTSInitFunc, err)
	}
	return k, nil
}

type streamInfoSymResolver struct{}

func (streamInfoSymResolver) Resolve(sym string) (rpntypes.ValueLoader[*StreamInfo, int64], error) {
	switch sym {
	case "pts_latest":
		return rpntypes.FuncValue[*StreamInfo, int64](func(streamInfo *StreamInfo) int64 {
			return streamInfo.PreviousPTS
		}), nil
	}
	return nil, fmt.Errorf("unknown symbol: %s", sym)
}

func (k *NodeKernel) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "NodeKernel.SendInputPacket()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.SendInputPacket(): %v", _err) }()
	return xsync.DoA3R1(ctx, &k.Locker, k.sendInputPacket, ctx, input, outputPacketsCh)
}

func (k *NodeKernel) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
) (_err error) {
	logger.Tracef(ctx, "NodeKernel.sendInputPacket()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.sendInputPacket(): %v", _err) }()

	isNewSource := input.Source != k.PreviousSource[input.StreamIndex()]
	if isNewSource {
		logger.Tracef(ctx, "New source for stream %d: %s", input.Stream, input.Source)
		k.PreviousSource[input.StreamIndex()] = input.Source
	}
	if err := k.handleCorrections(ctx, &input, isNewSource); err != nil {
		return fmt.Errorf("unable to handle corrections for stream %v: %w", input.Stream, err)
	}

	outPkt := packet.BuildOutput(
		packet.CloneAsReferenced(input.Packet),
		input.Stream,
		input.Source,
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

	if err := k.handleCorrections(ctx, &input, input.GetDTS() == 0); err != nil {
		return fmt.Errorf("unable to handle corrections for stream %d: %w", input.GetStreamIndex(), err)
	}

	outFrame := frame.BuildOutput(
		frame.CloneAsReferenced(input.Frame),
		input.CodecParameters,
		input.StreamIndex,
		input.StreamsCount,
		input.StreamDuration,
		input.TimeBase,
		input.Pos,
		input.Duration,
	)
	select {
	case outputFramesCh <- outFrame:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (k *NodeKernel) handleCorrections(
	ctx context.Context,
	input types.AbstractPacketOrFrame,
	isNewStream bool,
) (_err error) {
	logger.Tracef(ctx, "NodeKernel.handleCorrections()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.handleCorrections(): %v", _err) }()
	streamIndex := input.GetStreamIndex()

	streamInfo := k.StreamInfo[streamIndex]
	if streamInfo == nil {
		streamInfo = &StreamInfo{
			PreviousPTS: -1,
		}
		k.StreamInfo[streamIndex] = streamInfo
	}

	if input.GetDTS() > input.GetPTS() {
		return fmt.Errorf("DTS (%d) is greater than PTS (%d) for stream %d", input.GetDTS(), input.GetPTS(), streamIndex)
	}

	if !isNewStream {
		if ptsOffset := streamInfo.CurrentPTSOffset; ptsOffset != 0 {
			logger.Tracef(ctx, "Applying PTS offset %d to stream %d", ptsOffset, streamIndex)
			input.SetDTS(input.GetDTS() + ptsOffset)
			input.SetPTS(input.GetPTS() + ptsOffset)
		}
	} else {
		if streamInfo.PreviousPTS == -1 {
			logger.Tracef(ctx, "New stream %d", streamIndex)
		} else {
			logger.Tracef(ctx, "Renewed stream %d", streamIndex)

			newPTS := k.PTSInitFunc.Eval(streamInfo)
			ptsOffset := newPTS - input.GetPTS()
			streamInfo.CurrentPTSOffset = ptsOffset
			logger.Tracef(ctx, "Setting PTS to %d (offset %d) from %d for stream %d", newPTS, ptsOffset, input.GetPTS(), streamIndex)

			input.SetPTS(newPTS)
			input.SetDTS(input.GetDTS() + ptsOffset)
		}
	}

	streamInfo.PreviousPTS = input.GetPTS()
	return
}

func (k *NodeKernel) String() string {
	return "NodeKernel"
}

func (k *NodeKernel) Close(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "NodeKernel.Close()")
	defer func() { logger.Tracef(ctx, "/NodeKernel.Close(): %v", _err) }()
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
