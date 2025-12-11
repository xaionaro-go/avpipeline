package inputwithfallback

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/monotonicpts"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/xsync"
)

type InputWithFallback[K InputKernel, DF codec.DecoderFactory, C any] struct {
	InputPacketFilter   xatomic.Value[packetfiltercondition.Condition]
	InputFrameFilter    xatomic.Value[framefiltercondition.Condition]
	InputChainsLocker   xsync.Mutex
	InputChains         []*InputChain[K, DF, C]
	InputSwitch         *barrierstategetter.Switch
	InputSyncer         *barrierstategetter.Switch
	MonotonicPTS        *monotonicpts.Filter
	PreOutput           *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Passthrough]]
	Output              *node.NodeWithCustomData[C, *processor.FromKernel[*kernel.Passthrough]]
	Config              Config
	AllowCorruptPackets atomic.Bool

	newInputChainChan chan *InputChain[K, DF, C]
	isServing         atomic.Bool
	serveWaitGroup    sync.WaitGroup
}

// |  retryable:input0 -> inputSwitch (-> autofix -> decoder) -> inputSyncer ->-+
// |  (main)                   :                                      :         |
// |                           :                                      :         |               MonotonicPTS
// |  retryable:input1 -> inputSwitch (-> autofix -> decoder) -> inputSyncer ->-+-> Passthrough--------------> Passthrough -->--
// |  (fallback)               :                                      :         |                                          (one output)
// |                           :                                      :         |
// |  retryable:input2 -> inputSwitch (-> autofix -> decoder) -> inputSyncer ->-+
// |  (second fallback)        :                                      :         |
// |                           :                                      :         |
// |  ...                     ...          ...         ...           ...     ->-+
//
// K is the input kernel type (generally it is *kernel.Input).
// C is the custom data type associated with each input node (generally it is struct{}).
func New[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	inputFactories []InputFactory[K],
	opts ...Option,
) (_ret *InputWithFallback[K, DF, C], _err error) {
	logger.Debugf(ctx, "New")
	defer func() { logger.Debugf(ctx, "/New: %v", _err) }()

	i := &InputWithFallback[K, DF, C]{
		Config: Options(opts).Config(),
		PreOutput: node.NewWithCustomDataFromKernel[C, *kernel.Passthrough](ctx,
			&kernel.Passthrough{},
		),
		Output: node.NewWithCustomDataFromKernel[C, *kernel.Passthrough](ctx,
			&kernel.Passthrough{},
		),
		InputSwitch:       barrierstategetter.NewSwitch(),
		InputSyncer:       barrierstategetter.NewSwitch(),
		MonotonicPTS:      monotonicpts.New(true),
		newInputChainChan: make(chan *InputChain[K, DF, C], 100),
	}
	i.PreOutput.AddPushPacketsTo(ctx, nil, packetfiltercondition.PacketOrFrame{i.MonotonicPTS})

	err := i.AddFactory(ctx, inputFactories...)
	if err != nil {
		return nil, fmt.Errorf("cannot add input factories: %w", err)
	}

	return i, nil
}

func (i *InputWithFallback[K, DF, C]) String() string {
	return "InputWithFallback"
}

func (i *InputWithFallback[K, DF, C]) GetOutput() node.Abstract {
	return i.Output
}

func (i *InputWithFallback[K, DF, C]) initSwitches(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initSwitches")
	defer func() { logger.Debugf(ctx, "/initSwitches: %v", _err) }()

	i.InputSwitch.CurrentValue.Store(math.MinInt32)
	i.InputSyncer.CurrentValue.Store(math.MinInt32)

	switchKeepUnlessConds := packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.Or{
			packetorframecondition.IsKeyFrame(true),
			packetorframecondition.AtomicBool(&i.AllowCorruptPackets),
		},
	}
	logger.Debugf(ctx, "Switch: setting keep-unless conditions: %s", switchKeepUnlessConds)
	i.InputSwitch.SetKeepUnless(switchKeepUnlessConds)

	i.InputSwitch.SetOnBeforeSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx,
			"Switch.SetOnBeforeSwitch: %d -> %d",
			from, to,
		)
		inputNext := i.getInputChainByID(ctx, InputID(to))
		if err := inputNext.Unpause(ctx); err != nil {
			logger.Errorf(ctx, "Switch: unable to pause the previous input %d: %v", from, err)
		}
	})

	i.InputSwitch.SetOnAfterSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		if v := in.Get(); v != nil {
			ctx = belt.WithField(ctx, "media_type", v.GetMediaType().String())
		}
		logger.Debugf(ctx, "Switch.SetOnAfterSwitch: %d -> %d", from, to)

		in.AddPipelineSideData(kernel.SideFlagFlush{})

		logger.Debugf(ctx, "Syncer.SetValue(ctx, %d): from %d", to, from)
		err := i.InputSyncer.SetValue(ctx, to)
		logger.Debugf(ctx, "/Syncer.SetValue(ctx, %d): from %d: %v", to, from, err)

		inputPrev := i.getInputChainByID(ctx, InputID(from))
		if err := inputPrev.Pause(ctx); err != nil {
			logger.Errorf(ctx, "Switch: unable to pause the previous input %d: %v", from, err)
		}
	})
	i.InputSyncer.SetKeepUnless(packetorframecondition.Function(func(
		ctx context.Context,
		in packetorframe.InputUnion,
	) bool {
		return in.GetPipelineSideData().Contains(kernel.SideFlagFlush{})
	}))
	i.InputSyncer.Flags.Set(barrierstategetter.SwitchFlagInactiveBlock)

	logger.Tracef(ctx, "Switch: %p", i.InputSwitch)
	logger.Tracef(ctx, "Syncer: %p", i.InputSyncer)
	return nil
}

func (i *InputWithFallback[K, DF, C]) getInputChainByID(
	ctx context.Context,
	id InputID,
) *InputChain[K, DF, C] {
	return xsync.DoA2R1(ctx, &i.InputChainsLocker, i.getInputChainByIDLocked, ctx, id)
}

func (i *InputWithFallback[K, DF, C]) getInputChainByIDLocked(
	ctx context.Context,
	id InputID,
) *InputChain[K, DF, C] {
	if int(id) < 0 || int(id) >= len(i.InputChains) {
		return nil
	}
	return i.InputChains[id]
}

func (i *InputWithFallback[K, DF, C]) AddFactory(
	ctx context.Context,
	inputFactories ...InputFactory[K],
) (_err error) {
	logger.Debugf(ctx, "AddFactory")
	defer func() { logger.Debugf(ctx, "/AddFactory: %v", _err) }()
	return xsync.DoA2R1(ctx, &i.InputChainsLocker, i.addFactory, ctx, inputFactories)
}

func (i *InputWithFallback[K, DF, C]) addFactory(
	ctx context.Context,
	inputFactories []InputFactory[K],
) error {
	for _, inputFactory := range inputFactories {
		inputID := InputID(len(i.InputChains))
		inputChain, err := newInputChain[K, DF, C](ctx,
			inputID, inputFactory,
			i.InputSwitch.Output(int32(inputID)),
			i.InputSyncer.Output(int32(inputID)),
		)
		if err != nil {
			return fmt.Errorf("cannot create input chain for input %d: %w", inputID, err)
		}
		inputChain.GetInput().SetInputPacketFilter(ctx, i.inputPacketFilter())
		inputChain.GetInput().SetInputFrameFilter(ctx, i.inputFrameFilter())
		inputChain.GetOutput().AddPushPacketsTo(ctx, i.PreOutput)
		inputChain.GetOutput().AddPushFramesTo(ctx, i.PreOutput)
		i.InputChains = append(i.InputChains, inputChain)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case i.newInputChainChan <- inputChain:
		default:
			if err := inputChain.Close(ctx); err != nil {
				logger.Errorf(ctx, "unable to close input chain: %v", err)
			}
			return fmt.Errorf("cannot send new input chain to the init queue: it is already full")
		}
	}
	return nil
}

type asInputPacketFilter[K InputKernel, DF codec.DecoderFactory, C any] InputWithFallback[K, DF, C]

func (f *asInputPacketFilter[K, DF, C]) String() string {
	return "InputWithFallback:InputPacketFilter"
}

func (f *asInputPacketFilter[K, DF, C]) Match(
	ctx context.Context,
	in packetfiltercondition.Input,
) bool {
	v := f.InputPacketFilter.Load()
	if v == nil {
		return true
	}
	return v.Match(ctx, in)
}

func (i *InputWithFallback[K, DF, C]) inputPacketFilter() packetfiltercondition.Condition {
	return (*asInputPacketFilter[K, DF, C])(i)
}

type asInputFrameFilter[K InputKernel, DF codec.DecoderFactory, C any] InputWithFallback[K, DF, C]

func (f *asInputFrameFilter[K, DF, C]) String() string {
	return "InputWithFallback:InputFrameFilter"
}

func (f *asInputFrameFilter[K, DF, C]) Match(
	ctx context.Context,
	in framefiltercondition.Input,
) bool {
	v := f.InputFrameFilter.Load()
	if v == nil {
		return true
	}
	return v.Match(ctx, in)
}

func (i *InputWithFallback[K, DF, C]) inputFrameFilter() framefiltercondition.Condition {
	return (*asInputFrameFilter[K, DF, C])(i)
}
