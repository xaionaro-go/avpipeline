package router

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/nodewrapper"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

// TODO: remove StreamForwarder from package `router`
type StreamForwarderCopy[CS any, PS processor.Abstract] struct {
	xsync.Mutex
	CancelFunc     context.CancelFunc
	Input          *node.NodeWithCustomData[CS, PS]
	AutoFixer      *autofix.AutoFixerWithCustomData[CS]
	AutoFixerInput *nodewrapper.NoServe[node.Abstract]
	Output         node.Abstract
}

var _ StreamForwarder[*Route[any], *ProcessorRouting] = (*StreamForwarderCopy[*Route[any], *ProcessorRouting])(nil)

// TODO: remove StreamForwarder from package `router`
func NewStreamForwarderCopy[CS any, PS processor.Abstract](
	ctx context.Context,
	src *node.NodeWithCustomData[CS, PS],
	dst node.Abstract,
) (_ret *StreamForwarderCopy[CS, PS], _err error) {
	logger.Debugf(ctx, "NewStreamForwarderCopy(%s, %s)", src, dst)
	defer func() { logger.Debugf(ctx, "/NewStreamForwarderCopy(%s, %s): %p, %v", src, dst, _ret, _err) }()

	fwd := &StreamForwarderCopy[CS, PS]{
		Input:  src,
		Output: &nodewrapper.NoServe[node.Abstract]{Node: dst},
	}
	srcAsPacketSource := asPacketSource(src.GetProcessor())
	dstAsPacketSink := asPacketSink(dst.GetProcessor())
	if srcAsPacketSource != nil && dstAsPacketSink != nil {
		logger.Debugf(ctx, "adding an autoheaders node to handle Source->Sink conversion")
		fwd.AutoFixer = autofix.NewWithCustomData[CS](ctx, srcAsPacketSource, dstAsPacketSink, src.CustomData)
		fwd.AutoFixerInput = &nodewrapper.NoServe[node.Abstract]{Node: fwd.AutoFixer.Input()}
	}
	return fwd, nil
}

func (fwd *StreamForwarderCopy[CS, PS]) Start(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Start")
	defer func() { logger.Debugf(ctx, "/Start: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Mutex, fwd.addPushingFurther, ctx)
}

func (fwd *StreamForwarderCopy[CS, PS]) addPushingFurther(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "addPushingFurther")
	defer func() { logger.Debugf(ctx, "/addPushingFurther: %v", _err) }()
	if fwd.CancelFunc != nil {
		return ErrAlreadyOpen{}
	}
	ctx, cancelFn := context.WithCancel(ctx)
	fwd.CancelFunc = cancelFn

	dstNode := fwd.outputAsNode()

	pushTos := fwd.Input.GetPushPacketsTos()
	for _, pushTo := range pushTos {
		if pushTo.Node == dstNode {
			return fmt.Errorf("internal error: packets pushing is already added")
		}
	}

	err := avpipeline.NotifyAboutPacketSources(ctx, nil, fwd.Input)
	if err != nil {
		return fmt.Errorf("internal error: unable to notify about the packet source: %w", err)
	}
	fwd.AutoFixer.Output().AddPushPacketsTo(fwd.outputAsNode())
	fwd.AutoFixer.Output().AddPushFramesTo(fwd.outputAsNode())
	fwd.Input.AddPushPacketsTo(fwd.AutoFixerInput)
	fwd.Input.AddPushFramesTo(fwd.AutoFixerInput)
	errCh := make(chan node.Error, 100)
	observability.Go(ctx, func(ctx context.Context) {
		for err := range errCh {
			switch {
			case errors.Is(err, context.Canceled):
				logger.Debugf(ctx, "cancelled: %v", err)
			case errors.Is(err, io.EOF):
				logger.Debugf(ctx, "EOF: %v", err)
			default:
				logger.Errorf(ctx, "got an error: %v", err)
			}
			fwd.Stop(ctx)
		}
	})
	observability.Go(ctx, func(ctx context.Context) {
		fwd.AutoFixer.Serve(ctx, node.ServeConfig{}, errCh)
	})

	return nil
}

func (fwd *StreamForwarderCopy[CS, PS]) removePushingFurther(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "removePushingFurther")
	defer func() { logger.Debugf(ctx, "/removePushingFurther: %v", _err) }()
	if fwd.CancelFunc == nil {
		return ErrAlreadyClosed{}
	}
	var errs []error
	if fwd.AutoFixer != nil {
		err := node.RemovePushPacketsTo(ctx, fwd.Input, fwd.AutoFixerInput)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove packet pushing %s->%s", fwd.Input, fwd.AutoFixer))
		}
		err = node.RemovePushPacketsTo(ctx, fwd.AutoFixer.Output(), fwd.outputAsNode())
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove packet pushing %s->%s", fwd.AutoFixer, fwd.Output))
		}
		err = node.RemovePushFramesTo(ctx, fwd.Input, fwd.AutoFixerInput)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove frame pushing %s->%s", fwd.Input, fwd.AutoFixer))
		}
		err = node.RemovePushFramesTo(ctx, fwd.AutoFixer.Output(), fwd.outputAsNode())
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove frame pushing %s->%s", fwd.AutoFixer, fwd.Output))
		}
	} else {
		err := node.RemovePushPacketsTo(ctx, fwd.Input, fwd.outputAsNode())
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove packet pushing %s->%s", fwd.Input, fwd.Output))
		}
		err = node.RemovePushFramesTo(ctx, fwd.Input, fwd.outputAsNode())
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove frame pushing %s->%s", fwd.Input, fwd.Output))
		}
	}
	fwd.CancelFunc()
	fwd.CancelFunc = nil
	return errors.Join(errs...)
}

func (fwd *StreamForwarderCopy[CS, PS]) String() string {
	return fmt.Sprintf("fwd('%s'->'%s')", fwd.Input, fwd.Output)
}

func (fwd *StreamForwarderCopy[CS, PS]) Source() *node.NodeWithCustomData[CS, PS] {
	return fwd.Input
}

func (fwd *StreamForwarderCopy[CS, PS]) Destination() node.Abstract {
	return fwd.Output
}

func (fwd *StreamForwarderCopy[CS, PS]) Stop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Mutex, fwd.removePushingFurther, ctx)
}

func (fwd *StreamForwarderCopy[CS, PS]) outputAsNode() *forwarderCopyOutputAsNode[CS, PS] {
	return (*forwarderCopyOutputAsNode[CS, PS])(fwd)
}

type forwarderCopyOutputAsNode[CS any, PS processor.Abstract] StreamForwarderCopy[CS, PS]

var _ node.Abstract = (*forwarderCopyOutputAsNode[any, processor.Abstract])(nil)

func (fwd *forwarderCopyOutputAsNode[CS, PS]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetPushPacketsTos() node.PushPacketsTos {
	return fwd.Output.GetPushPacketsTos()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) AddPushPacketsTo(dst node.Abstract, conds ...packetfiltercondition.Condition) {
	fwd.Output.AddPushPacketsTo(dst, conds...)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetPushPacketsTos(pushTos node.PushPacketsTos) {
	fwd.Output.SetPushPacketsTos(pushTos)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetPushFramesTos() node.PushFramesTos {
	return fwd.Output.GetPushFramesTos()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) AddPushFramesTo(dst node.Abstract, conds ...framefiltercondition.Condition) {
	fwd.Output.AddPushFramesTo(dst, conds...)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetPushFramesTos(pushTos node.PushFramesTos) {
	fwd.Output.SetPushFramesTos(pushTos)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetStatistics() *node.Statistics {
	return fwd.Output.GetStatistics()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetProcessor() processor.Abstract {
	return fwd.Output.GetProcessor()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetInputPacketFilter() packetfiltercondition.Condition {
	return fwd.Output.GetInputPacketFilter()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetInputPacketFilter(cond packetfiltercondition.Condition) {
	fwd.Output.SetInputPacketFilter(cond)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetInputFrameFilter() framefiltercondition.Condition {
	return fwd.Output.GetInputFrameFilter()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetInputFrameFilter(cond framefiltercondition.Condition) {
	fwd.Output.SetInputFrameFilter(cond)
}
