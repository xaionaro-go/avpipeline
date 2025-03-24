package processor

import (
	"context"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
)

type Kernel interface {
	kernel.SendInputer
	kernel.CloseChaner
}

func ReaderLoop(
	ctx context.Context,
	inputPacketsChan <-chan packet.Input,
	inputFramesChan <-chan frame.Input,
	kernel Kernel,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
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
				logger.Tracef(ctx, "ReaderLoop[%s](closing): received %#+v", kernel, pkt)
				err := kernel.SendInputPacket(ctx, pkt, outputPacketsCh, outputFramesCh)
				logger.Tracef(ctx, "ReaderLoop[%s](closing): sent %#+v: %v", kernel, pkt, err)
				if err != nil {
					logger.Errorf(ctx, "ReaderLoop[%s](closing): unable to send packet: %v", kernel, err)
					return
				}
			case pkt, ok := <-inputFramesChan:
				if !ok {
					if inputPacketsChan == nil {
						return
					}
					inputFramesChan = nil
					continue
				}
				logger.Tracef(ctx, "ReaderLoop[%s](closing): received %#+v", kernel, pkt)
				err := kernel.SendInputFrame(ctx, pkt, outputPacketsCh, outputFramesCh)
				logger.Tracef(ctx, "ReaderLoop[%s](closing): sent %#+v: %v", kernel, pkt, err)
				if err != nil {
					logger.Errorf(ctx, "ReaderLoop[%s](closing): unable to send frame: %v", kernel, err)
					return
				}
			default:
				return
			}
		}
	}()

	ch := kernel.CloseChan()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
			return io.EOF
		case pkt, ok := <-inputPacketsChan:
			if !ok {
				if inputFramesChan == nil {
					return io.EOF
				}
				inputPacketsChan = nil
				continue
			}
			logger.Tracef(ctx, "ReaderLoop[%s]: received %#+v", kernel, pkt)
			err := kernel.SendInputPacket(ctx, pkt, outputPacketsCh, outputFramesCh)
			logger.Tracef(ctx, "ReaderLoop[%s]: sent %#+v: %v", kernel, pkt, err)
			if err != nil {
				return fmt.Errorf("unable to send packet: %w", err)
			}
		case f, ok := <-inputFramesChan:
			if !ok {
				if inputPacketsChan == nil {
					return io.EOF
				}
				inputFramesChan = nil
				continue
			}
			logger.Tracef(ctx, "ReaderLoop[%s]: received %#+v", kernel, f)
			err := kernel.SendInputFrame(ctx, f, outputPacketsCh, outputFramesCh)
			logger.Tracef(ctx, "ReaderLoop[%s]: sent %#+v: %v", kernel, f, err)
			if err != nil {
				return fmt.Errorf("unable to send frame: %w", err)
			}
		}
	}
}
