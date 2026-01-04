// node.go implements the node interface for the ScreenCapturer preset.

package screencapturer

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
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

func (a *ScreenCapturer[C]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(a)
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

func (a *ScreenCapturer[C]) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return a.Output().GetPushTos(ctx)
}

func (a *ScreenCapturer[C]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {
	a.Output().WithPushTos(ctx, callback)
}

func (a *ScreenCapturer[C]) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
	a.Output().AddPushTo(ctx, dst, conds...)
}

func (a *ScreenCapturer[C]) SetPushTos(
	ctx context.Context,
	pushTos node.PushTos,
) {
	a.Output().SetPushTos(ctx, pushTos)
}

func (a *ScreenCapturer[C]) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return a.Output().RemovePushTo(ctx, dst)
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
		Addressed: inputStats.Addressed,
		Missed:    inputStats.Missed,
		Received:  inputStats.Received,
		Sent:      outputStats.Sent,
	}
}

func (a *ScreenCapturer[C]) GetProcessor() processor.Abstract {
	return a
}

func (a *ScreenCapturer[C]) GetInputFilter(
	ctx context.Context,
) packetorframefiltercondition.Condition {
	return a.Input().GetInputFilter(ctx)
}

func (a *ScreenCapturer[C]) SetInputFilter(
	ctx context.Context,
	cond packetorframefiltercondition.Condition,
) {
	a.Input().SetInputFilter(ctx, cond)
}

func (a *ScreenCapturer[C]) GetChangeChanIsServing() <-chan struct{} {
	return a.Input().GetChangeChanIsServing()
}

func (a *ScreenCapturer[C]) GetChangeChanPushTo() <-chan struct{} {
	return a.Output().GetChangeChanPushTo()
}

func (a *ScreenCapturer[C]) GetChangeChanDrained() <-chan struct{} {
	return node.CombineGetChangeChanDrained(context.Background(), a.InputNode, a.DecoderNode)
}

func (a *ScreenCapturer[C]) IsDrained(ctx context.Context) bool {
	return node.CombineIsDrained(ctx, a.InputNode, a.DecoderNode)
}
