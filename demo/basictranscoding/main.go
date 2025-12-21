package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	_ "net/http/pprof"
	"os"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	framecondition "github.com/xaionaro-go/avpipeline/frame/condition"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/reduceframerate"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	xastiav "github.com/xaionaro-go/avpipeline/types/astiav"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

func main() {

	// parse the input

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: %s <URL-from> <URL-to>\n", os.Args[0])
		pflag.PrintDefaults()
	}

	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	videoCodec := pflag.String("vcodec", string(codec.NameCopy), "")
	hwDeviceName := pflag.String("hwdevice", "", "")
	scaleVideoFrameRate := pflag.Float64("scale-video-framerate", 1, "")
	frameDrop := pflag.Bool("framedrop", false, "")

	pflag.Parse()
	if len(pflag.Args()) != 2 {
		pflag.Usage()
		os.Exit(1)
	}

	fromURL := pflag.Arg(0)
	toURL := pflag.Arg(1)

	// init the context

	ctx := withLogger(context.Background(), loggerLevel)
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	defer belt.Flush(ctx)

	// input node

	logger.Debugf(ctx, "opening '%s' as the input...", fromURL)
	input, err := processor.NewInputFromURL(ctx, fromURL, secret.New(""), kernel.InputConfig{})
	assert(ctx, err == nil, err)
	defer input.Close(ctx)
	inputNode := node.New(input)

	// output node

	logger.Debugf(ctx, "opening '%s' as the output...", toURL)
	output, err := processor.NewOutputFromURL(
		ctx,
		toURL, secret.New(""),
		kernel.OutputConfig{},
	)
	assert(ctx, err == nil, err)
	defer output.Close(ctx)
	outputNode := node.New(output)

	// transcoder node

	hwDevName := codec.HardwareDeviceName(*hwDeviceName)
	transcoder, err := processor.NewTranscoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, &codec.NaiveDecoderFactoryParams{
			HardwareDeviceName: hwDevName,
		}),
		codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			VideoCodec:         codec.Name(*videoCodec),
			AudioCodec:         codec.NameCopy,
			HardwareDeviceType: 0,
			HardwareDeviceName: hwDevName,
			VideoOptions:       xastiav.DictionaryItemsToAstiav(ctx, globaltypes.DictionaryItems{{Key: "bf", Value: "0"}}),
		}),
		nil,
	)
	assert(ctx, err == nil, err)
	defer transcoder.Close(ctx)
	logger.Debugf(ctx, "initialized a transcoder to %s (hwdev:%s)...", *videoCodec, hwDeviceName)
	transcodingNode := node.New(transcoder)

	// configure optional FPS downscaling

	if *scaleVideoFrameRate != 1 {
		if *videoCodec == string(codec.NameCopy) {
			logger.Fatalf(ctx, "scaling video framerate down with 'copy' codec is not supported, please choose a re-encoding codec")
		}
		logger.Debugf(ctx, "scaling video framerate down by %f...", *scaleVideoFrameRate)
		fps := globaltypes.RationalFromApproxFloat64(*scaleVideoFrameRate)
		if fps.Float64() > 1 {
			logger.Fatalf(ctx, "scaling video framerate up is not supported in this demo, yet: %v", fps.Float64())
		}
		transcodingNode.Processor.Kernel.Filter = framecondition.Or{
			framecondition.Not{framecondition.MediaType(astiav.MediaTypeVideo)},
			framecondition.PacketOrFrame{
				reduceframerate.New(mathcondition.GetterStatic[globaltypes.Rational]{
					StaticValue: fps,
				}),
			},
		}
	}

	// route nodes: input -> transcoder -> output

	inputNode.AddPushPacketsTo(ctx, transcodingNode)
	transcodingNode.AddPushPacketsTo(ctx, outputNode)
	logger.Debugf(ctx, "resulting pipeline: %s", inputNode.String())
	logger.Debugf(ctx, "resulting pipeline (for graphviz):\n%s\n", inputNode.DotString(false))

	// start

	errCh := make(chan node.Error, 10)
	observability.Go(ctx, func(ctx context.Context) {
		defer cancelFn()
		avpipeline.Serve(ctx, avpipeline.ServeConfig{
			EachNode: node.ServeConfig{
				FrameDropVideo: *frameDrop,
				FrameDropAudio: *frameDrop,
				FrameDropOther: *frameDrop,
			},
		}, errCh, inputNode)
	})

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
				return
			}
			if errors.Is(err.Err, context.Canceled) {
				continue
			}
			if errors.Is(err.Err, io.EOF) {
				continue
			}
			if err.Err != nil {
				logger.Fatal(ctx, err)
				return
			}
		case <-statusTicker.C:
			inputStats := inputNode.GetCountersPtr()
			inputStatsJSON, err := json.Marshal(inputStats.Sent.Packets.ToStats())
			assert(ctx, err == nil, err)

			outputStats := outputNode.GetCountersPtr()
			outputStatsJSON, err := json.Marshal(outputStats.Received.Packets.ToStats())
			assert(ctx, err == nil, err)

			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		}
	}
}
