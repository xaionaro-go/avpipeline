package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/dustin/go-humanize"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	"github.com/xaionaro-go/avpipeline/node/filter/packetfilter/preset/monotonicpts"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/limitframerate"
	"github.com/xaionaro-go/avpipeline/preset/autofix"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/quality"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/observability/xlogger"
	"github.com/xaionaro-go/secret"
)

const (
	outputBSFEnabled = true
)

func main() {

	// parse the input

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: %s <URL-from-main> <URL-from-fallback> <URL-to>\n", os.Args[0])
		pflag.PrintDefaults()
	}

	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	netPprofAddr := pflag.String("net-pprof-listen-addr", "", "an address to listen for incoming net/pprof connections")
	frameDrop := pflag.Bool("framedrop", false, "")
	retryInterval := pflag.Duration("retry-interval", time.Second, "retry interval on input error")
	encoderVideoResolutionFlag := pflag.String("encoder-video-resolution", "", "encoding resolution, empty means same as input")
	encoderVideoBitRate := pflag.String("encoder-video-bitrate", "", "encoding video bitrate, empty means the default")
	encoderVideoCodec := pflag.String("encoder-video-codec", "libx264", "encoding video codec")
	encoderAudioCodec := pflag.String("encoder-audio-codec", "aac", "encoding audio codec")
	encoderAudioChannels := pflag.Int("encoder-audio-channels", 2, "encoding audio channels")
	videoMaxFPS := pflag.Float64("video-framerate", -1, "maximal video FPS; currently supports only downscaling; -1 means no limit")
	videoGOP := pflag.Int("video-gop", 60, "video GOP size in frames")

	pflag.Parse()
	if len(pflag.Args()) != 3 {
		pflag.Usage()
		os.Exit(1)
	}

	var encoderResolution *codectypes.Resolution
	if *encoderVideoResolutionFlag != "" {
		encoderResolution = &codectypes.Resolution{}
		err := encoderResolution.Parse(*encoderVideoResolutionFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: unable to parse encoder resolution '%s': %v\n", *encoderVideoResolutionFlag, err)
			os.Exit(1)
		}
	}

	var encoderQuality quality.Quality
	if *encoderVideoBitRate != "" {
		i, err := humanize.ParseBytes(*encoderVideoBitRate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: unable to parse encoder bitrate '%s': %v\n", *encoderVideoBitRate, err)
			os.Exit(1)
		}
		encoderQuality = quality.ConstantBitrate(uint64(i))
	}

	fromURLMain := pflag.Arg(0)
	fromURLFallback := pflag.Arg(1)
	toURL := pflag.Arg(2)

	// init the context

	ctx := withLogger(context.Background(), loggerLevel)
	ctx, cancelFn := context.WithCancel(ctx)
	defer func() {
		logger.Debugf(ctx, "canceling context...")
		cancelFn()
	}()
	defer belt.Flush(ctx)

	if *netPprofAddr != "" {
		observability.Go(ctx, func(context.Context) { logger.Error(ctx, http.ListenAndServe(*netPprofAddr, nil)) })
	}

	defer logger.Infof(ctx, "finishing: finalization")

	// input switch

	sw := newSwitch(ctx)

	// input nodes: main

	decoderHardwareDeviceType := globaltypes.HardwareDeviceTypeNone
	encoderHardwareDeviceType := globaltypes.HardwareDeviceTypeNone

	logger.Debugf(ctx, "opening '%s' as the main input...", fromURLMain)

	inputMain := kernel.NewRetryable(xlogger.CtxWithMaxLoggingLevel(ctx, logger.LevelWarning),
		func(ctx context.Context) (*kernel.Input, error) {
			return kernel.NewInputFromURL(ctx, fromURLMain, secret.New(""), kernel.InputConfig{
				KeepOpen: true,
			})
		},
		func(ctx context.Context, k *kernel.Input) error {
			sw.SetValue(ctx, 0)
			return nil
		},
		func(ctx context.Context, k *kernel.Input, err error) error {
			logger.Debugf(ctx, "main input error: %v", err)
			sw.SetValue(ctx, 1)
			time.Sleep(*retryInterval)
			observability.Go(ctx, func(ctx context.Context) { k.Close(ctx) })
			return kernel.ErrRetry{Err: err}
		},
	)
	defer inputMain.Close(belt.WithFields(ctx, field.Map[string]{"reason": "program_end", "input": "main"}))
	inputMainNode := node.NewFromKernel(ctx, inputMain)
	decoderMain := kernel.NewDecoder(ctx,
		codec.NewNaiveDecoderFactory(ctx, &codec.NaiveDecoderFactoryParams{
			HardwareDeviceType: decoderHardwareDeviceType,
		}),
	)
	decoderMainNode := node.NewFromKernel(ctx, decoderMain, processor.DefaultOptionsRecoder()...)

	// input nodes: fallback

	logger.Debugf(ctx, "opening '%s' as the fallback input...", fromURLFallback)
	inputFallback := kernel.NewRetryable(xlogger.CtxWithMaxLoggingLevel(ctx, logger.LevelWarning),
		func(ctx context.Context) (*kernel.Input, error) {
			return kernel.NewInputFromURL(ctx, fromURLFallback, secret.New(""), kernel.InputConfig{
				KeepOpen: true,
			})
		},
		func(ctx context.Context, k *kernel.Input) error {
			return nil
		},
		func(ctx context.Context, k *kernel.Input, err error) error {
			logger.Debugf(ctx, "fallback input error: %v", err)
			time.Sleep(*retryInterval)
			observability.Go(ctx, func(ctx context.Context) { k.Close(ctx) })
			return kernel.ErrRetry{Err: err}
		},
	)
	defer inputFallback.Close(belt.WithFields(ctx, field.Map[string]{"reason": "program_end", "input": "main"}))
	inputFallbackNode := node.NewFromKernel(ctx, inputFallback)
	decoderFallback := kernel.NewDecoder(ctx,
		codec.NewNaiveDecoderFactory(ctx, &codec.NaiveDecoderFactoryParams{
			HardwareDeviceType: decoderHardwareDeviceType,
		}),
	)
	decoderFallbackNode := node.NewFromKernel(ctx, decoderFallback, processor.DefaultOptionsRecoder()...)

	sw.SetOnAfterSwitch(func(ctx context.Context, pkt packetorframe.InputUnion, from, to int32) {
		logger.Debugf(ctx, "switched input from %d to %d", from, to)
		switch to {
		case 0:
			decoderMain.ResetHard(ctx)
		case 1:
			decoderFallback.ResetHard(ctx)
		}
	})

	// encoder

	logger.Debugf(ctx, "initializing encoder...")
	audioOptions := astiav.NewDictionary()
	audioOptions.Set("ac", fmt.Sprintf("%d", *encoderAudioChannels), 0)
	videoOptions := astiav.NewDictionary()
	videoOptions.Set("g", fmt.Sprintf("%d", *videoGOP), 0)
	encoder := kernel.NewEncoder(ctx,
		codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			HardwareDeviceType: encoderHardwareDeviceType,
			VideoCodec:         codec.Name(*encoderVideoCodec),
			VideoQuality:       encoderQuality,
			VideoResolution:    encoderResolution,
			VideoOptions:       videoOptions,
			AudioCodec:         codec.Name(*encoderAudioCodec),
			AudioOptions:       audioOptions,
		}),
		nil,
	)
	encoderNode := node.NewFromKernel(ctx, encoder, processor.DefaultOptionsRecoder()...)

	// output node

	logger.Debugf(ctx, "opening '%s' as the output...", toURL)
	output, err := processor.NewOutputFromURL(
		ctx,
		toURL, secret.New(""),
		kernel.OutputConfig{},
	)
	assert(ctx, err == nil, err)
	defer output.Close(belt.WithField(ctx, "reason", "program_end"))
	outputNode := node.New(output)

	defer logger.Infof(ctx, "finishing: nodes")

	// route nodes:
	//
	//                              ReduceFPS
	// input (main) -----> switch ->--------> MonotonicPTS + decoder --->-+
	//                      .                  .                          |
	//                      .                  .                          +->---------> encoder --> BSF --> output
	//                      .       ReduceFPS  .                          |
	// input (fallback) -> switch ->--------> MonotonicPTS + decoder --->-+
	//

	outputFormatName := outputNode.Processor.Kernel.FormatContext.OutputFormat().Name()
	logger.Infof(ctx, "output format: '%s'", outputFormatName)
	monotonicPTS := monotonicpts.New(true)

	// main
	inputMainSwitchNode := node.NewFromKernel(ctx, kernel.NewBarrier(ctx,
		sw.Output(0),
	))
	inputMainNode.AddPushPacketsTo(ctx,
		inputMainSwitchNode,
		monotonicPTS,
	)
	inputMainSwitchNode.AddPushPacketsTo(ctx, decoderMainNode)
	decoderMainNode.AddPushPacketsTo(ctx, encoderNode)
	decoderMainNode.AddPushFramesTo(ctx, encoderNode)

	// fallback
	inputFallbackSwitchNode := node.NewFromKernel(ctx, kernel.NewBarrier(ctx,
		sw.Output(1),
	))
	inputFallbackNode.AddPushPacketsTo(ctx,
		inputFallbackSwitchNode,
		monotonicPTS,
	)
	inputFallbackSwitchNode.AddPushPacketsTo(ctx, decoderFallbackNode)
	decoderFallbackNode.AddPushPacketsTo(ctx, encoderNode)
	decoderFallbackNode.AddPushFramesTo(ctx, encoderNode)

	if *videoMaxFPS >= 0 {
		r := globaltypes.RationalFromApproxFloat64(*videoMaxFPS)
		encoderNode.SetInputFrameFilter(ctx, framefiltercondition.Frame{framecondition.Or{
			framecondition.Not{framecondition.MediaType(astiav.MediaTypeVideo)},
			framecondition.PacketOrFrame{
				limitframerate.New(mathcondition.GetterStatic[globaltypes.Rational]{StaticValue: r}),
			},
		}})
	}

	if outputBSFEnabled {
		encoderBSF := autofix.New(ctx, encoder, output.Kernel)
		encoderNode.AddPushPacketsTo(ctx, encoderBSF)
		encoderBSF.AddPushPacketsTo(ctx, outputNode)
	} else {
		encoderNode.AddPushPacketsTo(ctx, outputNode)
	}

	pipelineInputs := node.Nodes[node.Abstract]{
		inputMainNode,
		inputFallbackNode,
	}
	logger.Debugf(ctx, "resulting pipeline: %s", pipelineInputs.String())
	logger.Debugf(ctx, "resulting pipeline (for graphviz):\n%s\n", pipelineInputs.DotString(false))

	defer logger.Infof(ctx, "finishing: routing")

	// start

	errCh := make(chan node.Error, 10)
	observability.Go(ctx, func(ctx context.Context) {
		defer func() {
			logger.Debugf(ctx, "cancelling context...")
			cancelFn()
		}()
		avpipeline.Serve[node.Abstract](ctx, avpipeline.ServeConfig{
			EachNode: node.ServeConfig{
				FrameDropVideo: *frameDrop,
				FrameDropAudio: *frameDrop,
				FrameDropOther: *frameDrop,
			},
		}, errCh, pipelineInputs...)
	})
	logger.Infof(ctx, "started")

	// observe

	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Infof(ctx, "finished")
			return
		case err, ok := <-errCh:
			if !ok {
				logger.Infof(ctx, "closed")
				return
			}
			if errors.Is(err.Err, context.Canceled) {
				logger.Infof(ctx, "canceled")
				continue
			}
			if errors.Is(err.Err, io.EOF) {
				logger.Infof(ctx, "EOF")
				continue
			}
			if err.Err != nil {
				logger.Fatal(ctx, err)
				return
			}
		case <-statusTicker.C:
			inputMainStats := inputMainNode.GetCountersPtr()
			inputMainStatsJSON, err := json.Marshal(inputMainStats.Sent.Packets)
			assert(ctx, err == nil, err)

			inputFallbackStats := inputFallbackNode.GetCountersPtr()
			inputFallbackStatsJSON, err := json.Marshal(inputFallbackStats.Sent.Packets)
			assert(ctx, err == nil, err)

			outputStats := outputNode.GetCountersPtr()
			outputStatsJSON, err := json.Marshal(outputStats.Received.Packets)
			assert(ctx, err == nil, err)

			fmt.Printf("input-main:%s + input-fallback:%s -> output:%s\n", inputMainStatsJSON, inputFallbackStatsJSON, outputStatsJSON)
		}
	}
}
