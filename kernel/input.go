package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/davecgh/go-spew/spew"
	"github.com/xaionaro-go/avpipeline/avconv"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
	"github.com/xaionaro-go/unsafetools"
)

const (
	inputDefaultWidth  = 1920
	inputDefaultHeight = 1080
	inputDefaultFPS    = 30
)

type InputConfig struct {
	CustomOptions      globaltypes.DictionaryItems
	RecvBufferSize     uint
	AsyncOpen          bool
	OnPostOpen         func(context.Context, *Input) error
	OnPreClose         func(context.Context, *Input) error
	KeepOpen           bool
	IgnoreIncorrectDTS bool
	IgnoreZeroDuration bool
}

type Input struct {
	*closuresignaler.ClosureSignaler
	initialized chan struct{}

	*astiav.FormatContext
	*astiav.IOInterrupter
	*astiav.Dictionary

	ID            InputID
	URL           string
	URLParsed     *url.URL
	DefaultWidth  int
	DefaultHeight int
	DefaultFPS    astiav.Rational

	KeepOpen           bool
	OnPreClose         func(context.Context, *Input) error
	IgnoreIncorrectDTS bool
	IgnoreZeroDuration bool

	WaitGroup sync.WaitGroup
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

		initialized:     make(chan struct{}),
		ClosureSignaler: closuresignaler.New(),

		KeepOpen:           cfg.KeepOpen,
		OnPreClose:         cfg.OnPreClose,
		IgnoreIncorrectDTS: cfg.IgnoreIncorrectDTS,
		IgnoreZeroDuration: cfg.IgnoreZeroDuration,
	}
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
					return nil, fmt.Errorf("unable to parse input size '%s': %w", opt.Value, err)
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
		cfg.OnPostOpen(ctx, i)
	}
	close(i.initialized)

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
	i.ClosureSignaler.Close(ctx)
	i.WaitGroup.Wait()
	if i.KeepOpen { // it means it won't be closed automatically, thus we should close it here, since this was a manual Close()
		if fn := i.OnPreClose; fn != nil {
			fn(ctx, i)
		}
		i.FormatContext.CloseInput()
	}
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
	case <-i.initialized:
	}

	if !i.KeepOpen {
		defer i.FormatContext.CloseInput()
		if fn := i.OnPreClose; fn != nil {
			defer fn(ctx, i)
		}
	}

	observability.Go(ctx, func(ctx context.Context) {
		<-ctx.Done()
		logger.Debugf(ctx, "interrupting IO")
		i.IOInterrupter.Interrupt()
	})

	lastDuration := map[int]int64{}
	sendPkt := func(outPkt *packet.Output) error {
		codecParams := outPkt.Stream.CodecParameters()
		switch codecParams.MediaType() {
		case astiav.MediaTypeVideo:
			if outPkt.CodecParameters().Width() != 0 && outPkt.CodecParameters().Width() != codecParams.Width() {
				logger.Debugf(ctx, "correcting packet width from %d to %d", outPkt.CodecParameters().Width(), codecParams.Width())
				codecParams.SetWidth(outPkt.CodecParameters().Width())
			}
			if codecParams.Width() == 0 {
				logger.Warnf(ctx, "width is zero, defaulting to %d", i.DefaultWidth)
				codecParams.SetWidth(i.DefaultWidth)
				outPkt.CodecParameters().SetWidth(i.DefaultWidth)
			}
			if outPkt.CodecParameters().Height() != 0 && outPkt.CodecParameters().Height() != codecParams.Height() {
				logger.Debugf(ctx, "correcting packet height from %d to %d", outPkt.CodecParameters().Height(), codecParams.Height())
				codecParams.SetHeight(outPkt.CodecParameters().Height())
			}
			if codecParams.Height() == 0 {
				logger.Warnf(ctx, "height is zero, defaulting to %d", i.DefaultHeight)
				codecParams.SetHeight(i.DefaultHeight)
				outPkt.CodecParameters().SetHeight(i.DefaultHeight)
			}
		}
		logger.Tracef(
			ctx,
			"sending a %s packet (stream:%d, pos:%d, pts:%d, dts:%d, dur:%d, isKey:%t), dataLen:%d, res:%dx%d",
			codecParams.MediaType(),
			outPkt.StreamIndex(),
			outPkt.Pos(), outPkt.Pts(), outPkt.Dts(), outPkt.Packet.Duration(),
			outPkt.Flags().Has(astiav.PacketFlagKey),
			len(outPkt.Data()),
			codecParams.Width(), codecParams.Height(),
		)

		lastDuration[outPkt.StreamIndex()] = outPkt.Duration()
		select {
		case outputPacketsCh <- *outPkt:
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
			select {
			case <-ctx.Done():
			case <-i.CloseChan():
			default:
			}
			sendPkt(pkt)
		}
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
					prevPkt.Packet.SetDuration(suggestedDuration)
					logger.Tracef(ctx, "the packet had no duration set; set it to: cur.pts - prev.pts: %d-%d=%d", curPkt.Pts(), prevPkt.Pts(), prevPkt.Packet.Duration())
				}

				if err := sendPkt(prevPkt); err != nil {
					return err
				}
			}

			if !i.IgnoreIncorrectDTS && curPkt.Dts() == astiav.NoPtsValue {
				curPkt.Packet.SetDts(curPkt.Pts())
				logger.Tracef(ctx, "the packet had no DTS set; setting DTS=PTS: %d", curPkt.Pts())
			}

			if !i.IgnoreZeroDuration {
				isDurationIncorrect := curPkt.Duration() <= 1
				if isDurationIncorrect {
					switch curPkt.GetMediaType() {
					case astiav.MediaTypeVideo:
						fps := stream.CodecParameters().FrameRate()
						if fps.Num() == 0 || fps.Den() == 0 {
							logger.Tracef(ctx, "the video packet had no duration set; stream has no FPS set; using default FPS %d", i.DefaultFPS.Float64())
							fps = i.DefaultFPS
						}
						duration := int(float64(1) / fps.Float64() / stream.TimeBase().Float64())
						curPkt.Packet.SetDuration(int64(duration))
						logger.Tracef(ctx, "the video packet had no duration set; setting duration to default FPS %d: %d", fps.Float64(), curPkt.Packet.Duration())
					default:
						if curPkt.Pts() >= curPkt.Dts() && // not a B-frame-like packet
							curPkt.Pts() != astiav.NoPtsValue { // PTS is set (thus duration can be calculated)
							assert(ctx, curPkt.Pts() >= 0, "previous packet PTS is negative")
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
			err := sendPkt(curPkt)
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

func (i *Input) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(i)
}

func (i *Input) String() string {
	return fmt.Sprintf("Input(%s)", i.URL)
}
