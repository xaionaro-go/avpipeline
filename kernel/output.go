package kernel

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"runtime/debug"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/codec/consts"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
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
	unwrapTLSViaProxy                   = false
	pendingPacketsAndFramesLimit        = 10000
	outputWaitForKeyFrames              = false
	outputWaitForStreams                = true
	outputCopyStreamIndex               = false
	outputUpdateStreams                 = false
	outputSendPendingPackets            = true
	skipTooHighTimestamps               = false
	flvForbidStreamIndexAbove1          = true
	outputAcceptOnlyKeyFramesUntilStart = true
	outputSetRTMPAppName                = false
	outputWriteHeaders                  = true
	outputDebug                         = true
	revert0c55f85                       = false
)

type OutputConfigWaitForOutputStreams struct {
	MinStreams       uint
	VideoBeforeAudio bool // this is a hack required to make MediaMTX see video tracks
}

type OutputConfig struct {
	CustomOptions  types.DictionaryItems
	AsyncOpen      bool
	OnOpened       func(context.Context, *Output) error
	SendBufferSize uint

	WaitForOutputStreams *OutputConfigWaitForOutputStreams

	ErrorOnNSequentialInvalidDTS uint
}

type OutputStream struct {
	*astiav.Stream
	LastKeyFrameSource packet.Source
	LastDTS            int64
}

func (outputStream *OutputStream) GetMediaType() astiav.MediaType {
	if outputStream == nil {
		return astiav.MediaTypeUnknown
	}
	params := outputStream.CodecParameters()
	if params == nil {
		return astiav.MediaTypeUnknown
	}
	return params.MediaType()
}

type OutputInputStream struct {
	packet.Source
	*astiav.Stream
}

type pendingPacket struct {
	*astiav.Packet
	Source      packet.Source
	InputStream *astiav.Stream
}

type Output struct {
	ID            OutputID
	StreamKey     secret.String
	InputStreams  map[int]OutputInputStream
	OutputStreams map[int]*OutputStream
	Filter        condition.Condition
	SenderLocker  xsync.Mutex
	Config        OutputConfig

	SequentialInvalidPacketsCount uint

	ioContext *astiav.IOContext
	proxy     *proxy.TCPProxy

	formatContextLocker xsync.CtxLocker

	URL       string
	URLParsed *url.URL

	LatestSentDTS time.Duration

	started          bool
	pendingPackets   []pendingPacket
	waitingKeyFrames map[int]struct{}

	*closuresignaler.ClosureSignaler
	*astiav.FormatContext
	*astiav.Dictionary
}

var _ Abstract = (*Output)(nil)
var _ packet.Sink = (*Output)(nil)

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
) (_ret *Output, _err error) {
	logger.Debugf(ctx, "NewOutputFromURL(ctx, '%s', streamKey, %#+v)", urlString, cfg)
	defer func() {
		logger.Debugf(ctx, "/NewOutputFromURL(ctx, '%s', streamKey, %#+v): %p %v", urlString, cfg, _ret, _err)
	}()
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

	if cfg.WaitForOutputStreams == nil {
		cfg.WaitForOutputStreams = &OutputConfigWaitForOutputStreams{}
		if outputWaitForStreams {
			cfg.WaitForOutputStreams.MinStreams = 2
		}
	}

	o := &Output{
		ID:              OutputID(nextOutputID.Add(1)),
		URL:             url.String(),
		StreamKey:       streamKey,
		InputStreams:    make(map[int]OutputInputStream),
		OutputStreams:   make(map[int]*OutputStream),
		Config:          cfg,
		ClosureSignaler: closuresignaler.New(),

		formatContextLocker: make(xsync.CtxLocker, 1),
		waitingKeyFrames:    make(map[int]struct{}),
	}

	rtmpAppName := strings.Trim(url.Path, "/")
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

	formatNameRequest := formatFromScheme(url.Scheme)

	if len(cfg.CustomOptions) > 0 {
		o.Dictionary = astiav.NewDictionary()
		setFinalizerFree(ctx, o.Dictionary)

		for _, opt := range cfg.CustomOptions {
			if opt.Key == "f" {
				formatNameRequest = opt.Value
				continue
			}
			logger.Debugf(ctx, "output.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
			o.Dictionary.Set(opt.Key, opt.Value, 0)
		}
	}

	func() {
		switch url.Scheme {
		case "rtmp", "rtmps":
			if o.Dictionary == nil {
				o.Dictionary = astiav.NewDictionary()
				setFinalizerFree(ctx, o.Dictionary)
			}

			for _, opt := range cfg.CustomOptions {
				if opt.Key == "rtmp_app" {
					continue // is already set, nothing is required from us here
				}
			}

			if outputSetRTMPAppName {
				logger.Debugf(ctx, "set 'rtmp_app':'%s'", rtmpAppName)
				o.Dictionary.Set("rtmp_app", rtmpAppName, 0)
			}
			o.Dictionary.Set("rtmp_live", "live", 0)
			o.Dictionary.Set("flvflags", "+no_sequence_end+no_metadata+no_duration_filesize", 0)
		case "rtsp", "srt":
			if o.Dictionary == nil {
				o.Dictionary = astiav.NewDictionary()
				setFinalizerFree(ctx, o.Dictionary)
			}

			o.Dictionary.Set("live", "1", 0)
		}
	}()

	logger.Debugf(ctx, "isAsync: %t", cfg.AsyncOpen)
	if cfg.AsyncOpen {
		observability.Go(ctx, func(ctx context.Context) {
			if err := o.doOpen(ctx, url, formatNameRequest, cfg); err != nil {
				logger.Errorf(ctx, "unable to open: %v", err)
				o.Close(ctx)
			}
		})
	} else {
		if err := o.doOpen(ctx, url, formatNameRequest, cfg); err != nil {
			return nil, err
		}
	}

	return o, nil
}

func (o *Output) doOpen(
	ctx context.Context,
	url *url.URL,
	formatNameRequest string,
	cfg OutputConfig,
) (_err error) {
	logger.Debugf(ctx, "doOpen(ctx, url, '%s', %#+v)", formatNameRequest, cfg)
	defer func() { logger.Debugf(ctx, "/doOpen(ctx, url, '%s', %#+v): %v", formatNameRequest, cfg, _err) }()

	logger.Debugf(observability.OnInsecureDebug(ctx), "URL: %s", url)
	formatContext, err := astiav.AllocOutputFormatContext(
		nil,
		formatNameRequest,
		url.String(),
	)
	if err != nil {
		return fmt.Errorf("allocating output format context failed using URL '%s': %w", url, err)
	}
	if formatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return fmt.Errorf("unable to allocate the output format context")
	}
	o.FormatContext = formatContext
	setFinalizerFree(ctx, o.FormatContext)

	defer func() {
		if _err == nil {
			if cfg.OnOpened != nil {
				cfg.OnOpened(ctx, o)
			}
		}
	}()

	formatName := o.FormatContext.OutputFormat().Name()
	logger.Debugf(ctx, "output format name: '%s'", formatName)

	if o.FormatContext.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		// if output is not a file then nothing else to do
		return nil
	}
	logger.Tracef(ctx, "destination '%s' is a file", url)

	ioContext, err := astiav.OpenIOContext(
		url.String(),
		astiav.NewIOContextFlags(astiav.IOContextFlagWrite),
		nil,
		o.Dictionary,
	)
	if err != nil {
		return fmt.Errorf("unable to open IO context (URL: '%s'): %w", url, err)
	}
	o.ioContext = ioContext
	o.FormatContext.SetPb(ioContext)
	o.URLParsed = url
	if cfg.SendBufferSize != 0 {
		if err := o.UnsafeSetSendBufferSize(ctx, cfg.SendBufferSize); err != nil {
			return fmt.Errorf("unable to set the send buffer size to %d: %w", cfg.SendBufferSize, err)
		}
		logger.Debugf(ctx, "set the send buffer size to %d", cfg.SendBufferSize)
	}

	return nil
}

func (o *Output) Close(
	ctx context.Context,
) (_err error) {
	logger.Debugf(ctx, "Close")
	defer func() { logger.Debugf(ctx, "/Close: %v", _err) }()
	o.ClosureSignaler.Close(ctx)

	var result []error
	o.formatContextLocker.Do(ctx, func() {
		if o.FormatContext == nil {
			logger.Debugf(ctx, "already closed")
			return
		}
		if o.started && len(o.FormatContext.Streams()) != 0 {
			err := func() error {
				defer func() {
					r := recover()
					if r != nil {
						result = append(result, fmt.Errorf("got panic: %v:\n%s\n\r", r, debug.Stack()))
					}
				}()
				logger.Debugf(ctx, "writing the trailer")
				err := o.FormatContext.WriteTrailer()
				logger.Debugf(ctx, "write the trailer, result: %v", err)
				return err
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
		o.FormatContext = nil
	})
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

		outputFormat := o.FormatContext.OutputFormat().Name()
		logger.Debugf(ctx, "output format is: '%s'", outputFormat)
		switch outputFormat {
		case "flv":
			if flvForbidStreamIndexAbove1 {
				if inputStreamIndex < 0 || inputStreamIndex >= 2 {
					return fmt.Errorf("too many streams: requested stream index is %d, while FLV supports only 0 for video and 1 for audio", inputStreamIndex)
				}
			}
			if len(o.OutputStreams) >= 2 {
				var haveIndexes []int
				for haveIndex := range o.OutputStreams {
					haveIndexes = append(haveIndexes, haveIndex)
				}
				sort.Ints(haveIndexes)
				return fmt.Errorf("too many streams: FLV supports only 1 video and 1 audio stream maximum; but I already have %d streams and yet I was requested to initialize at least one more; have indexes: %v, but requested %d", len(o.OutputStreams), haveIndexes, inputStreamIndex)
			}
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
		"new output stream: %d->%d: %s: %s: %s: %s: %s",
		inputStream.Index(),
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
	switch o.FormatContext.OutputFormat().Name() {
	case "flv":
		logger.Debugf(ctx, "this is a FLV output, setting CodecTag to zero")
		outputStream.CodecParameters().SetCodecTag(0)
	}

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

	logger.Debugf(ctx, "building new output stream for input stream #%d", inputStream.Index())
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
		err          error = ErrNoSourceFormatContext{}
	)
	o.formatContextLocker.Do(ctx, func() {
		inputPkt.Source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			outputStream, err = o.getOutputStream(ctx, inputPkt.Source, inputPkt.Stream, fmtCtx)
		})
		if err == (ErrNoSourceFormatContext{}) {
			outputStream = o.OutputStreams[inputPkt.StreamIndex()]
			if outputStream != nil {
				err = nil
			}
		}
	})
	if err != nil {
		return fmt.Errorf("unable to get the output stream: %w", err)
	}
	assert(ctx, outputStream != nil)

	err = xsync.DoR1(ctx, &o.SenderLocker, func() error {
		return o.send(ctx, pkt, inputPkt.Source, inputPkt.Stream, outputStream)
	})
	if err != nil {
		return err
	}
	return nil
}

type ErrNoSourceFormatContext struct{}

func (e ErrNoSourceFormatContext) Error() string {
	return "no source format context"
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
	pkt *astiav.Packet,
	source packet.Source,
	inputStream *astiav.Stream,
	outputStream *OutputStream,
) error {
	if o.started {
		return o.doWritePacket(ctx, pkt, source, inputStream, outputStream)
	}

	mediaType := inputStream.CodecParameters().MediaType()

	var expectedStreamsCount int
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		expectedStreamsCount = fmtCtx.NbStreams()
	})
	if o.Config.WaitForOutputStreams != nil {
		if expectedStreamsCount < int(o.Config.WaitForOutputStreams.MinStreams) {
			expectedStreamsCount = int(o.Config.WaitForOutputStreams.MinStreams)
		}
	}

	activeStreamCount := xsync.DoR1(ctx, &o.formatContextLocker, func() int {
		return o.FormatContext.NbStreams()
	})

	keyFrame := pkt.Flags().Has(astiav.PacketFlagKey)
	logger.Debugf(ctx, "isKeyFrame:%t", keyFrame)
	if !keyFrame && (revert0c55f85 || len(o.waitingKeyFrames) > 0) {
		if outputAcceptOnlyKeyFramesUntilStart {
			logger.Debugf(ctx, "not a key frame; skipping")
			return nil
		}
	}
	if o.Config.WaitForOutputStreams.VideoBeforeAudio || true {
		// we have to skip non-key-video packets here, otherwise mediamtx (https://github.com/bluenviron/mediamtx)
		// does not see the video track:
		if mediaType != astiav.MediaTypeVideo {
			logger.Tracef(ctx, "skipping a non-video packet to avoid MediaMTX from losing the video track")
			return nil
		}
	}

	streamIndex := pkt.StreamIndex()
	_, waitingKeyFrame := o.waitingKeyFrames[streamIndex]
	if waitingKeyFrame {
		delete(o.waitingKeyFrames, streamIndex)
		logger.Debugf(ctx, "len(waitingKeyFrames): decrease -> %d", len(o.waitingKeyFrames))
	}
	if outputSendPendingPackets {
		o.pendingPackets = append(o.pendingPackets, pendingPacket{
			Packet:      packet.CloneAsReferenced(pkt),
			Source:      source,
			InputStream: inputStream,
		})
		if len(o.pendingPackets) > pendingPacketsAndFramesLimit {
			logger.Errorf(ctx, "the limit of pending packets is exceeded, have to drop older packets")
			o.pendingPackets = o.pendingPackets[1:]
		}
	}
	if o.Config.WaitForOutputStreams != nil && activeStreamCount < expectedStreamsCount {
		logger.Tracef(ctx, "not starting sending the packets, yet: %d < %d; %s", activeStreamCount, expectedStreamsCount, mediaType)
		return nil
	}
	if outputWaitForKeyFrames && len(o.waitingKeyFrames) != 0 {
		logger.Tracef(ctx, "not starting sending the packets, yet: %d != 0; %s", len(o.waitingKeyFrames), mediaType)

		return nil
	}
	o.started = true

	logger.Debugf(ctx, "writing the header; streams: %d/%d; len(waitingKeyFrames): %d", activeStreamCount, expectedStreamsCount, len(o.waitingKeyFrames))
	var err error
	if outputWriteHeaders {
		o.formatContextLocker.Do(ctx, func() {
			err = o.FormatContext.WriteHeader(nil)
		})
	}
	if err != nil {
		return fmt.Errorf("unable to write the header: %w", err)
	}

	logger.Debugf(ctx, "started sending packets (have %d streams for %d expected streams); len(pendingPackets): %d; current_packet:%s %X", activeStreamCount, expectedStreamsCount, len(o.pendingPackets), mediaType, pkt.Flags())

	if !outputSendPendingPackets {
		return o.doWritePacket(ctx, pkt, source, inputStream, outputStream)
	}
	for _, pendingPkt := range o.pendingPackets {
		//pendingPkt.RescaleTs(pendingPkt.InputStream.TimeBase(), inputStream.TimeBase())
		err := o.doWritePacket(
			belt.WithField(ctx, "reason", "pending_packet"),
			pendingPkt.Packet,
			pendingPkt.Source,
			pendingPkt.InputStream,
			outputStream,
		)
		packet.Pool.Put(pendingPkt.Packet)
		if err != nil {
			return fmt.Errorf("unable to write a pending packet: %w", err)
		}
	}
	o.pendingPackets = o.pendingPackets[:0]
	return nil
}

type GetLatestSentDTSer interface {
	GetLatestSentDTS(ctx context.Context) time.Duration
}

func (o *Output) GetLatestSentDTS(
	ctx context.Context,
) time.Duration {
	return xsync.DoR1(ctx, &o.formatContextLocker, func() time.Duration {
		return o.LatestSentDTS
	})
}

func (o *Output) doWritePacket(
	ctx context.Context,
	pkt *astiav.Packet,
	source packet.Source,
	inputStream *astiav.Stream,
	outputStream *OutputStream,
) (_err error) {
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx,
			"unmodified packet with pos:%v (pts:%v, dts:%v, dur: %v) for %s stream %d (->%d) with flags 0x%016X",
			pkt.Pos(), pkt.Pts(), pkt.Dts(), pkt.Duration(),
			outputStream.GetMediaType(),
			pkt.StreamIndex(),
			outputStream.Index(),
			pkt.Flags(),
		)
	}
	if outputStream.TimeBase().Num() == 0 {
		return fmt.Errorf("internal error: TimeBase must be set")
	}

	if outputStream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
		codecID := outputStream.CodecParameters().CodecID()
		switch codecID {
		case astiav.CodecIDH264, astiav.CodecIDH265:
			logger.Tracef(ctx, "an H264/H265 packet: %s", codecID)
			if source != outputStream.LastKeyFrameSource {
				if pkt.Flags().Has(astiav.PacketFlagKey) {
					outputStream.LastKeyFrameSource = source
					logger.Debugf(ctx, "received a key frame from a new source: %p:%s", source, outputStream.LastKeyFrameSource)
				} else {
					if revert0c55f85 || outputStream.LastKeyFrameSource != nil {
						logger.Errorf(
							ctx,
							"ignoring a non-keyframe packet received from another source (%p:%s != %p:%s) until we start a group using a key frame from that source",
							source, source, outputStream.LastKeyFrameSource, outputStream.LastKeyFrameSource,
						)
						return nil
					}
				}
			}
		}
	}

	if skipTooHighTimestamps {
		if pkt.Dts() > 9000000000000000000 {
			logger.Errorf(ctx, "DTS is too high: %d", pkt.Dts())
			return nil
		}
		if pkt.Pts() > 9000000000000000000 {
			logger.Errorf(ctx, "PTS is too high: %d", pkt.Pts())
			return nil
		}
	}

	//pkt.SetPos(-1) // <- TODO: should this happen? why?
	pkt.RescaleTs(inputStream.TimeBase(), outputStream.TimeBase())
	isNoDTS := pkt.Dts() == consts.NoPTSValue
	isNoPTS := pkt.Pts() == consts.NoPTSValue
	if !isNoDTS && !isNoPTS && pkt.Dts() > pkt.Pts() {
		logger.Errorf(ctx, "DTS (%d) is greater than PTS (%d), setting DTS = PTS", pkt.Dts(), pkt.Pts())
		pkt.SetDts(pkt.Pts())
	}
	if !isNoDTS && pkt.Dts() < outputStream.LastDTS {
		o.SequentialInvalidPacketsCount++
		if o.Config.ErrorOnNSequentialInvalidDTS > 0 {
			if o.SequentialInvalidPacketsCount > o.Config.ErrorOnNSequentialInvalidDTS {
				return fmt.Errorf("received %d sequential invalid DTSes, the session seems broken", o.SequentialInvalidPacketsCount)
			}
		}
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
	o.SequentialInvalidPacketsCount = 0

	pkt.SetStreamIndex(outputStream.Index())
	if o.Filter != nil && !o.Filter.Match(ctx, packet.BuildInput(pkt, packet.BuildStreamInfo(outputStream.Stream, source, nil))) {
		return nil
	}

	var dataLen int
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		dataLen = len(pkt.Data())
		logger.Tracef(ctx,
			"writing packet with pos:%v (pts:%v(%v), dts:%v, dur:%v, dts_prev:%v; is_key:%v; source: %T) for %s stream %d (sample_rate: %v, time_base: %v) with flags 0x%016X and data length %d",
			pkt.Pos(), pkt.Pts(), avconv.Duration(pkt.Pts(), outputStream.TimeBase()), pkt.Dts(), pkt.Duration(), outputStream.LastDTS, pkt.Flags().Has(astiav.PacketFlagKey), source,
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(), outputStream.CodecParameters().SampleRate(), outputStream.TimeBase(),
			pkt.Flags(),
			len(pkt.Data()),
		)
	}

	pos, dts, pts, dur := pkt.Pos(), pkt.Dts(), pkt.Pts(), pkt.Duration()
	var err error
	o.formatContextLocker.Do(ctx, func() {
		err = o.FormatContext.WriteInterleavedFrame(pkt)
	})
	if err != nil {
		isKey := pkt.Flags().Has(astiav.PacketFlagKey)
		err = fmt.Errorf(
			"unable to write the packet with pos:%v (is_key:%v, pts:%v, dts:%v, dur:%v, dts_prev:%v) for %s stream %d (sample_rate: %v, time_base: %v) with flags 0x%016X and data length %d: %w",
			pos, isKey, pts, dts, dur, outputStream.LastDTS,
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(), outputStream.CodecParameters().SampleRate(), outputStream.TimeBase(),
			pkt.Flags(),
			dataLen,
			err,
		)
		return err
	}
	outputStream.LastDTS = dts
	o.LatestSentDTS = avconv.Duration(dts, outputStream.TimeBase())
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		logger.Tracef(ctx,
			"wrote a packet (pos: %d; pts: %d; dts: %d): %s: %s; len:%d: %v",
			pos, dts, pts,
			outputStream.CodecParameters().MediaType(),
			outputStream.CodecParameters().CodecID(),
			dataLen,
			err,
		)
	}
	if outputDebug {
		logger.Tracef(ctx, "current queue size: %#+v", o.GetInternalQueueSize(ctx))
	}
	return nil
}

func (o *Output) WithInputFormatContext(
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
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
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
