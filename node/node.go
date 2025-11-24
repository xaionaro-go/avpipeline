package node

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/go-ng/xatomic"
	"github.com/samber/lo"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type Abstract interface {
	fmt.Stringer

	Serve(context.Context, ServeConfig, chan<- Error)

	GetObjectID() globaltypes.ObjectID
	IsServing() bool

	GetPushPacketsTos(context.Context) PushPacketsTos
	AddPushPacketsTo(context.Context, Abstract, ...packetfiltercondition.Condition)
	SetPushPacketsTos(context.Context, PushPacketsTos)
	WithPushPacketsTos(context.Context, func(context.Context, *PushPacketsTos))
	RemovePushPacketsTo(context.Context, Abstract) error

	GetPushFramesTos(context.Context) PushFramesTos
	AddPushFramesTo(context.Context, Abstract, ...framefiltercondition.Condition)
	SetPushFramesTos(context.Context, PushFramesTos)
	WithPushFramesTos(context.Context, func(context.Context, *PushFramesTos))
	RemovePushFramesTo(context.Context, Abstract) error

	GetProcessor() processor.Abstract
	GetCountersPtr() *types.Counters

	GetInputPacketFilter() packetfiltercondition.Condition
	SetInputPacketFilter(packetfiltercondition.Condition)
	GetInputFrameFilter() framefiltercondition.Condition
	SetInputFrameFilter(framefiltercondition.Condition)

	GetChangeChanIsServing() <-chan struct{}
	GetChangeChanPushPacketsTo() <-chan struct{}
	GetChangeChanPushFramesTo() <-chan struct{}

	GetChangeChanDrained() <-chan struct{}
	IsDrained(context.Context) bool
	Flush(context.Context) error
}

type Pointer[T any] interface {
	Abstract
	*T
}

/* for easy copy-paste

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func (n *MyFancyNodePlaceholder) Serve(
	ctx context.Context,
	cfg node.ServeConfig,
	errCh chan<- node.Error,
) {

}

func (n *MyFancyNodePlaceholder) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(n)
}

func (n *MyFancyNodePlaceholder) String() string {
	return "MyFancyNodePlaceholder"
}

func (n *MyFancyNodePlaceholder) IsServing() bool {

}

func (n *MyFancyNodePlaceholder) GetPushPacketsTos(
	ctx context.Context,
) node.PushPacketsTos {
}

func (n *MyFancyNodePlaceholder) WithPushPacketsTos(
	ctx context.Context,
	callback func(context.Context, *node.PushPacketsTos),
) {

}

func (n *MyFancyNodePlaceholder) AddPushPacketsTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetfiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) SetPushPacketsTos(
	ctx context.Context,
	v node.PushPacketsTos,
) {

}

func (n *MyFancyNodePlaceholder) GetPushFramesTos(
	ctx context.Context,
) node.PushFramesTos {
}

func (n *MyFancyNodePlaceholder) WithPushFramesTos(
	ctx context.Context,
	callback func(context.Context, *node.PushFramesTos),
) {

}


func (n *MyFancyNodePlaceholder) AddPushFramesTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...framefiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) SetPushFramesTos(
	ctx context.Context,
	v node.PushFramesTos,
) {

}

func (n *MyFancyNodePlaceholder) GetCountersPtr() *nodetypes.Counters {

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

func (n *MyFancyNodePlaceholder) GetChangeChanIsServing() <-chan struct{} {

}

func (n *MyFancyNodePlaceholder) GetChangeChanPushPacketsTo() <-chan struct{} {

}

func (n *MyFancyNodePlaceholder) GetChangeChanPushFramesTo() <-chan struct{} {

}

func (n *MyFancyNodePlaceholder) GetChangeChanDrained() <-chan struct{} {

}

func (n *MyFancyNodePlaceholder) IsDrained(
	ctx context.Context,
) bool {

}

func (n *MyFancyNodePlaceholder) Flush(
	ctx context.Context,
) error {

}

*/

type GetCustomDataer[C any] interface {
	GetCustomData() C
}

type NodeWithCustomData[C any, T processor.Abstract] struct {
	Processor         T
	Counters          *types.Counters
	PushPacketsTos    PushPacketsTos
	PushFramesTos     PushFramesTos
	InputPacketFilter packetfiltercondition.Condition
	InputFrameFilter  framefiltercondition.Condition
	Locker            xsync.Mutex
	ServeDebugData    any
	IsServingValue    bool
	IsDrainedValue    atomic.Bool
	IsProcessorDirty  bool
	Config            Config

	ChangeChanIsServing     *chan struct{}
	ChangeChanPushPacketsTo *chan struct{}
	ChangeChanPushFramesTo  *chan struct{}
	ChangeChanDrained       *chan struct{}

	CustomData C
}

type Node[T processor.Abstract] = NodeWithCustomData[struct{}, T]

var _ Abstract = (*Node[processor.Abstract])(nil)
var _ DotBlockContentStringWriteToer = (*Node[processor.Abstract])(nil)
var _ GetCustomDataer[struct{}] = (*Node[processor.Abstract])(nil)

func New[T processor.Abstract](
	processor T,
	opts ...Option,
) *Node[T] {
	return NewWithCustomData[struct{}](processor, opts...)
}

func NewFromKernel[T kernel.Abstract](
	ctx context.Context,
	kernel T,
	procOpts ...processor.Option,
) *Node[*processor.FromKernel[T]] {
	return NewWithCustomDataFromKernel[struct{}](ctx, kernel, procOpts...)
}

func NewWithCustomData[C any, T processor.Abstract](
	processor T,
	opts ...Option,
) *NodeWithCustomData[C, T] {
	n := &NodeWithCustomData[C, T]{
		Processor:               processor,
		ChangeChanIsServing:     ptr(make(chan struct{})),
		ChangeChanPushPacketsTo: ptr(make(chan struct{})),
		ChangeChanPushFramesTo:  ptr(make(chan struct{})),
		ChangeChanDrained:       ptr(make(chan struct{})),
		Config:                  Options(opts).config(),
		Counters:                types.NewCounters(),
	}
	n.IsDrainedValue.Store(true)
	return n
}

func NewWithCustomDataFromKernel[C any, T kernel.Abstract](
	ctx context.Context,
	kernel T,
	procOpts ...processor.Option,
) *NodeWithCustomData[C, *processor.FromKernel[T]] {
	return NewWithCustomData[C](
		processor.NewFromKernel(
			ctx,
			kernel,
			procOpts...,
		),
	)
}

func (n *NodeWithCustomData[C, T]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(n)
}

func (n *NodeWithCustomData[C, T]) GetCustomData() C {
	return n.CustomData
}

func (n *NodeWithCustomData[C, T]) SetCustomData(v C) {
	n.CustomData = v
}

func (n *NodeWithCustomData[C, T]) IsServing() bool {
	if n == nil {
		return false
	}
	return xsync.DoR1(context.TODO(), &n.Locker, func() bool {
		return n.IsServingValue
	})
}

func (n *NodeWithCustomData[C, T]) GetProcessor() processor.Abstract {
	if n == nil {
		return nil
	}
	return n.Processor
}

func (n *NodeWithCustomData[C, T]) GetPushPacketsTos(
	ctx context.Context,
) PushPacketsTos {
	if n == nil {
		return nil
	}
	return xsync.DoR1(ctx, &n.Locker, func() PushPacketsTos {
		return slices.Clone(n.PushPacketsTos)
	})
}

func (n *NodeWithCustomData[C, T]) WithPushPacketsTos(
	ctx context.Context,
	callback func(context.Context, *PushPacketsTos),
) {
	if n == nil {
		return
	}
	n.Locker.Do(ctx, func() {
		c := slices.Clone(n.PushPacketsTos)
		callback(ctx, &n.PushPacketsTos)
		removed, added := lo.Difference(c, n.PushPacketsTos)
		if len(removed) == 0 && len(added) == 0 {
			return
		}
		if n.Config.CacheHandler != nil {
			for _, pushTo := range added {
				n.Config.CacheHandler.OnAddPushPacketsTo(ctx, pushTo)
			}
		}
		close(*xatomic.SwapPointer(&n.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))
		if n.Config.CacheHandler != nil {
			for _, pushTo := range removed {
				n.Config.CacheHandler.OnRemovePushPacketsTo(ctx, pushTo)
			}
		}
	})
}

// TODO: add deduplication of of PushPacketsTos, because CacheHandler won't handle that correctly.
func (n *NodeWithCustomData[C, T]) AddPushPacketsTo(
	ctx context.Context,
	dst Abstract,
	conds ...packetfiltercondition.Condition,
) {
	logger.Debugf(ctx, "AddPushPacketsTo: %s -> %s", n, dst)
	defer logger.Debugf(ctx, "/AddPushPacketsTo: %s -> %s", n, dst)
	n.Locker.Do(ctx, func() {
		if n.Config.CacheHandler != nil {
			n.Config.CacheHandler.OnAddPushPacketsTo(ctx, PushPacketsTo{
				Node:      dst,
				Condition: packetConds(conds...),
			})
		}
		n.PushPacketsTos.Add(dst, conds...)
		close(*xatomic.SwapPointer(&n.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))
	})
}

// TODO: add deduplication of of PushPacketsTos, because CacheHandler won't handle that correctly.
func (n *NodeWithCustomData[C, T]) SetPushPacketsTos(
	ctx context.Context,
	s PushPacketsTos,
) {
	logger.Tracef(ctx, "SetPushPacketsTos: %s", debug.Stack())

	n.Locker.Do(ctx, func() {
		if n.Config.CacheHandler != nil {
			for _, pushTo := range s {
				if !n.PushPacketsTos.Contains(pushTo) {
					n.Config.CacheHandler.OnAddPushPacketsTo(ctx, pushTo)
				}
			}
		}

		n.PushPacketsTos = s
		close(*xatomic.SwapPointer(&n.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))

		if n.Config.CacheHandler != nil {
			for _, pushTo := range n.PushPacketsTos {
				if !s.Contains(pushTo) {
					n.Config.CacheHandler.OnRemovePushPacketsTo(ctx, pushTo)
				}
			}
		}
	})
}

func (n *NodeWithCustomData[C, T]) GetPushFramesTos(
	ctx context.Context,
) PushFramesTos {
	if n == nil {
		return nil
	}
	return xsync.DoR1(ctx, &n.Locker, func() PushFramesTos {
		return slices.Clone(n.PushFramesTos)
	})
}

func (n *NodeWithCustomData[C, T]) WithPushFramesTos(
	ctx context.Context,
	callback func(context.Context, *PushFramesTos),
) {
	if n == nil {
		return
	}
	n.Locker.Do(ctx, func() {
		c := slices.Clone(n.PushFramesTos)
		callback(ctx, &n.PushFramesTos)
		removed, added := lo.Difference(c, n.PushFramesTos)
		if len(removed) == 0 && len(added) == 0 {
			return
		}
		close(*xatomic.SwapPointer(&n.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))
	})
}

func (n *NodeWithCustomData[C, T]) RemovePushPacketsTo(
	ctx context.Context,
	to Abstract,
) (_err error) {
	logger.Debugf(ctx, "RemovePushPacketsTo")
	defer func() { defer logger.Debugf(ctx, "/RemovePushPacketsTo: %v", _err) }()
	logger.Tracef(ctx, "RemovePushPacketsTo: %s", debug.Stack())

	if !xsync.DoR1(ctx, &n.Locker, func() bool {
		pushTos := n.PushPacketsTos
		for idx, pushTo := range pushTos {
			if pushTo.Node == to {
				pushTos = slices.Delete(pushTos, idx, idx+1)
				n.PushPacketsTos = pushTos
				close(*xatomic.SwapPointer(&n.ChangeChanPushPacketsTo, ptr(make(chan struct{}))))
				if n.Config.CacheHandler != nil {
					n.Config.CacheHandler.OnRemovePushPacketsTo(ctx, pushTo)
				}
				return true
			}
		}
		return false
	}) {
		return fmt.Errorf("%s does not push packets to %s", n, to)
	}
	return nil
}

func (n *NodeWithCustomData[C, P]) RemovePushFramesTo(
	ctx context.Context,
	to Abstract,
) (_err error) {
	logger.Debugf(ctx, "RemovePushFramesTo")
	defer func() { defer logger.Debugf(ctx, "/RemovePushFramesTo: %v", _err) }()
	logger.Tracef(ctx, "RemovePushFramesTo: %s", debug.Stack())

	return xsync.DoR1(ctx, &n.Locker, func() error {
		pushTos := n.PushFramesTos
		for idx, pushTo := range pushTos {
			if pushTo.Node == to {
				pushTos = slices.Delete(pushTos, idx, idx+1)
				n.PushFramesTos = pushTos
				close(*xatomic.SwapPointer(&n.ChangeChanPushFramesTo, ptr(make(chan struct{}))))
				return nil
			}
		}
		return fmt.Errorf("%s does not push frames to %s", n, to)
	})
}

func (n *NodeWithCustomData[C, T]) AddPushFramesTo(
	ctx context.Context,
	dst Abstract,
	conds ...framefiltercondition.Condition,
) {
	n.Locker.Do(ctx, func() {
		n.PushFramesTos.Add(dst, conds...)
		close(*xatomic.SwapPointer(&n.ChangeChanPushFramesTo, ptr(make(chan struct{}))))
	})
}

func (n *NodeWithCustomData[C, T]) SetPushFramesTos(
	ctx context.Context,
	s PushFramesTos,
) {
	logger.Tracef(ctx, "SetPushFramesTos: %s", debug.Stack())
	n.Locker.Do(ctx, func() {
		n.PushFramesTos = s
		close(*xatomic.SwapPointer(&n.ChangeChanPushFramesTo, ptr(make(chan struct{}))))
	})
}

func (n *NodeWithCustomData[C, T]) GetInputPacketFilter() (_ret packetfiltercondition.Condition) {
	if n == nil {
		return packetfiltercondition.Static(false)
	}
	ctx := context.TODO()
	n.Locker.ManualLock(ctx)
	defer n.Locker.ManualUnlock(ctx)
	return n.InputPacketFilter
}

func (n *NodeWithCustomData[C, T]) SetInputPacketFilter(cond packetfiltercondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputPacketFilter = cond
	})
}

func (n *NodeWithCustomData[C, T]) GetInputFrameFilter() (_ret framefiltercondition.Condition) {
	if n == nil {
		return framefiltercondition.Static(false)
	}
	ctx := context.TODO()
	n.Locker.Do(ctx, func() {
		_ret = n.InputFrameFilter
	})
	return
}

func (n *NodeWithCustomData[C, T]) SetInputFrameFilter(cond framefiltercondition.Condition) {
	n.Locker.Do(context.TODO(), func() {
		n.InputFrameFilter = cond
	})
}

func (n *NodeWithCustomData[C, T]) String() string {
	nodeString := n.Processor.String()
	if cdStringer, _ := any(n.CustomData).(fmt.Stringer); cdStringer != nil {
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
		if pushTo.Node == nil {
			continue
		}
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
		if pushTo.Node == nil {
			continue
		}
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

func (n *NodeWithCustomData[C, T]) GetChangeChanIsServing() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanIsServing)
}

func (n *NodeWithCustomData[C, T]) GetChangeChanPushPacketsTo() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanPushPacketsTo)
}

func (n *NodeWithCustomData[C, T]) GetChangeChanPushFramesTo() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanPushFramesTo)
}

func (n *NodeWithCustomData[C, T]) GetCountersPtr() *types.Counters {
	return n.Counters
}
