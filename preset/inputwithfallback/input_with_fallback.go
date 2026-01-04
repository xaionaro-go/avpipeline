// input_with_fallback.go implements a preset for an input with fallback sources.

// Package inputwithfallback provides a preset for an input with fallback sources.
package inputwithfallback

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/monotonicpts"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

const (
	debugConsistencyCheckLoop = true
)

type InputWithFallback[K InputKernel, DF codec.DecoderFactory, C any] struct {
	InputFilter         xatomic.Value[packetorframefiltercondition.Condition]
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
	syncingSince      xatomic.Value[time.Time]
	switchingProcN    xatomic.Int64

	// measurements
	Measurements                    map[astiav.MediaType]*TrackMeasurements
	CurrentBitRateMeasurementsCount atomic.Uint64
}

// |  retryable:input0 -> inputSwitch (-> autoheaders -> decoder) -> inputSyncer ->-+
// |  (main)                   :                                          :         |
// |                           :                                          :         |               MonotonicPTS
// |  retryable:input1 -> inputSwitch (-> autoheaders -> decoder) -> inputSyncer ->-+-> Passthrough--------------> Passthrough -->--
// |  (fallback)               :                                          :         |                                          (one output)
// |                           :                                          :         |
// |  retryable:input2 -> inputSwitch (-> autoheaders -> decoder) -> inputSyncer ->-+
// |  (second fallback)        :                                          :         |
// |                           :                                          :         |
// |  ...                     ...          ...             ...           ...     ->-+
//
// K is the input kernel type (generally it is *kernel.Input).
// C is the custom data type associated with each input node (generally it is struct{}).
func New[K InputKernel, DF codec.DecoderFactory, C any](
	ctx context.Context,
	inputFactories []InputFactory[K, DF, C],
	opts ...Option,
) (_ret *InputWithFallback[K, DF, C], _err error) {
	logger.Debugf(ctx, "New")
	defer func() { logger.Debugf(ctx, "/New: %v", _err) }()

	i := &InputWithFallback[K, DF, C]{
		Config:            Options(opts).Config(),
		PreOutput:         node.NewWithCustomDataFromKernel[C](ctx, &kernel.Passthrough{}),
		Output:            node.NewWithCustomDataFromKernel[C](ctx, &kernel.Passthrough{}),
		InputSwitch:       barrierstategetter.NewSwitch(),
		InputSyncer:       barrierstategetter.NewSwitch(),
		MonotonicPTS:      monotonicpts.New(true),
		newInputChainChan: make(chan *InputChain[K, DF, C], 100),
		Measurements: map[astiav.MediaType]*TrackMeasurements{
			astiav.MediaTypeVideo:   newTrackMeasurements(),
			astiav.MediaTypeAudio:   newTrackMeasurements(),
			astiav.MediaTypeUnknown: newTrackMeasurements(),
		},
	}
	i.PreOutput.AddPushTo(ctx, i.Output, packetorframefiltercondition.PacketOrFrame{i.MonotonicPTS})
	if err := i.initSwitches(ctx); err != nil {
		return nil, fmt.Errorf("cannot init switches: %w", err)
	}

	err := i.AddFactory(ctx, inputFactories...)
	if err != nil {
		return nil, fmt.Errorf("cannot add input factories: %w", err)
	}

	return i, nil
}

func (i *InputWithFallback[K, DF, C]) String() string {
	cur := i.InputSwitch.CurrentValue.Load()
	next := i.InputSwitch.NextValue.Load()
	ctx := context.TODO()
	if !i.InputChainsLocker.ManualTryLock(ctx) {
		return fmt.Sprintf("InputWithFallback(<locked>; cur:%d, next:%d)", cur, next)
	}
	defer i.InputChainsLocker.ManualUnlock(ctx)
	var inputChainStrs []string
	for _, inputChain := range i.InputChains {
		isPaused := inputChain.IsPaused(ctx)
		var s []string
		s = append(s, inputChain.String())
		if int(inputChain.ID) == int(cur) {
			s = append(s, "current")
		}
		if int(inputChain.ID) == int(next) {
			s = append(s, "next")
		}
		if isPaused {
			s = append(s, "paused")
		}
		inputChainStrs = append(inputChainStrs, strings.Join(s, ":"))
	}
	return fmt.Sprintf("InputWithFallback(%s)", strings.Join(inputChainStrs, ", "))
}

func (i *InputWithFallback[K, DF, C]) GetOutput() node.Abstract {
	return i.Output
}

func (i *InputWithFallback[K, DF, C]) GetInputChainsCount(
	ctx context.Context,
) int {
	return xsync.DoR1(ctx, &i.InputChainsLocker, func() int {
		return len(i.InputChains)
	})
}

func (i *InputWithFallback[K, DF, C]) initSwitches(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initSwitches")
	defer func() { logger.Debugf(ctx, "/initSwitches: %v", _err) }()

	i.InputSwitch.CurrentValue.Store(0)
	i.InputSyncer.CurrentValue.Store(0)

	switchKeepUnlessConds := packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.Or{
			packetorframecondition.IsKeyFrame(true),
			packetorframecondition.AtomicBool(&i.AllowCorruptPackets),
		},
	}
	logger.Debugf(ctx, "Switch: setting keep-unless conditions: %s", switchKeepUnlessConds)
	i.InputSwitch.SetKeepUnless(switchKeepUnlessConds)

	i.InputSwitch.SetOnSwitchRequest(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		to int32,
	) (_err error) {
		logger.Debugf(ctx, "Switch.SetOnSwitchRequest: -> %d", to)
		defer func() { logger.Debugf(ctx, "/Switch.SetOnSwitchRequest: -> %d: %v", to, _err) }()
		if v := i.switchingProcN.Add(1); v != 1 {
			i.switchingProcN.Add(-1)
			return fmt.Errorf("another switch is in progress (procN: %d), cannot switch to %d", v-1, to)
		}
		observability.Go(ctx, func(ctx context.Context) {
			defer i.switchingProcN.Add(-1)
			inputNext := i.getInputChainByID(ctx, InputID(to))
			if err := inputNext.Unpause(ctx); err != nil {
				logger.Errorf(ctx, "Switch: unable to unpause the previous input %d: %v", to, err)
			}
		})

		prevNext := i.InputSwitch.NextValue.Load()
		if prevNext == math.MinInt32 {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: no previous requested input")
			return nil
		}

		cur := i.InputSwitch.CurrentValue.Load()
		if prevNext <= cur {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: not pausing a higher priority input (than the currently active) %d <= %d", prevNext, cur)
			return nil
		}
		if prevNext == to {
			logger.Debugf(ctx, "Switch.SetOnSwitchRequest: not pausing the same input %d", prevNext)
			return nil
		}

		logger.Debugf(ctx, "Switch.SetOnSwitchRequest: pausing previous requested input %d", prevNext)
		i.switchingProcN.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer i.switchingProcN.Add(-1)
			inputPrev := i.getInputChainByID(ctx, InputID(prevNext))
			if err := inputPrev.Pause(ctx); err != nil {
				logger.Errorf(ctx, "Switch: unable to pause the previous requested input %d: %v", prevNext, err)
			}
		})
		return nil
	})

	i.InputSwitch.SetOnBeforeSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx, "Switch.SetOnBeforeSwitch: %d -> %d", from, to)
		i.switchingProcN.Add(1)
	})

	i.InputSwitch.SetOnInterruptedSwitch(func(
		ctx context.Context,
		in packetorframe.InputUnion,
		from, to int32,
	) {
		logger.Debugf(ctx, "Switch.SetOnInterruptedSwitch: %d -> %d", from, to)
		i.switchingProcN.Add(-1)
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

		assert(ctx, i.syncingSince.Load().IsZero(), "syncingSince must be zero")

		i.syncingSince.Store(time.Now())
		in.AddPipelineSideData(kernel.SideFlagFlush{})

		for inputID := from; inputID > to; inputID-- {
			inputID := inputID
			i.switchingProcN.Add(1)
			observability.Go(ctx, func(ctx context.Context) {
				defer i.switchingProcN.Add(-1)
				inputPrev := i.getInputChainByID(ctx, InputID(inputID))
				if err := inputPrev.Pause(ctx); err != nil {
					logger.Errorf(ctx, "Switch: unable to pause the previous input %d: %v", inputID, err)
				}
			})
		}

		logger.Debugf(ctx, "Syncer.SetValue(ctx, %d): from %d", to, from)
		err := i.InputSyncer.SetValue(ctx, to)
		logger.Debugf(ctx, "/Syncer.SetValue(ctx, %d): from %d: %v", to, from, err)
	})
	i.InputSyncer.SetKeepUnless(packetorframecondition.Function(func(
		ctx context.Context,
		in packetorframe.InputUnion,
	) (_ret bool) {
		defer func() {
			if _ret {
				i.syncingSince.Store(time.Time{})
				i.switchingProcN.Add(-1)
			}
		}()
		if in.GetPipelineSideData().Contains(kernel.SideFlagFlush{}) {
			return true
		}
		if time.Since(i.syncingSince.Load()) <= i.Config.SwitchKeepUnlessTimeout {
			return false
		}
		logger.Errorf(ctx, "Syncer: switching took too long")
		if in.Frame != nil {
			return true
		}
		// not decoded, we have to wait for a keyframe on the video track:
		if in.GetMediaType() != astiav.MediaTypeVideo {
			return false
		}
		if !in.IsKey() {
			return false
		}
		return true
	}))
	i.InputSyncer.Flags.Set(0 |
		barrierstategetter.SwitchFlagNextOutputStateBlock,
	)

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
	_ context.Context,
	id InputID,
) *InputChain[K, DF, C] {
	if int(id) < 0 || int(id) >= len(i.InputChains) {
		return nil
	}
	return i.InputChains[id]
}

func (i *InputWithFallback[K, DF, C]) AddFactory(
	ctx context.Context,
	inputFactories ...InputFactory[K, DF, C],
) (_err error) {
	logger.Debugf(ctx, "AddFactory")
	defer func() { logger.Debugf(ctx, "/AddFactory: %v", _err) }()
	return xsync.DoA2R1(ctx, &i.InputChainsLocker, i.addFactory, ctx, inputFactories)
}

func (i *InputWithFallback[K, DF, C]) addFactory(
	ctx context.Context,
	inputFactories []InputFactory[K, DF, C],
) error {
	for _, inputFactory := range inputFactories {
		inputID := InputID(len(i.InputChains))
		inputChain, err := newInputChain(ctx,
			inputID, inputFactory,
			i.InputSwitch.Output(int32(inputID)),
			i.InputSyncer.Output(int32(inputID)),
			i.onInputChainKernelOpen,
			i.onInputChainError,
		)
		if err != nil {
			return fmt.Errorf("cannot create input chain for input %d: %w", inputID, err)
		}
		inputChain.GetInput().SetInputFilter(ctx, i.inputFilter())
		inputChain.GetOutput().AddPushTo(ctx, i.PreOutput)
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

func (i *InputWithFallback[K, DF, C]) onInputChainKernelOpen(
	ctx context.Context,
	inputChain *InputChain[K, DF, C],
) {
	// When a kernel opens, prefer it if there is no active input or it has higher priority
	id := int(inputChain.ID)
	cur := int(i.InputSwitch.CurrentValue.Load())
	logger.Debugf(ctx, "onInputChainKernelOpen: input %d opened, current=%d", id, cur)
	if id >= cur {
		return
	}

	// If this input has a higher priority (lower index), request a switch back
	if err := i.InputSwitch.SetValue(ctx, int32(id)); err != nil {
		logger.Errorf(ctx, "onInputChainKernelOpen: unable to recover to input %d: %v", id, err)
	}
}

func (i *InputWithFallback[K, DF, C]) onInputChainError(
	ctx context.Context,
	inputChain *InputChain[K, DF, C],
	err error,
) (_err error) {
	logger.Debugf(ctx, "onInputChainError: input %d error: %v", int(inputChain.ID), err)
	defer func() {
		logger.Debugf(ctx, "/onInputChainError: input %d error: %v: %v", int(inputChain.ID), err, _err)
	}()

	if i.Config.RetryInterval < 0 {
		return fmt.Errorf("retries are disabled, and input %d errored: %w", int(inputChain.ID), err)
	}

	defer time.Sleep(i.Config.RetryInterval)

	id := inputChain.ID
	active := int(i.InputSwitch.CurrentValue.Load())
	next := int(i.InputSwitch.NextValue.Load())
	current := max(active, next)
	logger.Debugf(ctx, "onInputChainError: current:%d", current)

	// Only react to errors on the currently active input
	if current != int(id) {
		return nil
	}

	i.InputChainsLocker.Do(ctx, func() {
		// choose next fallback (simple next index)
		if id+1 >= InputID(len(i.InputChains)) {
			logger.Debugf(ctx, "onInputChainError: no fallbacks available: %d+1 >= %d", int(id), len(i.InputChains))
			return
		}
		nextID := id + 1
		logger.Infof(ctx, "onInputChainError: switching from %d to %d due to error: %v", int(id), nextID, err)
		if err := i.InputSwitch.SetValue(ctx, int32(nextID)); err != nil {
			logger.Errorf(ctx, "onInputChainError: unable to switch to fallback %d: %v", nextID, err)
		}
	})
	return nil
}

type asInputFilter[K InputKernel, DF codec.DecoderFactory, C any] InputWithFallback[K, DF, C]

func (f *asInputFilter[K, DF, C]) String() string {
	return "InputWithFallback:InputFilter"
}

func (f *asInputFilter[K, DF, C]) Match(
	ctx context.Context,
	in packetorframefiltercondition.Input,
) bool {
	v := f.InputFilter.Load()
	if v == nil {
		return true
	}
	return v.Match(ctx, in)
}

func (i *InputWithFallback[K, DF, C]) inputFilter() packetorframefiltercondition.Condition {
	return (*asInputFilter[K, DF, C])(i)
}

func (i *InputWithFallback[K, DF, C]) GetInputs(
	ctx context.Context,
) InputNodes[K, C] {
	return xsync.DoR1(ctx, &i.InputChainsLocker, i.getInputsLocked)
}

func (i *InputWithFallback[K, DF, C]) getInputsLocked() InputNodes[K, C] {
	inputs := make([]*InputNode[K, C], 0, len(i.InputChains))
	for _, inputChain := range i.InputChains {
		inputs = append(inputs, inputChain.Input)
	}
	return inputs
}

type TrackMeasurements struct {
	InputBitRate  atomic.Uint64
	OutputBitRate atomic.Uint64
}

func newTrackMeasurements() *TrackMeasurements {
	return &TrackMeasurements{}
}

func (i *InputWithFallback[K, DF, C]) getTrackMeasurements(mediaType astiav.MediaType) *TrackMeasurements {
	m := i.Measurements[mediaType]
	if m != nil {
		return m
	}
	return i.Measurements[astiav.MediaTypeUnknown]
}

func updateWithInertialValue(
	oldValue uint64,
	newValue uint64,
	inertia float64,
	measurementsCount uint64,
) uint64 {
	if measurementsCount == 0 {
		return newValue
	}
	return uint64(float64(oldValue)*inertia + float64(newValue)*(1.0-inertia))
}

func (i *InputWithFallback[K, DF, C]) inputBitRateMeasurerLoop(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "inputBitRateMeasurerLoop")
	defer func() { logger.Tracef(ctx, "/inputBitRateMeasurerLoop: %v", _err) }()

	t := time.NewTicker(time.Second / 4)
	defer t.Stop()
	bytesInputReadPrev := map[astiav.MediaType]uint64{}
	bytesOutputReadPrev := map[astiav.MediaType]uint64{}
	tsPrev := time.Now()
	for {
		var tsNext time.Time
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tsNext = <-t.C:
			duration := tsNext.Sub(tsPrev)

			bytesInputReadNext := map[astiav.MediaType]uint64{}
			i.InputChainsLocker.Do(ctx, func() {
				for _, inputChain := range i.InputChains {
					inputCounters := inputChain.Input.GetCountersPtr()
					bytesInputReadNext[astiav.MediaTypeVideo] += inputCounters.Received.Packets.Video.Bytes.Load()
					bytesInputReadNext[astiav.MediaTypeAudio] += inputCounters.Received.Packets.Audio.Bytes.Load()
					bytesInputReadNext[astiav.MediaTypeUnknown] += inputCounters.Received.Packets.Other.Bytes.Load()
				}
			})

			outputCounters := i.Output.GetCountersPtr()
			bytesOutputReadNext := map[astiav.MediaType]uint64{
				astiav.MediaTypeVideo:   outputCounters.Received.Packets.Video.Bytes.Load(),
				astiav.MediaTypeAudio:   outputCounters.Received.Packets.Audio.Bytes.Load(),
				astiav.MediaTypeUnknown: outputCounters.Received.Packets.Other.Bytes.Load(),
			}

			for _, mediaType := range []astiav.MediaType{astiav.MediaTypeVideo, astiav.MediaTypeAudio, astiav.MediaTypeUnknown} {
				m := i.getTrackMeasurements(mediaType)
				bytesInputRead := bytesInputReadNext[mediaType] - bytesInputReadPrev[mediaType]
				bitRateInput := int(float64(bytesInputRead*8) / duration.Seconds())
				oldInputValue := m.InputBitRate.Load()
				newInputValue := updateWithInertialValue(oldInputValue, uint64(bitRateInput), 0.9, i.CurrentBitRateMeasurementsCount.Load())
				m.InputBitRate.Store(newInputValue)

				bytesOutputRead := bytesOutputReadNext[mediaType] - bytesOutputReadPrev[mediaType]
				bitRateOutput := int(float64(bytesOutputRead*8) / duration.Seconds())
				oldOutputValue := m.OutputBitRate.Load()
				newOutputValue := updateWithInertialValue(oldOutputValue, uint64(bitRateOutput), 0.9, i.CurrentBitRateMeasurementsCount.Load())
				m.OutputBitRate.Store(newOutputValue)
			}

			bytesInputReadPrev = bytesInputReadNext
			bytesOutputReadPrev = bytesOutputReadNext
			tsPrev = tsNext
			i.CurrentBitRateMeasurementsCount.Add(1)
		}
	}
}

func (i *InputWithFallback[K, DF, C]) GetBitRates(
	ctx context.Context,
) *globaltypes.BitRates {
	video := i.getTrackMeasurements(astiav.MediaTypeVideo)
	audio := i.getTrackMeasurements(astiav.MediaTypeAudio)
	other := i.getTrackMeasurements(astiav.MediaTypeUnknown)
	return &globaltypes.BitRates{
		Input: globaltypes.BitRateInfo{
			Video: globaltypes.Ubps(video.InputBitRate.Load()),
			Audio: globaltypes.Ubps(audio.InputBitRate.Load()),
			Other: globaltypes.Ubps(other.InputBitRate.Load()),
		},
		Output: globaltypes.BitRateInfo{
			Video: globaltypes.Ubps(video.OutputBitRate.Load()),
			Audio: globaltypes.Ubps(audio.OutputBitRate.Load()),
			Other: globaltypes.Ubps(other.OutputBitRate.Load()),
		},
	}
}
