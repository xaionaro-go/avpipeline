package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

type InputConfig struct {
	CustomOptions types.DictionaryItems
}

type Input struct {
	*closeChan
	*astiav.FormatContext
	*astiav.Dictionary

	ID  InputID
	URL string
}

var _ Abstract = (*Input)(nil)

var nextInputID atomic.Uint64

func NewInputFromURL(
	ctx context.Context,
	url string,
	authKey secret.String,
	cfg InputConfig,
) (*Input, error) {
	if url == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}
	if authKey.Get() != "" {
		return nil, fmt.Errorf("authkeys are not supported, yet")
	}

	i := &Input{
		ID:  InputID(nextInputID.Add(1)),
		URL: url,

		closeChan: newCloseChan(),
	}

	if len(cfg.CustomOptions) > 0 {
		i.Dictionary = astiav.NewDictionary()
		setFinalizerFree(ctx, i.Dictionary)

		for _, opt := range cfg.CustomOptions {
			if opt.Key == "f" {
				return nil, fmt.Errorf("overriding input format is not supported, yet")
			}
			logger.Debugf(ctx, "input.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			i.Dictionary.Set(opt.Key, opt.Value, 0)
		}
	}

	i.FormatContext = astiav.AllocFormatContext()
	if i.FormatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate a format context")
	}

	if err := i.FormatContext.OpenInput(url, nil, i.Dictionary); err != nil {
		i.FormatContext.Free()
		return nil, fmt.Errorf("unable to open input by URL '%s': %w", url, err)
	}
	setFinalizer(ctx, i, func(i *Input) {
		i.FormatContext.CloseInput()
		i.FormatContext.Free()
	})

	if err := i.FormatContext.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("unable to get stream info: %w", err)
	}

	return i, nil
}

func (i *Input) Close(
	ctx context.Context,
) error {
	i.closeChan.Close()
	return nil
}

func (i *Input) readIntoPacket(
	_ context.Context,
	packet *astiav.Packet,
) error {
	err := i.FormatContext.ReadFrame(packet)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, astiav.ErrEof):
		return io.EOF
	default:
		return fmt.Errorf("unable to read a frame: %w", err)
	}
}

func (i *Input) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Debugf(ctx, "Generate")
	defer func() { logger.Debugf(ctx, "/Generate: %v", _err) }()
	defer func() {
		i.closeChan.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		pkt := packet.Pool.Get()
		err := i.readIntoPacket(ctx, pkt)
		switch err {
		case nil:
			logger.Tracef(
				ctx,
				"received a packet (stream:%d, pos:%d, pts:%d, dts:%d, dur:%d), data: 0x %X",
				pkt.StreamIndex(),
				pkt.Pos(), pkt.Pts(), pkt.Dts(), pkt.Duration(),
				pkt.Data(),
			)

			outputPacketsCh <- packet.BuildOutput(
				pkt,
				avconv.FindStreamByIndex(ctx, i.FormatContext, pkt.StreamIndex()),
				i.FormatContext,
			)
		case io.EOF:
			pkt.Free()
			return nil
		default:
			pkt.Free()
			return fmt.Errorf("unable to read a packet: %w", err)
		}
	}
}

func (i *Input) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	return fmt.Errorf("cannot send packets to an Input")
}

func (i *Input) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return fmt.Errorf("cannot send frames to an Input")
}

func (i *Input) String() string {
	return fmt.Sprintf("Input(%s)", i.URL)
}
