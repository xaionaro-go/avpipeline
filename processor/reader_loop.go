package processor

import (
	"context"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/kernel"
)

type Kernel interface {
	kernel.SendInputer
	kernel.CloseChaner
}

func ReaderLoop(
	ctx context.Context,
	inputChan <-chan InputPacket,
	kernel Kernel,
	outputCh chan<- OutputPacket,
) (_err error) {
	logger.Debugf(ctx, "ReaderLoop[%s]: chan %p", kernel, inputChan)
	defer func() { logger.Debugf(ctx, "/ReaderLoop[%s]: chan %p: %v", kernel, inputChan, _err) }()

	defer func() {
		for {
			select {
			case pkt, ok := <-inputChan:
				if !ok {
					return
				}
				logger.Tracef(ctx, "ReaderLoop[%s](closing): received %#+v", kernel, pkt)
				err := kernel.SendInput(ctx, pkt, outputCh)
				logger.Tracef(ctx, "ReaderLoop[%s](closing): sent %#+v: %v", kernel, pkt, err)
				if err != nil {
					logger.Errorf(ctx, "ReaderLoop[%s](closing): unable to send packet: %v", kernel, err)
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
		case pkt, ok := <-inputChan:
			if !ok {
				return io.EOF
			}
			logger.Tracef(ctx, "ReaderLoop[%s]: received %#+v", kernel, pkt)
			err := kernel.SendInput(ctx, pkt, outputCh)
			logger.Tracef(ctx, "ReaderLoop[%s]: sent %#+v: %v", kernel, pkt, err)
			if err != nil {
				return fmt.Errorf("unable to send packet: %w", err)
			}
		}
	}
}
