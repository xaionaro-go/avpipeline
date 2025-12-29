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
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
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

func (a *AutoFixerWithCustomData[T]) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return a.Output().GetPushTos(ctx)
}

func (a *AutoFixerWithCustomData[T]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {
	a.Output().WithPushTos(ctx, callback)
}

func (a *AutoFixerWithCustomData[T]) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
	a.Output().AddPushTo(ctx, dst, conds...)
}

func (a *AutoFixerWithCustomData[T]) SetPushTos(
	ctx context.Context,
	pushTos node.PushTos,
) {
	a.Output().SetPushTos(ctx, pushTos)
}

func (a *AutoFixerWithCustomData[T]) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return a.Output().RemovePushTo(ctx, dst)
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

func (a *AutoFixerWithCustomData[T]) GetInputFilter(
	ctx context.Context,
) packetorframefiltercondition.Condition {
	return a.Input().GetInputFilter(ctx)
}

func (a *AutoFixerWithCustomData[T]) SetInputFilter(
	ctx context.Context,
	cond packetorframefiltercondition.Condition,
) {
	a.Input().SetInputFilter(ctx, cond)
}

func (a *AutoFixerWithCustomData[T]) GetChangeChanIsServing() <-chan struct{} {
	return a.Input().GetChangeChanIsServing()
}

func (a *AutoFixerWithCustomData[T]) GetChangeChanPushTo() <-chan struct{} {
	return a.Output().GetChangeChanPushTo()
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
