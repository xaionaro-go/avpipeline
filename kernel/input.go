package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/unsafetools"
)

type InputConfig struct {
	CustomOptions types.DictionaryItems
	AsyncOpen     bool
	OnOpened      func(context.Context, *Input) error
}

type Input struct {
	*closuresignaler.ClosureSignaler
	initialized chan struct{}

	*astiav.FormatContext
	*astiav.IOInterrupter
	*astiav.Dictionary

	ID  InputID
	URL string
}

var _ Abstract = (*Input)(nil)
var _ packet.Source = (*Input)(nil)

var nextInputID atomic.Uint64

func NewInputFromURL(
	ctx context.Context,
	urlString string,
	authKey secret.String,
	cfg InputConfig,
) (*Input, error) {
	if urlString == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}
	if urlParsed, err := url.Parse(urlString); err == nil && urlParsed.Scheme != "" {
		logger.Debugf(ctx, "URL: %#+v", urlParsed)
		urlString += "/"
	}
	i := &Input{
		ID:  InputID(nextInputID.Add(1)),
		URL: urlString,

		initialized:     make(chan struct{}),
		ClosureSignaler: closuresignaler.New(),
	}

	var formatName string
	if len(cfg.CustomOptions) > 0 {
		i.Dictionary = astiav.NewDictionary()
		setFinalizerFree(ctx, i.Dictionary)
		for _, opt := range cfg.CustomOptions {
			if opt.Key == "f" {
				formatName = opt.Value
				logger.Debugf(ctx, "overriding input format to '%s'", opt.Value)
				continue
			}
			logger.Debugf(ctx, "input.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			i.Dictionary.Set(opt.Key, opt.Value, 0)
		}
	}

	var inputFormat *astiav.InputFormat
	if formatName != "" {
		inputFormat = astiav.FindInputFormat(formatName)
		if inputFormat == nil {
			logger.Errorf(ctx, "unable to find input format by name '%s'", formatName)
		} else {
			logger.Debugf(ctx, "using format '%s'", inputFormat.Name())
		}
	}

	i.FormatContext = astiav.AllocFormatContext()
	if i.FormatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate a format context")
	}

	if cfg.AsyncOpen {
		observability.Go(ctx, func(ctx context.Context) {
			if err := i.doOpen(ctx, urlString, authKey, inputFormat, cfg); err != nil {
				logger.Errorf(ctx, "unable to open: %v", err)
				i.Close(ctx)
			}
		})
	} else {
		if err := i.doOpen(ctx, urlString, authKey, inputFormat, cfg); err != nil {
			return nil, err
		}
	}

	return i, nil
}

func (i *Input) doOpen(
	ctx context.Context,
	urlString string,
	authKey secret.String,
	inputFormat *astiav.InputFormat,
	cfg InputConfig,
) error {
	urlWithSecret := urlString
	if authKey.Get() != "" {
		urlWithSecret += authKey.Get()
	}

	i.IOInterrupter = astiav.NewIOInterrupter()
	setFinalizerFree(ctx, i.IOInterrupter)
	i.FormatContext.SetIOInterrupter(i.IOInterrupter)

	if err := i.FormatContext.OpenInput(urlWithSecret, inputFormat, i.Dictionary); err != nil {
		i.FormatContext.Free()
		if authKey.Get() != "" {
			return fmt.Errorf("unable to open input by URL '%s/<HIDDEN>': %w", urlString, err)
		} else {
			return fmt.Errorf("unable to open input by URL '%s': %w", urlString, err)
		}
	}
	setFinalizer(ctx, i, func(i *Input) {
		i.FormatContext.CloseInput()
		i.FormatContext.Free()
	})

	if err := i.FormatContext.FindStreamInfo(nil); err != nil {
		return fmt.Errorf("unable to get stream info: %w", err)
	}

	for _, stream := range i.FormatContext.Streams() {
		logger.Debugf(ctx, "input stream #%d: %#+v", stream.Index(), spew.Sdump(unsafetools.FieldByNameInValue(reflect.ValueOf(stream.CodecParameters()), "c").Elem().Elem().Interface()))
	}

	if cfg.OnOpened != nil {
		cfg.OnOpened(ctx, i)
	}
	close(i.initialized)

	return nil
}

func (i *Input) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close[%s]", i)
	defer func() { logger.Debugf(ctx, "/Close[%s]: %v", i, _err) }()
	if i == nil {
		return nil
	}
	i.ClosureSignaler.Close(ctx)
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
	case errors.Is(err, astiav.ErrEio):
		return io.EOF
	default:
		return fmt.Errorf("unable to read a frame: %T:%w", err, err)
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
		i.ClosureSignaler.Close(ctx)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-i.initialized:
	}

	defer i.FormatContext.CloseInput()
	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
		logger.Debugf(ctx, "interrupting IO")
		i.IOInterrupter.Interrupt()
	})

	sendPkt := func(outPkt *packet.Output) error {
		logger.Tracef(
			ctx,
			"sending a %s packet (stream:%d, pos:%d, pts:%d, dts:%d, dur:%d, isKey:%t), dataLen:%d",
			outPkt.Stream.CodecParameters().MediaType(),
			outPkt.StreamIndex(),
			outPkt.Pos(), outPkt.Pts(), outPkt.Dts(), outPkt.Packet.Duration(),
			outPkt.Flags().Has(astiav.PacketFlagKey),
			len(outPkt.Data()),
		)

		select {
		case outputPacketsCh <- *outPkt:
		case <-ctx.Done():
			return ctx.Err()
		case <-i.CloseChan():
			return io.EOF
		}
		return nil
	}

	var curPkt *packet.Output
	defer func() {
		if curPkt == nil {
			return
		}
		select {
		case <-ctx.Done():
		case <-i.CloseChan():
		default:
		}
		sendPkt(curPkt)
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
			stream := avconv.FindStreamByIndex(ctx, i.FormatContext, pkt.StreamIndex())
			logger.Tracef(
				ctx,
				"received a %s packet (stream:%d, pos:%d, pts:%d, dts:%d, dur:%d, isKey:%t), dataLen:%d",
				stream.CodecParameters().MediaType(),
				pkt.StreamIndex(),
				pkt.Pos(), pkt.Pts(), pkt.Dts(), pkt.Duration(),
				pkt.Flags().Has(astiav.PacketFlagKey),
				len(pkt.Data()),
			)

			prevPkt := curPkt
			curPkt = ptr(packet.BuildOutput(
				pkt,
				packet.BuildStreamInfo(
					stream,
					i,
					nil,
				),
			))
			if prevPkt != nil {
				prevPkt.Packet.SetDuration(curPkt.Pts() - prevPkt.Pts())
				logger.Debugf(ctx, "the packet had no duration set; set it to: cur.pts - prev.pts: %d-%d=%d", curPkt.Pts(), prevPkt.Pts(), prevPkt.Packet.Duration())

				if err := sendPkt(prevPkt); err != nil {
					return err
				}
			}

			if curPkt.Duration() <= 0 {
				continue
			}
			// no correction is needed, let's send immediately
			err := sendPkt(curPkt)
			curPkt = nil
			if err != nil {
				return err
			}
		case io.EOF:
			pkt.Free()
			return nil
		default:
			pkt.Free()
			return fmt.Errorf("unable to read a packet: %w", err)
		}
	}
}

func (i *Input) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	logger.Tracef(ctx, "WithFormatContext")
	defer func() { logger.Tracef(ctx, "/WithFormatContext") }()
	select {
	case <-ctx.Done():
		logger.Debugf(ctx, "context is closed")
		return
	case <-i.initialized:
	}
	callback(i.FormatContext)
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
