// main.go is the main entry point for the fallback with transcoding demo.

// Package main is a demo application.
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
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	frameconditionextra "github.com/xaionaro-go/avpipeline/frame/condition/extra"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	framefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/framefilter/condition"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/limitframerate"
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
	"github.com/xaionaro-go/avpipeline/preset/inputwithfallback"
	"github.com/xaionaro-go/avpipeline/processor"
	goconvavp "github.com/xaionaro-go/avpipeline/protobuf/goconv/avpipeline"
	"github.com/xaionaro-go/avpipeline/quality"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
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

	inputs, err := inputwithfallback.New(
		ctx,
		[]inputwithfallback.InputFactory[*kernel.Input, *codec.NaiveDecoderFactory, struct{}]{
			&inputFactory{URL: fromURLMain},
			&inputFactory{URL: fromURLFallback},
		},
		inputwithfallback.OptionRetryInterval(*retryInterval),
	)
	assert(ctx, err == nil, err)

	// encoder

	logger.Debugf(ctx, "initializing encoder...")
	audioOptions := astiav.NewDictionary()
	audioOptions.Set("ac", fmt.Sprintf("%d", *encoderAudioChannels), 0)
	videoOptions := astiav.NewDictionary()
	videoOptions.Set("g", fmt.Sprintf("%d", *videoGOP), 0)
	encoder := kernel.NewEncoder(ctx,
		codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			HardwareDeviceType: globaltypes.HardwareDeviceTypeNone,
			VideoCodec:         codec.Name(*encoderVideoCodec),
			VideoQuality:       encoderQuality,
			VideoResolution:    encoderResolution,
			VideoOptions:       videoOptions,
			AudioCodec:         codec.Name(*encoderAudioCodec),
			AudioOptions:       audioOptions,
		}),
		nil,
	)
	encoderNode := node.NewFromKernel(ctx, encoder, processor.DefaultOptionsTranscoder()...)
	inputs.AddPushTo(ctx, encoderNode)

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

	outputFormatName := outputNode.Processor.Kernel.FormatContext.OutputFormat().Name()
	logger.Infof(ctx, "output format: '%s'", outputFormatName)

	if *videoMaxFPS >= 0 {
		r := globaltypes.RationalFromApproxFloat64(*videoMaxFPS)
		encoderNode.SetInputFilter(ctx, packetorframefiltercondition.FrameFilter{
			Condition: framefiltercondition.Frame{framecondition.Or{
				framecondition.Not{framecondition.MediaType(astiav.MediaTypeVideo)},
				frameconditionextra.PacketOrFrame{
					limitframerate.New(mathcondition.GetterStatic[globaltypes.Rational]{StaticValue: r}),
				},
			}},
		})
	}

	if outputBSFEnabled {
		encoderBSF := autoheaders.NewNode(ctx, output.Kernel)
		encoderNode.AddPushTo(ctx, encoderBSF)
		encoderBSF.AddPushTo(ctx, outputNode)
	} else {
		encoderNode.AddPushTo(ctx, outputNode)
	}

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
		}, errCh, inputs)
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

			fmt.Printf(
				"inputs: %s\ninput main: %s\ninput fallback: %s\noutput:%s\n",
				inputs,
				mustJSON(ctx, inputs.InputChains[0].Input.GetProcessor().CountersPtr().Generated.Packets.ToStats()),
				mustJSON(ctx, inputs.InputChains[1].Input.GetProcessor().CountersPtr().Generated.Packets.ToStats()),
				mustJSON(ctx, outputNode.GetCountersPtr().Received.Packets.ToStats()),
			)
			logger.Debugf(ctx, "main pipeline: %s", mustJSON(ctx, goconvavp.NodeToGRPC(ctx, inputs.InputChains[0].Input)))
			logger.Debugf(ctx, "fallback pipeline: %s", mustJSON(ctx, goconvavp.NodeToGRPC(ctx, inputs.InputChains[1].Input)))
		}
	}
}

func mustJSON(ctx context.Context, v any) string {
	b, err := json.Marshal(v)
	assert(ctx, err == nil, err)
	return string(b)
}
