package node

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

type Abstract interface {
	Serve(context.Context, ServeConfig, chan<- Error)

	IsServing() bool

	GetPushPacketsTos() PushPacketsTos
	AddPushPacketsTo(dst Abstract, conds ...packetfiltercondition.Condition)
	SetPushPacketsTos(PushPacketsTos)
	GetPushFramesTos() PushFramesTos
	AddPushFramesTo(dst Abstract, conds ...framefiltercondition.Condition)
	SetPushFramesTos(PushFramesTos)

	GetStatistics() *Statistics
	GetProcessor() processor.Abstract

	GetInputPacketFilter() packetfiltercondition.Condition
	SetInputPacketFilter(packetfiltercondition.Condition)
	GetInputFrameFilter() framefiltercondition.Condition
	SetInputFrameFilter(framefiltercondition.Condition)

	GetChangeChanPushPacketsTo() <-chan struct{}
	GetChangeChanPushFramesTo() <-chan struct{}
}

/* for easy copy-paste

func (n *MyFancyNodePlaceholder) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {

}

func (n *MyFancyNodePlaceholder) String() string {
	return "MyFancyNodePlaceholder"
}

func (n *MyFancyNodePlaceholder) IsServing() bool {

}

func (n *MyFancyNodePlaceholder) GetPushPacketsTos() node.PushPacketsTos {

}

func (n *MyFancyNodePlaceholder) AddPushPacketsTo(
	dst node.Abstract,
	conds ...packetfiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) SetPushPacketsTos(
	v node.PushPacketsTos,
) {

}

func (n *MyFancyNodePlaceholder) GetPushFramesTos() node.PushFramesTos {

}

func (n *MyFancyNodePlaceholder) AddPushFramesTo(
	dst node.Abstract,
	conds ...framefiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) SetPushFramesTos(
	v node.PushFramesTos,
) {

}

func (n *MyFancyNodePlaceholder) GetStatistics() *node.Statistics {

}

func (n *MyFancyNodePlaceholder) GetProcessor() processor.Abstract {

}

func (n *MyFancyNodePlaceholder) GetInputPacketFilter() packetfiltercondition.Condition {

}

func (n *MyFancyNodePlaceholder) SetInputPacketFilter(
	cond packetfiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) GetInputFrameFilter() framefiltercondition.Condition {

}

func (n *MyFancyNodePlaceholder) SetInputFrameFilter(
	cond framefiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) GetChangeChanPushPacketsTo() <-chan struct{} {

}

func (n *MyFancyNodePlaceholder) GetChangeChanPushFramesTo() <-chan struct{} {

}

*/

type NodeWithCustomData[C any, T processor.Abstract] struct {
	*Statistics
	Processor         T
	PushPacketsTos    PushPacketsTos
	PushFramesTos     PushFramesTos
	InputPacketFilter packetfiltercondition.Condition
	InputFrameFilter  framefiltercondition.Condition
	Locker            xsync.Mutex
	IsServingValue    bool

	ChangeChanPushPacketsTo *chan struct{}
	ChangeChanPushFramesTo  *chan struct{}

	CustomData C
}

type Node[T processor.Abstract] = NodeWithCustomData[struct{}, T]

var _ Abstract = (*Node[processor.Abstract])(nil)
var _ DotBlockContentStringWriteToer = (*Node[processor.Abstract])(nil)

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
		Statistics:              &Statistics{},
		Processor:               processor,
		ChangeChanPushPacketsTo: ptr(make(chan struct{})),
		ChangeChanPushFramesTo:  ptr(make(chan struct{})),
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

func (n *NodeWithCustomData[C, T]) IsServing() bool {
	if n == nil {
		return false
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() bool {
		return n.IsServingValue
	})
}

func (n *NodeWithCustomData[C, T]) GetStatistics() *Statistics {
	return xsync.DoR1(context.TODO(), &n.Locker, func() *Statistics {
		return n.Statistics
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
	conds ...packetfiltercondition.Condition,
) {
	ctx := context.TODO()
	logger.Debugf(ctx, "AddPushPacketsTo")
	defer logger.Debugf(ctx, "/AddPushPacketsTo")
	n.Locker.Do(ctx, func() {
		n.PushPacketsTos.Add(dst, conds...)
		close(*xatomic.SwapPointer(&n.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))
	})
}

func (n *NodeWithCustomData[C, T]) SetPushPacketsTos(s PushPacketsTos) {
	ctx := context.TODO()
	logger.Tracef(ctx, "SetPushPacketsTos: %s", debug.Stack())
	n.Locker.Do(context.TODO(), func() {
		n.PushPacketsTos = s
		close(*xatomic.SwapPointer(&n.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))
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
	logger.Tracef(ctx, "RemovePushPacketsTo: %s", debug.Stack())

	if !xsync.DoR1(ctx, &from.Locker, func() bool {
		pushTos := from.PushPacketsTos
		for idx, pushTo := range pushTos {
			if pushTo.Node == to {
				pushTos = slices.Delete(pushTos, idx, idx+1)
				from.PushPacketsTos = pushTos
				close(*xatomic.SwapPointer(&from.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))
				return true
			}
		}
		return false
	}) {
		return fmt.Errorf("%s does not push packets to %s", from, to)
	}
	return nil
}

func RemovePushFramesTo[C any, P processor.Abstract](
	ctx context.Context,
	from *NodeWithCustomData[C, P],
	to Abstract,
) (_err error) {
	logger.Debugf(ctx, "RemovePushFramesTo")
	defer func() { defer logger.Debugf(ctx, "/RemovePushFramesTo: %v", _err) }()
	logger.Tracef(ctx, "RemovePushFramesTo: %s", debug.Stack())

	return xsync.DoR1(ctx, &from.Locker, func() error {
		pushTos := from.PushFramesTos
		for idx, pushTo := range pushTos {
			if pushTo.Node == to {
				pushTos = slices.Delete(pushTos, idx, idx+1)
				from.PushFramesTos = pushTos
				close(*xatomic.SwapPointer(&from.ChangeChanPushFramesTo, ptr(make(chan struct{}))))
				return nil
			}
		}
		return fmt.Errorf("%s does not push frames to %s", from, to)
	})
}

func (n *NodeWithCustomData[C, T]) AddPushFramesTo(
	dst Abstract,
	conds ...framefiltercondition.Condition,
) {
	n.Locker.Do(context.TODO(), func() {
		n.PushFramesTos.Add(dst, conds...)
		close(*xatomic.SwapPointer(&n.ChangeChanPushFramesTo, ptr(make(chan struct{}))))
	})
}

func (n *NodeWithCustomData[C, T]) SetPushFramesTos(s PushFramesTos) {
	ctx := context.TODO()
	logger.Tracef(ctx, "SetPushFramesTos: %s", debug.Stack())
	n.Locker.Do(context.TODO(), func() {
		n.PushFramesTos = s
		close(*xatomic.SwapPointer(&n.ChangeChanPushFramesTo, ptr(make(chan struct{}))))
	})
}

func (n *NodeWithCustomData[C, T]) GetInputPacketFilter() packetfiltercondition.Condition {
	if n == nil {
		return packetfiltercondition.Static(false)
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() packetfiltercondition.Condition {
		return n.InputPacketFilter
	})
}

func (n *NodeWithCustomData[C, T]) SetInputPacketFilter(cond packetfiltercondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputPacketFilter = cond
	})
}

func (n *NodeWithCustomData[C, T]) GetInputFrameFilter() framefiltercondition.Condition {
	if n == nil {
		return framefiltercondition.Static(false)
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() framefiltercondition.Condition {
		return n.InputFrameFilter
	})
}

func (n *NodeWithCustomData[C, T]) SetInputFrameFilter(cond framefiltercondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputFrameFilter = cond
	})
}

func (n *NodeWithCustomData[C, T]) String() string {
	nodeString := Nodes[*NodeWithCustomData[C, T]]{n}.String()
	cdStringer, ok := any(n.CustomData).(fmt.Stringer)
	if ok {
		return fmt.Sprintf("%s:%s", nodeString, cdStringer)
	}
	return nodeString
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

	type connectionKey struct {
		From processor.Abstract
		To   processor.Abstract
	}
	connectionIsAlreadyPrint := map[connectionKey]struct{}{}

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
		key := connectionKey{
			From: n.Processor,
			To:   pushTo.Node.GetProcessor(),
		}
		if _, ok := connectionIsAlreadyPrint[key]; ok {
			continue
		}
		connectionIsAlreadyPrint[key] = struct{}{}

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
		key := connectionKey{
			From: n.Processor,
			To:   pushTo.Node.GetProcessor(),
		}
		if _, ok := connectionIsAlreadyPrint[key]; ok {
			continue
		}
		connectionIsAlreadyPrint[key] = struct{}{}

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

func (n *NodeWithCustomData[C, T]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanPushPacketsTo)
}

func (n *NodeWithCustomData[C, T]) GetChangeChanPushFramesTo() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanPushFramesTo)
}
