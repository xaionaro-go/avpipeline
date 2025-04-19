package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/bitstreamfilter"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/xsync"
)

type InternalBitstreamFilterInstance struct {
	*astiav.BitStreamFilter
	*astiav.BitStreamFilterContext
}

type BitstreamFilter struct {
	*closeChan
	xsync.Mutex
	Names   map[condition.Condition]bitstreamfilter.Name
	Filters map[int]*InternalBitstreamFilterInstance
}

var _ Abstract = (*BitstreamFilter)(nil)

func NewBitstreamFilter(
	ctx context.Context,
	names map[condition.Condition]bitstreamfilter.Name,
) (*BitstreamFilter, error) {
	logger.Debugf(ctx, "requested bitstream filters: %v", names)
	bsf := &BitstreamFilter{
		closeChan: newCloseChan(),
		Names:     names,
		Filters:   make(map[int]*InternalBitstreamFilterInstance),
	}
	return bsf, nil
}

func (bsf *BitstreamFilter) getFilter(
	ctx context.Context,
	input packet.Input,
) (*InternalBitstreamFilterInstance, error) {
	if r, ok := bsf.Filters[input.GetStreamIndex()]; ok {
		return r, nil
	}

	var bsfName bitstreamfilter.Name
	for cond, name := range bsf.Names {
		if !cond.Match(ctx, input) {
			continue
		}
		if bsfName != "" && bsfName != name {
			return nil, fmt.Errorf("two different BitstreamFilters requested")
		}
		bsfName = name
	}
	if bsfName == "" {
		return nil, nil
	}

	_bsf := astiav.FindBitStreamFilterByName(string(bsfName))
	if _bsf == nil {
		return nil, fmt.Errorf("unable to find a bitstream filter '%s'", bsfName)
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

	r := &InternalBitstreamFilterInstance{
		BitStreamFilter:        _bsf,
		BitStreamFilterContext: bsfCtx,
	}
	bsf.Filters[input.GetStreamIndex()] = r
	return r, nil
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
) error {
	bsfInstance, err := bsf.getFilter(ctx, input)
	if err != nil {
		return fmt.Errorf("unable to get a filter for stream #%d: %w", input.StreamIndex(), err)
	}

	if bsfInstance == nil {
		outputPacketsCh <- packet.BuildOutput(
			packet.CloneAsReferenced(input.Packet),
			input.Stream,
			input.Source,
		)
		return nil
	}

	err = bsfInstance.BitStreamFilterContext.SendPacket(input.Packet)
	if err != nil {
		return fmt.Errorf("unable to send the packet to the filter: %w", err)
	}

	for {
		pkt := packet.Pool.Get()
		err := bsfInstance.BitStreamFilterContext.ReceivePacket(pkt)
		if err != nil {
			isEOF := errors.Is(err, astiav.ErrEof)
			isEAgain := errors.Is(err, astiav.ErrEagain)
			logger.Tracef(ctx, "bsf.ReceivePacket(): %v (isEOF:%t, isEAgain:%t)", err, isEOF, isEAgain)
			packet.Pool.Pool.Put(pkt)
			if isEOF || isEAgain {
				break
			}
			return fmt.Errorf("unable receive the packet from the filter: %w", err)
		}

		outputPacketsCh <- packet.BuildOutput(
			pkt,
			input.Stream,
			input.Source,
		)
	}
	return nil
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
	b, _ := json.Marshal(bsf.Names)
	return fmt.Sprintf("BitstreamFilter%s", b)
}

func (bsf *BitstreamFilter) Close(ctx context.Context) error {
	bsf.closeChan.Close(ctx)
	return nil
}

func (bsf *BitstreamFilter) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}

/*
func (bsf *BitstreamFilter) WithFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	bsf.formatContextLocker.Do(ctx, func() {
		callback(bsf.FormatContext)
	})
}

func (bsf *BitstreamFilter) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_err error) {
	logger.Debugf(ctx, "NotifyAboutPacketSource(ctx, %T)", source)
	defer func() { logger.Debugf(ctx, "/NotifyAboutPacketSource(ctx, %T): %v", source, _err) }()
	var errs []error
	source.WithFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		bsf.formatContextLocker.Do(ctx, func() {
			for _, stream := range fmtCtx.Streams() {
				logger.Debugf(ctx, "making sure stream #%d is initialized", stream.Index())
				_, err := bsf.getOutputStream(ctx, source, stream, fmtCtx)
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to initialize an output stream for input stream %d from source %s: %w", stream.Index(), source, err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
*/
