// input.go implements the Input kernel for reading media streams from URLs or files.

package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	packetcondition "github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/ts"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/avpipeline/urltools"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/unsafetools"
	"github.com/xaionaro-go/xsync"
)

const (
	inputDefaultWidth  = 1920
	inputDefaultHeight = 1080
	inputDefaultFPS    = 30
)

type InputConfig = kerneltypes.InputConfig

type Input struct {
	*closuresignaler.ClosureSignaler
	openFinished chan struct{}
	openError    error
	isClosing    atomic.Bool

	*astiav.FormatContext
	*astiav.IOInterrupter
	*astiav.Dictionary

	ID            InputID
	URL           string
	URLParsed     *url.URL
	DefaultWidth  int
	DefaultHeight int
	DefaultFPS    astiav.Rational

	AutoClose          bool
	ForceRealTime      bool
	ForceStartPTS      int64
	ForceStartDTS      int64
	DisplayRotation    *float64
	AutoRotate         bool
	OnPreClose         func(context.Context, *Input) error
	IgnoreIncorrectDTS bool
	IgnoreZeroDuration bool

	PipelineSideData globaltypes.PipelineSideData
	liveRotation     *globaltypes.DisplayRotation

	SyncStreamIndex atomic.Int64
	ClockCalculator *ts.ClockCalculator
	// PTSShifts and DTSShifts hold the per-stream shift applied to raw
	// PTS/DTS when ForceStartPTS/ForceStartDTS are configured. Each
	// stream's shift is computed from the first packet of THAT stream
	// as (targetPTS - firstRawPTS) so every subsequent packet from the
	// same stream remains non-negative relative to the target — a
	// global shift computed from the first stream would overshift
	// later-arriving streams with a lower raw timestamp and wrap them
	// past zero.
	PTSShifts xsync.Map[int, int64]
	DTSShifts xsync.Map[int, int64]

	OutputFilters []packetcondition.Condition

	// generateMu serializes WaitGroup.Add(1) in Generate against
	// the isClosing-flag-set + WaitGroup.Wait() pair in Close. Without
	// this synchronization, Generate's Add(1) at counter==0 races with
	// Close's Wait() per Go's WaitGroup contract ("calls with a
	// positive delta that occur when the counter is zero must happen
	// before a Wait"), producing -race failures whenever Pause is
	// triggered while a Generate goroutine has been spawned but has
	// not yet reached its Add(1) line. Close grabs this mutex around
	// closing the close-chan + setting isClosing, then releases before
	// Wait — Generate observes either: (a) isClosing=true, returns
	// io.EOF without calling Add, or (b) isClosing=false, the Add
	// happens-before any subsequent Close's Wait acquires the mutex.
	generateMu sync.Mutex
	WaitGroup  sync.WaitGroup

	netConn
}

var (
	_ Abstract             = (*Input)(nil)
	_ packet.Source        = (*Input)(nil)
	_ WithNetworkConner    = (*Input)(nil)
	_ WithRawNetworkConner = (*Input)(nil)
)

var nextInputID atomic.Uint64

// logOpenFailure emits the by-design open-failure log lines (format-from-URL
// detection, AsyncOpen failures) at the right severity for the situation:
//
//   - quiet=true: demote to Debug. Caller has set
//     InputConfig.QuietOnOpenFailure for an Input whose absence is a
//     normal steady state (e.g. an upstream rtmp publisher not yet
//     connected, an empty fallback priority slot). The default (flag
//     unset) path retains the legacy levels below.
//   - quiet=false, isError=true: Errorf — the original semantic of
//     the "unable to open: ..." site (a doOpen failure).
//   - quiet=false, isError=false: Warnf — the original semantic of
//     the "attempting to detect input format from URL: ..." site
//     (a missing 'f' option that we are about to guess).
//
// Centralizing the gate avoids drift between the two call sites.
func logOpenFailure(
	ctx context.Context,
	quiet bool,
	isError bool,
	format string,
	args ...any,
) {
	switch {
	case quiet:
		logger.Debugf(ctx, format, args...)
	case isError:
		logger.Errorf(ctx, format, args...)
	default:
		logger.Warnf(ctx, format, args...)
	}
}

func NewInputFromURL(
	ctx context.Context,
	urlString string,
	authKey secret.String,
	cfg InputConfig,
) (*Input, error) {
	urlParsed, err := url.Parse(urlString)
	if err == nil && strings.HasPrefix(urlParsed.Scheme, "rtmp") {
		logger.Debugf(ctx, "URL: %#+v", urlParsed)
		urlString += "/"
	}
	i := &Input{
		ID:            InputID(nextInputID.Add(1)),
		URL:           urlString,
		URLParsed:     urlParsed,
		DefaultWidth:  inputDefaultWidth,
		DefaultHeight: inputDefaultHeight,

		openFinished:    make(chan struct{}),
		ClosureSignaler: closuresignaler.New(),

		AutoClose:          cfg.AutoClose,
		IgnoreIncorrectDTS: cfg.IgnoreIncorrectDTS,
		IgnoreZeroDuration: cfg.IgnoreZeroDuration,
		DisplayRotation:    cfg.DisplayRotation,
		AutoRotate:         cfg.AutoRotate != nil && *cfg.AutoRotate,
	}
	if cfg.OnPreClose != nil {
		i.OnPreClose = func(ctx context.Context, i *Input) error {
			return cfg.OnPreClose.FireHook(ctx, i)
		}
	}
	i.SyncStreamIndex.Store(math.MinInt64)
	defaultFPS := float64(inputDefaultFPS)

	var formatName string
	if len(cfg.CustomOptions) > 0 {
		i.Dictionary = astiav.NewDictionary()
		setFinalizerFree(ctx, i.Dictionary)
		for _, opt := range cfg.CustomOptions {
			switch opt.Key {
			case "f":
				formatName = opt.Value
				logger.Debugf(ctx, "overriding input format to '%s'", opt.Value)
			case "video_size":
				logger.Debugf(ctx, "setting input size to '%s'", opt.Value)
				var w, h int
				_, err := fmt.Sscanf(opt.Value, "%dx%d", &w, &h)
				if err != nil {
					return nil, fmt.Errorf("unable to parse video_size '%s': %w", opt.Value, err)
				}
				i.DefaultWidth = w
				i.DefaultHeight = h
				i.Set("video_size", opt.Value, 0)
			case "framerate":
				logger.Debugf(ctx, "setting input framerate to '%s'", opt.Value)
				var r float64
				_, err := fmt.Sscanf(opt.Value, "%f", &r)
				if err != nil {
					return nil, fmt.Errorf("unable to parse input framerate '%s': %w", opt.Value, err)
				}
				defaultFPS = r
				i.Set("framerate", opt.Value, 0)
			case "display_rotation":
				logger.Debugf(ctx, "setting display rotation to '%s'", opt.Value)
				var r float64
				_, err := fmt.Sscanf(opt.Value, "%f", &r)
				if err != nil {
					return nil, fmt.Errorf("unable to parse display_rotation '%s': %w", opt.Value, err)
				}
				i.DisplayRotation = &r
			case "autorotate":
				i.AutoRotate = true
			case "noautorotate":
				i.AutoRotate = false
			default:
				logger.Debugf(ctx, "input.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
				i.Set(opt.Key, opt.Value, 0)
			}
		}
	}

	// Set PipelineSideData AFTER processing custom options, because
	// custom options may override AutoRotate (via "autorotate"/"noautorotate").
	i.PipelineSideData = append(i.PipelineSideData, globaltypes.AutoRotate(i.AutoRotate))

	// Create a live-updatable rotation value for mid-stream rotation changes
	// (e.g., smartphone orientation changes). NaN means "use rotation from
	// codec parameters" (the default).
	initialRotation := math.NaN()
	if i.DisplayRotation != nil {
		initialRotation = *i.DisplayRotation
	}
	i.liveRotation = globaltypes.NewDisplayRotation(initialRotation)
	i.PipelineSideData = append(i.PipelineSideData, i.liveRotation)

	if formatName == "" {
		if urlParsed != nil {
			logOpenFailure(ctx, cfg.QuietOnOpenFailure, false,
				"attempting to detect input format from URL: %s", urlString)
			formatName = urltools.FormatNameFromURL(urlParsed)
		}
	}

	defaultFPSRational := globaltypes.RationalFromApproxFloat64(defaultFPS)
	i.DefaultFPS = astiav.NewRational(defaultFPSRational.Num, defaultFPSRational.Den)

	var inputFormat *astiav.InputFormat
	if formatName != "" {
		inputFormat = astiav.FindInputFormat(formatName)
		// astiav.FindInputFormat returns nil when the libav demuxer
		// table has no entry for `formatName`. Calling .Name() on a
		// nil receiver panics via observability (cgo'd nil deref).
		// The .Name() call here is a debug log of what we found, so
		// gate it behind the nil check that already guards real use.
		if inputFormat != nil {
			logger.Debugf(ctx, "using format '%s'", inputFormat.Name())
		}
	}

	if inputFormat != nil {
		switch inputFormat.Name() {
		case "rtsp":
			if i.Get("rtsp_transport", nil, 0) == nil {
				logger.Debugf(ctx, "setting rtsp_transport to 'tcp'")
				i.Set("rtsp_transport", "tcp", 0)
			}
		}
	} else if formatName != "" {
		// Fail fast with a typed error so callers can treat
		// "format not found" distinct from a successful open. The
		// previous code silently logged and continued, leading to
		// nil-deref further down.
		return nil, fmt.Errorf("unable to find input format by name %q (option 'f'); format is not registered with libav", formatName)
	}

	i.FormatContext = astiav.AllocFormatContext()
	if i.FormatContext == nil {
		// TODO: is there a way to extract the actual error code or something?
		return nil, fmt.Errorf("unable to allocate a format context")
	}

	if cfg.AsyncOpen {
		i.WaitGroup.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			var err error
			func() {
				defer i.WaitGroup.Done()
				err = i.doOpen(ctx, urlString, authKey, inputFormat, cfg)
			}()
			if err != nil {
				logOpenFailure(ctx, cfg.QuietOnOpenFailure, true,
					"unable to open: %v", err)
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
) (_err error) {
	logger.Debugf(ctx, "doOpen(%q, <HIDDEN>, %q, %+#v)", urlString, inputFormat, cfg)
	defer func() { logger.Debugf(ctx, "/doOpen(%q, <HIDDEN>, %q, %+#v): %v", urlString, inputFormat, cfg, _err) }()
	defer func() {
		i.openError = _err
		close(i.openFinished)
	}()
	urlWithSecret := urlString
	if authKey.Get() != "" {
		urlWithSecret += authKey.Get()
	}

	i.IOInterrupter = astiav.NewIOInterrupter()
	setFinalizerFree(ctx, i.IOInterrupter)
	i.SetIOInterrupter(i.IOInterrupter)

	if err := ctx.Err(); err != nil {
		i.FormatContext.Free()
		i.FormatContext = nil
		return fmt.Errorf("context cancelled before opening input: %w", err)
	}

	// interruptable OpenInput
	openInputDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			logger.Debugf(ctx, "context cancelled during OpenInput, interrupting IO")
			i.Interrupt()
		case <-openInputDone:
		}
	}()
	err := i.OpenInput(urlWithSecret, inputFormat, i.Dictionary)
	close(openInputDone)

	if err != nil {
		i.FormatContext.Free()
		i.FormatContext = nil
		if authKey.Get() != "" {
			return fmt.Errorf("unable to open input by URL '%s/<HIDDEN>' (format: %q): %w", urlString, inputFormat, err)
		} else {
			return fmt.Errorf("unable to open input by URL %q (format: %q): %w", urlString, inputFormat, err)
		}
	}
	setFinalizer(ctx, i, func(i *Input) {
		i.CloseInput()
		i.FormatContext.Free()
	})

	formatName := i.FormatContext.InputFormat().Name()
	logger.Debugf(ctx, "resulting format: %q", formatName)

	if cfg.ForceRealTime != nil {
		i.ForceRealTime = *cfg.ForceRealTime
	} else {
		i.ForceRealTime = false
	}
	if cfg.ForceStartPTS != nil {
		i.ForceStartPTS = *cfg.ForceStartPTS
	} else {
		i.ForceStartPTS = globaltypes.PTSKeep
	}
	if cfg.ForceStartDTS != nil {
		i.ForceStartDTS = *cfg.ForceStartDTS
	} else {
		i.ForceStartDTS = globaltypes.PTSKeep
	}
	switch formatName {
	case "pulse", "android_camera":
		// Anchor the first emitted PTS/DTS of every stream to the
		// process-wide shared monotonic epoch (kernel.PTSEpochNanos).
		// The previous behaviour seeded every live-capture kernel at
		// PTS=0 independently, so when a slow-starting video kernel
		// (Android Camera2: 200-2000ms cold start) was added next to
		// a fast-starting audio kernel (AAudio: 10-50ms), both first
		// frames were labelled as simultaneous and the encoder/muxer
		// produced a constant audio-leads-video desync of the cold-
		// start delta. With PTSEpoch the camera's first frame lands
		// at "now - epoch" in stream timebase, the mic's first frame
		// at the same delta in its own timebase, and the cold-start
		// gap turns into a benign DTS gap that the muxer handles
		// natively. See /tmp/av_sync_camera_mic_addinput.md option (b).
		if cfg.ForceStartPTS == nil {
			i.ForceStartPTS = globaltypes.PTSEpoch
		}
		if cfg.ForceStartDTS == nil {
			i.ForceStartDTS = globaltypes.PTSEpoch
		}
	default:
		if cfg.ForceRealTime == nil {
			if urltools.IsFileURL(ctx, urlString) {
				i.ForceRealTime = true
			}
		}
	}
	logger.Debugf(ctx, "ForceRealTime=%t ForceStartPTS=%d ForceStartDTS=%d", i.ForceRealTime, i.ForceStartPTS, i.ForceStartDTS)

	i.initNetworkConn(ctx)

	if cfg.RecvBufferSize != 0 {
		if err := i.SetRecvBufferSize(ctx, cfg.RecvBufferSize); err != nil {
			return fmt.Errorf("unable to set the recv buffer size to %d: %w", cfg.RecvBufferSize, err)
		}
	}

	if err := i.FindStreamInfo(nil); err != nil {
		return fmt.Errorf("unable to get stream info: %w", err)
	}

	for _, stream := range i.Streams() {
		logger.Debugf(ctx, "input stream #%d: %#+v", stream.Index(), spew.Sdump(unsafetools.FieldByNameInValue(reflect.ValueOf(stream.CodecParameters()), "c").Elem().Elem().Interface()))
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			if i.DisplayRotation != nil {
				dm := astiav.NewDisplayMatrixFromRotation(*i.DisplayRotation)
				cp := stream.CodecParameters()
				if cp == nil {
					return fmt.Errorf("stream #%d has no codec parameters to set display rotation", stream.Index())
				}
				if err := cp.SideData().DisplayMatrix().Add(dm); err != nil {
					return fmt.Errorf("unable to add display matrix to stream #%d codec parameters: %w", stream.Index(), err)
				}
				logger.Infof(ctx, "set display rotation to %f for stream #%d via codec parameters side data", *i.DisplayRotation, stream.Index())
			}

			// FFmpeg's device demuxers (v4l2, android_camera) set avg_frame_rate
			// and r_frame_rate on the stream but NOT codecpar->framerate. The
			// downstream pipeline (decoder → encoder) reads framerate from codec
			// parameters, so we must propagate it here. Generic libavformat code
			// also computes packet duration from avg_frame_rate, so packets arrive
			// with duration already set — which means the framerate-guessing code
			// in the packet loop never triggers.
			if stream.CodecParameters().FrameRate().Num() == 0 {
				if fps := stream.AvgFrameRate(); fps.Num() > 0 && fps.Den() > 0 {
					logger.Infof(ctx, "stream #%d: propagating avg_frame_rate %v to codec parameters framerate", stream.Index(), fps)
					stream.CodecParameters().SetFrameRate(fps)
				}
			}

			// Fallback: if the demuxer didn't set avg_frame_rate either (or
			// FindStreamInfo couldn't determine it), use the configured input
			// framerate option (DefaultFPS).
			if i.DefaultFPS.Num() != 0 {
				fps := i.DefaultFPS
				if stream.CodecParameters().FrameRate().Num() == 0 {
					logger.Infof(ctx, "stream #%d: codec parameters framerate is still 0; setting to %v from input options", stream.Index(), fps)
					stream.CodecParameters().SetFrameRate(fps)
				}
				if stream.AvgFrameRate().Num() == 0 {
					logger.Infof(ctx, "stream #%d: avg_frame_rate is 0; setting to %v from input options", stream.Index(), fps)
					stream.SetAvgFrameRate(fps)
				}
				if stream.RFrameRate().Num() == 0 {
					logger.Infof(ctx, "stream #%d: r_frame_rate is 0; setting to %v from input options", stream.Index(), fps)
					stream.SetRFrameRate(fps)
				}
			}
		}
	}

	if cfg.OnPostOpen != nil {
		cfg.OnPostOpen.FireHook(ctx, i)
	}

	return nil
}

func (i *Input) FormatName() string {
	if i == nil || i.FormatContext == nil {
		return ""
	}
	f := i.InputFormat()
	if f == nil {
		return ""
	}
	return f.Name()
}

func (i *Input) initNetworkConn(ctx context.Context) {
	if i.URLParsed == nil {
		logger.Debugf(ctx, "skipping network connection init: URLParsed == nil")
		return
	}

	i.Init(ctx, i.FormatContext)
}

func (i *Input) Close(
	ctx context.Context,
) (_err error) {
	if i == nil {
		return nil
	}
	if i.isClosing.Swap(true) {
		return fmt.Errorf("already closed or closing")
	}
	f, l := getCaller()
	logger.Debugf(ctx, "Close[%s]: called from %s:%d", i, f, l)
	defer func() { logger.Debugf(ctx, "/Close[%s]: %v", i, _err) }()

	logger.Debugf(ctx, "interrupting IO before close")
	i.Interrupt()

	select {
	case <-i.openFinished:
		logger.Debugf(ctx, "doOpen completed (%v), proceeding with close", i.openError)
	default:
		logger.Debugf(ctx, "doOpen not completed yet, waiting for it to finish")
	}

	// Order matters: close the close-chan under generateMu so any
	// Generate goroutine that has not yet entered its critical
	// section observes isClosing=true (set above) and skips Add(1).
	// Generate goroutines already past their Add(1) are reflected in
	// WaitGroup and will be reaped by Wait() below.
	i.generateMu.Lock()
	i.ClosureSignaler.Close(ctx)
	i.generateMu.Unlock()
	i.WaitGroup.Wait()

	var errs []error
	if !i.AutoClose { // it means it won't be closed automatically, thus we should close it here, since this was a manual Close()
		if fn := i.OnPreClose; fn != nil {
			if err := fn(ctx, i); err != nil {
				errs = append(errs, fmt.Errorf("input OnPreClose error: %w", err))
			}
		}
		if i.FormatContext != nil {
			i.CloseInput()
		}
	}
	if err := i.netConn.Close(ctx); err != nil {
		errs = append(errs, fmt.Errorf("unable to close network connection: %w", err))
	}
	return errors.Join(errs...)
}

// SetDisplayRotation atomically updates the rotation angle used by the decoder.
// This enables mid-stream rotation changes, e.g. when a smartphone's orientation
// changes during a live stream. The change takes effect on the next decoded frame.
func (i *Input) SetDisplayRotation(
	ctx context.Context,
	rotation float64,
) {
	logger.Debugf(ctx, "SetDisplayRotation(%v)", rotation)
	i.liveRotation.Store(rotation)
}

func (i *Input) readIntoPacket(
	_ context.Context,
	packet *astiav.Packet,
) error {
	err := i.ReadFrame(packet)
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

func (i *Input) autoDetectSyncStreamIndexIfNeeded(
	ctx context.Context,
	outPkt *packet.Output,
) bool {
	if i.SyncStreamIndex.Load() >= 0 {
		return true
	}

	switch outPkt.GetCodecParameters().MediaType() {
	case astiav.MediaTypeVideo, astiav.MediaTypeAudio:
		if i.SyncStreamIndex.CompareAndSwap(math.MinInt64, int64(outPkt.GetStreamIndex())) {
			logger.Debugf(ctx, "auto-detected sync stream index: %d (%v)", outPkt.GetStreamIndex(), outPkt.GetCodecParameters().MediaType())
		}
		return i.SyncStreamIndex.Load() >= 0
	default:
		return false
	}
}

func (i *Input) slowdownIfNeeded(
	ctx context.Context,
	outPkt *packet.Output,
) {
	if !i.ForceRealTime {
		return
	}

	if !i.autoDetectSyncStreamIndexIfNeeded(ctx, outPkt) {
		logger.Debugf(ctx, "unable to auto-detect sync stream index, skipping slowdown")
		return
	}

	if int64(outPkt.GetStreamIndex()) != i.SyncStreamIndex.Load() {
		return
	}

	timeBase := outPkt.GetTimeBase()
	if timeBase.Num() == 0 || timeBase.Den() == 0 {
		return
	}

	pts := outPkt.Pts()
	if pts == astiav.NoPtsValue {
		return
	}
	// No general lower bound on PTS exists: negative PTS values are
	// legitimate (B-frame reordering in h264 mp4, live RTMP, certain camera
	// captures emit negative values at stream start) and ClockCalculator
	// anchors StartTS on the first observed PTS regardless of sign. We only
	// sanity-check that PTS is not garbage — anything more than a year
	// before zero in the stream's timebase is almost certainly uninitialized
	// memory, a near-miss of the NoPtsValue sentinel (INT64_MIN), or
	// demuxer corruption, none of which legitimate B-frame reordering ever
	// produces (which stays within seconds, not years).
	const sanityYears = 1
	sanityBound := int64(sanityYears) * 365 * 24 * 3600 *
		int64(timeBase.Den()) / int64(timeBase.Num())
	assert(ctx, pts > -sanityBound, fmt.Sprintf(
		"PTS %d is more than %d year(s) before zero in timebase %d/%d — likely garbage",
		pts, sanityYears, timeBase.Num(), timeBase.Den(),
	))

	if i.ClockCalculator == nil {
		i.ClockCalculator = ts.NewClockCalculator(globaltypes.Rational{Num: timeBase.Num(), Den: timeBase.Den()})
	}

	sleepDuration := i.ClockCalculator.Until(ctx, pts)
	if sleepDuration <= 0 {
		return
	}

	const maxSleep = 10 * time.Second
	if sleepDuration > maxSleep {
		logger.Errorf(ctx, "sleepSeconds too large (%s), capping to %s: %s", sleepDuration, maxSleep, i.ClockCalculator)
		sleepDuration = maxSleep
	}

	logger.Tracef(ctx, "slowing down input by sleeping for %s (%s)", sleepDuration, i.ClockCalculator)
	timer := time.NewTimer(sleepDuration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-i.CloseChan():
		return
	case <-timer.C:
	}
}

func (i *Input) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	logger.Debugf(ctx, "Generate")
	defer func() { logger.Debugf(ctx, "/Generate: %v", _err) }()
	// Synchronize Add(1) with Close's Wait() (see generateMu doc on
	// the field). If Close has already started, return io.EOF
	// immediately so Wait sees a steady WaitGroup counter.
	i.generateMu.Lock()
	if i.isClosing.Load() {
		i.generateMu.Unlock()
		return io.EOF
	}
	i.WaitGroup.Add(1)
	i.generateMu.Unlock()
	defer i.WaitGroup.Done()

	defer func() {
		i.ClosureSignaler.Close(ctx)
	}()

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-i.CloseChan():
		return io.EOF
	case <-i.openFinished:
	}
	if i.openError != nil {
		return i.openError
	}

	if i.AutoClose {
		defer func() {
			if fn := i.OnPreClose; fn != nil {
				if err := fn(ctx, i); err != nil {
					if _err == nil {
						_err = fmt.Errorf("input OnPreClose error: %w", err)
					} else {
						logger.Errorf(ctx, "input OnPreClose error: %v", err)
					}
				}
			}
			i.CloseInput()
		}()
	}

	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
		logger.Debugf(ctx, "interrupting IO")
		i.Interrupt()
	})

	lastDuration := map[int]int64{}
	sendPkt := func(outPkt *packet.Output) error {
		for _, filter := range i.OutputFilters {
			if !filter.Match(ctx, (packet.Input)(*outPkt)) {
				logger.Tracef(ctx, "packet filtered out by %s: stream:%d pos:%d pts:%d", filter, outPkt.GetStreamIndex(), outPkt.Pos, outPkt.GetPTS())
				return nil
			}
		}

		i.slowdownIfNeeded(ctx, outPkt)

		codecParams := outPkt.GetStream().CodecParameters()
		switch codecParams.MediaType() {
		case astiav.MediaTypeVideo:
			if outPkt.GetCodecParameters().Width() != 0 && outPkt.GetCodecParameters().Width() != codecParams.Width() {
				logger.Debugf(ctx, "correcting packet width from %d to %d", outPkt.GetCodecParameters().Width(), codecParams.Width())
				codecParams.SetWidth(outPkt.GetCodecParameters().Width())
			}
			if codecParams.Width() == 0 {
				logger.Warnf(ctx, "width is zero, defaulting to %d", i.DefaultWidth)
				codecParams.SetWidth(i.DefaultWidth)
				outPkt.GetCodecParameters().SetWidth(i.DefaultWidth)
			}
			if outPkt.GetCodecParameters().Height() != 0 && outPkt.GetCodecParameters().Height() != codecParams.Height() {
				logger.Debugf(ctx, "correcting packet height from %d to %d", outPkt.GetCodecParameters().Height(), codecParams.Height())
				codecParams.SetHeight(outPkt.GetCodecParameters().Height())
			}
			if codecParams.Height() == 0 {
				logger.Warnf(ctx, "height is zero, defaulting to %d", i.DefaultHeight)
				codecParams.SetHeight(i.DefaultHeight)
				outPkt.GetCodecParameters().SetHeight(i.DefaultHeight)
			}
		}
		logger.Tracef(
			ctx,
			"sending a %s packet (stream:%d, pos:%d, pts:%d, dts:%d, dur:%d, time_base:%v, isKey:%t), dataLen:%d, res:%dx%d",
			codecParams.MediaType(),
			outPkt.GetStreamIndex(),
			outPkt.Pos, outPkt.GetPTS(), outPkt.GetDTS(), outPkt.GetDuration(),
			outPkt.GetTimeBase(),
			outPkt.Flags().Has(astiav.PacketFlagKey),
			len(outPkt.Data()),
			codecParams.Width(), codecParams.Height(),
		)

		lastDuration[outPkt.GetStreamIndex()] = outPkt.GetDuration()
		select {
		case outputCh <- packetorframe.OutputUnion{Packet: outPkt}:
		case <-ctx.Done():
			return ctx.Err()
		case <-i.CloseChan():
			return io.EOF
		}
		return nil
	}

	prevPkts := map[int]*packet.Output{}
	defer func() {
		for _, pkt := range prevPkts {
			if err := sendPkt(pkt); err != nil {
				logger.Warnf(ctx, "unable to send remaining packet during cleanup: %v", err)
			}
		}
	}()

	wantPTSShift := i.ForceStartPTS != globaltypes.PTSKeep
	wantDTSShift := i.ForceStartDTS != globaltypes.PTSKeep

	// applyPerStreamShift computes the shift for a stream on that
	// stream's FIRST packet and caches it; every subsequent packet from
	// the same stream is shifted by the cached value. A global shift
	// computed from the first-read stream would poison later-arriving
	// streams whose raw DTS begins below the first stream's, since
	// target - firstStreamDTS + laterStreamDTS can easily go negative
	// and wrap the FLV muxer's uint32 DTS field to ~4.29e9.
	//
	// When the configured target is the PTSEpoch sentinel, the target
	// is resolved on each stream's first packet to "now since shared
	// monotonic epoch" expressed in that stream's timebase — anchoring
	// independently-opened media kernels (camera, microphone, ...) to
	// a common wall-clock origin instead of each restarting at PTS=0.
	applyPerStreamShift := func(
		ctx context.Context,
		pkt *astiav.Packet,
		streamIndex int,
		stream *astiav.Stream,
	) {
		// When PTS and DTS both target PTSEpoch we MUST resolve the
		// target once and reuse the same monotonic-clock reading for
		// both, so the shifts match and the original (pkt.PTS - pkt.DTS)
		// gap is preserved post-shift. Two independent reads of
		// nowMonotonicNanos() differ by the time spent between the
		// calls (a few hundred ns — but in the camera path with
		// timebase 1/1e9 that delta lands directly in the packet's
		// timestamp field), pushing shifted DTS past shifted PTS. The
		// downstream MonotonicPTS filter then rejects every such
		// packet (commons line 59-61: "frame PTS < DTS, skipping"),
		// so the cam frames silently disappear before reaching the
		// output's TranscoderNode and the encoder never instantiates
		// — surfacing as the "unable to get encoder" loop in
		// AutoBitRateHandler. Resolving once also matches the
		// docstring intent at applyPerStreamShift: "the shift for a
		// stream on that stream's first packet" — singular shift,
		// not two slightly-different ones.
		var sharedEpochTarget int64
		var sharedEpochResolved bool
		resolveTarget := func(forceStart int64) int64 {
			if forceStart != globaltypes.PTSEpoch {
				return resolvePTSShiftTarget(forceStart, stream.TimeBase())
			}
			if !sharedEpochResolved {
				sharedEpochTarget = resolvePTSShiftTarget(forceStart, stream.TimeBase())
				sharedEpochResolved = true
			}
			return sharedEpochTarget
		}

		if wantPTSShift && pkt.Pts() != astiav.NoPtsValue {
			shift, ok := i.PTSShifts.Load(streamIndex)
			if !ok {
				target := resolveTarget(i.ForceStartPTS)
				shift = target - pkt.Pts()
				i.PTSShifts.Store(streamIndex, shift)
				logger.Infof(ctx, "stream #%d: applying PTS shift of %d (target=%d)", streamIndex, shift, target)
			}
			pkt.SetPts(pkt.Pts() + shift)
		}
		if wantDTSShift && pkt.Dts() != astiav.NoPtsValue {
			shift, ok := i.DTSShifts.Load(streamIndex)
			if !ok {
				target := resolveTarget(i.ForceStartDTS)
				shift = target - pkt.Dts()
				i.DTSShifts.Store(streamIndex, shift)
				logger.Infof(ctx, "stream #%d: applying DTS shift of %d (target=%d)", streamIndex, shift, target)
			}
			pkt.SetDts(pkt.Dts() + shift)
		}
	}

	// processPacket handles a single packet after its PTS/DTS shift has
	// already been applied (or no shift is needed).
	processPacket := func(
		ctx context.Context,
		pkt *astiav.Packet,
		streamIndex int,
		stream *astiav.Stream,
	) error {
		prevPkt := prevPkts[streamIndex]
		curPkt := ptr(packet.BuildOutput(
			pkt,
			packet.BuildStreamInfo(
				stream,
				i,
				i.PipelineSideData,
			),
		))

		if !i.IgnoreZeroDuration && prevPkt != nil {
			assert(ctx, prevPkt.Pts() != astiav.NoPtsValue, "previous packet PTS is not set")
			// Sign of prevPkt.Pts() does not matter: only the delta
			// (curPkt.Pts() - prevPkt.Pts()) is used below, and negative
			// PTS values are legitimate for many real inputs.
			suggestedDuration := curPkt.Pts() - prevPkt.Pts()
			frameSecs := stream.TimeBase().Float64() * float64(suggestedDuration)
			if frameSecs > 1 || suggestedDuration <= 0 {
				logger.Tracef(ctx, "the packet had no duration set; cannot use cur.pts - prev.pts: %d-%d=%d as it suggests too large or invalid duration (%f secs); trying last known duration", curPkt.Pts(), prevPkt.Pts(), suggestedDuration, frameSecs)
				suggestedDuration = lastDuration[streamIndex]
				frameSecs = stream.TimeBase().Float64() * float64(suggestedDuration)
			}
			if frameSecs > 1 || suggestedDuration <= 0 {
				logger.Warnf(ctx, "the packet had no duration set; but cannot find a reasonable suggestion how to fix it: pts_cur:%d pts_prev:%d suggested_duration:%d time_base:%f", curPkt.Pts(), prevPkt.Pts(), suggestedDuration, stream.TimeBase().Float64())
			} else {
				prevPkt.SetDuration(suggestedDuration)
				logger.Tracef(ctx, "the packet had no duration set; set it to: cur.pts - prev.pts: %d-%d=%d", curPkt.GetPTS(), prevPkt.GetPTS(), prevPkt.GetDuration())

				// Fallback: derive framerate from observed PTS intervals. The primary
				// mechanism is the demuxer or doOpen setting avg_frame_rate from the
				// 'framerate' option. This handles edge cases where that didn't happen
				// (e.g., no 'framerate' option was provided for a device input).
				if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo &&
					stream.CodecParameters().FrameRate().Num() == 0 &&
					frameSecs > 0 {
					fpsFloat := 1.0 / frameSecs
					fpsRational := globaltypes.RationalFromApproxFloat64(fpsFloat)
					fps := astiav.NewRational(fpsRational.Num, fpsRational.Den)
					logger.Infof(ctx, "stream #%d: derived framerate %v from PTS interval (%f secs)", streamIndex, fps, frameSecs)
					stream.CodecParameters().SetFrameRate(fps)
					if stream.AvgFrameRate().Num() == 0 {
						stream.SetAvgFrameRate(fps)
					}
					if stream.RFrameRate().Num() == 0 {
						stream.SetRFrameRate(fps)
					}
				}
			}

			if err := sendPkt(prevPkt); err != nil {
				return err
			}
		}

		if !i.IgnoreIncorrectDTS && curPkt.GetDTS() == astiav.NoPtsValue {
			curPkt.SetDTS(curPkt.GetPTS())
			logger.Tracef(ctx, "the packet had no DTS set; setting DTS=PTS: %d", curPkt.GetPTS())
		}

		if !i.IgnoreZeroDuration {
			isDurationIncorrect := curPkt.GetDuration() <= 1
			if isDurationIncorrect {
				fps := i.GuessFrameRate(stream, nil)
				logger.Tracef(ctx, "guessed FPS from format context: %v", fps)
				if fps.Num() == 0 || fps.Den() == 0 {
					fps = stream.AvgFrameRate()
					logger.Tracef(ctx, "guessed FPS from avg frame rate: %v", fps)
				}
				if fps.Num() == 0 || fps.Den() == 0 {
					fps = stream.RFrameRate()
					logger.Tracef(ctx, "guessed FPS from r frame rate: %v", fps)
				}
				if fps.Num() == 0 || fps.Den() == 0 {
					fps = stream.CodecParameters().FrameRate()
					logger.Tracef(ctx, "guessed FPS from codec parameters frame rate: %v", fps)
				}

				if fps.Num() != 0 && fps.Den() != 0 {
					if stream.CodecParameters().FrameRate().Num() == 0 {
						stream.CodecParameters().SetFrameRate(fps)
					}
					if stream.TimeBase().Num() != 0 && stream.TimeBase().Den() != 0 {
						duration := int64(math.Round(float64(1) / fps.Float64() / stream.TimeBase().Float64()))
						curPkt.SetDuration(duration)
						logger.Tracef(ctx, "the packet had no duration set; setting duration to FPS %v: %d", fps, curPkt.GetDuration())
					}
				}

				if curPkt.GetDuration() <= 1 {
					if curPkt.GetPTS() >= curPkt.GetDTS() && // not a B-frame-like packet
						curPkt.GetPTS() != astiav.NoPtsValue { // PTS is set (thus duration can be calculated)
						// Sign of curPkt.GetPTS() does not matter: this packet
						// is buffered so the next packet's PTS-delta can supply
						// a duration. Negative PTS is legitimate for many inputs.
						logger.Tracef(ctx, "the packet has no duration set; waiting for the next packet to suggest a duration")
						prevPkts[streamIndex] = curPkt
						return nil
					}
					logger.Tracef(ctx, "the packet has no duration set; using the last known duration")
					curPkt.SetDuration(lastDuration[streamIndex])
				}

				if curPkt.GetDuration() <= 1 && curPkt.GetMediaType() == astiav.MediaTypeVideo {
					fps = i.DefaultFPS
					logger.Warnf(ctx, "using default FPS (%v) to calculate the frame duration; the calculation is likely incorrect", fps)
					if fps.Num() != 0 && fps.Den() != 0 && stream.TimeBase().Num() != 0 && stream.TimeBase().Den() != 0 {
						logger.Tracef(ctx, "the video packet still has no duration set; using default FPS %d", fps.Float64())
						duration := int64(math.Round(float64(1) / fps.Float64() / stream.TimeBase().Float64()))
						curPkt.SetDuration(duration)
					}
				}
			} else {
				delete(prevPkts, streamIndex)
			}
		}

		// no correction is needed, let's send immediately
		return sendPkt(curPkt)
	}

	for {
		select {
		case <-i.CloseChan():
			logger.Debugf(ctx, "input is closed, stopping packet generation")
			return io.EOF
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pkt := packet.Pool.Get()
		err := i.readIntoPacket(ctx, pkt)
		switch err {
		case nil:
		case io.EOF:
			pkt.Free()
			logger.Debugf(ctx, "end of input reached")
			return io.EOF
		default:
			pkt.Free()
			return fmt.Errorf("unable to read a packet: %w", err)
		}

		streamIndex := pkt.StreamIndex()
		stream := avconv.FindStreamByIndex(ctx, i.FormatContext, streamIndex)
		codecParams := stream.CodecParameters()
		logger.Tracef(
			ctx,
			"received a %s packet (stream:%d, pos:%d, pts:%d, dts:%d, dur:%d, time_base:%v, isKey:%t), dataLen:%d, extraData:%s",
			codecParams.MediaType(),
			streamIndex,
			pkt.Pos(), pkt.Pts(), pkt.Dts(), pkt.Duration(), stream.TimeBase(),
			pkt.Flags().Has(astiav.PacketFlagKey),
			len(pkt.Data()), extradata.Raw(codecParams.ExtraData()),
		)

		applyPerStreamShift(ctx, pkt, streamIndex, stream)

		if err := processPacket(ctx, pkt, streamIndex, stream); err != nil {
			return err
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
	case <-i.CloseChan():
		return
	case <-ctx.Done():
		logger.Debugf(ctx, "context is closed")
		return
	case <-i.openFinished:
	}
	callback(i.FormatContext)
}

func (i *Input) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) (_err error) {
	switch {
	case input.Packet != nil:
		return fmt.Errorf("cannot send packets to an Input")
	case input.Frame != nil:
		return fmt.Errorf("cannot send frames to an Input")
	default:
		return kerneltypes.ErrUnexpectedInputType{}
	}
}

func (i *Input) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(i)
}

func (i *Input) String() string {
	return fmt.Sprintf("Input(%s)", i.URL)
}
