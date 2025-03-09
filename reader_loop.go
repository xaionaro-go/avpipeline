package avpipeline

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type SendPacketer interface {
	SendPacket(
		ctx context.Context,
		input InputPacket,
	) error
}

func ReaderLoop(
	ctx context.Context,
	inputChan <-chan InputPacket,
	sendPacketer SendPacketer,
) (_err error) {
	logger.Debugf(ctx, "readerLoop[%T]: chan %p", sendPacketer, inputChan)
	defer func() { logger.Debugf(ctx, "/readerLoop[%T]: chan %p: %v", sendPacketer, inputChan, _err) }()

	defer func() {
		for {
			select {
			case pkt, ok := <-inputChan:
				if !ok {
					return
				}
				logger.Tracef(ctx, "readerLoop[%T](closing): received %#+v", sendPacketer, pkt)
				err := sendPacketer.SendPacket(ctx, pkt)
				logger.Tracef(ctx, "readerLoop[%T](closing): sent %#+v: %v", sendPacketer, pkt, err)
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
			logger.Tracef(ctx, "readerLoop[%T]: received %#+v", sendPacketer, pkt)
			err := sendPacketer.SendPacket(ctx, pkt)
			logger.Tracef(ctx, "readerLoop[%T]: sent %#+v: %v", sendPacketer, pkt, err)
			if err != nil {
				return fmt.Errorf("unable to send packet: %w", err)
			}
		}
	}
}

type processingNode interface {
	outChanError() chan<- error
	addToCloser(func())
	readLoop(context.Context) error
	finalize(context.Context) error
}

func startReaderLoop(
	ctx context.Context,
	p processingNode,
) {
	logger.Tracef(ctx, "startReaderLoop[%T]", p)
	defer func() { logger.Tracef(ctx, "/startReaderLoop[%T]", p) }()

	errCh := p.outChanError()

	ctx, cancelFn := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func() {
		defer observability.Go(ctx, func() {
			defer wg.Done()
			logger.Tracef(ctx, "finalize[%T]", p)
			err := p.finalize(ctx)
			logger.Tracef(ctx, "/finalize[%T]: %v", p, err)
			errmon.ObserveErrorCtx(ctx, err)
			if err != nil {
				errCh <- err
			}
			close(errCh)
		})
		logger.Tracef(ctx, "readLoop[%T]", p)
		err := p.readLoop(ctx)
		logger.Tracef(ctx, "/readLoop[%T]: %v", p, err)
		if err != nil {
			errCh <- err
		}
	})
	var once sync.Once
	p.addToCloser(func() {
		once.Do(func() {
			logger.Tracef(ctx, "close[%T]", p)
			defer logger.Tracef(ctx, "/close[%T]", p)
			cancelFn()
			wg.Wait()
		})
	})
}
