package node

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/facebookincubator/go-belt/tool/logger"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/kernel"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

type Abstract interface {
	Serve(context.Context, ServeConfig, chan<- Error)

	GetPushPacketsTos() PushPacketsTos
	AddPushPacketsTo(dst Abstract, conds ...packetcondition.Condition)
	SetPushPacketsTos(PushPacketsTos)
	GetPushFramesTos() PushFramesTos
	AddPushFramesTo(dst Abstract, conds ...framecondition.Condition)
	SetPushFramesTos(PushFramesTos)

	GetStatistics() *NodeStatistics
	GetProcessor() processor.Abstract

	GetInputPacketCondition() packetcondition.Condition
	SetInputPacketCondition(packetcondition.Condition)
	GetInputFrameCondition() framecondition.Condition
	SetInputFrameCondition(framecondition.Condition)
}

type NodeWithCustomData[C any, T processor.Abstract] struct {
	*NodeStatistics
	Processor            T
	PushPacketsTos       PushPacketsTos
	PushFramesTos        PushFramesTos
	InputPacketCondition packetcondition.Condition
	InputFrameCondition  framecondition.Condition
	Locker               xsync.Mutex
	IsServing            bool

	CustomData C
}

type Node[T processor.Abstract] = NodeWithCustomData[struct{}, T]

var _ Abstract = (*Node[processor.Abstract])(nil)

func New[T processor.Abstract](processor T) *Node[T] {
	return NewWithCustomData[struct{}](processor)
}

func NewFromKernel[T kernel.Abstract](
	ctx context.Context,
	kernel T,
	opts ...processor.Option,
) *Node[*processor.FromKernel[T]] {
	return NewWithCustomDataFromKernel[struct{}](ctx, kernel, opts...)
}

func NewWithCustomData[C any, T processor.Abstract](
	processor T,
) *NodeWithCustomData[C, T] {
	return &NodeWithCustomData[C, T]{
		NodeStatistics: &NodeStatistics{},
		Processor:      processor,
	}
}

func NewWithCustomDataFromKernel[C any, T kernel.Abstract](
	ctx context.Context,
	kernel T,
	opts ...processor.Option,
) *NodeWithCustomData[C, *processor.FromKernel[T]] {
	return NewWithCustomData[C](
		processor.NewFromKernel(
			ctx,
			kernel,
			opts...,
		),
	)
}

func (n *NodeWithCustomData[C, T]) GetStatistics() *NodeStatistics {
	return xsync.DoR1(context.TODO(), &n.Locker, func() *NodeStatistics {
		return n.NodeStatistics
	})
}

func (n *NodeWithCustomData[C, T]) GetProcessor() processor.Abstract {
	if n == nil {
		return nil
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() processor.Abstract {
		return n.Processor
	})
}

func (n *NodeWithCustomData[C, T]) GetPushPacketsTos() PushPacketsTos {
	if n == nil {
		return nil
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() PushPacketsTos {
		return n.PushPacketsTos
	})
}

func (n *NodeWithCustomData[C, T]) AddPushPacketsTo(
	dst Abstract,
	conds ...packetcondition.Condition,
) {
	ctx := context.TODO()
	logger.Debugf(ctx, "AddPushPacketsTo")
	defer logger.Debugf(ctx, "/AddPushPacketsTo")
	n.Locker.Do(ctx, func() {
		n.PushPacketsTos.Add(dst, conds...)
	})
}

func (n *NodeWithCustomData[C, T]) SetPushPacketsTos(s PushPacketsTos) {
	n.Locker.Do(context.TODO(), func() {
		n.PushPacketsTos = s
	})
}

func (n *NodeWithCustomData[C, T]) GetPushFramesTos() PushFramesTos {
	if n == nil {
		return nil
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() PushFramesTos {
		return n.PushFramesTos
	})
}

func RemovePushPacketsTo[C any, P processor.Abstract](
	ctx context.Context,
	from *NodeWithCustomData[C, P],
	to Abstract,
) (_err error) {
	logger.Debugf(ctx, "RemovePushPacketsTo")
	defer func() { defer logger.Debugf(ctx, "/RemovePushPacketsTo: %v", _err) }()

	return xsync.DoR1(ctx, &from.Locker, func() error {
		pushTos := from.PushPacketsTos
		for idx, pushTo := range pushTos {
			if pushTo.Node == to {
				pushTos = slices.Delete(pushTos, idx, idx+1)
				from.PushPacketsTos = pushTos
				return nil
			}
		}
		return fmt.Errorf("%s does not push packets to %s", from, to)
	})
}

func RemovePushFramesTo[C any, P processor.Abstract](
	ctx context.Context,
	from *NodeWithCustomData[C, P],
	to Abstract,
) (_err error) {
	logger.Debugf(ctx, "RemovePushFramesTo")
	defer func() { defer logger.Debugf(ctx, "/RemovePushFramesTo: %v", _err) }()

	return xsync.DoR1(ctx, &from.Locker, func() error {
		pushTos := from.PushFramesTos
		for idx, pushTo := range pushTos {
			if pushTo.Node == to {
				pushTos = slices.Delete(pushTos, idx, idx+1)
				from.PushFramesTos = pushTos
				return nil
			}
		}
		return fmt.Errorf("%s does not push frames to %s", from, to)
	})
}

func (n *NodeWithCustomData[C, T]) AddPushFramesTo(
	dst Abstract,
	conds ...framecondition.Condition,
) {
	n.Locker.Do(context.TODO(), func() {
		n.PushFramesTos.Add(dst, conds...)
	})
}

func (n *NodeWithCustomData[C, T]) SetPushFramesTos(s PushFramesTos) {
	n.Locker.Do(context.TODO(), func() {
		n.PushFramesTos = s
	})
}

func (n *NodeWithCustomData[C, T]) GetInputPacketCondition() packetcondition.Condition {
	if n == nil {
		return packetcondition.Static(false)
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() packetcondition.Condition {
		return n.InputPacketCondition
	})
}

func (n *NodeWithCustomData[C, T]) SetInputPacketCondition(cond packetcondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputPacketCondition = cond
	})
}

func (n *NodeWithCustomData[C, T]) GetInputFrameCondition() framecondition.Condition {
	if n == nil {
		return framecondition.Static(false)
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() framecondition.Condition {
		return n.InputFrameCondition
	})
}

func (n *NodeWithCustomData[C, T]) SetInputFrameCondition(cond framecondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputFrameCondition = cond
	})
}

func (n *NodeWithCustomData[C, T]) String() string {
	return Nodes[*NodeWithCustomData[C, T]]{n}.String()
}

func (n *NodeWithCustomData[C, T]) DotString(withStats bool) string {
	return Nodes[*NodeWithCustomData[C, T]]{n}.DotString(withStats)
}

func (n *NodeWithCustomData[C, T]) DotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	n.dotBlockContentStringWriteTo(w, alreadyPrinted)
}

func (n *NodeWithCustomData[C, T]) dotBlockContentStringWriteTo(
	w io.Writer,
	alreadyPrinted map[processor.Abstract]struct{},
) {
	if n == nil {
		return
	}
	sanitizeString := func(s string) string {
		s = strings.ReplaceAll(s, `"`, ``)
		s = strings.ReplaceAll(s, "\n", `\n`)
		s = strings.ReplaceAll(s, "\t", ``)
		return s
	}

	if _, ok := alreadyPrinted[n.Processor]; !ok {
		fmt.Fprintf(
			w,
			"\tnode_%p [label="+`"%s"`+"]\n",
			any(n.Processor),
			sanitizeString(n.Processor.String()),
		)
		alreadyPrinted[n.Processor] = struct{}{}
	}
	for _, pushTo := range n.PushPacketsTos {
		writer, ok := pushTo.Node.(DotBlockContentStringWriteToer)
		if !ok {
			continue
		}
		writer.DotBlockContentStringWriteTo(w, alreadyPrinted)
		if pushTo.Condition == nil {
			fmt.Fprintf(w, "\tnode_%p -> node_%p\n", any(n.Processor), pushTo.Node.GetProcessor())
			continue
		}
		fmt.Fprintf(
			w,
			"\tnode_%p -> node_%p [label="+`"%s"`+"]\n",
			any(n.Processor),
			pushTo.Node.GetProcessor(),
			sanitizeString(n.Processor.String()),
		)
	}
	for _, pushTo := range n.PushFramesTos {
		writer, ok := pushTo.Node.(DotBlockContentStringWriteToer)
		if !ok {
			continue
		}
		writer.DotBlockContentStringWriteTo(w, alreadyPrinted)
		if pushTo.Condition == nil {
			fmt.Fprintf(w, "\tnode_%p -> node_%p\n", any(n.Processor), pushTo.Node.GetProcessor())
			continue
		}
		fmt.Fprintf(
			w,
			"\tnode_%p -> node_%p [label="+`"%s"`+"]\n",
			any(n.Processor),
			pushTo.Node.GetProcessor(),
			sanitizeString(n.Processor.String()),
		)
	}
}
