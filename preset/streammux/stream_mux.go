package streammux

import (
	"context"
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
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
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
	nodeboilerplate.Statistics
	nodeboilerplate.InputFilter

	startedCh *chan struct{}
	waitGroup sync.WaitGroup
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
		InputNode:     newInputNode[C](ctx),
		OutputSwitch:  barrierstategetter.NewSwitch(),
		OutputSyncer:  barrierstategetter.NewSwitch(),
		OutputFactory: outputFactory,
		OutputsMap:    map[OutputKey]*Output{},
		MuxMode:       muxMode,

		startedCh: ptr(make(chan struct{})),
	}
	s.InputNodeAsPacketSource = s.InputNode.Processor.GetPacketSource()
	s.CurrentInputBitRate.Store(math.MaxUint64)
	s.initSwitches()

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

func (s *StreamMux[C]) initSwitches() {
	s.OutputSwitch.SetKeepUnless(packetorframecondition.And{
		packetorframecondition.MediaType(astiav.MediaTypeVideo),
		packetorframecondition.IsKeyFrame(true),
	})

	var (
		prevDataLocker sync.Mutex
		prevOutputID   int32
	)

	type switchNotifierPacket struct {
		SwitchFromOutputID int32
	}

	// We want to notify the OutputSyncer that it needs to switch to the next output, and
	// send a packet that would notify that there are no packets left in the previous chain.
	s.OutputSwitch.Flags.Set(barrierstategetter.SwitchFlagFirstPacketAfterSwitchPassBothOutputs)
	s.OutputSwitch.SetOnAfterSwitch(func(ctx context.Context, in packetorframe.InputUnion, from, to int32) {
		logger.Debugf(ctx, "prevDataLocker.Lock()")
		prevDataLocker.Lock()
		prevOutputID = from
		logger.Debugf(ctx, "s.OutputSyncFilter.SetValue(ctx, %d): from %d", to, from)
		err := s.OutputSyncer.SetValue(ctx, to)
		logger.Debugf(ctx, "/s.OutputSyncFilter.SetValue(ctx, %d): from %d: %v", to, from, err)

		// this will be our notification packet (that nothing is left in the previous chain):
		in.Packet.SetFlags(in.Packet.Flags().Add(astiav.PacketFlagKey))
		in.Packet.PipelineSideData = append(in.Packet.PipelineSideData, switchNotifierPacket{
			SwitchFromOutputID: from,
		})

		// just in case let's make sure we will not block forever:
		observability.Go(ctx, func(ctx context.Context) {
			t := time.NewTicker(100 * time.Millisecond)
			deadline := time.NewTimer(time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-deadline.C:
					logger.Debugf(ctx, "giving up waiting for the previous output chain to be flushed")
					s.OutputSyncer.CommitMutex.Do(ctx, func() {
						s.OutputSyncer.CurrentValue.Store(to)
						prevDataLocker.Unlock()
					})
				case <-t.C:
				}

				if prevDataLocker.TryLock() {
					prevDataLocker.Unlock()
					return
				}

				s.Locker.Do(ctx, func() {
					err := s.Outputs[from].Flush(ctx)
					if err != nil {
						logger.Errorf(ctx, "unable to flush the output %d: %v", from, err)
					}
				})
			}
		})
	})

	// Waiting for the "last packet" notification to reach the OutputSyncer on the active
	// output chain, since we want to make the final switch only after the last packet in the
	// chain is processed.
	s.OutputSyncer.Flags.Set(barrierstategetter.SwitchFlagForbidTakeoverInKeepUnless)
	s.OutputSyncer.SetKeepUnless(
		packetorframecondition.And{
			packetorframecondition.HasPipelineSideData(switchNotifierPacket{
				SwitchFromOutputID: prevOutputID,
			}),
			// unlocking prevDataLocker if ready to switch to the next output
			packetorframecondition.Function(func(ctx context.Context, in packetorframe.InputUnion) bool {
				if s.OutputSyncer.CurrentValue.Load() == prevOutputID {
					logger.Debugf(ctx, "prevDataLocker.Unlock()")
					prevDataLocker.Unlock()
				}
				return true
			}),
		},
	)
	// keep/hold the traffic flowing to the next output if the OutputSyncer is not switched yet
	s.OutputSyncer.Flags.Set(barrierstategetter.SwitchFlagNextOutputStateBlock)
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
	return s.RecodingConfig
}

func (s *StreamMux[C]) SetRecoderConfig(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
	logger.Tracef(ctx, "SetRecoderConfig(ctx, %#+v)", cfg)
	defer func() { logger.Tracef(ctx, "/SetRecoderConfig(ctx, %#+v): %v", cfg, _err) }()
	return xsync.DoA2R1(ctx, &s.Locker, s.setRecoderConfigLocked, ctx, cfg)
}

func (s *StreamMux[C]) setRecoderConfigLocked(
	ctx context.Context,
	cfg types.RecoderConfig,
) (_err error) {
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

	s.RecodingConfig = cfg
	return nil
}

func (s *StreamMux[C]) GetAllStats(
	ctx context.Context,
) map[string]*node.ProcessingStatistics {
	return xsync.DoA1R1(ctx, &s.Locker, s.getAllStatsLocked, ctx)
}

func (s *StreamMux[C]) getAllStatsLocked(
	_ context.Context,
) map[string]*node.ProcessingStatistics {
	m := map[string]*node.ProcessingStatistics{}
	tryGetStats := func(key string, n node.Abstract) {
		getter, ok := n.(interface {
			GetStats() *node.ProcessingStatistics
		})
		if !ok {
			return
		}
		m[key] = getter.GetStats()
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

func (s *StreamMux[C]) GetActiveOutput(
	ctx context.Context,
) *Output {
	return xsync.DoA1R1(ctx, &s.Locker, s.getActiveOutputLocked, ctx)
}

func (s *StreamMux[C]) getActiveOutputLocked(
	ctx context.Context,
) *Output {
	outputID := s.OutputSwitch.CurrentValue.Load()
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
	logger.Tracef(ctx, "setResolutionBitRateCodec: %v, %d, %s, %s", res, bitrate, videoCodec, audioCodec)
	defer func() {
		logger.Tracef(ctx, "/setResolutionBitRateCodec: %v, %d, %s, %s: %v", res, bitrate, videoCodec, audioCodec, _err)
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
	if len(cfg.AudioTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (have %d)", len(cfg.AudioTrackConfigs))
	}
	audioCfg := cfg.AudioTrackConfigs[0]

	if len(cfg.VideoTrackConfigs) != 1 {
		return fmt.Errorf("currently we support only exactly one output video track config (have %d)", len(cfg.VideoTrackConfigs))
	}
	videoCfg := cfg.VideoTrackConfigs[0]

	if strings.HasSuffix(string(videoCfg.CodecName), "_mediacodec") && res.Height < 720 {
		// TODO: this should not be here, it should be somewhere else.
		return ErrNotImplemented{Err: fmt.Errorf("when scaling from 1080p to let's say 480p, we get a distorted image when using mediacodec, to be investigated; until then this is forbidden")}
	}

	audioCfg.CodecName = audioCodec
	cfg.AudioTrackConfigs[0] = audioCfg

	curRes := videoCfg.Resolution
	if curRes == res {
		logger.Tracef(ctx, "the resolution is already set to %s", res)
		return nil
	}
	videoCfg.Resolution = res
	videoCfg.AverageBitRate = bitrate
	videoCfg.CodecName = videoCodec
	cfg.VideoTrackConfigs[0] = videoCfg
	return s.setRecoderConfigLocked(ctx, cfg)
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

	t := time.NewTicker(time.Second)
	defer t.Stop()
	activeOutput := s.GetActiveOutput(ctx)
	bytesInputReadTotalPrev := int64(s.InputNode.Statistics.BytesCountRead.Load())
	bytesOutputReadTotalPrev := int64(activeOutput.OutputNode.GetStatistics().BytesCountRead.Load())
	s.PrevMeasuredOutputID.Store(uint64(activeOutput.ID))
	tsPrev := time.Now()
	for {
		var tsNext time.Time
		select {
		case <-ctx.Done():
			return ctx.Err()
		case tsNext = <-t.C:
			duration := tsNext.Sub(tsPrev)
			activeOutput := s.GetActiveOutput(ctx)
			bytesInputReadTotalNext := int64(s.GetStatistics().BytesCountRead.Load())
			bytesOutputReadTotalNext := int64(activeOutput.OutputNode.GetStatistics().BytesCountRead.Load())

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
