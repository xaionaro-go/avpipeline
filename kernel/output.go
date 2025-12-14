package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/codec/consts"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/stream"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/proxy"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/xsync"
	"tailscale.com/util/ringbuffer"
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
	outputWriteTrailer                  = true
	outputDebug                         = false
	revert0c55f85                       = false
)

type OutputConfigWaitForOutputStreams struct {
	MinStreams       uint
	MinStreamsVideo  uint
	MinStreamsAudio  uint
	VideoBeforeAudio *bool
}

type OutputConfig struct {
	CustomOptions  globaltypes.DictionaryItems
	AsyncOpen      bool
	OnOpened       func(context.Context, *Output) error
	SendBufferSize uint

	WaitForOutputStreams *OutputConfigWaitForOutputStreams

	ErrorOnNSequentialInvalidDTS uint
}

type OutputPacketMonitor interface {
	ObserveOutputPacket(
		ctx context.Context,
		stream *astiav.Stream,
		output *astiav.Packet,
	)
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
	FrameInfo   *FrameInfo
	Source      packet.Source
	InputStream *astiav.Stream
}

type outTS struct {
	PacketSize uint64
	PTS        time.Duration
	DTS        time.Duration
}

// Note: it is strongly recommended to put MonotonicDTS before Output.
type Output struct {
	ID            OutputID
	StreamKey     secret.String
	InputStreams  map[int]OutputInputStream
	OutputStreams map[int]*OutputStream
	Filter        condition.Condition
	SenderLocker  xsync.Mutex
	Config        OutputConfig

	SequentialInvalidPacketsCount uint
	OutputMonitor                 xatomic.Value[OutputPacketMonitor]

	headerSent bool
	ioContext  *astiav.IOContext
	proxy      *proxy.TCPProxy

	formatContextLocker xsync.CtxLocker

	URL       string
	URLParsed *url.URL

	LatestSentPTS time.Duration
	LatestSentDTS time.Duration

	PreallocatedAudioStreams []*OutputStream
	PreallocatedVideoStreams []*OutputStream

	started          bool
	pendingPackets   []pendingPacket
	waitingKeyFrames map[int]struct{}
	outputFormatName string
	outTSs           *ringbuffer.RingBuffer[outTS]

	*closuresignaler.ClosureSignaler
	*astiav.FormatContext
	*astiav.Dictionary
}

var _ Abstract = (*Output)(nil)
var _ packet.Sink = (*Output)(nil)

func formatFromURL(url *url.URL) string {
	switch url.Scheme {
	case "":
		ext := filepath.Ext(url.Path)
		if ext == "" {
			return ""
		}
		return ext[1:]
	case "rtmp", "rtmps":
		return "flv"
	case "srt":
		return "mpegts"
	default:
		return url.Scheme
	}
}

var nextOutputID atomic.Uint64

func NewOutputFromURL(
	ctx context.Context,
	urlString string,
	streamKey secret.String,
	cfg OutputConfig,
) (_ret *Output, _err error) {
	logger.Debugf(ctx, "NewOutputFromURL(ctx, '%s', streamKey, %s)", urlString, spew.Sdump(cfg))
	defer func() {
		logger.Debugf(ctx, "/NewOutputFromURL(ctx, '%s', streamKey, %s): %p %v", urlString, spew.Sdump(cfg), _ret, _err)
	}()

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
		outTSs:              ringbuffer.New[outTS](10000),
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

	formatNameRequest := formatFromURL(url)

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

	switch formatNameRequest {
	case "flv":
		if cfg.WaitForOutputStreams.VideoBeforeAudio == nil {
			if cfg.WaitForOutputStreams.MinStreamsVideo == 0 {
				cfg.WaitForOutputStreams.MinStreamsVideo = 1
			}
			cfg.WaitForOutputStreams.VideoBeforeAudio = ptr(true)
		}
	}

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
	if cfg.WaitForOutputStreams.VideoBeforeAudio == nil {
		cfg.WaitForOutputStreams.VideoBeforeAudio = ptr(false)
	}
	logger.Debugf(ctx, "output.WaitForOutputStreams: %s", spew.Sdump(cfg.WaitForOutputStreams))

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

	switch url.Scheme {
	case "rtmp", "rtmps":
		for i := 0; i < int(cfg.WaitForOutputStreams.MinStreamsVideo); i++ {
			outputStream := &OutputStream{
				Stream:  o.FormatContext.NewStream(nil),
				LastDTS: math.MinInt64,
			}
			o.PreallocatedVideoStreams = append(o.PreallocatedVideoStreams, outputStream)
			o.waitingKeyFrames[outputStream.Index()] = struct{}{}
		}
	}

	formatName := o.FormatContext.OutputFormat().Name()
	logger.Debugf(ctx, "output format name: '%s'", formatName)
	o.outputFormatName = formatName

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
				if !o.headerSent || !outputWriteTrailer {
					return nil
				}
				logger.Debugf(ctx, "writing the trailer")
				err := o.FormatContext.WriteTrailer()
				logger.Debugf(ctx, "wrote the trailer, result: %v", err)
				o.headerSent = false
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
			o.ioContext = nil
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

	var outputStream *OutputStream
	switch inputStream.CodecParameters().MediaType() {
	case astiav.MediaTypeVideo:
		if len(o.PreallocatedVideoStreams) > 0 {
			outputStream = o.PreallocatedVideoStreams[0]
			o.PreallocatedVideoStreams = o.PreallocatedVideoStreams[1:]
			logger.Debugf(ctx, "reusing preallocated video output stream for input stream #%d", inputStream.Index())
		}
	case astiav.MediaTypeAudio:
		if len(o.PreallocatedAudioStreams) > 0 {
			outputStream = o.PreallocatedAudioStreams[0]
			o.PreallocatedAudioStreams = o.PreallocatedAudioStreams[1:]
			logger.Debugf(ctx, "reusing preallocated audio output stream for input stream #%d", inputStream.Index())
		}
	}
	if outputStream == nil {
		outputStream = &OutputStream{
			Stream:  o.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
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
		"new output stream: %d->%d: %s: %s: %s: %s: %s; extraData: %s",
		inputStream.Index(),
		outputStream.Index(),
		outputStream.CodecParameters().MediaType(),
		outputStream.CodecParameters().CodecID(),
		outputStream.TimeBase(),
		spew.Sdump(outputStream),
		spew.Sdump(outputStream.CodecParameters()),
		extradata.Raw(outputStream.CodecParameters().ExtraData()),
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

	switch outputStream.CodecParameters().MediaType() {
	case astiav.MediaTypeVideo:
		w, h := outputStream.CodecParameters().Width(), outputStream.CodecParameters().Height()
		if w == 0 || h == 0 {
			return fmt.Errorf("video stream has invalid dimensions: %dx%d", w, h)
		}
		logger.Debugf(ctx, "video stream dimensions: %dx%d", w, h)
	}

	return nil
}

func (o *Output) preallocateOutputStream(
	ctx context.Context,
	inputStream *astiav.Stream,
) (_err error) {
	inputStreamIndex := inputStream.Index()
	if _, ok := o.OutputStreams[inputStreamIndex]; ok {
		logger.Tracef(ctx, "stream #%d already exists, not preallocating", inputStreamIndex)
		return nil
	}

	logger.Debugf(ctx, "preallocating output stream for input stream #%d", inputStreamIndex)
	switch inputStream.CodecParameters().MediaType() {
	case astiav.MediaTypeAudio:
		outputStream := &OutputStream{
			Stream:  o.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
		o.PreallocatedAudioStreams = append(o.PreallocatedAudioStreams, outputStream)

		o.waitingKeyFrames[outputStream.Index()] = struct{}{}
		logger.Debugf(ctx, "waiting for key frames from %d streams", len(o.waitingKeyFrames))
		o.OutputStreams[inputStreamIndex] = nil
	case astiav.MediaTypeVideo:
		outputStream := &OutputStream{
			Stream:  o.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
		o.PreallocatedVideoStreams = append(o.PreallocatedVideoStreams, outputStream)
		o.OutputStreams[inputStreamIndex] = nil
	default:
		logger.Tracef(ctx, "not preallocating output stream for input stream #%d: media type is %s", inputStreamIndex, inputStream.CodecParameters().MediaType())
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
		"SendInputPacket (stream: %d:%s, pkt:%p, pos:%d, pts:%d, dts:%d, dur:%d, size: %d)",
		inputPkt.StreamIndex(), inputPkt.GetMediaType(), inputPkt.Packet, inputPkt.Packet.Pos(), inputPkt.Packet.Pts(), inputPkt.Packet.Dts(), inputPkt.Packet.Duration(), inputPkt.Packet.Size(),
	)
	defer func() {
		logger.Tracef(ctx, "/SendInputPacket (stream: %d:%s, pkt:%p): %v",
			inputPkt.StreamIndex(), inputPkt.GetMediaType(), inputPkt.Packet, _err)
	}()

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
		err          error = ErrNoSourceFormatContext{
			StreamIndex: inputPkt.StreamIndex(),
		}
	)
	o.formatContextLocker.Do(ctx, func() {
		inputPkt.Source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			outputStream, err = o.getOutputStream(ctx, inputPkt.Source, inputPkt.Stream, fmtCtx)
		})
		if errors.As(err, &ErrNoSourceFormatContext{}) {
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

	frameInfo := FrameInfoFromPacketInput(inputPkt)

	err = xsync.DoR1(ctx, &o.SenderLocker, func() error {
		return o.send(ctx, pkt, frameInfo, inputPkt.Source, inputPkt.Stream, outputStream)
	})
	if err != nil {
		return err
	}
	return nil
}

type ErrNoSourceFormatContext struct {
	StreamIndex int
}

func (e ErrNoSourceFormatContext) Error() string {
	return fmt.Sprintf("no source format context (stream_index: %d)", e.StreamIndex)
}

func (o *Output) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return fmt.Errorf("cannot send raw frames, one need to encode them into packets and send as packets")
}

func (o *Output) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(o)
}

func (o *Output) String() string {
	return fmt.Sprintf("Output(%s)", o.URL)
}

func (o *Output) send(
	ctx context.Context,
	pkt *astiav.Packet,
	frameInfo *FrameInfo,
	source packet.Source,
	inputStream *astiav.Stream,
	outputStream *OutputStream,
) error {
	if o.started {
		return o.doWritePacket(ctx, pkt, frameInfo, source, inputStream, outputStream)
	}

	mediaType := inputStream.CodecParameters().MediaType()

	var expectedStreamsCount uint
	var expectedStreamsVideoCount uint
	var expectedStreamsAudioCount uint
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		for _, inputStream := range fmtCtx.Streams() {
			switch inputStream.CodecParameters().MediaType() {
			case astiav.MediaTypeVideo:
				expectedStreamsVideoCount++
			case astiav.MediaTypeAudio:
				expectedStreamsAudioCount++
			}
			expectedStreamsCount++
		}
	})
	if o.Config.WaitForOutputStreams != nil {
		if expectedStreamsCount < o.Config.WaitForOutputStreams.MinStreams {
			expectedStreamsCount = o.Config.WaitForOutputStreams.MinStreams
		}
		expectedStreamsVideoCount = max(expectedStreamsVideoCount, o.Config.WaitForOutputStreams.MinStreamsVideo)
		expectedStreamsAudioCount = max(expectedStreamsAudioCount, o.Config.WaitForOutputStreams.MinStreamsAudio)
	}

	activeStreamCount := xsync.DoR1(ctx, &o.formatContextLocker, func() uint {
		return uint(len(o.OutputStreams))
	})

	keyFrame := pkt.Flags().Has(astiav.PacketFlagKey)
	logger.Debugf(ctx, "isKeyFrame:%t, expectedStreamsCount:%d, expectedStreamsVideoCount:%d, expectedStreamsAudioCount:%d", keyFrame, expectedStreamsCount, expectedStreamsVideoCount, expectedStreamsAudioCount)
	if !keyFrame && (revert0c55f85 || len(o.waitingKeyFrames) > 0) {
		if outputAcceptOnlyKeyFramesUntilStart {
			logger.Debugf(ctx, "not a key frame; skipping")
			return nil
		}
	}
	if *o.Config.WaitForOutputStreams.VideoBeforeAudio && expectedStreamsVideoCount > 0 {
		// we have to skip non-key-video packets here, otherwise mediamtx (https://github.com/bluenviron/mediamtx)
		// does not see the video track:
		if mediaType != astiav.MediaTypeVideo {
			logger.Debugf(ctx, "skipping a non-video packet to avoid MediaMTX from losing the video track")
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
			FrameInfo:   frameInfo,
			Source:      source,
			InputStream: inputStream,
		})
		if len(o.pendingPackets) > pendingPacketsAndFramesLimit {
			logger.Errorf(ctx, "the limit of pending packets is exceeded, have to drop older packets")
			o.pendingPackets = o.pendingPackets[1:]
		}
	}
	var activeVideoStreamCount uint
	var activeAudioStreamCount uint
	for _, stream := range o.OutputStreams {
		switch stream.CodecParameters().MediaType() {
		case astiav.MediaTypeVideo:
			activeVideoStreamCount++
		case astiav.MediaTypeAudio:
			activeAudioStreamCount++
		}
	}
	if o.Config.WaitForOutputStreams != nil {
		if activeStreamCount < expectedStreamsCount {
			logger.Tracef(ctx, "not starting sending the packets, yet: total streams: %d < %d; %s", activeStreamCount, expectedStreamsCount, mediaType)
			return nil
		}
		if activeVideoStreamCount < expectedStreamsVideoCount {
			logger.Tracef(ctx, "not starting sending the packets, yet: video streams: %d < %d; %s", activeVideoStreamCount, expectedStreamsVideoCount, mediaType)
			return nil
		}
		if activeAudioStreamCount < expectedStreamsAudioCount {
			logger.Tracef(ctx, "not starting sending the packets, yet: audio streams: %d < %d; %s", activeAudioStreamCount, expectedStreamsAudioCount, mediaType)
			return nil
		}
	}
	if outputWaitForKeyFrames && len(o.waitingKeyFrames) != 0 {
		logger.Tracef(ctx, "not starting sending the packets, yet: %d != 0; %s", len(o.waitingKeyFrames), mediaType)
		return nil
	}
	o.started = true

	var err error
	if outputWriteHeaders {
		o.formatContextLocker.Do(ctx, func() {
			if o.FormatContext == nil {
				err = io.EOF
				return
			}
			logger.Debugf(
				ctx,
				"writing the header; streams: *:%d/%d, a:%d/%d, v:%d/%d; len(waitingKeyFrames): %d",
				activeStreamCount, expectedStreamsCount,
				activeAudioStreamCount, expectedStreamsAudioCount,
				activeVideoStreamCount, expectedStreamsVideoCount,
				len(o.waitingKeyFrames),
			)
			err = o.FormatContext.WriteHeader(nil)
			o.headerSent = true
			logger.Debugf(ctx, "wrote the header: %v", err)
		})
	}
	if err != nil {
		return fmt.Errorf("unable to write the header: %w", err)
	}

	logger.Debugf(ctx, "started sending packets (have %d streams for %d expected streams); len(pendingPackets): %d; current_packet:%s %X", activeStreamCount, expectedStreamsCount, len(o.pendingPackets), mediaType, pkt.Flags())

	if !outputSendPendingPackets {
		return o.doWritePacket(ctx, pkt, frameInfo, source, inputStream, outputStream)
	}
	for _, pendingPkt := range o.pendingPackets {
		//pendingPkt.RescaleTs(pendingPkt.InputStream.TimeBase(), inputStream.TimeBase())
		err := o.doWritePacket(
			belt.WithField(ctx, "reason", "pending_packet"),
			pendingPkt.Packet,
			pendingPkt.FrameInfo,
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

type OutputMonitorer interface {
	SetOutputMonitor(
		ctx context.Context,
		monitor OutputPacketMonitor,
	)
	GetOutputMonitor(
		ctx context.Context,
	) OutputPacketMonitor
}

func (o *Output) SetOutputMonitor(
	ctx context.Context,
	monitor OutputPacketMonitor,
) {
	o.OutputMonitor.Store(monitor)
}

func (o *Output) GetOutputMonitor(
	ctx context.Context,
) OutputPacketMonitor {
	return o.OutputMonitor.Load()
}

func (o *Output) doWritePacket(
	ctx context.Context,
	pkt *astiav.Packet,
	frameInfo *FrameInfo,
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
		logger.Errorf(ctx, "DTS (%d) is greater than PTS (%d), setting DTS = PTS (pict-type: 0x%02X)", pkt.Dts(), pkt.Pts(), int(frameInfo.GetPictureType()))
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

	pos, dts, pts, dur := pkt.Pos(), pkt.Dts(), pkt.Pts(), pkt.Duration()

	var ptsDuration time.Duration
	if pts == consts.NoPTSValue {
		logger.Warnf(ctx, "PTS is missing in the packet")
	} else {
		ptsDuration = avconv.Duration(pts, outputStream.TimeBase())
	}
	var dtsDuration time.Duration
	if dts == consts.NoPTSValue {
		dtsDuration = ptsDuration
	} else {
		dtsDuration = avconv.Duration(dts, outputStream.TimeBase())
	}
	o.outTSs.Add(outTS{
		PTS:        ptsDuration,
		DTS:        dtsDuration,
		PacketSize: o.getBinarySize(ctx, pkt),
	})

	var dataLen int
	if logger.FromCtx(ctx).Level() >= logger.LevelTrace {
		resolution := codectypes.Resolution{
			Width:  uint32(outputStream.CodecParameters().Width()),
			Height: uint32(outputStream.CodecParameters().Height()),
		}
		sampleRate := outputStream.CodecParameters().SampleRate()
		channels := outputStream.CodecParameters().ChannelLayout().Channels()
		dataLen = len(pkt.Data())
		logger.Tracef(ctx,
			"writing packet with pos:%v (pts:%v(%v), dts:%v, dur:%v, dts_prev:%v; is_key:%v; source: %T) for %s stream %d (res: %s, sample_rate: %v, channels: %v, time_base: %v) with flags 0x%016X and data length %d",
			pos, pts, ptsDuration, dts, dur, outputStream.LastDTS, pkt.Flags().Has(astiav.PacketFlagKey), source,
			outputStream.CodecParameters().MediaType(),
			pkt.StreamIndex(), resolution, sampleRate, channels, outputStream.TimeBase(),
			pkt.Flags(),
			len(pkt.Data()),
		)
	}

	if outputMonitor := o.OutputMonitor.Load(); outputMonitor != nil {
		outputMonitor.ObserveOutputPacket(ctx, outputStream.Stream, pkt)
	}

	var err error
	o.formatContextLocker.Do(ctx, func() {
		if o.FormatContext == nil {
			err = io.EOF
			return
		}
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
	o.LatestSentPTS = ptsDuration
	o.LatestSentDTS = dtsDuration
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

func (o *Output) getBinarySize(
	ctx context.Context,
	pkt *astiav.Packet,
) (_ret uint64) {
	defer func() { logger.Tracef(ctx, "getBinarySize: %d", _ret) }()
	var size uint64
	size += uint64(pkt.Size())
	switch o.outputFormatName {
	case "flv":
		// FLV tag header + PreviousTagSize
		size += 11 + 4
	}
	if o.URLParsed != nil {
		switch o.URLParsed.Scheme {
		case "rtmp", "rtmps":
			// 1 byte:  FrameType + CodecID (VideoTagHeader)
			// 1 byte:  AVCPacketType
			// 3 bytes: CompositionTime
			size += 5
		}
	}
	return size
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
				err := o.preallocateOutputStream(ctx, stream)
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to preallocate an output stream for input stream %d from source %s: %w", stream.Index(), source, err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
