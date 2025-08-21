package autoheaders

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	kernelboilerplate "github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

type AutoHeaders struct {
	Locker    xsync.Mutex
	Sink      packet.Sink
	Processor *processor.FromKernel[kerneltypes.Abstract]
	IsSet     bool
	CallCount atomic.Uint64
}

var _ kernelboilerplate.CustomHandlerWithContextFormat = (*AutoHeaders)(nil)

func (h *AutoHeaders) String() string {
	return "AutoHeaders"
}

type Kernel = kernelboilerplate.BaseWithFormatContext[*AutoHeaders]

func NewKernel(
	ctx context.Context,
	sink packet.Sink,
) *Kernel {
	return kernelboilerplate.NewKernelWithFormatContext(ctx, &AutoHeaders{
		Sink: sink,
	})
}

func (h *AutoHeaders) SetProcessor(p *processor.FromKernel[kerneltypes.Abstract]) {
	h.Processor = p
}

var _ kerneltypes.SendInputPacketer = (*AutoHeaders)(nil)

func (h *AutoHeaders) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket: %s", input.Source)
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()
	return xsync.DoA4R1(ctx, &h.Locker, h.sendInputPacketLocked, ctx, input, outputPacketsCh, outputFramesCh)
}

func (h *AutoHeaders) sendInputPacketLocked(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	if h.Processor == nil {
		return fmt.Errorf("processor is not set")
	}
	if h.IsSet {
		return h.Processor.Kernel.SendInputPacket(
			ctx,
			input,
			outputPacketsCh,
			outputFramesCh,
		)
	}
	if h.CallCount.Add(1) > 1 {
		// there is no real reason to limit the amount of calls;
		// but this is just for early misbehavior detection
		return fmt.Errorf("this kernel is supposed to be used only once")
	}

	newKernel, err := h.detectAppropriateFixerKernel(ctx, input)
	if err != nil {
		return fmt.Errorf("unable to detect appropriate fixer kernel: %w", err)
	}
	if newKernel == nil {
		newKernel = kernel.Passthrough{} // no fixing is needed
	}

	// so that we detect the new kernel only once and after that use it directly:
	h.Processor.Kernel = newKernel
	h.IsSet = true

	// to get the input finally processed
	return h.Processor.Kernel.SendInputPacket(
		ctx,
		input,
		outputPacketsCh,
		outputFramesCh,
	)
}

func (h *AutoHeaders) detectAppropriateFixerKernel(
	ctx context.Context,
	input packet.Input,
) (_ret kerneltypes.Abstract, _err error) {
	logger.Debugf(ctx, "detectAppropriateFixerKernel: %v", input.Source)
	defer func() { logger.Debugf(ctx, "/detectAppropriateFixerKernel: %v %v", _ret, _err) }()

	logger.Debugf(ctx, "detecting for: %s %s", input.Source, h.Sink)
	var inputFormatName string
	inputAmountOfStreams := 0
	input.Source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		if fmtCtx == nil {
			logger.Errorf(ctx, "the output has no format context")
			return
		}
		inputAmountOfStreams = fmtCtx.NbStreams()
		fmt := fmtCtx.InputFormat()
		if fmt == nil {
			logger.Debugf(ctx, "the output has no format (an intermediate node, not an actual output?)")
			return
		}
		inputFormatName = fmt.Name()
	})
	logger.Debugf(ctx, "input format: '%s' (amount of streams: %d; source: %T)", inputFormatName, inputAmountOfStreams, input.Source)

	var outputFormatName string
	h.Sink.WithInputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		if fmtCtx == nil {
			logger.Errorf(ctx, "the output has no format context")
			return
		}
		fmt := fmtCtx.OutputFormat()
		if fmt == nil {
			logger.Debugf(ctx, "the output has no format (an intermediate node, not an actual output?)")
			return
		}
		outputFormatName = fmt.Name()
	})
	logger.Debugf(ctx, "output format: '%s'", outputFormatName)

	isOOBHeadersInput := isOOBHeadersByFormatName(ctx, inputFormatName)
	isOOBHeadersOutput := isOOBHeadersByFormatName(ctx, outputFormatName)
	logger.Debugf(ctx, "isOOBHeaders: input:%t output:%t", isOOBHeadersInput, isOOBHeadersOutput)

	bsfKernel := h.getBSFKernel(ctx, isOOBHeadersInput, isOOBHeadersOutput)
	if bsfKernel == nil {
		return nil, nil
	}
	return bsfKernel, nil
}

func (h *AutoHeaders) getBSFKernel(
	ctx context.Context,
	isOOBHeadersInput bool,
	isOOBHeadersOutput bool,
) (_ret *kernel.BitstreamFilter) {
	logger.Tracef(ctx, "getBSFKernel: isOOBHeadersInput:%t isOOBHeadersOutput:%t", isOOBHeadersInput, isOOBHeadersOutput)
	defer func() { logger.Tracef(ctx, "/getBSFKernel: %v", _ret) }()
	switch {
	case isOOBHeadersInput == isOOBHeadersOutput:
		if isOOBHeadersOutput {
			return tryNewBSFForCorrectedOOBHeaders(ctx)
		}
		return nil
	case isOOBHeadersOutput:
		return tryNewBSFForOOBHeaders(ctx)
	case isOOBHeadersInput:
		return tryNewBSFForInBandHeaders(ctx)
	default:
		panic("this is supposed to be impossible")
	}
}
