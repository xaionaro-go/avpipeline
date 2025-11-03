package streammux

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	kernelboilerplate "github.com/xaionaro-go/avpipeline/kernel/boilerplate"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

// TODO: delete me: this was unnecessary, just send directly to the Output instead
type SenderHandler[C any] struct {
	SenderFactory SenderFactory
	SenderKey     types.SenderKey

	SenderLocker xsync.Mutex
	Sender       node.Abstract
	SenderCancel context.CancelFunc
	SenderConfig types.SenderConfig
	SenderWG     sync.WaitGroup
}

var _ kernelboilerplate.CustomHandlerWithContextFormat = (*SenderHandler[any])(nil)
var _ kerneltypes.SendInputPacketer = (*SenderHandler[any])(nil)

func (h *SenderHandler[C]) String() string {
	if h == nil {
		return "<SenderHandler:nil>"
	}
	if h.Sender == nil {
		return "SendingHandler(<nil>)"
	}
	return fmt.Sprintf("SendingHandler(%s)", h.Sender.String())
}

type NodeSender[C any] struct {
	*node.NodeWithCustomData[
		C, *processor.FromKernel[*KernelSender[C]],
	]
}

type KernelSender[C any] struct {
	*kernelboilerplate.BaseWithFormatContext[*SenderHandler[C]]
}

var _ kernel.GetInternalQueueSizer = (*KernelSender[any])(nil)

func newKernelSender[C any](
	ctx context.Context,
	f SenderFactory,
	senderKey types.SenderKey,
) *KernelSender[C] {
	return &KernelSender[C]{
		kernelboilerplate.NewKernelWithFormatContext(ctx, &SenderHandler[C]{
			SenderFactory: f,
			SenderKey:     senderKey,
		}),
	}
}

func (k *KernelSender[C]) GetInternalQueueSize(
	ctx context.Context,
) (_ret map[string]uint64) {
	k.Handler.SenderLocker.Do(ctx, func() {
		if k.Handler.Sender == nil {
			_ret = map[string]uint64{}
			return
		}
		qSizer, ok := k.Handler.Sender.GetProcessor().(kerneltypes.GetInternalQueueSizer)
		if !ok {
			logger.Debugf(ctx, "GetInternalQueueSize: sender backend %T does not implement GetInternalQueueSizer", k.Handler.Sender)
			return
		}
		_ret = qSizer.GetInternalQueueSize(ctx)
	})
	return
}

func newSenderNode[C any](
	ctx context.Context,
	f SenderFactory,
	senderKey types.SenderKey,
) *NodeSender[C] {
	return &NodeSender[C]{node.NewWithCustomDataFromKernel[C](ctx, newKernelSender[C](ctx, f, senderKey))}
}

func (h *SenderHandler[C]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	_ chan<- packet.Output,
	_ chan<- frame.Output,
) (_err error) {
	return xsync.DoA2R1(ctx, &h.SenderLocker, h.sendInputPacketLocked, ctx, input)
}

func (h *SenderHandler[C]) sendInputPacketLocked(
	ctx context.Context,
	input packet.Input,
) (_err error) {
	logger.Tracef(ctx, "SendInputPacket: %s", input.Source)
	defer func() { logger.Tracef(ctx, "/SendInputPacket: %v", _err) }()

	if h.Sender == nil {
		logger.Warnf(ctx, "backend is not connected; skipping the packet")
		return nil
	}

	for {
		proc := h.Sender.GetProcessor()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case proc.InputPacketChan() <- input:
			return nil
		case <-h.Sender.GetChangeChanIsServing():
			if !h.Sender.IsServing() {
				logger.Debugf(ctx, "backend is not serving anymore; reinitializing")
				if err := h.deinitLocked(ctx); err != nil {
					return fmt.Errorf("unable to disconnect backend: %w", err)
				}
			}
		}
	}
}

func (h *SenderHandler[C]) Init(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Init")
	defer func() { logger.Debugf(ctx, "/Init: %v", _err) }()
	return xsync.DoR1(ctx, &h.SenderLocker, func() error {
		if h.Sender != nil {
			return nil
		}
		return h.initSenderLocked(ctx)
	})
}

func (h *SenderHandler[C]) initSenderLocked(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "initSenderLocked")
	defer func() { logger.Debugf(ctx, "/initSenderLocked: %v", _err) }()
	backendCtx, backendCancel := context.WithCancel(ctx)
	backend, cfg, err := h.SenderFactory.NewSender(backendCtx, h.SenderKey)
	if err != nil {
		backendCancel()
		return fmt.Errorf("unable to create sender: %w", err)
	}
	if backend == nil {
		backendCancel()
		return fmt.Errorf("constructed a nil sender")
	}
	h.Sender = backend
	h.SenderCancel = backendCancel
	h.SenderConfig = cfg
	h.SenderWG.Add(1)
	errCh := make(chan node.Error, 10)
	observability.Go(ctx, func(ctx context.Context) {
		defer h.SenderWG.Done()
		h.Sender.Serve(backendCtx, node.ServeConfig{}, errCh)
	})
	observability.Go(ctx, func(ctx context.Context) {
		defer h.Deinit(backendCtx)
		var err error
		select {
		case <-backendCtx.Done():
			err = backendCtx.Err()
		case err = <-errCh:
		}
		switch {
		case err == nil:
			logger.Errorf(ctx, "sender %T exited without error", h.Sender)
			return
		case err == context.Canceled:
			logger.Debugf(ctx, "sender %T exited", h.Sender)
		case errors.Is(err, io.EOF):
			logger.Debugf(ctx, "sender %T exited (EOF)", h.Sender)
		default:
			logger.Errorf(ctx, "sender %T exited with error: %v", h.Sender, err)
		}
	})
	return nil
}

func (h *SenderHandler[C]) Deinit(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Deinit")
	defer func() { logger.Debugf(ctx, "/Deinit: %v", _err) }()
	h.SenderLocker.Do(ctx, func() {
		_err = h.deinitLocked(ctx)
	})
	return
}

func (h *SenderHandler[C]) deinitLocked(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "deinitLocked")
	defer func() { logger.Debugf(ctx, "/deinitLocked: %v", _err) }()
	if h.SenderCancel != nil {
		h.SenderCancel()
	}
	h.SenderWG.Wait()
	h.Sender = nil
	h.SenderCancel = nil
	return nil
}

func (h *SenderHandler[C]) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	logger.Tracef(ctx, "WithOutputFormatContext")
	defer func() { logger.Tracef(ctx, "/WithOutputFormatContext") }()
}

func (h *SenderHandler[C]) WithInputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	logger.Tracef(ctx, "WithInputFormatContext")
	defer func() { logger.Tracef(ctx, "/WithInputFormatContext") }()

	err := h.withConnectedBackend(ctx, func(ctx context.Context, backend node.Abstract) {
		if fmter, ok := backend.(packet.WithInputFormatContexter); ok {
			fmter.WithInputFormatContext(ctx, callback)
		}
	})
	if err != nil {
		logger.Errorf(ctx, "unable to call WithInputFormatContext on the backend: %v", err)
		callback(nil)
	}
}

func (h *SenderHandler[C]) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_err error) {
	logger.Debugf(ctx, "NotifyAboutPacketSource(ctx, %T)", source)
	defer func() { logger.Debugf(ctx, "/NotifyAboutPacketSource(ctx, %T): %v", source, _err) }()

	var err error
	err = h.withConnectedBackend(ctx, func(ctx context.Context, backend node.Abstract) {
		if notifier, ok := backend.(packet.NotifyAboutPacketSourcer); ok {
			err = notifier.NotifyAboutPacketSource(ctx, source)
		}
	})
	return err
}

func (h *SenderHandler[C]) withConnectedBackend(
	ctx context.Context,
	callback func(ctx context.Context, backend node.Abstract),
) (_err error) {
	h.SenderLocker.Do(ctx, func() {
		if h.Sender == nil {
			err := h.initSenderLocked(ctx)
			if err != nil {
				_err = fmt.Errorf("unable to init sending backend: %w", err)
				return
			}
		}
		callback(ctx, h.Sender)
	})
	return
}
