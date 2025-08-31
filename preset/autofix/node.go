package autofix

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
)

var _ node.Abstract = (*AutoFixerWithCustomData[struct{}])(nil)

func (a *AutoFixerWithCustomData[T]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	var wg sync.WaitGroup
	defer wg.Wait()

	if a.AutoHeadersNode != nil {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			a.AutoHeadersNode.Serve(ctx, cfg, errCh)
		})
	}
	if a.MapStreamIndicesNode != nil {
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			a.MapStreamIndicesNode.Serve(ctx, cfg, errCh)
		})
	}
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

func (a *AutoFixerWithCustomData[T]) GetPushPacketsTos() node.PushPacketsTos {
	return a.Output().GetPushPacketsTos()
}

func (a *AutoFixerWithCustomData[T]) AddPushPacketsTo(dst node.Abstract, conds ...packetfiltercondition.Condition) {
	a.Output().AddPushPacketsTo(dst, conds...)
}

func (a *AutoFixerWithCustomData[T]) SetPushPacketsTos(pushTos node.PushPacketsTos) {
	a.Output().SetPushPacketsTos(pushTos)
}

func (a *AutoFixerWithCustomData[T]) GetPushFramesTos() node.PushFramesTos {
	return a.Output().GetPushFramesTos()
}

func (a *AutoFixerWithCustomData[T]) AddPushFramesTo(dst node.Abstract, conds ...framefiltercondition.Condition) {
	a.Output().AddPushFramesTo(dst, conds...)
}

func (a *AutoFixerWithCustomData[T]) SetPushFramesTos(pushTos node.PushFramesTos) {
	a.Output().SetPushFramesTos(pushTos)
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
		Missed:   inputStats.Missed,
		Received: inputStats.Received,
		Sent:     outputStats.Sent,
	}
}

func (a *AutoFixerWithCustomData[T]) GetProcessor() processor.Abstract {
	return a
}

func (a *AutoFixerWithCustomData[T]) GetInputPacketFilter() packetfiltercondition.Condition {
	return a.Input().GetInputPacketFilter()
}

func (a *AutoFixerWithCustomData[T]) SetInputPacketFilter(cond packetfiltercondition.Condition) {
	a.Input().SetInputPacketFilter(cond)
}

func (a *AutoFixerWithCustomData[T]) GetInputFrameFilter() framefiltercondition.Condition {
	return a.Input().GetInputFrameFilter()
}

func (a *AutoFixerWithCustomData[T]) SetInputFrameFilter(cond framefiltercondition.Condition) {
	a.Input().SetInputFrameFilter(cond)
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
