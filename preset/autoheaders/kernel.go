// kernel.go implements the kernel for the autoheaders preset.

package autoheaders

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/kernel"
	kernelboilerplate "github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

// TODO: consider using AVCodecParserContext instead
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

var _ kerneltypes.SendInputer = (*AutoHeaders)(nil)

func (h *AutoHeaders) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Tracef(ctx, "SendInput: %s", input)
	defer func() { logger.Tracef(ctx, "/SendInput: %s: %v", input, _err) }()
	return xsync.DoA3R1(ctx, &h.Locker, h.sendInputLocked, ctx, input, outputCh)
}

func (h *AutoHeaders) sendInputLocked(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	if h.Processor == nil {
		return fmt.Errorf("processor is not set")
	}
	if h.IsSet {
		return h.Processor.Kernel.SendInput(
			ctx,
			input,
			outputCh,
		)
	}
	if h.CallCount.Add(1) > 1 {
		// there is no real reason to limit the amount of calls;
		// but this is just for early misbehavior detection
		return fmt.Errorf("this kernel is supposed to be used only once")
	}

	var newKernel kerneltypes.Abstract
	if input.Packet != nil {
		var err error
		newKernel, err = h.detectAppropriateFixerKernel(ctx, *input.Packet)
		if err != nil {
			return fmt.Errorf("unable to detect appropriate fixer kernel: %w", err)
		}
	}
	if newKernel == nil {
		newKernel = &kernel.Passthrough{} // no fixing is needed
	}

	// so that we detect the new kernel only once and after that use it directly:
	h.Processor.Kernel = newKernel
	h.IsSet = true

	// to get the input finally processed
	return h.Processor.Kernel.SendInput(
		ctx,
		input,
		outputCh,
	)
}

func (h *AutoHeaders) detectAppropriateFixerKernel(
	ctx context.Context,
	input packet.Input,
) (_ret kerneltypes.Abstract, _err error) {
	logger.Debugf(ctx, "detectAppropriateFixerKernel: %v", input.Source)
	defer func() { logger.Debugf(ctx, "/detectAppropriateFixerKernel: %s: %v %v", input.Source, _ret, _err) }()

	defer func() {
		if r := recover(); r != nil {
			_err = fmt.Errorf("panic: %v: %s", r, debug.Stack())
			_ret = nil
			logger.Errorf(ctx, "%v", _err)
		}
	}()

	logger.Debugf(ctx, "detecting for: %s %s", input.GetSource(), h.Sink)
	var inputFormatName string
	inputAmountOfStreams := 0
	input.GetSource().WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
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

	isOOBHeadersInput := input.IsOOBHeaders()
	isOOBHeadersInput = isOOBHeadersByFormatName(ctx, inputFormatName)
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
