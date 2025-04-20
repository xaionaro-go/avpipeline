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
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/xaionaro-go/avpipeline/codec/consts"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/stream"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/proxy"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/xsync"
)

const (
	unwrapTLSViaProxy      = false
	pendingPacketsLimit    = 10000
	outputWaitForKeyFrames = true
	outputCopyStreamIndex  = true
	outputUpdateStreams    = false
)

type OutputConfig struct {
	CustomOptions types.DictionaryItems
}

type OutputStream struct {
	*astiav.Stream
	LastDTS int64
}

type OutputInputStream struct {
	packet.Source
	*astiav.Stream
}

type Output struct {
	ID            OutputID
	URL           string
	StreamKey     secret.String
	InputStreams  map[int]OutputInputStream
	OutputStreams map[int]*OutputStream
	Filter        condition.Condition
	SenderLocker  xsync.Mutex

	ioContext *astiav.IOContext
	proxy     *proxy.TCPProxy

	formatContextLocker xsync.RWMutex

	started          bool
	pendingPackets   []*astiav.Packet
	waitingKeyFrames map[int]struct{}

	*closeChan
	*astiav.FormatContext
	*astiav.Dictionary
}

var _ Abstract = (*Output)(nil)
var _ packet.Source = (*Output)(nil)

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
		ID:            OutputID(nextOutputID.Add(1)),
		URL:           url.String(),
		StreamKey:     streamKey,
		InputStreams:  make(map[int]OutputInputStream),
		OutputStreams: make(map[int]*OutputStream),
		closeChan:     newCloseChan(),

		waitingKeyFrames: make(map[int]struct{}),
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
	o.closeChan.Close(ctx)

	var result []error
	if o.started && len(o.FormatContext.Streams()) != 0 {
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

func (o *Output) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}

func (o *Output) updateOutputFormat(
	ctx context.Context,
	inputSource packet.Source,
	inputFmt *astiav.FormatContext,
) (_err error) {
	logger.Debugf(ctx, "updateOutputFormat: %d streams", inputFmt.NbStreams())
	defer func() { logger.Debugf(ctx, "/updateOutputFormat: %v", _err) }()
	for _, inputStream := range inputFmt.Streams() {
		inputStreamIndex := inputStream.Index()
		if _, ok := o.OutputStreams[inputStreamIndex]; ok {
			logger.Tracef(ctx, "stream #%d already exists, not initializing", inputStreamIndex)
			continue
		}

		outputStream, err := o.initOutputStreamFor(ctx, inputSource, inputStream)
		if err != nil {
			return fmt.Errorf("unable to initialize an output stream for input stream #%d: %w", inputStreamIndex, err)
		}

		if outputStream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			o.waitingKeyFrames[outputStream.Index()] = struct{}{}
			logger.Debugf(ctx, "len(waitingKeyFrames): increase -> %d", len(o.waitingKeyFrames))
		}
	}
	return nil
}

func (o *Output) initOutputStreamFor(
	ctx context.Context,
	inputSource packet.Source,
	inputStream *astiav.Stream,
) (_ *OutputStream, _err error) {
	logger.Tracef(ctx, "initOutputStreamFor(ctx, stream[%d])", inputStream.Index())
	defer func() { logger.Tracef(ctx, "/initOutputStreamFor(ctx, stream[%d]) %v", inputStream.Index(), _err) }()

	outputStream := &OutputStream{
		Stream:  o.FormatContext.NewStream(nil),
		LastDTS: math.MinInt64,
	}

	if err := o.configureOutputStream(ctx, outputStream, inputSource, inputStream); err != nil {
		return nil, err
	}

	return outputStream, nil
}

func (o *Output) configureOutputStream(
	ctx context.Context,
	outputStream *OutputStream,
	inputSource packet.Source,
	inputStream *astiav.Stream,
) error {
	if err := stream.CopyParameters(ctx, outputStream.Stream, inputStream); err != nil {
		return fmt.Errorf("unable to copy stream parameters: %w", err)
	}

	logger.Debugf(
		ctx,
		"new output stream: %d: %s: %s: %s: %s: %s",
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
	)

	if outputCopyStreamIndex {
		outputStream.SetIndex(inputStream.Index())
	}
	o.InputStreams[inputStream.Index()] = OutputInputStream{Source: inputSource, Stream: inputStream}
	o.OutputStreams[inputStream.Index()] = outputStream
	return nil
}

func (o *Output) getOutputStream(
	ctx context.Context,
	inputSource packet.Source,
	inputStream *astiav.Stream,
	fmtCtx *astiav.FormatContext,
) (*OutputStream, error) {
	outputStream := o.OutputStreams[inputStream.Index()]
	if outputStream != nil {
		if outputUpdateStreams {
			origInputStream := o.InputStreams[inputStream.Index()]
			if origInputStream.Source == inputSource {
				return outputStream, nil
			}
			logger.Debugf(ctx,
				"input %s stream changed: %p -> %p",
				inputStream.CodecParameters().MediaType(),
				origInputStream, inputStream,
			)
			timeBase := outputStream.TimeBase()
			o.configureOutputStream(ctx, outputStream, inputSource, inputStream)
			outputStream.SetTimeBase(timeBase) // Otherwise MPEGTS does not work, sometimes
			o.InputStreams[inputStream.Index()] = OutputInputStream{
				Source: inputSource,
				Stream: inputStream,
			}
		}
		return outputStream, nil
	}

	logger.Debugf(ctx, "new output stream (#%d)", inputStream.Index())
	err := o.updateOutputFormat(ctx, inputSource, fmtCtx)
	if err != nil {
		return nil, fmt.Errorf("unable to update the output format: %w", err)
	}
	outputStream = o.OutputStreams[inputStream.Index()]
	assert(ctx, outputStream != nil)
	return outputStream, nil
}

func (o *Output) SendInputPacket(
	ctx context.Context,
	inputPkt packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) (_err error) {
	logger.Tracef(ctx,
		"SendInputPacket (pkt: %p, pos:%d, pts:%d, dts:%d, dur:%d)",
		inputPkt.Packet, inputPkt.Packet.Pos(), inputPkt.Packet.Pts(), inputPkt.Packet.Dts(), inputPkt.Packet.Duration(),
	)
	defer func() { logger.Tracef(ctx, "/SendInputPacket (pkt: %p): %v", inputPkt.Packet, _err) }()

	pkt := inputPkt.Packet
	if pkt == nil {
		return fmt.Errorf("packet == nil")
	}

	if pkt.Flags().Has(astiav.PacketFlagDiscard) {
		logger.Tracef(ctx, "the packet has a discard flag; discarding")
		return nil
	}

	var (
		outputStream *OutputStream
		err          error
	)
	o.formatContextLocker.Do(ctx, func() {
		inputPkt.Source.WithFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			outputStream, err = o.getOutputStream(ctx, inputPkt.Source, inputPkt.GetStream(), fmtCtx)
		})
	})

	if err != nil {
		return fmt.Errorf("unable to get the output stream: %w", err)
	}
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx,
			"unmodified packet with pos:%v (pts:%v, dts:%v, dur: %v) for %s stream %d (->%d) with flags 0x%016X",
			pkt.Pos(), pkt.Pts(), pkt.Dts(), pkt.Duration(),
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(),
			outputStream.Index(),
			pkt.Flags(),
		)
	}
	if outputStream.TimeBase().Num() == 0 {
		return fmt.Errorf("internal error: TimeBase must be set")
	}

	pkt.SetStreamIndex(outputStream.Index())
	pkt.RescaleTs(inputPkt.Stream.TimeBase(), outputStream.TimeBase())

	var inputStreamsCount int
	inputPkt.Source.WithFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		inputStreamsCount = fmtCtx.NbStreams()
	})

	logger.Tracef(ctx, "sending the current packet")

	err = xsync.DoA4R1(ctx, &o.SenderLocker, o.send, ctx, inputStreamsCount, pkt, outputStream)
	if err != nil {
		return err
	}
	return nil
}

func (o *Output) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return fmt.Errorf("cannot send raw frames, one need to encode them into packets and send as packets")
}

func (o *Output) String() string {
	return fmt.Sprintf("Output(%s)", o.URL)
}

func (o *Output) send(
	ctx context.Context,
	expectedStreamsCount int,
	pkt *astiav.Packet,
	outputStream *OutputStream,
) error {
	if o.started {
		return o.doWritePacket(ctx, pkt, outputStream)
	}

	activeStreamCount := o.FormatContext.NbStreams()
	if outputWaitForKeyFrames && (activeStreamCount < expectedStreamsCount || len(o.waitingKeyFrames) != 0) {
		logger.Tracef(ctx, "not starting sending the packets, yet: %d < %d; %s", activeStreamCount, expectedStreamsCount, outputStream.CodecParameters().MediaType())
		// we have to skip non-key-video packets here, otherwise mediamtx (https://github.com/bluenviron/mediamtx)
		// does not see the video track:
		if outputStream.CodecParameters().MediaType() != astiav.MediaTypeVideo {
			return nil
		}
		keyFrame := pkt.Flags().Has(astiav.PacketFlagKey)
		if keyFrame {
			logger.Debugf(ctx, "got a key video frame")
		}
		streamIndex := pkt.StreamIndex()
		_, waitingKeyFrame := o.waitingKeyFrames[streamIndex]
		if waitingKeyFrame {
			delete(o.waitingKeyFrames, streamIndex)
			logger.Debugf(ctx, "len(waitingKeyFrames): decrease -> %d", len(o.waitingKeyFrames))
		}
		if keyFrame && waitingKeyFrame {
			o.pendingPackets = append(o.pendingPackets, packet.CloneAsReferenced(pkt))
			if len(o.pendingPackets) > pendingPacketsLimit {
				logger.Errorf(ctx, "the limit of pending packets is exceeded, have to drop older packets")
				o.pendingPackets = o.pendingPackets[1:]
			}
		}
		return nil
	} else {
		o.pendingPackets = append(o.pendingPackets, packet.CloneAsReferenced(pkt))
	}
	o.started = true

	logger.Debugf(ctx, "writing the header; streams: %d/%d; len(waitingKeyFrames): %d", o.FormatContext.NbStreams(), expectedStreamsCount, len(o.waitingKeyFrames))
	var err error
	o.formatContextLocker.Do(ctx, func() {
		err = o.FormatContext.WriteHeader(nil)
	})
	if err != nil {
		return fmt.Errorf("unable to write the header: %w", err)
	}

	logger.Debugf(ctx, "started sending packets (have %d streams for %d expected streams); len(pendingPackets): %d; current_packet:%s", activeStreamCount, expectedStreamsCount, len(o.pendingPackets), outputStream.CodecParameters().MediaType())

	for _, pkt := range o.pendingPackets {
		err := o.doWritePacket(belt.WithField(ctx, "reason", "pending_packet"), pkt, outputStream)
		packet.Pool.Put(pkt)
		if err != nil {
			return fmt.Errorf("unable to write a pending packet: %w", err)
		}
	}
	o.pendingPackets = o.pendingPackets[:0]
	return nil
}

func (o *Output) doWritePacket(
	ctx context.Context,
	pkt *astiav.Packet,
	outputStream *OutputStream,
) (_err error) {
	if o.Filter != nil && !o.Filter.Match(ctx, packet.BuildInput(pkt, outputStream.Stream, o)) {
		return nil
	}

	//pkt.SetPos(-1) // <- TODO: should this happen? why?
	isNoDTS := pkt.Dts() == consts.NoPTSValue
	isNoPTS := pkt.Pts() == consts.NoPTSValue
	if !isNoDTS && !isNoPTS && pkt.Dts() > pkt.Pts() {
		logger.Errorf(ctx, "DTS (%d) is greater than PTS (%d), setting DTS = PTS", pkt.Dts(), pkt.Pts())
		pkt.SetDts(pkt.Pts())
	}
	if !isNoDTS && pkt.Dts() <= outputStream.LastDTS {
		// TODO: do not skip B-frames
		logger.Errorf(ctx,
			"received a DTS from the stream's past or has invalid value (%v), ignoring the packet from stream #%d: %d < %d",
			outputStream.CodecParameters().MediaType(),
			outputStream.Index(),
			pkt.Dts(),
			outputStream.LastDTS,
		)
		return nil
	}
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx,
			"writing packet with pos:%v (pts:%v, dts:%v, dur:%v, dts_prev:%v) for %s stream %d (sample_rate: %v, time_base: %v) with flags 0x%016X and data 0x %X",
			pkt.Pos(), pkt.Dts(), pkt.Pts(), pkt.Duration(), outputStream.LastDTS,
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(), outputStream.CodecParameters().SampleRate(), outputStream.TimeBase(),
			pkt.Flags(),
			pkt.Data(),
		)
	}

	var err error
	pos, dts, pts, dur := pkt.Pos(), pkt.Dts(), pkt.Pts(), pkt.Duration()
	o.formatContextLocker.Do(ctx, func() {
		err = o.FormatContext.WriteInterleavedFrame(pkt)
	})
	if err != nil {
		err = fmt.Errorf(
			"unable to write the packet with pos:%v (pts:%v, dts:%v, dur:%v, dts_prev:%v) for %s stream %d (sample_rate: %v, time_base: %v) with flags 0x%016X and data length %d: %w",
			pos, dts, pts, dur, outputStream.LastDTS,
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(), outputStream.CodecParameters().SampleRate(), outputStream.TimeBase(),
			pkt.Flags(),
			len(pkt.Data()),
			err,
		)
		return err
	}
	outputStream.LastDTS = dts
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx,
			"wrote a packet (pos: %d; pts: %d; dts: %d): %s: %s: %v",
			pos, dts, pts,
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
			err,
		)
	}
	return nil
}

func (o *Output) WithFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	o.formatContextLocker.Do(ctx, func() {
		callback(o.FormatContext)
	})
}

func (o *Output) NotifyAboutPacketSource(
	ctx context.Context,
	source packet.Source,
) (_ret error) {
	logger.Debugf(ctx, "NotifyAboutPacketSource(ctx, %T)", source)
	defer func() { logger.Debugf(ctx, "/NotifyAboutPacketSource(ctx, %T): %v", source, _ret) }()
	var errs []error
	source.WithFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		o.formatContextLocker.Do(ctx, func() {
			for _, stream := range fmtCtx.Streams() {
				logger.Debugf(ctx, "making sure stream #%d is initialized", stream.Index())
				_, err := o.getOutputStream(ctx, source, stream, fmtCtx)
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to initialize an output stream for input stream %d from source %s: %w", stream.Index(), source, err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
