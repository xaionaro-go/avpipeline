package screencapturer

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
)

var _ node.Abstract = (*ScreenCapturer[any])(nil)

func (a *ScreenCapturer[C]) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {
	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		a.InputNode.Serve(ctx, cfg, errCh)
	})

	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		a.DecoderNode.Serve(ctx, cfg, errCh)
	})
}

func (a *ScreenCapturer[C]) String() string {
	return "ScreenCapturer"
}

func (a *ScreenCapturer[C]) DotBlockContentStringWriteTo(
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

	if _, ok := alreadyPrinted[a.Input().GetProcessor()]; !ok {
		fmt.Fprintf(
			w,
			"\tnode_%p [label="+`"%s"`+"]\n",
			any(a.Input().GetProcessor()),
			sanitizeString(a.String()),
		)
		alreadyPrinted[a.Input().GetProcessor()] = struct{}{}
	}

	a.Output().DotBlockContentStringWriteTo(w, alreadyPrinted)
	fmt.Fprintf(w, "\tnode_%p -> node_%p\n", a.Input().GetProcessor(), a.Output().GetProcessor())
}

func (a *ScreenCapturer[C]) GetPushPacketsTos() node.PushPacketsTos {
	return a.Output().GetPushPacketsTos()
}

func (a *ScreenCapturer[C]) AddPushPacketsTo(dst node.Abstract, conds ...packetfiltercondition.Condition) {
	a.Output().AddPushPacketsTo(dst, conds...)
}

func (a *ScreenCapturer[C]) SetPushPacketsTos(pushTos node.PushPacketsTos) {
	a.Output().SetPushPacketsTos(pushTos)
}

func (a *ScreenCapturer[C]) GetPushFramesTos() node.PushFramesTos {
	return a.Output().GetPushFramesTos()
}

func (a *ScreenCapturer[C]) AddPushFramesTo(dst node.Abstract, conds ...framefiltercondition.Condition) {
	a.Output().AddPushFramesTo(dst, conds...)
}

func (a *ScreenCapturer[C]) SetPushFramesTos(pushTos node.PushFramesTos) {
	a.Output().SetPushFramesTos(pushTos)
}

func (a *ScreenCapturer[C]) IsServing() bool {
	if a == nil {
		return false
	}
	return a.Input().IsServing() && a.Output().IsServing()
}

func (a *ScreenCapturer[C]) GetCountersPtr() *nodetypes.Counters {
	inputStats := a.Input().GetCountersPtr()
	outputStats := a.Output().GetCountersPtr()
	return &nodetypes.Counters{
		Missed:   inputStats.Missed,
		Received: inputStats.Received,
		Sent:     outputStats.Sent,
	}
}

func (a *ScreenCapturer[C]) GetProcessor() processor.Abstract {
	return a
}

func (a *ScreenCapturer[C]) GetInputPacketFilter() packetfiltercondition.Condition {
	return a.Input().GetInputPacketFilter()
}

func (a *ScreenCapturer[C]) SetInputPacketFilter(cond packetfiltercondition.Condition) {
	a.Input().SetInputPacketFilter(cond)
}

func (a *ScreenCapturer[C]) GetInputFrameFilter() framefiltercondition.Condition {
	return a.Input().GetInputFrameFilter()
}

func (a *ScreenCapturer[C]) SetInputFrameFilter(cond framefiltercondition.Condition) {
	a.Input().SetInputFrameFilter(cond)
}

func (a *ScreenCapturer[C]) GetChangeChanIsServing() <-chan struct{} {
	return a.Input().GetChangeChanIsServing()
}

func (a *ScreenCapturer[C]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return a.Output().GetChangeChanPushPacketsTo()
}

func (a *ScreenCapturer[C]) GetChangeChanPushFramesTo() <-chan struct{} {
	return a.Output().GetChangeChanPushFramesTo()
}

func (a *ScreenCapturer[C]) GetChangeChanDrained() <-chan struct{} {
	return node.CombineGetChangeChanDrained(context.Background(), a.InputNode, a.DecoderNode)
}

func (a *ScreenCapturer[C]) IsDrained(ctx context.Context) bool {
	return node.CombineIsDrained(ctx, a.InputNode, a.DecoderNode)
}
