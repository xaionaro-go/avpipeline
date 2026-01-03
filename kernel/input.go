package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"reflect"
	"runtime"
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
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/avpipeline/urltools"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/unsafetools"
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
	OnPreClose         func(context.Context, *Input) error
	IgnoreIncorrectDTS bool
	IgnoreZeroDuration bool

	SyncStreamIndex   atomic.Int64
	SyncStartPTS      atomic.Int64
	SyncStartUnixNano atomic.Int64
	PTSShift          atomic.Int64
	DTSShift          atomic.Int64

	OutputFilters []packetcondition.Condition

	WaitGroup sync.WaitGroup
}

var (
	_ Abstract      = (*Input)(nil)
	_ packet.Source = (*Input)(nil)
)

var nextInputID atomic.Uint64

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
	}
	if cfg.OnPreClose != nil {
		i.OnPreClose = func(ctx context.Context, i *Input) error {
			return cfg.OnPreClose.FireHook(ctx, i)
		}
	}
	i.SyncStreamIndex.Store(math.MinInt64)
	i.SyncStartPTS.Store(math.MinInt64)
	i.SyncStartUnixNano.Store(math.MinInt64)
	i.PTSShift.Store(math.MinInt64)
	i.DTSShift.Store(math.MinInt64)
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
				i.Dictionary.Set("video_size", opt.Value, 0)
			case "framerate":
				logger.Debugf(ctx, "setting input framerate to '%s'", opt.Value)
				var r float64
				_, err := fmt.Sscanf(opt.Value, "%f", &r)
				if err != nil {
					return nil, fmt.Errorf("unable to parse input framerate '%s': %w", opt.Value, err)
				}
				defaultFPS = r
				i.Dictionary.Set("framerate", opt.Value, 0)
			default:
				logger.Debugf(ctx, "input.Dictionary['%s'] = '%s'", opt.Key, opt.Value)
				i.Dictionary.Set(opt.Key, opt.Value, 0)
			}
		}
	}

	defaultFPSRational := globaltypes.RationalFromApproxFloat64(defaultFPS)
	i.DefaultFPS = astiav.NewRational(defaultFPSRational.Num, defaultFPSRational.Den)

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
		i.WaitGroup.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			var err error
			func() {
				defer i.WaitGroup.Done()
				err = i.doOpen(ctx, urlString, authKey, inputFormat, cfg)
			}()
			if err != nil {
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
	i.FormatContext.SetIOInterrupter(i.IOInterrupter)

	if err := ctx.Err(); err != nil {
		i.FormatContext.Free()
		return fmt.Errorf("context cancelled before opening input: %w", err)
	}

	// interruptable OpenInput
	openInputDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			logger.Debugf(ctx, "context cancelled during OpenInput, interrupting IO")
			i.IOInterrupter.Interrupt()
		case <-openInputDone:
		}
	}()
	err := i.FormatContext.OpenInput(urlWithSecret, inputFormat, i.Dictionary)
	close(openInputDone)

	if err != nil {
		i.FormatContext.Free()
		if authKey.Get() != "" {
			return fmt.Errorf("unable to open input by URL '%s/<HIDDEN>' (format: %q): %w", urlString, inputFormat, err)
		} else {
			return fmt.Errorf("unable to open input by URL %q (format: %q): %w", urlString, inputFormat, err)
		}
	}
	setFinalizer(ctx, i, func(i *Input) {
		i.FormatContext.CloseInput()
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
	case "pulse":
		if cfg.ForceStartPTS == nil {
			i.ForceStartPTS = 0
		}
		if cfg.ForceStartDTS == nil {
			i.ForceStartDTS = 0
		}
	case "android_camera":
		if cfg.ForceStartPTS == nil {
			i.ForceStartPTS = int64(0)
		}
		if cfg.ForceStartDTS == nil {
			i.ForceStartDTS = int64(0)
		}
	default:
		if cfg.ForceRealTime == nil {
			if urltools.IsFileURL(urlString) {
				i.ForceRealTime = true
			}
		}
	}
	logger.Debugf(ctx, "ForceRealTime=%t ForceStartPTS=%d ForceStartDTS=%d", i.ForceRealTime, i.ForceStartPTS, i.ForceStartDTS)

	if cfg.RecvBufferSize != 0 {
		if err := i.UnsafeSetRecvBufferSize(ctx, cfg.RecvBufferSize); err != nil {
			return fmt.Errorf("unable to set the recv buffer size to %d: %w", cfg.RecvBufferSize, err)
		}
	}

	if err := i.FormatContext.FindStreamInfo(nil); err != nil {
		return fmt.Errorf("unable to get stream info: %w", err)
	}

	for _, stream := range i.FormatContext.Streams() {
		logger.Debugf(ctx, "input stream #%d: %#+v", stream.Index(), spew.Sdump(unsafetools.FieldByNameInValue(reflect.ValueOf(stream.CodecParameters()), "c").Elem().Elem().Interface()))
	}

	if cfg.OnPostOpen != nil {
		cfg.OnPostOpen.FireHook(ctx, i)
	}

	return nil
}

func (i *Input) Close(
	ctx context.Context,
) (_err error) {
	f, l := getCaller()
	logger.Debugf(ctx, "Close[%s]: called from %s:%d", i, f, l)
	defer func() { logger.Debugf(ctx, "/Close[%s]: %v", i, _err) }()
	if i == nil {
		return nil
	}

	logger.Debugf(ctx, "interrupting IO before close")
	i.IOInterrupter.Interrupt()

	select {
	case <-i.openFinished:
		logger.Debugf(ctx, "doOpen completed (%v), proceeding with close", i.openError)
	default:
		logger.Debugf(ctx, "doOpen not completed yet, waiting for it to finish")
	}

	i.ClosureSignaler.Close(ctx)
	i.WaitGroup.Wait()

	var errs []error
	if !i.AutoClose { // it means it won't be closed automatically, thus we should close it here, since this was a manual Close()
		if fn := i.OnPreClose; fn != nil {
			if err := fn(ctx, i); err != nil {
				errs = append(errs, fmt.Errorf("input OnPreClose error: %w", err))
			}
		}
		i.FormatContext.CloseInput()
	}
	return errors.Join(errs...)
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

func (i *Input) autoDetectSyncStreamIndexIfNeeded(
	ctx context.Context,
	outPkt *packet.Output,
) bool {
	if i.SyncStreamIndex.Load() >= 0 {
		return true
	}

	switch outPkt.GetCodecParameters().MediaType() {
	case astiav.MediaTypeAudio:
		if i.SyncStreamIndex.CompareAndSwap(math.MinInt64, int64(outPkt.GetStreamIndex())) {
			logger.Debugf(ctx, "auto-detected sync stream index: %d (video)", outPkt.GetStreamIndex())
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

	pts := outPkt.Pts()
	if pts == astiav.NoPtsValue {
		return
	}

	timeBase := outPkt.GetTimeBase()
	if timeBase.Num() == 0 || timeBase.Den() == 0 {
		return
	}
	wantSeconds := timeBase.Float64() * float64(pts)

	assert(ctx, pts >= 0, "PTS is negative")
	nowUnixNano := time.Now().UnixNano()
	startUnixNano := i.SyncStartUnixNano.Load()

	// Initialize base reference on the first sync packet.
	startPTS := i.SyncStartPTS.Load()
	if startUnixNano == math.MinInt64 {
		if startPTS == math.MinInt64 {
			for {
				i.SyncStartPTS.CompareAndSwap(math.MinInt64, pts)
				startPTS = i.SyncStartPTS.Load()
				if startPTS >= 0 {
					break
				}
				runtime.Gosched()
			}
		}
		i.SyncStartUnixNano.Store(nowUnixNano)
		startUnixNano = nowUnixNano
	}

	currentSeconds := float64(nowUnixNano-startUnixNano) / float64(time.Second.Nanoseconds())
	diffSeconds := wantSeconds - float64(startPTS)*timeBase.Float64()
	if diffSeconds <= 0 {
		return
	}

	sleepSeconds := diffSeconds - currentSeconds
	if sleepSeconds <= 0 {
		return
	}

	if sleepSeconds > 10 {
		logger.Errorf(ctx, "sleepSeconds too large (%f), capping to 10 seconds (wantSeconds:%f currentSeconds:%f startPTS:%d)", sleepSeconds, wantSeconds, currentSeconds, startPTS)
		sleepSeconds = 10
	}

	logger.Tracef(ctx, "slowing down input by sleeping for %f seconds (wantSeconds:%f currentSeconds:%f startPTS:%d)", sleepSeconds, wantSeconds, currentSeconds, startPTS)
	sleepDur := time.Duration(sleepSeconds * float64(time.Second))
	timer := time.NewTimer(sleepDur)
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
	i.WaitGroup.Add(1)
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
			i.FormatContext.CloseInput()
		}()
	}

	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
		logger.Debugf(ctx, "interrupting IO")
		i.IOInterrupter.Interrupt()
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
	for {
		select {
		case <-i.CloseChan():
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
			return nil
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
		if i.ForceStartPTS != globaltypes.PTSKeep && pkt.Pts() != astiav.NoPtsValue {
			if i.PTSShift.Load() == math.MinInt64 {
				ptsShift := i.ForceStartPTS - pkt.Pts()
				i.PTSShift.Store(ptsShift)
				logger.Infof(ctx, "applying PTS shift of %d to input packets", ptsShift)
			}
			pkt.SetPts(pkt.Pts() + i.PTSShift.Load())
		}
		if i.ForceStartDTS != globaltypes.PTSKeep && pkt.Dts() != astiav.NoPtsValue {
			if i.DTSShift.Load() == math.MinInt64 {
				dtsShift := i.ForceStartDTS - pkt.Dts()
				i.DTSShift.Store(dtsShift)
				logger.Infof(ctx, "applying DTS shift of %d to input packets", dtsShift)
			}
			pkt.SetDts(pkt.Dts() + i.DTSShift.Load())
		}

		prevPkt := prevPkts[streamIndex]
		curPkt := ptr(packet.BuildOutput(
			pkt,
			packet.BuildStreamInfo(
				stream,
				i,
				nil,
			),
		))

		if !i.IgnoreZeroDuration && prevPkt != nil {
			assert(ctx, prevPkt.Pts() != astiav.NoPtsValue, "previous packet PTS is not set")
			assert(ctx, prevPkt.Pts() >= 0, "previous packet PTS is negative")
			suggestedDuration := curPkt.Pts() - prevPkt.Pts()
			frameSecs := stream.TimeBase().Float64() * float64(suggestedDuration)
			if frameSecs > 1 || suggestedDuration <= 0 {
				logger.Tracef(ctx, "the packet had no duration set; cannot use cur.pts - prev.pts: %d-%d=%d as it suggests too large or invalid duration (%f secs); trying last known duration", curPkt.Pts(), prevPkt.Pts(), suggestedDuration, frameSecs)
				suggestedDuration = lastDuration[streamIndex]
			}
			if frameSecs > 1 || suggestedDuration <= 0 {
				logger.Warnf(ctx, "the packet had no duration set; but cannot find a reasonable suggestion how to fix it: pts_cur:%d pts_prev:%d suggested_duration:%d time_base:%f", curPkt.Pts(), prevPkt.Pts(), suggestedDuration, stream.TimeBase().Float64())
			} else {
				prevPkt.SetDuration(suggestedDuration)
				logger.Tracef(ctx, "the packet had no duration set; set it to: cur.pts - prev.pts: %d-%d=%d", curPkt.GetPTS(), prevPkt.GetPTS(), prevPkt.GetDuration())
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
				switch curPkt.GetMediaType() {
				case astiav.MediaTypeVideo:
					fps := stream.CodecParameters().FrameRate()
					if fps.Num() == 0 || fps.Den() == 0 {
						logger.Tracef(ctx, "the video packet had no duration set; stream has no FPS set; using default FPS %d", i.DefaultFPS.Float64())
						fps = i.DefaultFPS
					}
					duration := int(float64(1) / fps.Float64() / stream.TimeBase().Float64())
					curPkt.SetDuration(int64(duration))
					logger.Tracef(ctx, "the video packet had no duration set; setting duration to default FPS %d: %d", fps.Float64(), curPkt.GetDuration())
				default:
					if curPkt.GetPTS() >= curPkt.GetDTS() && // not a B-frame-like packet
						curPkt.GetPTS() != astiav.NoPtsValue { // PTS is set (thus duration can be calculated)
						assert(ctx, curPkt.GetPTS() >= 0, "previous packet PTS is negative")
						logger.Tracef(ctx, "the packet has no duration set; waiting for the next packet to suggest a duration")
						prevPkt = nil
						prevPkts[streamIndex] = curPkt
						continue
					} else {
						logger.Tracef(ctx, "the B-frame packet has no duration set; using the last known duration")
						curPkt.SetDuration(lastDuration[streamIndex])
					}
				}
			} else {
				prevPkt = nil
				delete(prevPkts, streamIndex)
			}
		}

		// no correction is needed, let's send immediately
		err = sendPkt(curPkt)
		if err != nil {
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
