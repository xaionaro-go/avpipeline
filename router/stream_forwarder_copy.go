// stream_forwarder_copy.go provides a StreamForwarder implementation that copies streams without transcoding.

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
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/nodewrapper"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
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
	dstAsPacketSink := asPacketSink(dst.GetProcessor())
	if dstAsPacketSink != nil {
		logger.Debugf(ctx, "adding an autoheaders node to handle Source->Sink conversion")
		fwd.AutoFixer = autofix.NewWithCustomData(ctx, dstAsPacketSink, src.CustomData)
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
	for _, pushTo := range fwd.Input.GetPushTos(ctx) {
		if pushTo.Node == dstNode {
			return fmt.Errorf("internal error: packets pushing is already added")
		}
	}

	err := avpipeline.NotifyAboutPacketSources(ctx, nil, fwd.Input)
	if err != nil {
		return fmt.Errorf("internal error: unable to notify about the packet source: %w", err)
	}
	if fwd.AutoFixer == nil {
		fwd.Input.AddPushTo(ctx, dstNode)
		return nil
	}

	fwd.Input.AddPushTo(ctx, fwd.AutoFixerInput)
	fwd.AutoFixer.Output().AddPushTo(ctx, dstNode)
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
		fwd.AutoFixer.Serve(ctx, node.ServeConfig{
			DebugData: fwd,
		}, errCh)
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
		err := fwd.Input.RemovePushTo(ctx, fwd.AutoFixerInput)
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove pushing %s->%s", fwd.Input, fwd.AutoFixer))
		}
		err = fwd.AutoFixer.Output().RemovePushTo(ctx, fwd.outputAsNode())
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove pushing %s->%s", fwd.AutoFixer, fwd.Output))
		}
	} else {
		err := fwd.Input.RemovePushTo(ctx, fwd.outputAsNode())
		if err != nil {
			errs = append(errs, fmt.Errorf("unable to remove pushing %s->%s", fwd.Input, fwd.Output))
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

var (
	_ node.Abstract                       = (*forwarderCopyOutputAsNode[any, processor.Abstract])(nil)
	_ node.DotBlockContentStringWriteToer = (*forwarderCopyOutputAsNode[any, processor.Abstract])(nil)
)

func (fwd *forwarderCopyOutputAsNode[CS, PS]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(fwd)
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

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return fwd.Output.GetPushTos(ctx)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {
	fwd.Output.WithPushTos(ctx, callback)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
	fwd.Output.AddPushTo(ctx, dst, conds...)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetPushTos(
	ctx context.Context,
	pushTos node.PushTos,
) {
	fwd.Output.SetPushTos(ctx, pushTos)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return fwd.Output.RemovePushTo(ctx, dst)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) IsServing() bool {
	return fwd.Output.IsServing()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetProcessor() processor.Abstract {
	return fwd.Output.GetProcessor()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetInputFilter(
	ctx context.Context,
) packetorframefiltercondition.Condition {
	return fwd.Output.GetInputFilter(ctx)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) SetInputFilter(
	ctx context.Context,
	cond packetorframefiltercondition.Condition,
) {
	fwd.Output.SetInputFilter(ctx, cond)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetChangeChanIsServing() <-chan struct{} {
	return fwd.Output.GetChangeChanIsServing()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetChangeChanPushTo() <-chan struct{} {
	return fwd.Output.GetChangeChanPushTo()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetChangeChanDrained() <-chan struct{} {
	return fwd.Output.GetChangeChanDrained()
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) IsDrained(ctx context.Context) bool {
	return (fwd.AutoFixer == nil || fwd.AutoFixer.IsDrained(ctx)) && fwd.Output.IsDrained(ctx)
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) Flush(ctx context.Context) (_err error) {
	logger.Tracef(ctx, "Flush: %v:%p", fwd, fwd)
	defer func() { logger.Tracef(ctx, "/Flush: %v:%p: %v", fwd, fwd, _err) }()

	if fwd.AutoFixer != nil {
		err := fwd.AutoFixer.Flush(ctx)
		if err != nil {
			return fmt.Errorf("unable to flush autofixer: %w", err)
		}
	}
	err := fwd.Output.Flush(ctx)
	if err != nil {
		return fmt.Errorf("unable to flush output: %w", err)
	}
	return nil
}

func (fwd *forwarderCopyOutputAsNode[CS, PS]) GetCountersPtr() *nodetypes.Counters {
	return fwd.Output.GetCountersPtr()
}
