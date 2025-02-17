package avpipeline

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/experimental/errmon"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/proxy"
	"github.com/xaionaro-go/xsync"
)

const unwrapTLSViaProxy = false
const pendingPacketsLimit = 1000

type OutputConfig struct {
	CustomOptions DictionaryItems
}

type OutputStream struct {
	*astiav.Stream
	LastDTS int64
}

type Output struct {
	ID             OutputID
	Streams        map[int]*OutputStream
	Locker         xsync.Mutex
	inputChan      chan InputPacket
	pendingPackets []*astiav.Packet
	*astikit.Closer
	*astiav.FormatContext
	*astiav.Dictionary
}

var _ ProcessingNode = (*Output)(nil)

func formatFromScheme(scheme string) string {
	switch scheme {
	case "rtmp", "rtmps":
		return "flv"
	case "srt":
		return "mpegts"
	default:
		return scheme
	}
}

var nextOutputID atomic.Uint64

func NewOutputFromURL(
	ctx context.Context,
	urlString string,
	streamKey string,
	cfg OutputConfig,
) (*Output, error) {
	if urlString == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}

	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", url, err)
	}

	if streamKey != "" {
		switch {
		case url.Path == "" || url.Path == "/":
			url.Path = "//"
		case !strings.HasSuffix(url.Path, "/"):
			url.Path += "/"
		}
		url.Path += streamKey
	}

	if url.Port() == "" {
		switch url.Scheme {
		case "rtmp":
			url.Host += ":1935"
		case "rtmps":
			url.Host += ":443"
		}
	}

	needUnwrapTLSFor := ""
	switch url.Scheme {
	case "rtmps":
		needUnwrapTLSFor = "rtmp"
	}

	o := &Output{
		ID:        OutputID(nextOutputID.Add(1)),
		Streams:   make(map[int]*OutputStream),
		Closer:    astikit.NewCloser(),
		inputChan: make(chan InputPacket, 600),
	}

	if needUnwrapTLSFor != "" && unwrapTLSViaProxy {
		proxy := proxy.NewTCP(url.Host, &proxy.TCPConfig{
			DestinationIsTLS: true,
		})
		proxyAddr, err := proxy.ListenRandomPort(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to make a TLS-proxy: %w", err)
		}
		o.Closer.Add(func() {
			err := proxy.Close()
			if err != nil {
				logger.Errorf(ctx, "unable to close the TLS-proxy: %v", err)
			}
		})
		url.Scheme = needUnwrapTLSFor
		url.Host = proxyAddr.String()
	}

	formatName := formatFromScheme(url.Scheme)

	if len(cfg.CustomOptions) > 0 {
		o.Dictionary = astiav.NewDictionary()
		o.Closer.Add(o.Dictionary.Free)

		for _, opt := range cfg.CustomOptions {
			if opt.Key == "f" {
				formatName = opt.Value
				continue
			}
			logger.Debugf(ctx, "output.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			o.Dictionary.Set(opt.Key, opt.Value, 0)
		}
	}

	logger.Debugf(observability.OnInsecureDebug(ctx), "URL: %s", url)
	formatContext, err := astiav.AllocOutputFormatContext(
		nil,
		formatName,
		url.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("allocating output format context failed using URL '%s': %w", url, err)
	}
	if formatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate the output format context")
	}
	o.FormatContext = formatContext
	o.Closer.Add(o.FormatContext.Free)

	if o.FormatContext.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		// if output is not a file then nothing else to do
		return o, nil
	}
	logger.Tracef(ctx, "destination '%s' is a file", url)

	ioContext, err := astiav.OpenIOContext(
		url.String(),
		astiav.NewIOContextFlags(astiav.IOContextFlagWrite),
		nil,
		o.Dictionary,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to open IO context (URL: '%s'): %w", url, err)
	}

	o.Closer.Add(func() {
		err := ioContext.Close()
		if err != nil {
			logger.Errorf(ctx, "unable to close the IO context (URL: %s): %v", url, err)
		}
	})
	o.FormatContext.SetPb(ioContext)

	observability.Go(ctx, func() {
		defer func() {
			err := o.finalize(ctx)
			if err != nil {
				errmon.ObserveErrorCtx(ctx, err)
			}
		}()
		err := o.readerLoop(ctx)
		if err != nil {
			errmon.ObserveErrorCtx(ctx, err)
		}
	})
	return o, nil
}

func (o *Output) finalize(
	_ context.Context,
) error {
	return o.FormatContext.WriteTrailer()
}

func (o *Output) readerLoop(
	ctx context.Context,
) error {
	return ReaderLoop(ctx, o.inputChan, o)
}

func (o *Output) SendPacketChan() chan<- InputPacket {
	return o.inputChan
}

var noOutputPacketsChan chan OutputPacket

func (o *Output) SendPacket(
	ctx context.Context,
	input InputPacket,
) error {
	return xsync.DoR1(ctx, &o.Locker, func() error {
		return o.writePacket(ctx, input)
	})
}

func (o *Output) writePacket(
	ctx context.Context,
	pkt InputPacket,
) (_err error) {
	logger.Tracef(ctx,
		"writePacket (pos:%d, pts:%d, dts:%d, dur:%d)",
		pkt.Packet.Pos(), pkt.Packet.Pts(), pkt.Packet.Dts(), pkt.Packet.Duration(),
	)
	defer func() { logger.Tracef(ctx, "/writePacket: %v", _err) }()

	packet := pkt.Packet
	if packet == nil {
		return fmt.Errorf("packet == nil")
	}

	outputStream := o.Streams[pkt.StreamIndex()]
	if outputStream == nil {
		logger.Debugf(ctx, "new output stream")
		outputStream = &OutputStream{
			Stream:  o.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
		if err := CopyStreamParameters(ctx, outputStream.Stream, pkt.Stream); err != nil {
			return fmt.Errorf("unable to copy stream parameters: %w", err)
		}
		logger.Tracef(
			ctx,
			"new output stream: %s: %s: %s: %s: %s",
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
			outputStream.TimeBase(),
			spew.Sdump(outputStream),
			spew.Sdump(outputStream.CodecParameters()),
		)

		o.Streams[pkt.StreamIndex()] = outputStream
		if len(o.Streams) < 2 {
			// TODO: delete me; an ugly hack to make sure we have both video and audio track before sending a header
			return nil
		}
		if err := o.FormatContext.WriteHeader(nil); err != nil {
			return fmt.Errorf("unable to write the header: %w", err)
		}
	}
	assert(ctx, outputStream != nil)
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"unmodified packet with pos:%v (pts:%v, dts:%v, dur: %v) for %s stream %d (->%d) with flags 0x%016X",
			packet.Pos(), packet.Pts(), packet.Dts(), packet.Duration(),
			outputStream.CodecParameters().MediaType(),
			packet.StreamIndex(),
			outputStream.Index(),
			packet.Flags(),
		)
	}

	if packet.Dts() < outputStream.LastDTS {
		logger.Errorf(ctx, "received a DTS from the past, ignoring the packet: %d < %d", packet.Dts(), outputStream.LastDTS)
		return nil
	}

	packet.SetStreamIndex(outputStream.Index())
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"writing packet with pos:%v (pts:%v, dts:%v, dur:%v) for %s stream %d (sample_rate: %v, time_base: %v) with flags 0x%016X and data 0x %X",
			packet.Pos(), packet.Pts(), packet.Pts(), packet.Duration(),
			outputStream.CodecParameters().MediaType(),
			packet.StreamIndex(), outputStream.CodecParameters().SampleRate(), outputStream.TimeBase(),
			packet.Flags(),
			packet.Data(),
		)
	}

	if len(o.Streams) < 2 {
		if len(o.pendingPackets) >= pendingPacketsLimit {
			return fmt.Errorf("exceeded the limit of pending packets: %d", len(o.pendingPackets))
		}
		o.pendingPackets = append(o.pendingPackets, ClonePacketAsReferenced(packet))
		return nil
	}
	for _, packet := range o.pendingPackets {
		if err := o.doWritePacket(ctx, packet, outputStream); err != nil {
			return err
		}
	}
	o.pendingPackets = o.pendingPackets[:0]
	if err := o.doWritePacket(ctx, packet, outputStream); err != nil {
		return err
	}
	return nil
}

func (o *Output) doWritePacket(
	ctx context.Context,
	packet *astiav.Packet,
	outputStream *OutputStream,
) (_err error) {
	err := o.FormatContext.WriteInterleavedFrame(packet)
	if err != nil {
		return fmt.Errorf("unable to write the frame: %w", err)
	}
	outputStream.LastDTS = packet.Dts()
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"wrote a packet (dts: %d): %s: %s",
			packet.Dts(),
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
		)
	}
	return nil
}

func (o *Output) OutputPacketsChan() <-chan OutputPacket {
	return noOutputPacketsChan
}

func (o *Output) GetOutputStream(_ context.Context, streamIndex int) *astiav.Stream {
	return nil
}
