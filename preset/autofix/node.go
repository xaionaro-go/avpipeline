package autofix

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

var _ node.Abstract = (*AutoFixerWithCustomData[struct{}])(nil)

func (a *AutoFixerWithCustomData[T]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	logger.Tracef(ctx, "AutoFixer[%p: %p, %p]: Serve: %s", a, a.AutoHeadersNode, a.MapStreamIndicesNode, debug.Stack())
	defer logger.Tracef(ctx, "AutoFixer[%p: %p, %p]: /Serve", a, a.AutoHeadersNode, a.MapStreamIndicesNode)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	var wg sync.WaitGroup
	defer wg.Wait()

	if a.AutoHeadersNode != nil { // TODO: this block is not thread-safe, fix
		if a.AutoHeadersNode.IsServing() {
			logger.Errorf(ctx, "AutoHeadersNode[%p] is already serving", a.AutoHeadersNode)
			errCh <- node.Error{
				Node: a.AutoHeadersNode,
				Err: fmt.Errorf("%w: %s", node.ErrAlreadyStarted{
					PreviousDebugData: a.AutoHeadersNode.ServeDebugData,
				}, debug.Stack()),
			}
			cancelFn()
			return
		}
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			a.AutoHeadersNode.Serve(ctx, cfg, errCh)
		})
	}
	if a.MapStreamIndicesNode != nil { // TODO: this block is not thread-safe, fix
		if a.MapStreamIndicesNode.IsServing() {
			logger.Errorf(ctx, "MapStreamIndicesNode[%p] is already serving", a.MapStreamIndicesNode)
			errCh <- node.Error{
				Node: a.MapStreamIndicesNode,
				Err: fmt.Errorf("%w: %s", node.ErrAlreadyStarted{
					PreviousDebugData: a.MapStreamIndicesNode.ServeDebugData,
				}, debug.Stack()),
			}
			cancelFn()
			return
		}
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			a.MapStreamIndicesNode.Serve(ctx, cfg, errCh)
		})
	}
}

func (a *AutoFixerWithCustomData[T]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(a)
}

func (a *AutoFixerWithCustomData[T]) String() string {
	return "AutoFixer"
}

func (a *AutoFixerWithCustomData[T]) DotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	if a == nil {
		return
	}
	sanitizeString := func(s string) string {
		s = strings.ReplaceAll(s, `"`, ``)
		s = strings.ReplaceAll(s, "\n", `\n`)
		s = strings.ReplaceAll(s, "\t", ``)
		return s
	}

	if _, ok := alreadyPrinted[a.GetProcessor()]; !ok {
		fmt.Fprintf(
			w,
			"\tnode_%p [label="+`"%s"`+"]\n",
			any(a.GetProcessor()),
			sanitizeString(a.String()),
		)
		alreadyPrinted[a.GetProcessor()] = struct{}{}
	}

	a.Output().DotBlockContentStringWriteTo(w, alreadyPrinted)
	fmt.Fprintf(w, "\tnode_%p -> node_%p\n", a.GetProcessor(), a.Output().GetProcessor())
}

func (a *AutoFixerWithCustomData[T]) GetPushPacketsTos(
	ctx context.Context,
) node.PushPacketsTos {
	return a.Output().GetPushPacketsTos(ctx)
}

func (a *AutoFixerWithCustomData[T]) WithPushPacketsTos(
	ctx context.Context,
	callback func(context.Context, *node.PushPacketsTos),
) {
	a.Output().WithPushPacketsTos(ctx, callback)
}

func (a *AutoFixerWithCustomData[T]) AddPushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetfiltercondition.Condition,
) {
	a.Output().AddPushPacketsTo(ctx, dst, conds...)
}

func (a *AutoFixerWithCustomData[T]) SetPushPacketsTos(
	ctx context.Context,
	pushTos node.PushPacketsTos,
) {
	a.Output().SetPushPacketsTos(ctx, pushTos)
}

func (a *AutoFixerWithCustomData[T]) RemovePushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return a.Output().RemovePushPacketsTo(ctx, dst)
}

func (a *AutoFixerWithCustomData[T]) GetPushFramesTos(
	ctx context.Context,
) node.PushFramesTos {
	return a.Output().GetPushFramesTos(ctx)
}

func (a *AutoFixerWithCustomData[T]) WithPushFramesTos(
	ctx context.Context,
	callback func(context.Context, *node.PushFramesTos),
) {
	a.Output().WithPushFramesTos(ctx, callback)
}

func (a *AutoFixerWithCustomData[T]) AddPushFramesTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...framefiltercondition.Condition,
) {
	a.Output().AddPushFramesTo(ctx, dst, conds...)
}

func (a *AutoFixerWithCustomData[T]) SetPushFramesTos(
	ctx context.Context,
	pushTos node.PushFramesTos,
) {
	a.Output().SetPushFramesTos(ctx, pushTos)
}

func (a *AutoFixerWithCustomData[T]) RemovePushFramesTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return a.Output().RemovePushFramesTo(ctx, dst)
}

func (a *AutoFixerWithCustomData[T]) IsServing() bool {
	if a == nil {
		return false
	}
	return a.Input().IsServing() && a.Output().IsServing()
}

func (a *AutoFixerWithCustomData[T]) GetCountersPtr() *nodetypes.Counters {
	inputStats := a.Input().GetCountersPtr()
	outputStats := a.Output().GetCountersPtr()
	return &nodetypes.Counters{
		Addressed: inputStats.Addressed,
		Missed:    inputStats.Missed,
		Received:  inputStats.Received,
		Sent:      outputStats.Sent,
	}
}

func (a *AutoFixerWithCustomData[T]) GetProcessor() processor.Abstract {
	return a
}

func (a *AutoFixerWithCustomData[T]) GetInputPacketFilter(
	ctx context.Context,
) packetfiltercondition.Condition {
	return a.Input().GetInputPacketFilter(ctx)
}

func (a *AutoFixerWithCustomData[T]) SetInputPacketFilter(
	ctx context.Context,
	cond packetfiltercondition.Condition,
) {
	a.Input().SetInputPacketFilter(ctx, cond)
}

func (a *AutoFixerWithCustomData[T]) GetInputFrameFilter(
	ctx context.Context,
) framefiltercondition.Condition {
	return a.Input().GetInputFrameFilter(ctx)
}

func (a *AutoFixerWithCustomData[T]) SetInputFrameFilter(
	ctx context.Context,
	cond framefiltercondition.Condition,
) {
	a.Input().SetInputFrameFilter(ctx, cond)
}

func (a *AutoFixerWithCustomData[T]) GetChangeChanIsServing() <-chan struct{} {
	return a.Input().GetChangeChanIsServing()
}

func (a *AutoFixerWithCustomData[T]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return a.Output().GetChangeChanPushPacketsTo()
}

func (a *AutoFixerWithCustomData[T]) GetChangeChanPushFramesTo() <-chan struct{} {
	return a.Output().GetChangeChanPushFramesTo()
}

func (a *AutoFixerWithCustomData[T]) GetChangeChanDrained() <-chan struct{} {
	logger.Tracef(context.Background(), "GetChangeChanDrained")
	return node.CombineGetChangeChanDrained(
		context.Background(),
		a.MapStreamIndicesNode,
		a.AutoHeadersNode,
	)
}

func (a *AutoFixerWithCustomData[T]) IsDrained(ctx context.Context) bool {
	return node.CombineIsDrained(ctx, a.MapStreamIndicesNode, a.AutoHeadersNode)
}
