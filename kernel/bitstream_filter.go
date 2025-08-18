package kernel

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
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
	GetChainParamser bitstreamfilter.GetChainParamser
	FilterChains     map[int][]*InternalBitstreamFilterInstance
}

var _ Abstract = (*BitstreamFilter)(nil)

func NewBitstreamFilter(
	ctx context.Context,
	paramsGetter bitstreamfilter.GetChainParamser,
) (*BitstreamFilter, error) {
	bsf := &BitstreamFilter{
		ClosureSignaler:  closuresignaler.New(),
		GetChainParamser: paramsGetter,
		FilterChains:     make(map[int][]*InternalBitstreamFilterInstance),
	}
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

	pkts := []*astiav.Packet{packet.CloneAsReferenced(input.Packet)}
	for _, filter := range filterChain {
		for _, pkt := range pkts {
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

			logger.Tracef(ctx, "received a packet from %s", filter.Name())
			pkts = append(pkts, pkt)
		}
	}

	bsf.Mutex.UDo(ctx, func() {
		for _, pkt := range pkts {
			pkt := packet.BuildOutput(
				pkt,
				input.Stream,
				input.Source,
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

func (bsf *BitstreamFilter) SendInputFrame(
	context.Context,
	frame.Input,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return fmt.Errorf("BitstreamFilter could be used only for Packet-s, but not for Frame-s")
}

func (bsf *BitstreamFilter) String() string {
	return "BitstreamFilter"
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
