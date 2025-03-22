package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/secret"
)

type InputConfig struct {
	CustomOptions DictionaryItems
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
	outputCh chan<- OutputPacket,
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

		packet := packet.Pool.Get()
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
			outputCh <- OutputPacket{
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

func (i *Input) SendInput(
	ctx context.Context,
	input InputPacket,
	outputCh chan<- OutputPacket,
) (_err error) {
	return fmt.Errorf("cannot send packets to an Input")
}

func (i *Input) GetOutputFormatContext(ctx context.Context) *astiav.FormatContext {
	return i.FormatContext
}

func (i *Input) String() string {
	return fmt.Sprintf("Input(%s)", i.URL)
}
