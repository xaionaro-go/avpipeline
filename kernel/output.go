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
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/facebookincubator/go-belt"
	"github.com/go-ng/xatomic"
	"github.com/xaionaro-go/avcommon"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/codec/consts"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	netraw "github.com/xaionaro-go/avpipeline/net/raw"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/stream"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/proxy"
	"github.com/xaionaro-go/secret"
	tcpopt "github.com/xaionaro-go/tcp/opt"
	"github.com/xaionaro-go/xsync"
	"tailscale.com/util/ringbuffer"
)

const (
	unwrapTLSViaProxy                   = false
	pendingPacketsAndFramesLimit        = 10000
	outputWaitForKeyFrames              = false
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
	// rtmpDefaultOutChunkSize is the chunk size we negotiate with the RTMP
	// peer at connection time. FFmpeg's libavformat ships with a 128-byte
	// default which causes one ~12-byte RTMP chunk header per 128 bytes of
	// payload, fragmenting a single video frame across many tiny TCP
	// segments and pinning av_interleaved_write_frame at high CPU. Modern
	// servers (nginx-rtmp, MediaMTX, AVD's own ingest) accept much larger
	// chunks, so we raise the chunk size to match the typical TCP send
	// buffer / MSS-multiple regime. Override per-output via the
	// "rtmp_out_chunk_size" custom option key.
	rtmpDefaultOutChunkSize = 65536
	// rtmpMaxChunkSize is the protocol-defined upper bound on the RTMP
	// chunk size. Per RTMP Chunk Stream spec §5.4.1 the Set-Chunk-Size
	// payload is a 32-bit big-endian integer whose high bit MUST be 0,
	// so the maximum legal value is 0x7FFFFFFF.
	rtmpMaxChunkSize = 0x7FFFFFFF
)

type OutputConfigWaitForOutputStreams struct {
	MinStreams         uint
	MinStreamsVideo    uint
	MinStreamsAudio    uint
	MinStreamsSubtitle uint
	MinStreamsData     uint
	VideoBeforeAudio   *bool
	// Timeout bounds how long send() will defer WriteHeader while
	// waiting for the configured Min* stream counts to be satisfied.
	// The deadline is armed when the first packet is buffered as
	// pending, and once it expires the kernel commits to writing the
	// header with whatever streams have been registered so far. Any
	// stream that arrives later is rejected via ErrLateStreamAddition.
	//
	// Zero means "use a sensible default for live forwarding"
	// (see defaultWaitForOutputStreamsTimeout); a negative value
	// disables the timeout (wait indefinitely until Min* are met).
	Timeout time.Duration
}

// defaultWaitForOutputStreamsTimeout is the WriteHeader-deferral
// window used when WaitForOutputStreams.Timeout is left at its zero
// value. It is sized for live forwarding of high-bitrate inputs
// (SRT / MPEGTS / RTSP / RTMP), where the demuxer may need a few
// seconds to parse the audio/video stream descriptors before they
// become visible in the source format context.
const defaultWaitForOutputStreamsTimeout = 3 * time.Second

type OutputConfig struct {
	CustomOptions  globaltypes.DictionaryItems
	AsyncOpen      bool
	OnOpened       func(context.Context, *Output) error
	SendBufferSize uint

	WaitForOutputStreams *OutputConfigWaitForOutputStreams

	ErrorOnNSequentialInvalidDTS  uint
	IgnoreNoSourceFormatCtxErrors bool
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

// Output represents an output kernel that sends packets/frames to a specified destination.
//
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
	netConn

	formatContextLocker xsync.CtxLocker

	URL       string
	URLParsed *url.URL

	LatestSentPTS time.Duration
	LatestSentDTS time.Duration

	PreallocatedAudioStreams    []*OutputStream
	PreallocatedVideoStreams    []*OutputStream
	PreallocatedSubtitleStreams []*OutputStream
	PreallocatedDataStreams     []*OutputStream

	sendingAllowed       bool
	firstVideoPacketSeen bool
	openFinished         chan struct{}
	openError            error
	pendingPackets       []pendingPacket
	// pendingPacketsDeadline is armed when the first packet is queued
	// into pendingPackets while we are still waiting for the configured
	// Min* stream counts (see Config.WaitForOutputStreams). Once
	// time.Now() crosses this deadline, send() commits to writing the
	// muxer header with whatever streams are currently registered, even
	// if Min* are not yet satisfied. Zero value means "not armed" /
	// "no buffered packets yet".
	pendingPacketsDeadline time.Time
	waitingKeyFrames       map[int]struct{}
	outputFormatName       string
	outTSs                 *ringbuffer.RingBuffer[outTS]

	*closuresignaler.ClosureSignaler
	*astiav.FormatContext
	*astiav.Dictionary
}

var (
	_ Abstract              = (*Output)(nil)
	_ packet.Sink           = (*Output)(nil)
	_ GetInternalQueueSizer = (*Output)(nil)
	_ WithNetworkConner     = (*Output)(nil)
	_ WithRawNetworkConner  = (*Output)(nil)
)

// muxerAllowsLateStreamAddition reports whether the muxer with the
// given libavformat name can safely accept a new stream after its
// header has been written. The decision is per-muxer because the
// container specification varies. Unknown muxers default to refuse
// (safe). When libavformat grows real runtime stream-table mutation
// for a muxer, only that muxer's arm of the switch needs to flip
// to true.
func muxerAllowsLateStreamAddition(muxerName string) bool {
	switch muxerName {
	case "flv":
		// Spec quote: "There shall be no more than one audio and one
		// video stream, synchronized together, in an FLV file. An FLV
		// file shall not define multiple independent streams of a
		// single type."
		// Source: Adobe Flash Video File Format Specification v10.1,
		// Annex E.1 (page 68).
		// URL: https://veovera.org/docs/legacy/video-file-format-v10-1-spec.pdf
		return false
	case "mpegts":
		// MPEG-TS spec [ISO/IEC 13818-1] allows late streams via PMT
		// version_number increment, but libavformat's mpegtsenc does
		// not implement dynamic PMT updates (write_header allocates
		// per-stream state once at libavformat/mpegtsenc.c:1166;
		// tables_version is written once; streams added after
		// avformat_write_header() have NULL priv_data and crash
		// mpegts_write_packet on first reference). Treat as refuse
		// until upstream gains support.
		return false
	case "matroska", "webm":
		// Spec structure: a Segment contains exactly one Tracks Master
		// element, defined in the initialization region. Adding a new
		// TrackEntry after the Tracks element is serialized would
		// require rewriting the segment header.
		// Source: Matroska element spec.
		// URL: https://www.matroska.org/technical/elements.html
		return false
	case "mp4", "mov":
		// ISO BMFF: track_IDs and per-track sample tables are defined
		// in the initialization segment (moov/trak/trex). A media
		// fragment must be decodable using only the init segment, so
		// new tracks cannot appear after moov is written.
		// Source: ISO/IEC 14496-12 / ISOBMFF byte-stream format.
		// URL: https://www.w3.org/2013/12/byte-stream-format-registry/isobmff-byte-stream-format.html
		return false
	case "hls", "m3u8":
		// Spec quote: "If the encoding parameters or codec values
		// change… an EXT-X-DISCONTINUITY tag MUST be present in the
		// Media Playlist before the first Media Segment with a
		// different value."
		// Source: RFC 8216 §3.5 (EXT-X-DISCONTINUITY semantics).
		// URL: https://datatracker.ietf.org/doc/html/rfc8216
		return false
	default:
		// Unknown muxer — refuse to be safe. Adding a stream that the
		// muxer does not expect risks SIGFPE inside
		// av_interleaved_write_frame on division by sample_rate=0 or
		// time_base.den=0 for the unconfigured stream entry.
		return false
	}
}

func formatFromURL(url *url.URL) string {
	switch url.Scheme {
	case "":
		if url.Path == "/dev/null" {
			return "null"
		}
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

		openFinished:        make(chan struct{}),
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
	if cfg.WaitForOutputStreams.Timeout == 0 {
		// Default for live forwarding: SRT/MPEGTS/RTSP/RTMP demuxers
		// often need a few seconds to parse codec parameters of all
		// elementary streams from a high-bitrate feed before they
		// surface in the source format context. Use the package
		// default so the late-stream gate has a chance to release
		// after both audio and video are visible.
		cfg.WaitForOutputStreams.Timeout = defaultWaitForOutputStreamsTimeout
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
			o.Close(ctx)
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

	defer func() {
		o.openError = _err
		close(o.openFinished)
	}()

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
	case "rtmp", "rtmps", "rtsp", "srt":
		for i := 0; i < int(cfg.WaitForOutputStreams.MinStreamsVideo); i++ {
			outputStream := &OutputStream{
				Stream:  o.FormatContext.NewStream(nil),
				LastDTS: math.MinInt64,
			}
			o.PreallocatedVideoStreams = append(o.PreallocatedVideoStreams, outputStream)
			o.waitingKeyFrames[outputStream.Index()] = struct{}{}
		}
		for i := 0; i < int(cfg.WaitForOutputStreams.MinStreamsAudio); i++ {
			outputStream := &OutputStream{
				Stream:  o.FormatContext.NewStream(nil),
				LastDTS: math.MinInt64,
			}
			o.PreallocatedAudioStreams = append(o.PreallocatedAudioStreams, outputStream)
		}
		for i := 0; i < int(cfg.WaitForOutputStreams.MinStreamsSubtitle); i++ {
			outputStream := &OutputStream{
				Stream:  o.FormatContext.NewStream(nil),
				LastDTS: math.MinInt64,
			}
			o.PreallocatedSubtitleStreams = append(o.PreallocatedSubtitleStreams, outputStream)
		}
		for i := 0; i < int(cfg.WaitForOutputStreams.MinStreamsData); i++ {
			outputStream := &OutputStream{
				Stream:  o.FormatContext.NewStream(nil),
				LastDTS: math.MinInt64,
			}
			o.PreallocatedDataStreams = append(o.PreallocatedDataStreams, outputStream)
		}
	}

	formatName := o.FormatContext.OutputFormat().Name()
	flags := o.FormatContext.OutputFormat().Flags()
	logger.Debugf(ctx, "output format name: '%s', flags: %v (NOFILE:%v)", formatName, flags, flags.Has(astiav.IOFormatFlagNofile))
	o.outputFormatName = formatName

	if url.String() != "" && !o.FormatContext.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
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
	}
	o.URLParsed = url

	o.initNetworkConn(ctx)

	if cfg.SendBufferSize != 0 {
		err := o.WithRawNetworkConn(
			ctx,
			func(
				ctx context.Context,
				rawConn syscall.RawConn,
				_ string,
			) error {
				return netraw.SetTCPSockOption(ctx, rawConn, tcpopt.SendBuffer(cfg.SendBufferSize))
			},
		)
		if err != nil {
			return ErrUnableToSetSendBufferSize{
				Size: cfg.SendBufferSize,
				Err:  err,
			}
		}
		logger.Debugf(ctx, "set the send buffer size to %d", cfg.SendBufferSize)
	}

	if err := o.maybeRaiseRTMPOutChunkSize(ctx, url, cfg); err != nil {
		return fmt.Errorf("unable to raise RTMP out_chunk_size: %w", err)
	}

	return nil
}

// maybeRaiseRTMPOutChunkSize sends an RTMP Set-Chunk-Size control message on
// the underlying TCP socket and updates rt->out_chunk_size in the libavformat
// RTMPContext, raising it from the FFmpeg default of 128 bytes to
// rtmpDefaultOutChunkSize. This must run after initNetworkConn (which sets up
// netConn.avioCtx and the RTMP URLContext) and before the muxer starts
// writing FLV/RTMP packets — i.e. before WriteHeader. The RTMP Chunk Stream
// protocol allows either side to change its outgoing chunk size at any time
// (see librtmp / FFmpeg rtmpproto.c handle_chunk_size), so it is safe to do
// this once at session start.
//
// Caller contract: this function MUST be invoked between initNetworkConn and
// the first muxer write (WriteHeader). Within that window no other goroutine
// is allowed to interact with the same RTMPContext: we both mutate
// rt->out_chunk_size via cgo and write a Set-Chunk-Size frame directly to
// the raw TCP fd, bypassing the FFmpeg AVIO buffer. Concurrent muxer writes
// or reads on the same connection during this window would race the byte
// stream and the RTMPContext field. Currently the sequential doOpen flow is
// what enforces this (no other goroutine has a handle to o.netConn yet).
//
// Excludes the rtmps:// scheme on purpose: rtmps wraps the RTMP byte stream
// in TLS, and the raw-fd write here would land plaintext below the TLS
// layer (corrupting the session). The cgo TCPContext() lookup we use to
// reach the RTMPContext also panics when the protocol name is "tls"
// instead of "tcp", so an rtmps connection cannot be safely tuned this way.
func (o *Output) maybeRaiseRTMPOutChunkSize(
	ctx context.Context,
	url *url.URL,
	cfg OutputConfig,
) error {
	switch url.Scheme {
	case "rtmp":
	default:
		return nil
	}

	desired := rtmpDefaultOutChunkSize
	if v := cfg.CustomOptions.GetFirst("rtmp_out_chunk_size"); v != nil {
		parsed, err := strconv.Atoi(*v)
		if err != nil {
			return fmt.Errorf("invalid rtmp_out_chunk_size value '%s': %w", *v, err)
		}
		if parsed <= 0 {
			return fmt.Errorf("rtmp_out_chunk_size must be > 0, got %d", parsed)
		}
		if parsed > rtmpMaxChunkSize {
			return fmt.Errorf("rtmp_out_chunk_size must be <= %d (RTMP spec §5.4.1), got %d", rtmpMaxChunkSize, parsed)
		}
		desired = parsed
	}

	var rtmpCtx *avcommon.RTMPContext
	o.netConn.locker.ManualRLock(ctx)
	rtmpCtx = o.netConn.unsafeGetRawRTMPContext(ctx)
	o.netConn.locker.ManualRUnlock(ctx)
	if rtmpCtx == nil {
		logger.Debugf(ctx, "no RTMP context available; skipping out_chunk_size raise")
		return nil
	}
	current := rtmpCtx.OutChunkSize()
	if current >= desired {
		logger.Debugf(ctx, "RTMP out_chunk_size is already %d (>= %d); not raising", current, desired)
		return nil
	}

	// RTMP Chunk Stream Set-Chunk-Size message:
	//   chunk basic header  : 0x02         (fmt=0, csid=2 — protocol control channel)
	//   chunk message header: ts(BE24)=0, msg_len(BE24)=4, msg_type=0x01,
	//                          msg_stream_id(LE32)=0
	//   chunk payload       : new_chunk_size(BE32)
	setChunkSizeMsg := [16]byte{
		0x02,
		0x00, 0x00, 0x00,
		0x00, 0x00, 0x04,
		0x01,
		0x00, 0x00, 0x00, 0x00,
		byte(desired >> 24),
		byte(desired >> 16),
		byte(desired >> 8),
		byte(desired),
	}

	err := o.WithRawNetworkConn(
		ctx,
		func(_ context.Context, rc syscall.RawConn, _ string) error {
			var werr error
			cerr := rc.Write(func(fd uintptr) bool {
				// Loop until the whole Set-Chunk-Size frame is written.
				// syscall.Write may return short on a non-blocking
				// socket or under back-pressure; sending a partial
				// SCS frame would desync the RTMP chunk stream.
				buf := setChunkSizeMsg[:]
				for len(buf) > 0 {
					n, err := syscall.Write(int(fd), buf)
					if err != nil {
						werr = err
						return true
					}
					if n <= 0 {
						werr = fmt.Errorf("syscall.Write returned %d for SCS frame", n)
						return true
					}
					buf = buf[n:]
				}
				return true
			})
			if cerr != nil {
				return cerr
			}
			return werr
		},
	)
	if err != nil {
		return fmt.Errorf("unable to send Set-Chunk-Size control packet: %w", err)
	}

	rtmpCtx.SetOutChunkSize(desired)
	logger.Debugf(ctx, "raised RTMP out_chunk_size: %d -> %d", current, desired)
	return nil
}

func (o *Output) initNetworkConn(ctx context.Context) {
	if o.FormatContext.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		logger.Debugf(ctx, "not initializing network connection: output format has NOFILE flag")
		return
	}

	if o.URLParsed == nil {
		logger.Errorf(ctx, "cannot init network connection: URLParsed == nil")
		return
	}

	o.netConn.Init(ctx, o.FormatContext)
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
		if o.sendingAllowed && len(o.FormatContext.Streams()) != 0 {
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
				return err
			}()
			if err != nil {
				result = append(result, fmt.Errorf("unable to write the tailer: %w", err))
			}
		}
		if o.ioContext != nil {
			o.ioContext.Flush()
			if err := o.ioContext.Close(); err != nil {
				result = append(result, fmt.Errorf("unable to close the IO context: %w", err))
			}
			o.ioContext = nil
		}
		if err := o.netConn.Close(ctx); err != nil {
			result = append(result, fmt.Errorf("unable to close the network connection: %w", err))
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
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (o *Output) updateOutputFormat(
	ctx context.Context,
	inputSource packet.Source,
	inputFmt *astiav.FormatContext,
) (_err error) {
	if inputFmt == nil {
		return fmt.Errorf("input format context is nil")
	}
	if inputFmt.Class() == nil {
		return fmt.Errorf("input format context is closed")
	}
	if inputFmt.NbStreams() <= 0 {
		return fmt.Errorf("input format context has no streams")
	}
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
			return fmt.Errorf("(output) unable to initialize an output stream for input stream #%d: %w", inputStreamIndex, err)
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
	case astiav.MediaTypeSubtitle:
		if len(o.PreallocatedSubtitleStreams) > 0 {
			outputStream = o.PreallocatedSubtitleStreams[0]
			o.PreallocatedSubtitleStreams = o.PreallocatedSubtitleStreams[1:]
			logger.Debugf(ctx, "reusing preallocated subtitle output stream for input stream #%d", inputStream.Index())
		}
	case astiav.MediaTypeData:
		if len(o.PreallocatedDataStreams) > 0 {
			outputStream = o.PreallocatedDataStreams[0]
			o.PreallocatedDataStreams = o.PreallocatedDataStreams[1:]
			logger.Debugf(ctx, "reusing preallocated data output stream for input stream #%d", inputStream.Index())
		}
	}
	if outputStream == nil {
		outputStream = &OutputStream{
			Stream:  o.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
	}

	// Save the muxer-set timebase before configureOutputStream, which
	// copies all parameters (including timebase) from the input stream.
	// After WriteHeader the muxer has already chosen the correct
	// timebase for the container (e.g. 1/1000 for FLV); overwriting it
	// with the encoder's codec timebase (e.g. 1/48000 for AAC) makes
	// the RescaleTs in doWritePacket a no-op, causing raw sample counts
	// to be written as milliseconds into the stream.
	savedTimeBase := outputStream.TimeBase()

	if err := o.configureOutputStream(ctx, outputStream, inputSource, inputStream); err != nil {
		return nil, err
	}

	if o.headerSent && savedTimeBase.Den() != 0 {
		logger.Debugf(
			ctx,
			"restoring muxer-set timebase %s (was overwritten to %s by configureOutputStream)",
			savedTimeBase, outputStream.TimeBase(),
		)
		outputStream.SetTimeBase(savedTimeBase)
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
		// Reject video streams whose time_base is not yet populated.
		// Without a non-zero denominator, RescaleTs and the muxer
		// internals (e.g. mpegts PCR computation) divide by zero,
		// producing SIGFPE inside av_interleaved_write_frame. This
		// mirrors the audio sample_rate guard below.
		if outputStream.TimeBase().Den() == 0 {
			return fmt.Errorf("video stream has invalid time_base: %s", outputStream.TimeBase())
		}
		logger.Debugf(ctx, "video stream dimensions: %dx%d", w, h)
	case astiav.MediaTypeAudio:
		// Reject audio streams whose codec parameters are not yet populated.
		// Without sample_rate, container muxers like mpegts log "sample rate
		// not set", make WriteHeader return EINVAL, and then SIGFPE inside
		// av_interleaved_write_frame on division by sample_rate=0. Returning
		// an error here lets the caller retry once the demuxer has populated
		// the parameters from later packets.
		sr := outputStream.CodecParameters().SampleRate()
		if sr == 0 {
			return fmt.Errorf("audio stream has invalid sample_rate: %d", sr)
		}
		logger.Debugf(ctx, "audio stream sample_rate: %d", sr)
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
	// Output may be in the process of being torn down (Close set FormatContext
	// to nil under formatContextLocker). The auto_bitrate handler can call into
	// NotifyAboutPacketSource asynchronously, racing with Close — without this
	// guard we'd dereference a nil FormatContext in NewStream below.
	if o.FormatContext == nil {
		logger.Debugf(ctx, "FormatContext is nil; skipping preallocation of output stream for input stream #%d", inputStreamIndex)
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
	case astiav.MediaTypeSubtitle:
		outputStream := &OutputStream{
			Stream:  o.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
		o.PreallocatedSubtitleStreams = append(o.PreallocatedSubtitleStreams, outputStream)
		o.OutputStreams[inputStreamIndex] = nil
	case astiav.MediaTypeData:
		outputStream := &OutputStream{
			Stream:  o.FormatContext.NewStream(nil),
			LastDTS: math.MinInt64,
		}
		o.PreallocatedDataStreams = append(o.PreallocatedDataStreams, outputStream)
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

	if o.headerSent && !muxerAllowsLateStreamAddition(o.outputFormatName) {
		// Once WriteHeader has run, the muxer's stream table is committed
		// for muxers that do not support runtime stream-table mutation.
		// Adding a stream now would crash av_interleaved_write_frame
		// with SIGFPE on the first write to that index. Surface the
		// typed error so the upstream forwarder can recreate the kernel.
		logger.Warnf(ctx, "input stream #%d not registered before muxer header was written; refusing to add (muxer=%s)", inputStream.Index(), o.outputFormatName)
		return nil, ErrLateStreamAddition{StreamIndex: inputStream.Index()}
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

// getOutputStreamFromPacket initializes an output stream directly from the
// packet's stream metadata, without requiring a full source format context.
// This is the lazy-init fallback for when WithOutputFormatContext does not
// invoke its callback (e.g. the upstream Retryable kernel has not connected
// yet).
func (o *Output) getOutputStreamFromPacket(
	ctx context.Context,
	inputSource packet.Source,
	inputStream *astiav.Stream,
) (*OutputStream, error) {
	streamIndex := inputStream.Index()
	if existing := o.OutputStreams[streamIndex]; existing != nil {
		return existing, nil
	}

	outputStream, err := o.initOutputStreamFor(ctx, inputSource, inputStream)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize output stream for input stream #%d: %w", streamIndex, err)
	}

	o.OutputStreams[streamIndex] = outputStream
	o.InputStreams[streamIndex] = OutputInputStream{
		Source: inputSource,
		Stream: inputStream,
	}

	if outputStream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
		o.waitingKeyFrames[streamIndex] = struct{}{}
		logger.Debugf(ctx, "len(waitingKeyFrames): increase -> %d (lazy init)", len(o.waitingKeyFrames))
	}

	return outputStream, nil
}

func (o *Output) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	_ chan<- packetorframe.OutputUnion,
) (_err error) {
	pkt, frame := input.Unwrap()
	switch {
	case pkt != nil:
		return o.sendPacket(ctx, *pkt)
	case frame != nil:
		return o.sendFrame(ctx, *frame)
	default:
		return types.ErrUnexpectedInputType{}
	}
}

func (o *Output) sendPacket(
	ctx context.Context,
	inputPkt packet.Input,
) (_err error) {
	pkt := inputPkt.Packet
	if pkt == nil {
		return fmt.Errorf("packet == nil")
	}
	logger.Tracef(ctx,
		"sendPacket (stream: %d:%s, pkt:%p, pos:%d, pts:%d, dts:%d, dur:%d, size: %d)",
		inputPkt.GetStreamIndex(), inputPkt.GetMediaType(), pkt, pkt.Pos(), pkt.Pts(), pkt.Dts(), pkt.Duration(), pkt.Size(),
	)
	defer func() {
		logger.Tracef(ctx, "/sendPacket (stream: %d:%s, pkt:%p): %v",
			inputPkt.GetStreamIndex(), inputPkt.GetMediaType(), pkt, _err)
	}()

	if pkt.Flags().Has(astiav.PacketFlagDiscard) {
		logger.Tracef(ctx, "the packet has a discard flag; discarding")
		return nil
	}

	var (
		outputStream *OutputStream
		err          error = ErrNoSourceFormatContext{
			StreamIndex: inputPkt.GetStreamIndex(),
		}
	)
	o.formatContextLocker.Do(ctx, func() {
		inputPkt.GetSource().WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
			outputStream, err = o.getOutputStream(ctx, inputPkt.GetSource(), inputPkt.GetStream(), fmtCtx)
		})
		if errors.As(err, &ErrNoSourceFormatContext{}) {
			outputStream = o.OutputStreams[inputPkt.GetStreamIndex()]
			if outputStream != nil {
				err = nil
			}
		}
		if errors.As(err, &ErrNoSourceFormatContext{}) {
			// The source's WithOutputFormatContext did not invoke the callback
			// (e.g. the upstream Retryable kernel is not ready yet). Fall back
			// to initializing the output stream directly from the packet's own
			// stream metadata so that we do not silently discard the packet.
			if inputStream := inputPkt.GetStream(); inputStream != nil && inputStream.CodecParameters() != nil &&
				inputStream.CodecParameters().CodecID() != astiav.CodecIDNone {
				if o.headerSent && !muxerAllowsLateStreamAddition(o.outputFormatName) {
					// Creating a new stream after WriteHeader is unsafe for
					// muxers that do not support runtime stream-table
					// mutation (see ErrLateStreamAddition docs). Don't
					// lazy-init; surface the error instead of producing a
					// SIGFPE later.
					logger.Warnf(ctx, "packet for stream #%d arrived after muxer header was written; refusing to lazy-init (muxer=%s)", inputStream.Index(), o.outputFormatName)
					err = ErrLateStreamAddition{StreamIndex: inputStream.Index()}
				} else {
					logger.Debugf(ctx, "source format context unavailable; lazily initializing output stream from packet stream #%d", inputStream.Index())
					var initErr error
					outputStream, initErr = o.getOutputStreamFromPacket(ctx, inputPkt.GetSource(), inputStream)
					if initErr != nil {
						logger.Warnf(ctx, "lazy output stream init failed: %v", initErr)
					} else {
						err = nil
					}
				}
			}
		}
	})
	if err != nil {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("unable to get output stream because the context is Done: %w", err)
		}
		if o.Config.IgnoreNoSourceFormatCtxErrors && errors.As(err, &ErrNoSourceFormatContext{}) {
			logger.Warnf(ctx, "ignoring error: %v", err)
			return nil
		}
		return fmt.Errorf("unable to get the output stream: %w", err)
	}
	assert(ctx, outputStream != nil)

	frameInfo := FrameInfoFromPacketInput(inputPkt)

	err = xsync.DoR1(ctx, &o.SenderLocker, func() error {
		return o.send(ctx, pkt, frameInfo, inputPkt.GetSource(), inputPkt.GetStream(), outputStream)
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

func (o *Output) sendFrame(
	context.Context,
	frame.Input,
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
	if o.sendingAllowed {
		return o.doWritePacket(ctx, pkt, frameInfo, source, inputStream, outputStream)
	}

	mediaType := inputStream.CodecParameters().MediaType()

	var expectedStreamsCount uint
	var expectedStreamsVideoCount uint
	var expectedStreamsAudioCount uint
	var expectedStreamsSubtitleCount uint
	var expectedStreamsDataCount uint
	source.WithOutputFormatContext(ctx, func(fmtCtx *astiav.FormatContext) {
		for _, inputStream := range fmtCtx.Streams() {
			switch inputStream.CodecParameters().MediaType() {
			case astiav.MediaTypeVideo:
				expectedStreamsVideoCount++
			case astiav.MediaTypeAudio:
				expectedStreamsAudioCount++
			case astiav.MediaTypeSubtitle:
				expectedStreamsSubtitleCount++
			case astiav.MediaTypeData:
				expectedStreamsDataCount++
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
		expectedStreamsSubtitleCount = max(expectedStreamsSubtitleCount, o.Config.WaitForOutputStreams.MinStreamsSubtitle)
		expectedStreamsDataCount = max(expectedStreamsDataCount, o.Config.WaitForOutputStreams.MinStreamsData)
	}

	activeStreamCount := xsync.DoR1(ctx, &o.formatContextLocker, func() uint {
		return uint(len(o.OutputStreams))
	})

	keyFrame := pkt.Flags().Has(astiav.PacketFlagKey)
	logger.Debugf(ctx, "isKeyFrame:%t, expectedStreamsCount:%d, expectedStreamsVideoCount:%d, expectedStreamsAudioCount:%d, expectedStreamsSubtitleCount:%d, expectedStreamsDataCount:%d, videoBeforeAudio:%t", keyFrame, expectedStreamsCount, expectedStreamsVideoCount, expectedStreamsAudioCount, expectedStreamsSubtitleCount, expectedStreamsDataCount, *o.Config.WaitForOutputStreams.VideoBeforeAudio)
	if !keyFrame && (revert0c55f85 || len(o.waitingKeyFrames) > 0) {
		if outputAcceptOnlyKeyFramesUntilStart {
			logger.Debugf(ctx, "not a key frame; skipping")
			return nil
		}
	}
	if *o.Config.WaitForOutputStreams.VideoBeforeAudio && expectedStreamsVideoCount > 0 {
		// Block non-video packets only until the first video packet is seen.
		// Previously this blocked audio permanently, which deadlocked with
		// WaitForOutputStreams requiring audio streams to exist.
		if mediaType == astiav.MediaTypeVideo {
			o.firstVideoPacketSeen = true
		} else if !o.firstVideoPacketSeen {
			logger.Debugf(ctx, "skipping a non-video (%s) packet to avoid MediaMTX from losing the video track", mediaType)
			return nil
		}
	}

	outputStreamIndex := outputStream.Index()
	_, waitingKeyFrame := o.waitingKeyFrames[outputStreamIndex]
	if waitingKeyFrame {
		delete(o.waitingKeyFrames, outputStreamIndex)
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
		// Arm the bounded WriteHeader-deferral deadline on the first
		// buffered packet. Without this, a producer that delivers only
		// one media type (e.g. video before the demuxer has parsed
		// audio) would block WriteHeader forever waiting for the
		// configured Min* counts.
		if o.pendingPacketsDeadline.IsZero() && o.Config.WaitForOutputStreams != nil &&
			o.Config.WaitForOutputStreams.Timeout > 0 {
			o.pendingPacketsDeadline = time.Now().Add(o.Config.WaitForOutputStreams.Timeout)
			logger.Debugf(ctx, "armed WaitForOutputStreams deadline: %s (timeout %s)",
				o.pendingPacketsDeadline, o.Config.WaitForOutputStreams.Timeout)
		}
	}
	var activeVideoStreamCount uint
	var activeAudioStreamCount uint
	var activeSubtitleStreamCount uint
	var activeDataStreamCount uint
	for _, stream := range o.OutputStreams {
		if stream == nil {
			continue
		}
		switch stream.CodecParameters().MediaType() {
		case astiav.MediaTypeVideo:
			activeVideoStreamCount++
		case astiav.MediaTypeAudio:
			activeAudioStreamCount++
		case astiav.MediaTypeSubtitle:
			activeSubtitleStreamCount++
		case astiav.MediaTypeData:
			activeDataStreamCount++
		}
	}
	// timeoutExpired tells us the bounded WriteHeader-deferral window
	// has elapsed and we must commit with whatever streams are present.
	// It is only meaningful while o.pendingPacketsDeadline is armed.
	timeoutExpired := !o.pendingPacketsDeadline.IsZero() &&
		time.Now().After(o.pendingPacketsDeadline)

	if o.Config.WaitForOutputStreams != nil && !timeoutExpired {
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
		if activeSubtitleStreamCount < expectedStreamsSubtitleCount {
			logger.Tracef(ctx, "not starting sending the packets, yet: subtitle streams: %d < %d; %s", activeSubtitleStreamCount, expectedStreamsSubtitleCount, mediaType)
			return nil
		}
		if activeDataStreamCount < expectedStreamsDataCount {
			logger.Tracef(ctx, "not starting sending the packets, yet: data streams: %d < %d; %s", activeDataStreamCount, expectedStreamsDataCount, mediaType)
			return nil
		}
	}
	if timeoutExpired {
		logger.Warnf(
			ctx,
			"WaitForOutputStreams deadline expired (timeout %s); committing WriteHeader with current streams: *:%d/%d, v:%d/%d, a:%d/%d, s:%d/%d, d:%d/%d",
			o.Config.WaitForOutputStreams.Timeout,
			activeStreamCount, expectedStreamsCount,
			activeVideoStreamCount, expectedStreamsVideoCount,
			activeAudioStreamCount, expectedStreamsAudioCount,
			activeSubtitleStreamCount, expectedStreamsSubtitleCount,
			activeDataStreamCount, expectedStreamsDataCount,
		)
	}
	if outputWaitForKeyFrames && len(o.waitingKeyFrames) != 0 {
		logger.Tracef(ctx, "not starting sending the packets, yet: %d != 0; %s", len(o.waitingKeyFrames), mediaType)
		return nil
	}
	o.sendingAllowed = true

	var err error
	if outputWriteHeaders && !o.headerSent {
		o.formatContextLocker.Do(ctx, func() {
			if o.FormatContext == nil {
				err = io.EOF
				return
			}
			logger.Debugf(
				ctx,
				"writing the header; streams: *:%d/%d, a:%d/%d, v:%d/%d, s:%d/%d, d:%d/%d; len(waitingKeyFrames): %d",
				activeStreamCount, expectedStreamsCount,
				activeAudioStreamCount, expectedStreamsAudioCount,
				activeVideoStreamCount, expectedStreamsVideoCount,
				activeSubtitleStreamCount, expectedStreamsSubtitleCount,
				activeDataStreamCount, expectedStreamsDataCount,
				len(o.waitingKeyFrames),
			)
			err = o.FormatContext.WriteHeader(o.Dictionary)
			o.headerSent = true
			// Flush the IO buffer immediately so that RTMP servers
			// (particularly AVD's proxied listener) receive the header
			// without waiting for the buffer to fill. Without this,
			// slow producers (e.g. phone h264_mediacodec at 30fps)
			// may never fill the default 32KB AVIO buffer, causing
			// the receiving side's avformat_find_stream_info to block
			// indefinitely.
			if err == nil && o.ioContext != nil {
				o.ioContext.Flush()
			}
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
		// pendingPkt.RescaleTs(pendingPkt.InputStream.TimeBase(), inputStream.TimeBase())
		outputStream := o.OutputStreams[pendingPkt.InputStream.Index()]
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

func (o *Output) SetSendingAllowed(
	allowed bool,
) {
	o.sendingAllowed = allowed
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

	// pkt.SetPos(-1) // <- TODO: should this happen? why?
	pkt.RescaleTs(inputStream.TimeBase(), outputStream.TimeBase())
	isNoDTS := pkt.Dts() == consts.NoPTSValue
	isNoPTS := pkt.Pts() == consts.NoPTSValue
	if isNoDTS && !isNoPTS {
		logger.Tracef(ctx, "DTS is missing but PTS is set (%d), setting DTS = PTS", pkt.Pts())
		pkt.SetDts(pkt.Pts())
		isNoDTS = false
	}
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
			"received a DTS from the stream's past or has invalid value (%v), ignoring the packet from stream #%d: %d < %d (delta:%d, source:%T, input_tb:%v, output_tb:%v, pts:%d)",
			outputStream.CodecParameters().MediaType(),
			outputStream.Index(),
			pkt.Dts(),
			outputStream.LastDTS,
			outputStream.LastDTS-pkt.Dts(),
			source,
			inputStream.TimeBase(),
			outputStream.TimeBase(),
			pkt.Pts(),
		)
		return nil
	}
	o.SequentialInvalidPacketsCount = 0

	pkt.SetStreamIndex(outputStream.Index())
	if o.Filter != nil && !o.Filter.Match(ctx, packet.BuildInput(pkt, packet.BuildStreamInfo(outputStream.Stream, source, nil))) {
		return nil
	}

	pos, dts, pts, dur := pkt.Pos(), pkt.Dts(), pkt.Pts(), pkt.Duration()
	isKey := pkt.Flags().Has(astiav.PacketFlagKey)

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
			"writing packet with pos:%v (is_key:%v, pts:%v(%v), dts:%v, dur:%v, dts_prev:%v; is_key:%v; source: %T) for %s stream %d (res: %s, sample_rate: %v, channels: %v, time_base: %v) with flags 0x%016X and data length %d",
			pos, isKey, pts, ptsDuration, dts, dur, outputStream.LastDTS, pkt.Flags().Has(astiav.PacketFlagKey), source,
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
		if outputStream.TimeBase().Num() == 0 || outputStream.TimeBase().Den() == 0 {
			err = fmt.Errorf("time_base is invalid (%v) for stream %d", outputStream.TimeBase(), pkt.StreamIndex())
			return
		}
		if pkt.StreamIndex() >= int(o.FormatContext.NbStreams()) {
			err = fmt.Errorf("stream index %d is out of bounds (nb_streams: %d)", pkt.StreamIndex(), o.FormatContext.NbStreams())
			return
		}
		err = o.FormatContext.WriteInterleavedFrame(pkt)
		if err != nil {
			return
		}
		// Flush after every packet for real-time streaming. Without
		// this, data stays in the AVIO 32KB buffer and slow producers
		// (phone h264_mediacodec at 30fps) never fill it, causing
		// the receiving side to starve.
		if o.ioContext != nil {
			o.ioContext.Flush()
		}
		// Update high-water-marks under the same lock so concurrent
		// readers (GetLatestSentDTS) observe a consistent snapshot.
		// Previously these assignments lived outside the lock — a
		// pre-existing race fixed here.
		outputStream.LastDTS = dts
		o.LatestSentPTS = ptsDuration
		o.LatestSentDTS = dtsDuration
	})
	if err != nil {
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
				idx := stream.Index()
				if o.headerSent && !muxerAllowsLateStreamAddition(o.outputFormatName) {
					if _, ok := o.OutputStreams[idx]; !ok {
						// The muxer has already written its header with a fixed
						// stream table and does not support runtime stream-
						// table mutation; adding a new stream now would crash
						// av_interleaved_write_frame on SIGFPE. Surface a typed
						// error so upstream can decide to recreate this kernel.
						logger.Warnf(ctx, "stream #%d appeared after the muxer header was written; refusing to preallocate (muxer=%s)", idx, o.outputFormatName)
						errs = append(errs, ErrLateStreamAddition{StreamIndex: idx})
						continue
					}
				}
				logger.Debugf(ctx, "making sure stream #%d is initialized", idx)
				err := o.preallocateOutputStream(ctx, stream)
				if err != nil {
					errs = append(errs, fmt.Errorf("unable to preallocate an output stream for input stream %d from source %s: %w", idx, source, err))
				}
			}
		})
	})
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// GetInternalQueueSize returns the size of internal queues used by the output.
func (o *Output) GetInternalQueueSize(
	ctx context.Context,
) (_ret map[string]uint64) {
	defer func() { logger.Tracef(ctx, "GetInternalQueueSize: %#+v", _ret) }()
	defer func() {
		if rec := recover(); rec != nil {
			logger.Debugf(ctx, "panic recovered in %s in GetInternalQueueSize: %v\n%s", o, rec, debug.Stack())
		}
	}()
	if o.proxy != nil {
		logger.Debugf(ctx, "getting the internal queue size from the proxy is not implemented, yet")
		return nil
	}

	return o.netConn.GetInternalQueueSize(ctx)
}

var _ kerneltypes.GetOldestDTSInTheQueuer = (*Output)(nil)

func (o *Output) GetOldestDTSInTheQueue(
	ctx context.Context,
) (_ret time.Duration, _err error) {
	return o.netConn.GetOldestDTSInTheQueue(ctx, o.URL, o.outTSs.GetAll())
}
