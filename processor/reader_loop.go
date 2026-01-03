// reader_loop.go contains the core execution logic for FromKernel, reading from input and passing to the kernel.

package processor

import (
	"context"
	"errors"
	"fmt"
	"io"

	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Kernel interface {
	kerneltypes.SendInputer
	kerneltypes.CloseChaner
}

func readerLoop(
	ctx context.Context,
	inputChan <-chan packetorframe.InputUnion,
	kernel Kernel,
	outputCh chan<- packetorframe.OutputUnion,
	countersPtr *Counters,
) (_err error) {
	logger.Debugf(ctx, "ReaderLoop[%s]: chan %p", kernel, inputChan)
	defer func() { logger.Debugf(ctx, "/ReaderLoop[%s]: chan %p: %v", kernel, inputChan, _err) }()

	defer func() {
		for {
			select {
			case input, ok := <-inputChan:
				if !ok {
					return
				}
				mediaType := input.GetMediaType()
				objSize := uint64(input.GetSize())
				logger.Tracef(ctx, "ReaderLoop[%s](closing): received %#+v", kernel, input)
				err := kernel.SendInput(ctx, input, outputCh)
				pkt, frame := input.Unwrap()
				if pkt != nil {
					countersPtr.Processed.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
				} else if frame != nil {
					countersPtr.Processed.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
				}
				logger.Tracef(ctx, "ReaderLoop[%s](closing): sent %#+v: %v", kernel, input, err)
				if err != nil {
					if eofErr(err) {
						logger.Debugf(ctx, "ReaderLoop[%s](closing): EOF reached", kernel)
						return
					}
					logger.Errorf(ctx, "ReaderLoop[%s](closing): unable to send input: %v", kernel, err)
					return
				}
			default:
				return
			}
		}
	}()

	defer func() {
		logger.Debugf(ctx, "ReaderLoop[%s]: closing/flushing (%v)", kernel, _err)
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
		case input, ok := <-inputChan:
			if !ok {
				logger.Debugf(ctx, "the input channel is closed")
				return io.EOF
			}
			mediaType := input.GetMediaType()
			objSize := uint64(input.GetSize())
			logger.Tracef(ctx, "ReaderLoop[%s]: received %#+v", kernel, input)
			err := kernel.SendInput(ctx, input, outputCh)
			pkt, frame := input.Unwrap()
			if pkt != nil {
				countersPtr.Processed.Packets.Increment(globaltypes.MediaType(mediaType), objSize)
			} else if frame != nil {
				countersPtr.Processed.Frames.Increment(globaltypes.MediaType(mediaType), objSize)
			}
			logger.Tracef(ctx, "ReaderLoop[%s]: sent %#+v: %v", kernel, input, err)
			if err != nil {
				return fmt.Errorf("unable to send input: %w", err)
			}
		}
	}
}

func eofErr(err error) bool {
	switch {
	case errors.Is(err, io.EOF):
		return true
	case errors.Is(err, context.Canceled):
		return true
	}
	return false
}
