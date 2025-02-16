package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type InputConfig struct {
	CustomOptions DictionaryItems
}

type Input struct {
	ID         InputID
	OutputChan chan OutputPacket
	*astikit.Closer
	*astiav.FormatContext
	*astiav.Dictionary
}

var _ ProcessingNode = (*Input)(nil)

var nextInputID atomic.Uint64

func NewInputFromURL(
	ctx context.Context,
	url string,
	authKey string,
	cfg InputConfig,
) (*Input, error) {
	if url == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}

	input := &Input{
		ID:         InputID(nextInputID.Add(1)),
		Closer:     astikit.NewCloser(),
		OutputChan: make(chan OutputPacket, 1),
	}

	input.FormatContext = astiav.AllocFormatContext()
	if input.FormatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate a format context")
	}
	input.Closer.Add(input.FormatContext.Free)

	if len(cfg.CustomOptions) > 0 {
		input.Dictionary = astiav.NewDictionary()
		input.Closer.Add(input.Dictionary.Free)

		for _, opt := range cfg.CustomOptions {
			if opt.Key == "f" {
				return nil, fmt.Errorf("overriding input format is not supported, yet")
			}
			logger.Debugf(ctx, "input.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			input.Dictionary.Set(opt.Key, opt.Value, 0)
		}
	}

	if err := input.FormatContext.OpenInput(url, nil, input.Dictionary); err != nil {
		return nil, fmt.Errorf("unable to open input by URL '%s': %w", url, err)
	}
	input.Closer.Add(input.FormatContext.CloseInput)

	if err := input.FormatContext.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("unable to get stream info: %w", err)
	}

	observability.Go(ctx, func() {
		defer input.Close()
		err := input.readLoop(ctx)
		if err != nil && !errors.Is(err, io.EOF) {
			errmon.ObserveErrorCtx(ctx, err)
		}
	})
	return input, nil
}

func (i *Input) readLoop(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "readLoop")
	defer func() { logger.Debugf(ctx, "/readLoop: %v", _err) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		packet := PacketPool.Get()
		err := i.readIntoPacket(ctx, packet)
		switch err {
		case nil:
			logger.Tracef(
				ctx,
				"received a frame (pos:%d, pts:%d, dts:%d, dur:%d), data: 0x %X",
				packet.Pos(), packet.Pts(), packet.Dts(), packet.Duration(),
				packet.Data(),
			)
			i.OutputChan <- OutputPacket{
				Packet: packet,
			}
		case io.EOF:
			packet.Free()
			return nil
		default:
			packet.Free()
			return fmt.Errorf("unable to read a packet: %w", err)
		}

	}
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

var noInputPacketsChan chan InputPacket

func (i *Input) SendPacketChan() chan<- InputPacket {
	return noInputPacketsChan
}

func (i *Input) SendPacket(
	ctx context.Context,
	input InputPacket,
) (_haveDecoded bool, _haveEncoded bool, _err error) {
	return false, false, fmt.Errorf("cannot send packets to an Input")
}

func (i *Input) OutputPacketsChan() <-chan OutputPacket {
	return i.OutputChan
}

func (i *Input) GetStream(streamIndex int) *astiav.Stream {
	for _, stream := range i.FormatContext.Streams() {
		if stream.Index() == streamIndex {
			return stream
		}
	}
	return nil
}
