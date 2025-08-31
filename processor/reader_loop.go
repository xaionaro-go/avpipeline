package processor

import (
	"context"
	"fmt"
	"io"

	"github.com/xaionaro-go/avpipeline/frame"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Kernel interface {
	kerneltypes.SendInputer
	kerneltypes.CloseChaner
}

func readerLoop(
	ctx context.Context,
	inputPacketsChan <-chan packet.Input,
	inputFramesChan <-chan frame.Input,
	kernel Kernel,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
	countersPtr *Counters,
) (_err error) {
	logger.Debugf(ctx, "ReaderLoop[%s]: chan %p", kernel, inputPacketsChan)
	defer func() { logger.Debugf(ctx, "/ReaderLoop[%s]: chan %p: %v", kernel, inputPacketsChan, _err) }()

	defer func() {
		for {
			select {
			case pkt, ok := <-inputPacketsChan:
				if !ok {
					if inputFramesChan == nil {
						return
					}
					inputPacketsChan = nil
					continue
				}
				mediaType := pkt.GetMediaType()
				objSize := uint64(pkt.GetSize())
				logger.Tracef(ctx, "ReaderLoop[%s](closing): received %#+v", kernel, pkt)
				err := kernel.SendInputPacket(ctx, pkt, outputPacketsCh, outputFramesCh)
				countersPtr.Processed.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
				logger.Tracef(ctx, "ReaderLoop[%s](closing): sent %#+v: %v", kernel, pkt, err)
				if err != nil {
					logger.Errorf(ctx, "ReaderLoop[%s](closing): unable to send packet: %v", kernel, err)
					return
				}
			case f, ok := <-inputFramesChan:
				if !ok {
					if inputPacketsChan == nil {
						return
					}
					inputFramesChan = nil
					continue
				}
				mediaType := f.GetMediaType()
				objSize := uint64(f.GetSize())
				logger.Tracef(ctx, "ReaderLoop[%s](closing): received %#+v", kernel, f)
				err := kernel.SendInputFrame(ctx, f, outputPacketsCh, outputFramesCh)
				countersPtr.Processed.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
				logger.Tracef(ctx, "ReaderLoop[%s](closing): sent %#+v: %v", kernel, f, err)
				if err != nil {
					logger.Errorf(ctx, "ReaderLoop[%s](closing): unable to send frame: %v", kernel, err)
					return
				}
			default:
				return
			}
		}
	}()

	defer func() {
		logger.Debugf(ctx, "ReaderLoop[%s]: closing/flushing (%v)", _err)
	}()

	ch := kernel.CloseChan()
	for {
		select {
		case <-ctx.Done():
			logger.Debugf(ctx, "context is closed")
			return ctx.Err()
		case <-ch:
			logger.Debugf(ctx, "kernel is closed")
			return io.EOF
		case pkt, ok := <-inputPacketsChan:
			if !ok {
				if inputFramesChan == nil {
					logger.Debugf(ctx, "the frames input channels is closed")
					return io.EOF
				}
				inputPacketsChan = nil
				continue
			}
			mediaType := pkt.GetMediaType()
			objSize := uint64(pkt.GetSize())
			logger.Tracef(ctx, "ReaderLoop[%s]: received %#+v", kernel, pkt)
			err := kernel.SendInputPacket(ctx, pkt, outputPacketsCh, outputFramesCh)
			countersPtr.Processed.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
			logger.Tracef(ctx, "ReaderLoop[%s]: sent %#+v: %v", kernel, pkt, err)
			if err != nil {
				return fmt.Errorf("unable to send packet: %w", err)
			}
		case f, ok := <-inputFramesChan:
			if !ok {
				if inputPacketsChan == nil {
					logger.Debugf(ctx, "the packets input channels is closed")
					return io.EOF
				}
				inputFramesChan = nil
				continue
			}
			mediaType := f.GetMediaType()
			objSize := uint64(f.GetSize())
			logger.Tracef(ctx, "ReaderLoop[%s]: received %#+v", kernel, f)
			err := kernel.SendInputFrame(ctx, f, outputPacketsCh, outputFramesCh)
			countersPtr.Processed.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
			logger.Tracef(ctx, "ReaderLoop[%s]: sent %#+v: %v", kernel, f, err)
			if err != nil {
				return fmt.Errorf("unable to send frame: %w", err)
			}
		}
	}
}
