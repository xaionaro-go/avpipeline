package router

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/logger"
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
		fwd.AutoFixer = autofix.NewWithCustomData(ctx, srcAsPacketSource, dstAsPacketSink, src.CustomData)
		fwd.AutoFixerInput = &nodewrapper.NoServe[node.Abstract]{Node: fwd.AutoFixer.Input()}
	}
	return fwd, nil
}

func (fwd *StreamForwarderCopy[CS, PS]) Start(ctx context.Context) (_err error) {
	ctx = belt.WithField(ctx, "input", fwd.Input.String())
	ctx = belt.WithField(ctx, "output", fwd.Output)
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
	if fwd.AutoFixer == nil {
		fwd.Input.AddPushPacketsTo(dstNode)
		fwd.Input.AddPushFramesTo(dstNode)
		return nil
	}

	fwd.Input.AddPushPacketsTo(fwd.AutoFixerInput)
	fwd.Input.AddPushFramesTo(fwd.AutoFixerInput)
	fwd.AutoFixer.Output().AddPushPacketsTo(dstNode)
	fwd.AutoFixer.Output().AddPushFramesTo(dstNode)
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
	ctx = belt.WithField(ctx, "input", fwd.Input.String())
	ctx = belt.WithField(ctx, "route", fwd.Output)
	logger.Debugf(ctx, "Stop")
	defer func() { logger.Debugf(ctx, "/Stop: %v", _err) }()
	return xsync.DoA1R1(ctx, &fwd.Mutex, fwd.removePushingFurther, ctx)
}

func (fwd *StreamForwarderCopy[CS, PS]) outputAsNode() *forwarderCopyOutputAsNode[CS, PS] {
	return (*forwarderCopyOutputAsNode[CS, PS])(fwd)
}

type forwarderCopyOutputAsNode[CS any, PS processor.Abstract] StreamForwarderCopy[CS, PS]

var _ node.Abstract = (*forwarderCopyOutputAsNode[any, processor.Abstract])(nil)
var _ node.DotBlockContentStringWriteToer = (*forwarderCopyOutputAsNode[any, processor.Abstract])(nil)

func (fwd *forwarderCopyOutputAsNode[CS, PS]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) DotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	if writeToer, ok := any(fwd.Output).(node.DotBlockContentStringWriteToer); ok {
		writeToer.DotBlockContentStringWriteTo(w, alreadyPrinted)
	}
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) String() string {
	stringer, ok := any(fwd.Output).(fmt.Stringer)
	if !ok {
		return "FwdCpyOutput"
	}
	return fmt.Sprintf("FwdCpyOutput(%s [%s])", stringer, (*StreamForwarderCopy[CS, PS])(fwd).String())
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) OriginalNodeAbstract() node.Abstract {
	return fwd.Output
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

func (fwd *forwarderCopyOutputAsNode[CS, PS]) IsServing() bool {
	return fwd.Output.IsServing()
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

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetChangeChanIsServing() <-chan struct{} {
	return fwd.Output.GetChangeChanIsServing()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return fwd.Output.GetChangeChanPushPacketsTo()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetChangeChanPushFramesTo() <-chan struct{} {
	return fwd.Output.GetChangeChanPushFramesTo()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetChangeChanDrained() <-chan struct{} {
	return fwd.Output.GetChangeChanDrained()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) IsDrained(ctx context.Context) bool {
	return fwd.Input.IsDrained(ctx) && fwd.Output.IsDrained(ctx) && (fwd.AutoFixer == nil || fwd.AutoFixer.IsDrained(ctx))

}
func (fwd *forwarderCopyOutputAsNode[CS, PS]) NotifyInputSent() {
	fwd.Output.NotifyInputSent()
}
