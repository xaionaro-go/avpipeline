// node.go defines the Node structure and its core methods.

// Package node provides the core building blocks for the media processing pipeline.
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
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
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

	GetPushTos(context.Context) PushTos
	AddPushTo(context.Context, Abstract, ...packetorframefiltercondition.Condition)
	SetPushTos(context.Context, PushTos)
	WithPushTos(context.Context, func(context.Context, *PushTos))
	RemovePushTo(context.Context, Abstract) error

	GetProcessor() processor.Abstract
	GetCountersPtr() *types.Counters

	GetInputFilter(context.Context) packetorframefiltercondition.Condition
	SetInputFilter(context.Context, packetorframefiltercondition.Condition)

	GetChangeChanIsServing() <-chan struct{}
	GetChangeChanPushTo() <-chan struct{}

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
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
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
	return false
}

func (n *MyFancyNodePlaceholder) GetPushTos(
	ctx context.Context,
) node.PushTos {
	return nil
}

func (n *MyFancyNodePlaceholder) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *node.PushTos),
) {

}

func (n *MyFancyNodePlaceholder) AddPushTo(
	ctx context.Context,
	dst node.Abstract,
	conds ...packetorframefiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) SetPushTos(
	ctx context.Context,
	v node.PushTos,
) {

}

func (n *MyFancyNodePlaceholder) RemovePushTo(
	ctx context.Context,
	dst node.Abstract,
) error {
	return nil
}

func (n *MyFancyNodePlaceholder) GetCountersPtr() *nodetypes.Counters {
	return nil
}

}

func (n *MyFancyNodePlaceholder) GetProcessor() processor.Abstract {

}

func (n *MyFancyNodePlaceholder) GetInputFilter(
	ctx context.Context,
) packetorframefiltercondition.Condition {

}

func (n *MyFancyNodePlaceholder) SetInputFilter(
	ctx context.Context,
	cond packetorframefiltercondition.Condition,
) {

}

func (n *MyFancyNodePlaceholder) GetChangeChanIsServing() <-chan struct{} {
	return nil
}

func (n *MyFancyNodePlaceholder) GetChangeChanPushTo() <-chan struct{} {
	return nil
}

func (n *MyFancyNodePlaceholder) GetChangeChanDrained() <-chan struct{} {
	return nil
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
	Processor        T
	Counters         *types.Counters
	PushTos          PushTos
	InputFilter      packetorframefiltercondition.Condition
	Locker           xsync.Mutex
	ServeDebugData   any
	IsServingValue   bool
	IsDrainedValue   atomic.Bool
	IsProcessorDirty bool
	Config           Config

	ChangeChanIsServing *chan struct{}
	ChangeChanPushTo    *chan struct{}
	ChangeChanDrained   *chan struct{}

	CustomData C
}

type Node[T processor.Abstract] = NodeWithCustomData[struct{}, T]

var (
	_ Abstract                       = (*Node[processor.Abstract])(nil)
	_ DotBlockContentStringWriteToer = (*Node[processor.Abstract])(nil)
	_ GetCustomDataer[struct{}]      = (*Node[processor.Abstract])(nil)
)

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
		Processor:           processor,
		ChangeChanIsServing: ptr(make(chan struct{})),
		ChangeChanPushTo:    ptr(make(chan struct{})),
		ChangeChanDrained:   ptr(make(chan struct{})),
		Config:              Options(opts).config(),
		Counters:            types.NewCounters(),
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

func (n *NodeWithCustomData[C, T]) GetPushTos(
	ctx context.Context,
) PushTos {
	if n == nil {
		return nil
	}
	return xsync.DoR1(ctx, &n.Locker, func() PushTos {
		return slices.Clone(n.PushTos)
	})
}

func (n *NodeWithCustomData[C, T]) WithPushTos(
	ctx context.Context,
	callback func(context.Context, *PushTos),
) {
	if n == nil {
		return
	}
	n.Locker.Do(ctx, func() {
		c := slices.Clone(n.PushTos)
		callback(ctx, &n.PushTos)
		removed, added := lo.Difference(c, n.PushTos)
		if len(removed) == 0 && len(added) == 0 {
			return
		}
		if n.Config.CacheHandler != nil {
			for _, pushTo := range added {
				n.Config.CacheHandler.OnAddPushTo(ctx, pushTo)
			}
		}
		close(*xatomic.SwapPointer(&n.ChangeChanPushTo, ptr(make(chan struct{}))))
		if n.Config.CacheHandler != nil {
			for _, pushTo := range removed {
				n.Config.CacheHandler.OnRemovePushTo(ctx, pushTo)
			}
		}
	})
}

func (n *NodeWithCustomData[C, T]) AddPushTo(
	ctx context.Context,
	dst Abstract,
	conds ...packetorframefiltercondition.Condition,
) {
	logger.Debugf(ctx, "AddPushTo: %s -> %s", n, dst)
	defer logger.Debugf(ctx, "/AddPushTo: %s -> %s", n, dst)
	n.Locker.Do(ctx, func() {
		if n.Config.CacheHandler != nil {
			n.Config.CacheHandler.OnAddPushTo(ctx, PushTo{
				Node:      dst,
				Condition: pushToConds(conds...),
			})
		}
		n.PushTos.Add(dst, conds...)
		close(*xatomic.SwapPointer(&n.ChangeChanPushTo, ptr(make(chan struct{}))))
	})
}

func (n *NodeWithCustomData[C, T]) SetPushTos(
	ctx context.Context,
	s PushTos,
) {
	logger.Tracef(ctx, "SetPushTos: %s", debug.Stack())

	n.Locker.Do(ctx, func() {
		if n.Config.CacheHandler != nil {
			for _, pushTo := range s {
				if !n.PushTos.Contains(pushTo) {
					n.Config.CacheHandler.OnAddPushTo(ctx, pushTo)
				}
			}
		}

		n.PushTos = s
		close(*xatomic.SwapPointer(&n.ChangeChanPushTo, ptr(make(chan struct{}))))

		if n.Config.CacheHandler != nil {
			for _, pushTo := range n.PushTos {
				if !s.Contains(pushTo) {
					n.Config.CacheHandler.OnRemovePushTo(ctx, pushTo)
				}
			}
		}
	})
}

func (n *NodeWithCustomData[C, T]) RemovePushTo(
	ctx context.Context,
	to Abstract,
) (_err error) {
	logger.Debugf(ctx, "RemovePushTo")
	defer func() { logger.Debugf(ctx, "/RemovePushTo: %v", _err) }()
	logger.Tracef(ctx, "RemovePushTo: %s", debug.Stack())

	if !xsync.DoR1(ctx, &n.Locker, func() bool {
		pushTos := n.PushTos
		for idx, pushTo := range pushTos {
			if pushTo.Node == to {
				pushTos = slices.Delete(pushTos, idx, idx+1)
				n.PushTos = pushTos
				close(*xatomic.SwapPointer(&n.ChangeChanPushTo, ptr(make(chan struct{}))))
				if n.Config.CacheHandler != nil {
					n.Config.CacheHandler.OnRemovePushTo(ctx, pushTo)
				}
				return true
			}
		}
		return false
	}) {
		return fmt.Errorf("%s does not push to %s", n, to)
	}
	return nil
}

func (n *NodeWithCustomData[C, T]) GetInputFilter(ctx context.Context) (_ret packetorframefiltercondition.Condition) {
	if n == nil {
		return packetorframefiltercondition.Static(false)
	}
	n.Locker.ManualLock(ctx)
	defer n.Locker.ManualUnlock(ctx)
	return n.InputFilter
}

func (n *NodeWithCustomData[C, T]) SetInputFilter(ctx context.Context, cond packetorframefiltercondition.Condition) {
	n.Locker.Do(ctx, func() {
		n.InputFilter = cond
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
	for _, pushTo := range n.PushTos {
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

func (n *NodeWithCustomData[C, T]) GetChangeChanPushTo() <-chan struct{} {
	return *xatomic.LoadPointer(&n.ChangeChanPushTo)
}

func (n *NodeWithCustomData[C, T]) GetCountersPtr() *types.Counters {
	return n.Counters
}
