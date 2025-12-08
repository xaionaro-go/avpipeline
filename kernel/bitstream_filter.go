package kernel

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type InternalBitstreamFilterInstance struct {
	*astiav.BitStreamFilter
	*astiav.BitStreamFilterContext
	Params bitstreamfilter.Params
}

type BitstreamFilter struct {
	*closuresignaler.ClosureSignaler
	xsync.Mutex
	GetChainParamser    bitstreamfilter.GetChainParamser
	FilterChains        map[int][]*InternalBitstreamFilterInstance
	OutputFormatContext *astiav.FormatContext
	OutputStreams       map[int]*astiav.Stream

	SentPacketsWithoutOutput uint64
}

var _ Abstract = (*BitstreamFilter)(nil)
var _ packet.Source = (*BitstreamFilter)(nil)

func NewBitstreamFilter(
	ctx context.Context,
	paramsGetter bitstreamfilter.GetChainParamser,
) (*BitstreamFilter, error) {
	bsf := &BitstreamFilter{
		ClosureSignaler:     closuresignaler.New(),
		GetChainParamser:    paramsGetter,
		FilterChains:        make(map[int][]*InternalBitstreamFilterInstance),
		OutputFormatContext: astiav.AllocFormatContext(),
		OutputStreams:       make(map[int]*astiav.Stream),
	}
	setFinalizerFree(ctx, bsf.OutputFormatContext)
	return bsf, nil
}

func (bsf *BitstreamFilter) getFilterChain(
	ctx context.Context,
	input packet.Input,
) ([]*InternalBitstreamFilterInstance, error) {
	if r, ok := bsf.FilterChains[input.GetStreamIndex()]; ok {
		return r, nil
	}

	paramss := bsf.GetChainParamser.GetChainParams(ctx, input)
	if paramss == nil {
		return nil, nil
	}

	var filterChain []*InternalBitstreamFilterInstance
	for _, params := range paramss {
		_bsf := astiav.FindBitStreamFilterByName(string(params.Name))
		if _bsf == nil {
			return nil, fmt.Errorf("unable to find a bitstream filter '%s'", string(params.Name))
		}

		bsfCtx, err := astiav.AllocBitStreamFilterContext(_bsf)
		if err != nil {
			return nil, fmt.Errorf("unable to allocate a BitStreamFilter context: %w", err)
		}
		setFinalizerFree(ctx, bsfCtx)

		if err := input.Stream.CodecParameters().Copy(bsfCtx.InputCodecParameters()); err != nil {
			return nil, fmt.Errorf("unable to copy codec parameters: %w", err)
		}

		bsfCtx.SetInputTimeBase(input.Stream.TimeBase())

		if err := bsfCtx.Initialize(); err != nil {
			return nil, fmt.Errorf("unable to initialize the bitstream filter: %w", err)
		}

		filterChain = append(filterChain, &InternalBitstreamFilterInstance{
			BitStreamFilter:        _bsf,
			BitStreamFilterContext: bsfCtx,
			Params:                 params,
		})
	}
	bsf.FilterChains[input.GetStreamIndex()] = filterChain
	return filterChain, nil
}

func (bsf *BitstreamFilter) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket(ctx, input, outputPacketsCh, _)")
	defer func() { logger.Tracef(ctx, "/SendInputPacket(ctx, input, outputPacketsCh, _): %v", _err) }()
	return xsync.DoA3R1(ctx, &bsf.Mutex, bsf.sendInputPacket, ctx, input, outputPacketsCh)
}

func (bsf *BitstreamFilter) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
) (_err error) {
	filterChain, err := bsf.getFilterChain(ctx, input)
	if err != nil {
		return fmt.Errorf("unable to get a filter for stream #%d: %w", input.StreamIndex(), err)
	}
	if len(filterChain) == 0 {
		bsf.Mutex.UDo(ctx, func() {
			select {
			case <-ctx.Done():
			case outputPacketsCh <- packet.BuildOutput(
				input.Packet,
				input.StreamInfo,
			):
			}
		})
		return ctx.Err()
	}

	pkts := []*astiav.Packet{packet.CloneAsReferenced(input.Packet)}
	for idx, filter := range filterChain {
		for _, pkt := range pkts {
			bsf.SentPacketsWithoutOutput++
			logger.Tracef(ctx, "sending a packet to %s", filter.Name())
			err = filter.SendPacket(pkt)
			packet.Pool.Put(pkt)
			if err != nil {
				if filter.Params.SkipOnFailure {
					logger.Debugf(ctx, "received failure %v (on sending), skipping", err)
					continue
				}
				return fmt.Errorf("unable to send the packet to the filter: %w", err)
			}
		}
		pkts = pkts[:0]

		for {
			pkt := packet.Pool.Get()
			logger.Tracef(ctx, "receiving a packet from %s", filter.Name())
			err := filter.ReceivePacket(pkt)
			if err != nil {
				isEOF := errors.Is(err, astiav.ErrEof)
				isEAgain := errors.Is(err, astiav.ErrEagain)
				logger.Tracef(ctx, "bsf.ReceivePacket(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
				packet.Pool.Pool.Put(pkt)
				if isEOF || isEAgain {
					break
				}
				if filter.Params.SkipOnFailure {
					logger.Debugf(ctx, "received failure %v (on receiving), skipping", err)
					continue
				}
				return fmt.Errorf("unable receive the packet from the filter: %w", err)
			}

			outCodecParams := filter.BitStreamFilterContext.OutputCodecParameters()

			if packetSideData := pkt.SideData(); packetSideData != nil {
				if newExtraData, ok := packetSideData.NewExtraData().Get(); ok {
					extraData := extradata.Raw(newExtraData)
					logger.Debugf(ctx, "updating extra data for output stream #%d: %s", input.StreamIndex(), extraData)
					outCodecParams.SetExtraData(newExtraData)
					if idx < len(filterChain)-1 {
						nextFilter := filterChain[idx+1]
						if err := nextFilter.BitStreamFilterContext.InputCodecParameters().Copy(outCodecParams); err != nil {
							return fmt.Errorf("unable to copy updated codec parameters to the next filter in the chain: %w", err)
						}
					}
				}
			}

			bsf.SentPacketsWithoutOutput = 0
			logger.Tracef(ctx,
				"received a %s packet from %s (isKey:%v)",
				outCodecParams.MediaType(),
				filter.Name(),
				pkt.Flags().Has(astiav.PacketFlagKey),
			)
			pkts = append(pkts, pkt)
		}

		if enableAntiStucking {
			if bsf.SentPacketsWithoutOutput > 30 {
				logger.Errorf(ctx, "bitstream filter %s seems stuck (did not produce any output packets for %d input packets); resetting the filter chain", filter.Name(), bsf.SentPacketsWithoutOutput)
				bsf.FilterChains[input.StreamIndex()] = nil
			}
		}
	}

	latestFilter := filterChain[len(filterChain)-1]
	codecParams := latestFilter.BitStreamFilterContext.OutputCodecParameters()
	logger.Debugf(ctx,
		"stream #%d: bitstream filter chain %s produced %d output packets",
		input.StreamIndex(),
		bsf.GetChainParamser,
		len(pkts),
	)

	outputStream := bsf.getOutputStream(ctx, input.StreamIndex(), codecParams, input.Stream.TimeBase())
	input.StreamInfo = &packet.StreamInfo{
		Stream:           outputStream,
		Source:           bsf,
		PipelineSideData: input.PipelineSideData,
	}

	bsf.Mutex.UDo(ctx, func() {
		for _, pkt := range pkts {
			pkt := packet.BuildOutput(
				pkt,
				input.StreamInfo,
			)
			select {
			case <-ctx.Done():
				return
			case outputPacketsCh <- pkt:
			}
		}
	})
	return ctx.Err()
}

func (bsf *BitstreamFilter) getOutputStream(
	ctx context.Context,
	inputStreamIndex int,
	codecParams *astiav.CodecParameters,
	timeBase astiav.Rational,
) *astiav.Stream {
	if outputStream, ok := bsf.OutputStreams[inputStreamIndex]; ok {
		return outputStream
	}

	outputStream := bsf.OutputFormatContext.NewStream(nil)
	codecParams.Copy(outputStream.CodecParameters())
	outputStream.SetTimeBase(timeBase)
	outputStream.SetIndex(inputStreamIndex)
	bsf.OutputStreams[inputStreamIndex] = outputStream

	logger.Debugf(
		ctx,
		"new output stream %d: %s: %s: %s: %s: %s",
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)
	return outputStream
}

func (bsf *BitstreamFilter) SendInputFrame(
	context.Context,
	frame.Input,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return fmt.Errorf("BitstreamFilter could be used only for Packet-s, but not for Frame-s")
}

func (bsf *BitstreamFilter) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(bsf)
}

func (bsf *BitstreamFilter) String() string {
	return fmt.Sprintf("BitstreamFilter(%s)", bsf.GetChainParamser)
}

func (bsf *BitstreamFilter) Close(ctx context.Context) error {
	bsf.ClosureSignaler.Close(ctx)
	return nil
}

func (bsf *BitstreamFilter) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}

func (bsf *BitstreamFilter) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	bsf.Do(ctx, func() {
		callback(bsf.OutputFormatContext)
	})
}
