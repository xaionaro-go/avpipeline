package avpipeline

import (
	"context"
	"fmt"
	"io"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func ReaderLoop(
	ctx context.Context,
	inputChan <-chan InputPacket,
	sendPacketer SendPacketer,
) (_err error) {
	logger.Debugf(ctx, "readerLoop")
	defer func() { logger.Debugf(ctx, "/readerLoop: %v", _err) }()

	defer func() {
		for {
			select {
			case pkt, ok := <-inputChan:
				if !ok {
					return
				}
				err := sendPacketer.SendPacket(ctx, pkt)
				if err != nil {
					logger.Errorf(ctx, "unable to send packet: %v", err)
					return
				}
			default:
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case pkt, ok := <-inputChan:
			if !ok {
				return io.EOF
			}
			err := sendPacketer.SendPacket(ctx, pkt)
			if err != nil {
				return fmt.Errorf("unable to send packet: %w", err)
			}
		}
	}
}
