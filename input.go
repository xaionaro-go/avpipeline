package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
)

type InputConfig struct {
	CustomOptions DictionaryItems
}

type Input struct {
	ID             InputID
	URL            string
	OutputChan     chan OutputPacket
	ErrorChanValue chan error
	closeOnce      sync.Once
	closer         *astikit.Closer
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
		ID:             InputID(nextInputID.Add(1)),
		URL:            url,
		closer:         astikit.NewCloser(),
		OutputChan:     make(chan OutputPacket, 1),
		ErrorChanValue: make(chan error, 2),
	}

	input.FormatContext = astiav.AllocFormatContext()
	if input.FormatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate a format context")
	}
	setFinalizerFree(ctx, input.FormatContext)

	if len(cfg.CustomOptions) > 0 {
		input.Dictionary = astiav.NewDictionary()
		setFinalizerFree(ctx, input.Dictionary)

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
	input.closer.Add(input.FormatContext.CloseInput)

	if err := input.FormatContext.FindStreamInfo(nil); err != nil {
		return nil, fmt.Errorf("unable to get stream info: %w", err)
	}

	startReaderLoop(ctx, input)
	return input, nil
}

func (i *Input) addToCloser(callback func()) {
	i.closer.Add(callback)
}

func (i *Input) outChanError() chan<- error {
	return i.ErrorChanValue
}

func (i *Input) Close() error {
	var err error
	i.closeOnce.Do(func() {
		err = i.closer.Close()
	})
	return err
}

func (i *Input) finalize(ctx context.Context) error {
	observability.Go(ctx, func() {
		err := i.Close()
		if err != nil {
			logger.Errorf(ctx, "unable to close input %s: %w", i, err)
		}
	})
	close(i.OutputChan)
	return nil
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
				"received a packet (stream:%d, pos:%d, pts:%d, dts:%d, dur:%d), data: 0x %X",
				packet.StreamIndex(),
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
) (_err error) {
	return fmt.Errorf("cannot send packets to an Input")
}

func (i *Input) OutputPacketsChan() <-chan OutputPacket {
	return i.OutputChan
}

func (i *Input) ErrorChan() <-chan error {
	return i.ErrorChanValue
}

func (i *Input) GetOutputFormatContext(ctx context.Context) *astiav.FormatContext {
	return i.FormatContext
}

func (i *Input) String() string {
	return fmt.Sprintf("Input(%s)", i.URL)
}
