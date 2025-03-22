package kernel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"runtime/debug"
	"strings"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/stream"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/proxy"
	"github.com/xaionaro-go/secret"
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
	ID        OutputID
	URL       string
	StreamKey secret.String
	Streams   map[int]*OutputStream

	pendingPackets []*astiav.Packet
	ioContext      *astiav.IOContext
	proxy          *proxy.TCPProxy

	formatContextLocker xsync.RWMutex

	*closeChan
	*astiav.FormatContext
	*astiav.Dictionary
}

var _ Abstract = (*Output)(nil)

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
	streamKey secret.String,
	cfg OutputConfig,
) (_ *Output, _err error) {
	if urlString == "" {
		return nil, fmt.Errorf("the provided URL is empty")
	}

	url, err := url.Parse(urlString)
	if err != nil {
		return nil, fmt.Errorf("unable to parse URL '%s': %w", url, err)
	}

	if url.Port() == "" {
		switch url.Scheme {
		case "rtmp":
			url.Host += ":1935"
		case "rtmps":
			url.Host += ":443"
		}
	}

	o := &Output{
		ID:        OutputID(nextOutputID.Add(1)),
		URL:       url.String(),
		StreamKey: streamKey,
		Streams:   make(map[int]*OutputStream),
		closeChan: newCloseChan(),
	}

	if streamKey.Get() != "" {
		switch {
		case url.Path == "" || url.Path == "/":
			url.Path = "//"
		case !strings.HasSuffix(url.Path, "/"):
			url.Path += "/"
		}
		url.Path += streamKey.Get()
	}

	needUnwrapTLSFor := ""
	switch url.Scheme {
	case "rtmps":
		needUnwrapTLSFor = "rtmp"
	}

	if needUnwrapTLSFor != "" && unwrapTLSViaProxy {
		proxy := proxy.NewTCP(url.Host, &proxy.TCPConfig{
			DestinationIsTLS: true,
		})
		proxyAddr, err := proxy.ListenRandomPort(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to make a TLS-proxy: %w", err)
		}
		o.proxy = proxy
		url.Scheme = needUnwrapTLSFor
		url.Host = proxyAddr.String()
	}

	formatName := formatFromScheme(url.Scheme)

	if len(cfg.CustomOptions) > 0 {
		o.Dictionary = astiav.NewDictionary()
		setFinalizerFree(ctx, o.Dictionary)

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
	setFinalizerFree(ctx, o.FormatContext)

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
	o.ioContext = ioContext
	o.FormatContext.SetPb(ioContext)

	return o, nil
}

func (o *Output) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	o.closeChan.Close()

	var result []error
	if len(o.FormatContext.Streams()) != 0 {
		err := func() error {
			defer func() {
				r := recover()
				if r != nil {
					_err = fmt.Errorf("got panic: %v:\n%s\n", r, debug.Stack())
				}
			}()
			return o.FormatContext.WriteTrailer()
		}()
		if err != nil {
			result = append(result, fmt.Errorf("unable to write the tailer: %w", err))
		}
	}
	if o.ioContext != nil {
		if err := o.ioContext.Close(); err != nil {
			result = append(result, fmt.Errorf("unable to close the IO context: %w", err))
		}
	}
	if o.proxy != nil {
		if err := o.proxy.Close(); err != nil {
			result = append(result, fmt.Errorf("unable to close the TLS-proxy: %v", err))
		}
	}
	return errors.Join(result...)
}

func (o *Output) Generate(ctx context.Context, outputCh chan<- OutputPacket) error {
	return nil
}

func (o *Output) SendInput(
	ctx context.Context,
	inputPkt InputPacket,
	outputCh chan<- OutputPacket,
) (_err error) {
	logger.Tracef(ctx,
		"SendInput (pkt: %p, pos:%d, pts:%d, dts:%d, dur:%d)",
		inputPkt.Packet, inputPkt.Packet.Pos(), inputPkt.Packet.Pts(), inputPkt.Packet.Dts(), inputPkt.Packet.Duration(),
	)
	defer func() { logger.Tracef(ctx, "/SendInput (pkt: %p): %v", inputPkt.Packet, _err) }()

	pkt := inputPkt.Packet
	if pkt == nil {
		return fmt.Errorf("packet == nil")
	}

	outputStream := o.Streams[inputPkt.StreamIndex()]
	if outputStream == nil {
		logger.Debugf(ctx, "new output stream")
		o.formatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
			outputStream = &OutputStream{
				Stream:  o.FormatContext.NewStream(nil),
				LastDTS: math.MinInt64,
			}
		})
		if err := stream.CopyParameters(ctx, outputStream.Stream, inputPkt.Stream); err != nil {
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

		o.Streams[inputPkt.StreamIndex()] = outputStream
		if len(o.Streams) < 2 {
			// TODO: delete me; an ugly hack to make sure we have both video and audio track before sending a header
			return nil
		}
		var err error
		o.formatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
			err = o.FormatContext.WriteHeader(nil)
		})
		if err != nil {
			return fmt.Errorf("unable to write the header: %w", err)
		}
	}
	assert(ctx, outputStream != nil)
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"unmodified packet with pos:%v (pts:%v, dts:%v, dur: %v) for %s stream %d (->%d) with flags 0x%016X",
			pkt.Pos(), pkt.Pts(), pkt.Dts(), pkt.Duration(),
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(),
			outputStream.Index(),
			pkt.Flags(),
		)
	}

	if pkt.Dts() < outputStream.LastDTS {
		logger.Errorf(ctx, "received a DTS from the past, ignoring the packet: %d < %d", pkt.Dts(), outputStream.LastDTS)
		return nil
	}

	pkt.SetStreamIndex(outputStream.Index())
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"writing packet with pos:%v (pts:%v, dts:%v, dur:%v) for %s stream %d (sample_rate: %v, time_base: %v) with flags 0x%016X and data 0x %X",
			pkt.Pos(), pkt.Pts(), pkt.Pts(), pkt.Duration(),
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(), outputStream.CodecParameters().SampleRate(), outputStream.TimeBase(),
			pkt.Flags(),
			pkt.Data(),
		)
	}

	if len(o.Streams) < 2 {
		if len(o.pendingPackets) >= pendingPacketsLimit {
			return fmt.Errorf("exceeded the limit of pending packets: %d", len(o.pendingPackets))
		}
		o.pendingPackets = append(o.pendingPackets, packet.CloneAsReferenced(pkt))
		return nil
	}
	for idx, packet := range o.pendingPackets {
		if err := o.doWritePacket(ctx, packet, outputStream); err != nil {
			o.pendingPackets = o.pendingPackets[idx:]
			return err
		}
	}
	o.pendingPackets = o.pendingPackets[:0]
	if err := o.doWritePacket(ctx, pkt, outputStream); err != nil {
		return err
	}
	return nil
}

func (o *Output) doWritePacket(
	ctx context.Context,
	packet *astiav.Packet,
	outputStream *OutputStream,
) (_err error) {
	var err error
	o.formatContextLocker.Do(xsync.WithNoLogging(ctx, true), func() {
		err = o.FormatContext.WriteInterleavedFrame(packet)
	})
	if err != nil {
		return fmt.Errorf("unable to write the frame: %w", err)
	}
	outputStream.LastDTS = packet.Dts()
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(
			ctx,
			"wrote a packet (dts: %d): %s: %s: %v",
			packet.Dts(),
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
			err,
		)
	}
	return nil
}

func (o *Output) GetOutputFormatContext(ctx context.Context) *astiav.FormatContext {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &o.formatContextLocker, func() *astiav.FormatContext {
		return o.FormatContext
	})
}

func (o *Output) String() string {
	return fmt.Sprintf("Output(%s)", o.URL)
}
