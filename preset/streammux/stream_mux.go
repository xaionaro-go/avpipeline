package streammux

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/dustin/go-humanize"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	nodeboilerplate "github.com/xaionaro-go/avpipeline/node/boilerplate"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
	"tailscale.com/util/ringbuffer"
)

const (
	switchTimeout = time.Hour
	switchDebug   = false
)

type StreamMux[C any] struct {
	MuxMode        types.MuxMode
	RecodingConfig types.RecoderConfig
	Locker         xsync.Mutex

	// switches:
	OutputSwitch *barrierstategetter.Switch
	OutputSyncer *barrierstategetter.Switch

	// nodes:
	InputNode     *NodeInput[C]
	Outputs       []*Output
	OutputsMap    map[OutputKey]*Output
	OutputFactory OutputFactory

	// aux
	InputNodeAsPacketSource packet.Source
	AutoBitRateHandler      *AutoBitRateHandler[C]
	FPSFractionNumDen       atomic.Uint64
	OutputIDBeforeBypass    int

	// measurements
	CurrentInputBitRate             atomic.Uint64
	CurrentOutputBitRate            atomic.Uint64
	CurrentBitRateMeasurementsCount atomic.Uint64
	PrevMeasuredOutputID            atomic.Uint64

	// to become a fake node myself:
	nodeboilerplate.Counters
	nodeboilerplate.InputFilter

	// private:
	startedCh     *chan struct{}
	waitGroup     sync.WaitGroup
	lastKeyFrames map[int]*ringbuffer.RingBuffer[packet.Input]
}

type OutputFactory interface {
	NewOutput(
		ctx context.Context,
		outputKey OutputKey,
	) (node.Abstract, OutputConfig, error)
}

func New(
	ctx context.Context,
	muxMode types.MuxMode,
	autoBitRate *AutoBitRateConfig,
	outputFactory OutputFactory,
) (*StreamMux[struct{}], error) {
	return NewWithCustomData[struct{}](
		ctx,
		muxMode,
		autoBitRate,
		outputFactory,
	)
}

func NewWithCustomData[C any](
	ctx context.Context,
	muxMode types.MuxMode,
	autoBitRate *AutoBitRateConfig,
	outputFactory OutputFactory,
) (*StreamMux[C], error) {
	s := &StreamMux[C]{
		OutputSwitch:  barrierstategetter.NewSwitch(),
		OutputSyncer:  barrierstategetter.NewSwitch(),
		OutputFactory: outputFactory,
		OutputsMap:    map[OutputKey]*Output{},
		MuxMode:       muxMode,

		lastKeyFrames: map[int]*ringbuffer.RingBuffer[packet.Input]{},
		startedCh:     ptr(make(chan struct{})),
	}
	s.InputNode = newInputNode[C](ctx, s)
	s.InputNodeAsPacketSource = s.InputNode.Processor.GetPacketSource()
	s.CurrentInputBitRate.Store(math.MaxUint64)
	s.initSwitches(ctx)

	if autoBitRate != nil {
		logger.Tracef(ctx, "enabling automatic bitrate control")
		if len(autoBitRate.ResolutionsAndBitRates) == 0 {
			return nil, fmt.Errorf("at least one resolution must be specified for automatic bitrate control")
		}
		h := s.initAutoBitRateHandler(*autoBitRate)
		observability.Go(ctx, func(ctx context.Context) {
			defer logger.Debugf(ctx, "autoBitRateHandler.ServeContext(): done")
			err := h.ServeContext(ctx)
			logger.Debugf(ctx, "autoBitRateHandler.ServeContext(): %v", err)
		})
	}

	return s, nil
}

func (s *StreamMux[C]) initSwitches(
	ctx context.Context,
) {
	s.OutputSwitch.SetKeepUnless(packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.IsKeyFrame(true),
	})

	if switchDebug {
		s.OutputSwitch.SetOnBeforeSwitch(func(ctx context.Context, in packetorframe.InputUnion, from, to int32) {
			logger.Debugf(ctx, "s.OutputSwitch.SetOnBeforeSwitch: %d -> %d", from, to)
			outputNext := xsync.DoR1(ctx, &s.Locker, func() *Output {
				return s.Outputs[to]
			})
			outputNext.FirstNodeAfterFilter().SetInputPacketFilter(nil)
		})
	}
	s.OutputSwitch.SetOnAfterSwitch(func(ctx context.Context, in packetorframe.InputUnion, from, to int32) {
		logger.Debugf(ctx, "s.OutputSwitch.SetOnAfterSwitch: %d -> %d", from, to)
		outputPrev := xsync.DoR1(ctx, &s.Locker, func() *Output {
			return s.Outputs[from]
		})
		if switchDebug {
			outputPrev.FirstNodeAfterFilter().SetInputPacketFilter(packetfiltercondition.Panic("somehow received a packet, while the output is inactive"))
		}

		observability.Go(ctx, func(ctx context.Context) {
			{
				ctx, cancelFn := context.WithTimeout(ctx, switchTimeout)
				defer cancelFn()
				err := outputPrev.FlushAfterFilter(ctx)
				if err != nil {
					logger.Errorf(ctx, "unable to flush the output %d: %v", from, err)
				}
			}
			logger.Debugf(ctx, "s.OutputSyncFilter.SetValue(ctx, %d): from %d", to, from)
			err := s.OutputSyncer.SetValue(ctx, to)
			logger.Debugf(ctx, "/s.OutputSyncFilter.SetValue(ctx, %d): from %d: %v", to, from, err)
		})
	})
	s.OutputSyncer.Flags.Set(barrierstategetter.SwitchFlagInactiveBlock)

	logger.Tracef(ctx, "o.OutputSwitch: %p", s.OutputSwitch)
	logger.Tracef(ctx, "o.OutputSyncer: %p", s.OutputSyncer)
}

func (s *StreamMux[C]) Close(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "StreamMux.Close()")
	defer func() { logger.Debugf(ctx, "/StreamMux.Close(): %v", _err) }()
	var errs []error

	if err := s.InputNode.GetProcessor().Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close the input node: %w", err))
	}
	for _, output := range s.Outputs {
		if err := output.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close the output chain %d: %w", output.ID, err))
		}
	}
	if s.AutoBitRateHandler != nil {
		if err := s.AutoBitRateHandler.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("unable to close auto bitrate handler: %w", err))
		}
	}
	return errors.Join(errs...)
}

func (s *StreamMux[C]) setPreferredOutput(
	ctx context.Context,
	outputKey OutputKey,
) (_err error) {
	logger.Tracef(ctx, "setPreferredOutput(ctx, %s)", outputKey)
	defer func() { logger.Tracef(ctx, "/setPreferredOutput(ctx, %s): %v", outputKey, _err) }()

	output := s.OutputsMap[outputKey]

	// the order is important to avoid race conditions:
	id1 := s.OutputSyncer.GetValue(ctx)
	id0 := s.OutputSwitch.GetValue(ctx)
	if id0 != id1 {
		return fmt.Errorf("an output switch process is already in progress (from %d to %d)", id0, id1)
	}

	err := s.OutputSwitch.SetValue(ctx, int32(output.ID))
	if err != nil {
		return fmt.Errorf("unable to set the preferred output %d:%s: %w", output.ID, outputKey, err)
	}

	return nil
}

func (s *StreamMux[C]) InitOutput(
	ctx context.Context,
	outputKey types.OutputKey,
	opts ...InitOutputOption,
) (output *Output, _err error) {
	logger.Tracef(ctx, "InitOutput(%#+v)", outputKey)
	defer func() { logger.Tracef(ctx, "/InitOutput(%#+v): %v", outputKey, _err) }()
	return xsync.DoA3R2(ctx, &s.Locker, s.getOrInitOutputLocked, ctx, outputKey, opts)
}

func (s *StreamMux[C]) getOrInitOutputLocked(
	ctx context.Context,
	outputKey types.OutputKey,
	opts []InitOutputOption,
) (_ret *Output, _err error) {
	logger.Tracef(ctx, "getOrInitOutputLocked: %#+v, %#+v", outputKey, opts)
	defer func() { logger.Tracef(ctx, "/getOrInitOutputLocked: %#+v, %#+v: %v, %v", outputKey, opts, _ret, _err) }()

	if outputKey.Resolution == (codec.Resolution{}) && outputKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return nil, fmt.Errorf("output resolution is not set")
	}

	if output, ok := s.OutputsMap[outputKey]; ok {
		return output, nil
	}
	switch s.MuxMode {
	case types.UndefinedMuxMode:
		return nil, fmt.Errorf("mux mode is not defined")
	case types.MuxModeForbid:
		if len(s.Outputs) > 0 {
			return nil, fmt.Errorf("mux mode %s forbids adding new outputs, but already have %d outputs", s.MuxMode, len(s.Outputs))
		}
	case types.MuxModeSameOutputSameTracks, types.MuxModeSameOutputDifferentTracks:
		if len(s.Outputs) > 0 {
			return s.Outputs[0], nil
		}
	}

	cfg := InitOutputOptions(opts).config()
	_ = cfg // currently unused

	outputID := len(s.Outputs)
	output, err := newOutput(
		ctx,
		outputID,
		s.Input(),
		s.OutputFactory, outputKey,
		s.OutputSwitch.Output(int32(outputID)),
		s.OutputSyncer.Output(int32(outputID)),
		newStreamIndexAssigner(s.MuxMode, outputID, s.InputNode.Processor.Kernel),
		s,
		s,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create an output: %w", err)
	}
	s.Outputs = append(s.Outputs, output)
	s.OutputsMap[outputKey] = output
	s.Input().AddPushPacketsTo(output.Input())
	err = avpipeline.NotifyAboutPacketSources(ctx, s.InputNodeAsPacketSource, output.Input())
	if err != nil {
		logger.Errorf(ctx, "received an error while notifying about packet sources (%s -> %s): %v", s.InputNodeAsPacketSource, output.Input(), err)
	}
	return output, nil
}

func (s *StreamMux[C]) EnableRecodingBypass(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "EnableRecodingBypass")
	defer func() { logger.Tracef(ctx, "/EnableRecodingBypass: %v", _err) }()
	return xsync.DoA1R1(ctx, &s.Locker, s.enableRecodingBypassLocked, ctx)
}

func (s *StreamMux[C]) enableRecodingBypassLocked(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "enableRecodingBypassLocked")
	defer func() { logger.Tracef(ctx, "/enableRecodingBypassLocked: %v: %v", _err) }()

	outputKey := OutputKey{
		AudioCodec: codectypes.Name(codec.NameCopy),
		VideoCodec: codectypes.Name(codec.NameCopy),
	}
	output, err := s.getOrInitOutputLocked(ctx, outputKey, nil)
	if err != nil {
		return fmt.Errorf("unable to get-or-initialize the bypass output: %w", err)
	}

	err = s.setPreferredOutput(ctx, outputKey)
	if err != nil {
		return fmt.Errorf("unable to set the preferred output %d:%s: %w", output.ID, outputKey, err)
	}
	s.OutputIDBeforeBypass = output.ID

	return nil
}

func (s *StreamMux[C]) Input() *NodeInput[C] {
	return s.InputNode
}

func (s *StreamMux[C]) GetRecoderConfig(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	logger.Tracef(ctx, "GetRecoderConfig")
	defer func() { logger.Tracef(ctx, "/GetRecoderConfig: %v", _ret) }()
	if s == nil {
		return types.RecoderConfig{}
	}
	return xsync.DoA1R1(ctx, &s.Locker, s.getRecoderConfigLocked, ctx)
}

func (s *StreamMux[C]) getRecoderConfigLocked(
	ctx context.Context,
) (_ret types.RecoderConfig) {
	assertNoError(json.Unmarshal(must(json.Marshal(s.RecodingConfig)), &_ret)) // deep copy of a poor man
	return
}

func (s *StreamMux[C]) SetRecoderConfig(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "SetRecoderConfig(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/SetRecoderConfig(ctx, %#+v): %v", cfg, _err) }()
	return xsync.DoA3R1(ctx, &s.Locker, s.setRecoderConfigLocked, ctx, cfg, true)
}

func (s *StreamMux[C]) setRecoderConfigLocked(
	ctx context.Context,
	cfg types.RecoderConfig,
	persistent bool,
) (_err error) {
	logger.Tracef(ctx, "setRecoderConfigLocked: %#+v", cfg)
	defer func() { logger.Tracef(ctx, "/setRecoderConfigLocked: %#+v: %v", cfg, _err) }()

	outputKey := OutputKeyFromRecoderConfig(ctx, &cfg)
	if outputKey.Resolution == (codec.Resolution{}) && outputKey.VideoCodec != codectypes.Name(codec.NameCopy) {
		return fmt.Errorf("output resolution is not set")
	}

	var err error
	output, err := s.getOrInitOutputLocked(ctx, outputKey, nil)
	if err != nil {
		return fmt.Errorf("unable to get-or-initialize the output %s: %w", outputKey, err)
	}

	logger.Debugf(ctx, "reconfiguring the output %d:%s", output.ID, outputKey)
	err = output.reconfigureRecoder(ctx, cfg)
	if err != nil {
		return fmt.Errorf("unable to reconfigure the output %d:%s: %w", output.ID, outputKey, err)
	}

	logger.Debugf(ctx, "notifying about the sources")
	err = avpipeline.NotifyAboutPacketSources(ctx,
		s.InputNodeAsPacketSource,
		output.InputFilter,
	)
	if err != nil {
		return fmt.Errorf("received an error while notifying nodes about packet sources (%s -> %s): %w", s.InputNodeAsPacketSource, output.InputFilter, err)
	}

	err = s.setPreferredOutput(ctx, outputKey)
	if err != nil {
		return fmt.Errorf("unable to set the preferred output %d:%s: %w", output.ID, outputKey, err)
	}

	if persistent {
		s.RecodingConfig = cfg
	}
	return nil
}

func (s *StreamMux[C]) GetAllStats(
	ctx context.Context,
) map[string]globaltypes.Statistics {
	return xsync.DoA1R1(ctx, &s.Locker, s.getAllStatsLocked, ctx)
}

func (s *StreamMux[C]) getAllStatsLocked(
	_ context.Context,
) map[string]globaltypes.Statistics {
	m := map[string]globaltypes.Statistics{}
	tryGetStats := func(key string, n node.Abstract) {
		m[key] = nodetypes.ToStatistics(n.GetCountersPtr(), n.GetProcessor().CountersPtr())
	}
	tryGetStats("Input", s.InputNode)
	for outputKey, output := range s.OutputsMap {
		tryGetStats(fmt.Sprintf("Output(%s):InputFilter", outputKey), output.InputFilter)
		tryGetStats(fmt.Sprintf("Output(%s):InputFixer", outputKey), output.InputFixer)
		tryGetStats(fmt.Sprintf("Output(%s):RecoderNode", outputKey), output.RecoderNode)
		tryGetStats(fmt.Sprintf("Output(%s):MapIndexes", outputKey), output.MapIndices)
		tryGetStats(fmt.Sprintf("Output(%s):OutputFixer", outputKey), output.OutputFixer)
		tryGetStats(fmt.Sprintf("Output(%s):OutputSyncFilter", outputKey), output.OutputSyncer)
	}
	return m
}

func (s *StreamMux[C]) Start(
	ctx context.Context,
	serveCfg node.ServeConfig,
) (_err error) {
	logger.Debugf(ctx, "Start(ctx)")
	defer logger.Debugf(ctx, "/Start(ctx): %v", _err)

	ctx, cancelFn := context.WithCancel(ctx)

	// == spawn an observer ==

	errCh := make(chan node.Error, 100)
	s.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer s.waitGroup.Done()
		defer cancelFn()
		logger.Debugf(ctx, "Serve: started the error listening loop")
		defer logger.Debugf(ctx, "Serve: finished the error listening loop")
		for {
			select {
			case err := <-ctx.Done():
				logger.Debugf(ctx, "stopping listening for errors: %v", err)
				return
			case err, ok := <-errCh:
				if !ok {
					logger.Debugf(ctx, "the error channel is closed")
					return
				}
				if errors.Is(err.Err, node.ErrAlreadyStarted{}) {
					logger.Errorf(ctx, "%#+v", err)
					continue
				}
				cancelFn()
				if errors.Is(err.Err, context.Canceled) {
					logger.Debugf(ctx, "cancelled: %#+v", err)
					continue
				}
				if errors.Is(err.Err, io.EOF) {
					logger.Debugf(ctx, "EOF: %#+v", err)
					continue
				}
				logger.Errorf(ctx, "stopping because received error: %v", err)
				return
			}
		}
	})

	// == prepare ==

	logger.Debugf(ctx, "resulting graph: %s", node.Nodes[node.Abstract]{s.InputNode}.StringRecursive())
	logger.Debugf(ctx, "resulting graph (graphviz): %s", node.Nodes[node.Abstract]{s.InputNode}.DotString(false))

	// == launch ==

	s.waitGroup.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer s.waitGroup.Done()
		defer cancelFn()
		defer logger.Debugf(ctx, "finished the serving routine")

		s.Serve(ctx, serveCfg, errCh)
	})

	return nil
}

func (s *StreamMux[C]) WaitForStartChan() <-chan struct{} {
	return *xatomic.LoadPointer(&s.startedCh)
}

func (s *StreamMux[C]) WaitForStart(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "WaitForStart")
	defer func() { logger.Tracef(ctx, "/WaitForStart: %v", _err) }()
	select {
	case <-s.WaitForStartChan():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *StreamMux[C]) WaitForStop(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "WaitForStop")
	defer func() { logger.Tracef(ctx, "/WaitForStop: %v", _err) }()
	s.waitGroup.Wait()
	return nil
}

func (s *StreamMux[C]) WaitForActiveOutput(
	ctx context.Context,
) *Output {
	t := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			output := s.GetActiveOutput(ctx)
			if output != nil {
				return output
			}
		}
	}
}

func (s *StreamMux[C]) waitForActiveOutputLocked(
	ctx context.Context,
) *Output {
	t := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			output := s.getActiveOutputLocked(ctx)
			if output != nil {
				return output
			}
		}
	}
}

func (s *StreamMux[C]) GetActiveOutput(
	ctx context.Context,
) *Output {
	return xsync.DoA1R1(ctx, &s.Locker, s.getActiveOutputLocked, ctx)
}

func (s *StreamMux[C]) getActiveOutputLocked(
	ctx context.Context,
) *Output {
	outputID := s.OutputSwitch.CurrentValue.Load()
	logger.Tracef(ctx, "getActiveOutputLocked: outputID=%d", outputID)
	if int(outputID) < 0 || int(outputID) >= len(s.Outputs) {
		return nil
	}
	return s.Outputs[outputID]
}

type ErrNotImplemented struct {
	Err error
}

func (e ErrNotImplemented) Error() string {
	return fmt.Sprintf("not implemented: %v", e.Err)
}

func (s *StreamMux[C]) setResolutionBitRateCodec(
	ctx context.Context,
	res codec.Resolution,
	bitrate uint64,
	videoCodec codectypes.Name,
	audioCodec codectypes.Name,
) (_err error) {
	logger.Tracef(ctx, "setResolutionBitRateCodec: %v, %d, '%s', '%s'", res, bitrate, videoCodec, audioCodec)
	defer func() {
		logger.Tracef(ctx, "/setResolutionBitRateCodec: %v, %d, '%s', '%s': %v", res, bitrate, videoCodec, audioCodec, _err)
	}()
	return xsync.DoR1(ctx, &s.Locker, func() error {
		return s.setResolutionBitRateCodecLocked(ctx, res, bitrate, videoCodec, audioCodec)
	})
}

func (s *StreamMux[C]) setResolutionBitRateCodecLocked(
	ctx context.Context,
	res codec.Resolution,
	bitrate uint64,
	videoCodec codectypes.Name,
	audioCodec codectypes.Name,
) (_err error) {
	cfg := s.getRecoderConfigLocked(ctx)
	if len(cfg.Output.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (have %d)", len(cfg.Output.AudioTrackConfigs))
	}
	audioCfg := cfg.Output.AudioTrackConfigs[0]

	if len(cfg.Output.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (have %d)", len(cfg.Output.VideoTrackConfigs))
	}
	videoCfg := cfg.Output.VideoTrackConfigs[0]

	if strings.HasSuffix(string(videoCfg.CodecName), "_mediacodec") && res.Height < 720 {
		// TODO: this should not be here, it should be somewhere else.
		return ErrNotImplemented{Err: fmt.Errorf("when scaling from 1080p to let's say 480p, we get a distorted image when using mediacodec, to be investigated; until then this is forbidden")}
	}

	if videoCfg.Resolution == res && videoCfg.CodecName == videoCodec && audioCfg.CodecName == audioCodec {
		logger.Tracef(ctx, "the config is already set to %v '%s' '%s'", res, videoCodec, audioCodec)
		encoderV, _ := s.getVideoEncoderLocked(ctx)
		if videoCodec == codectypes.Name(codec.NameCopy) != codec.IsEncoderCopy(encoderV) {
			logger.Errorf(ctx, "the video codec is set to '%s', but the encoder is %s", videoCodec, encoderV)
		} else {
			videoCfg.AverageBitRate = bitrate
			cfg.Output.VideoTrackConfigs[0] = videoCfg
			return nil
		}
	}

	audioCfg.CodecName = audioCodec
	cfg.Output.AudioTrackConfigs[0] = audioCfg

	videoCfg.Resolution = res
	videoCfg.AverageBitRate = bitrate
	videoCfg.CodecName = videoCodec
	cfg.Output.VideoTrackConfigs[0] = videoCfg

	return s.setRecoderConfigLocked(ctx, cfg, false)
}

func (s *StreamMux[C]) SetFPSFraction(ctx context.Context, num, den uint32) {
	logger.Debugf(ctx, "SetFPSFraction: %d/%d", num, den)
	s.FPSFractionNumDen.Store((uint64(num) << 32) | uint64(den))
}

func (s *StreamMux[C]) GetFPSFraction(ctx context.Context) (num, den uint32) {
	numDen := s.FPSFractionNumDen.Load()
	num = uint32(numDen >> 32)
	den = uint32(numDen & 0xFFFFFFFF)
	return
}

func (s *StreamMux[C]) inputBitRateMeasurerLoop(
	ctx context.Context,
) (_err error) {
	logger.Tracef(ctx, "inputBitRateMeasurerLoop")
	defer func() { logger.Tracef(ctx, "/inputBitRateMeasurerLoop: %v", _err) }()

	t := time.NewTicker(time.Second / 4)
	defer t.Stop()
	activeOutput := s.WaitForActiveOutput(ctx)
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	assert(ctx, activeOutput != nil)
	assert(ctx, activeOutput.OutputNode != nil)
	inputCounters := s.InputNode.GetCountersPtr()
	bytesInputReadTotalPrev := inputCounters.Received.TotalBytes()
	outputCounters := activeOutput.OutputNode.GetCountersPtr()
	bytesOutputReadTotalPrev := outputCounters.Received.TotalBytes()
	s.PrevMeasuredOutputID.Store(uint64(activeOutput.ID))
	tsPrev := time.Now()
	for {
		var tsNext time.Time
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tsNext = <-t.C:
			duration := tsNext.Sub(tsPrev)
			activeOutput := s.WaitForActiveOutput(ctx)
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			assert(ctx, activeOutput != nil)
			assert(ctx, activeOutput.OutputNode != nil)
			assert(ctx, activeOutput.OutputNode.GetCountersPtr() != nil)

			inputCounters := s.InputNode.GetCountersPtr()
			bytesInputReadTotalNext := inputCounters.Received.TotalBytes()

			outputCounters := activeOutput.OutputNode.GetCountersPtr()
			bytesOutputReadTotalNext := outputCounters.Received.TotalBytes()

			bytesInputRead := bytesInputReadTotalNext - bytesInputReadTotalPrev
			bitRateInput := int(float64(bytesInputRead*8) / duration.Seconds())

			oldInputValue := s.CurrentInputBitRate.Load()
			newInputValue := updateWithInertialValue(oldInputValue, uint64(bitRateInput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			logger.Debugf(ctx, "input bitrate: %d (%s): (%d-%d)*8/%v; setting %d as the new value",
				bitRateInput, humanize.SI(float64(bitRateInput), "bps"),
				bytesInputReadTotalNext, bytesInputReadTotalPrev, duration,
				newInputValue,
			)
			s.CurrentInputBitRate.Store(newInputValue)

			bytesOutputRead := bytesOutputReadTotalNext - bytesOutputReadTotalPrev
			bitRateOutput := int(float64(bytesOutputRead*8) / duration.Seconds())

			oldOutputValue := s.CurrentOutputBitRate.Load()
			newOutputValue := updateWithInertialValue(oldOutputValue, uint64(bitRateOutput), 0.9, s.CurrentBitRateMeasurementsCount.Load())
			logger.Debugf(ctx, "output bitrate: %d (%s): (%d-%d)*8/%v; setting %d as the new value",
				bitRateOutput, humanize.SI(float64(bitRateOutput), "bps"),
				bytesOutputReadTotalNext, bytesOutputReadTotalPrev, duration,
				newOutputValue,
			)
			if s.PrevMeasuredOutputID.Load() == uint64(activeOutput.ID) {
				s.CurrentOutputBitRate.Store(newOutputValue)
			} else {
				s.PrevMeasuredOutputID.Store(uint64(activeOutput.ID))
			}

			s.CurrentBitRateMeasurementsCount.Add(1)

			bytesInputReadTotalPrev, bytesOutputReadTotalPrev = bytesInputReadTotalNext, bytesOutputReadTotalNext
			tsPrev = tsNext
		}
	}
}

func updateWithInertialValue(
	oldValue, newValue uint64,
	inertia float64,
	measurementsCount uint64,
) uint64 {
	// make it volatile in the beginning, and more stable later:
	effectiveInertia := inertia * (float64(measurementsCount) / float64(measurementsCount+3))
	return uint64(float64(oldValue)*effectiveInertia + float64(newValue)*(1-effectiveInertia))
}

func (s *StreamMux[C]) GetBestNotBypassOutput(
	ctx context.Context,
) (_ret *Output) {
	logger.Tracef(ctx, "GetBestNotBypassOutput")
	defer func() { logger.Tracef(ctx, "/GetBestNotBypassOutput: %v", _ret) }()
	return xsync.DoA1R1(ctx, &s.Locker, s.getBestNotBypassOutputLocked, ctx)
}

func (s *StreamMux[C]) getBestNotBypassOutputLocked(
	ctx context.Context,
) (_ret *Output) {
	outputKeys := OutputKeys(slices.Collect(maps.Keys(s.OutputsMap)))
	outputKeys.Sort()
	for _, outputKey := range outputKeys {
		if outputKey.VideoCodec == codectypes.Name(codec.NameCopy) {
			continue
		}
		return s.OutputsMap[outputKey]
	}
	return nil
}

func (s *StreamMux[C]) GetEncoders(
	ctx context.Context,
) (codec.Encoder, codec.Encoder) {
	return xsync.DoA1R2(ctx, &s.Locker, s.getVideoEncoderLocked, ctx)
}

func (s *StreamMux[C]) getVideoEncoderLocked(
	ctx context.Context,
) (codec.Encoder, codec.Encoder) {
	o := s.getActiveOutputLocked(ctx)
	if o == nil {
		return nil, nil
	}

	var (
		vEnc codec.Encoder
		aEnc codec.Encoder
	)

	vEncoders := o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.VideoEncoders
	if len(vEncoders) == 1 {
		vEnc = vEncoders[0]
	}

	aEncoders := o.RecoderNode.Processor.Kernel.Encoder.EncoderFactory.AudioEncoders
	if len(aEncoders) == 1 {
		aEnc = aEncoders[0]
	}

	return vEnc, aEnc
}

func (s *StreamMux[C]) onInputPacket(
	ctx context.Context,
	pkt *packet.Input,
) (_err error) {
	logger.Tracef(ctx, "onInputPacket: %s", pkt)
	defer func() { logger.Tracef(ctx, "/onInputPacket: %s: %v", pkt, _err) }()

	if pkt.GetMediaType() != astiav.MediaTypeVideo {
		return nil
	}

	isKey := pkt.Flags().Has(astiav.PacketFlagKey)
	if !isKey {
		return nil
	}

	streamIndex := pkt.StreamIndex()

	s.Locker.Do(ctx, func() {
		if s.lastKeyFrames[streamIndex] == nil {
			s.lastKeyFrames[streamIndex] = ringbuffer.New[packet.Input](2)
		}
		s.lastKeyFrames[streamIndex].Add(packet.BuildInput(
			packet.CloneAsReferenced(pkt.Packet),
			pkt.StreamInfo,
		))
	})
	return nil
}

func (s *StreamMux[C]) InitOutputStreams(
	ctx context.Context,
	receiver node.Abstract,
	outputKey OutputKey,
) (_err error) {
	isBypass := outputKey.VideoCodec == codectypes.Name(codec.NameCopy)
	logger.Debugf(ctx, "InitOutputStreams: isBypass:%v", isBypass)
	defer func() { logger.Debugf(ctx, "/InitOutputStreams: isBypass:%v: %v", isBypass, _err) }()
	if !isBypass {
		return nil
	}
	return xsync.DoA2R1(ctx, &s.Locker, s.initOutputStreamsLocked, ctx, receiver)
}

func (s *StreamMux[C]) initOutputStreamsLocked(
	ctx context.Context,
	receiver node.Abstract,
) (_err error) {
	logger.Tracef(ctx, "initOutputStreamsLocked")
	defer func() { logger.Tracef(ctx, "/initOutputStreamsLocked: %v", _err) }()
	counters := receiver.GetCountersPtr()
	inputCh := receiver.GetProcessor().InputPacketChan()
	var streamIndices []int
	for streamIndex := range s.lastKeyFrames {
		streamIndices = append(streamIndices, streamIndex)
	}
	slices.Sort(streamIndices)
	for _, streamIndex := range streamIndices {
		buf := s.lastKeyFrames[streamIndex]
		items := buf.GetAll()
		logger.Debugf(ctx, "initOutputStreamsLocked: re-sending %d last keyframes for stream index %d", len(items), streamIndex)
		for _, pkt := range items {
			mediaType := globaltypes.MediaType(pkt.GetMediaType())
			dataSize := uint64(len(pkt.Data()))
			counters.Addressed.Packets.Get(mediaType).Increment(dataSize)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case inputCh <- pkt:
			}
			counters.Received.Packets.Get(mediaType).Increment(dataSize)
		}
	}
	return nil
}
