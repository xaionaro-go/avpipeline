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

	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/types"
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
	videoCodec := pflag.String("vcodec", "copy", "")
	hwDeviceName := pflag.String("hwdevice", "", "")
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
	inputNode := avpipeline.NewNode(input)

	// output node

	logger.Debugf(ctx, "opening '%s' as the output...", toURL)
	output, err := processor.NewOutputFromURL(
		ctx,
		toURL, secret.New(""),
		kernel.OutputConfig{},
	)
	assert(ctx, err == nil, err)
	defer output.Close(ctx)
	outputNode := avpipeline.NewNode(output)

	// recoder node

	hwDevName := codec.HardwareDeviceName(*hwDeviceName)
	recoder, err := processor.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, 0, hwDevName, nil, nil),
		codec.NewNaiveEncoderFactory(ctx, *videoCodec, "copy", 0, hwDevName, types.DictionaryItems{
			{Key: "bf", Value: "0"}, // to disable B-frames
		}, nil),
		nil,
	)
	assert(ctx, err == nil, err)
	defer recoder.Close(ctx)
	logger.Debugf(ctx, "initialized a recoder to %s (hwdev:%s)...", *videoCodec, hwDeviceName)
	recodingNode := avpipeline.NewNode(recoder)

	// route nodes: input -> recoder -> output

	inputNode.AddPushPacketsTo(recodingNode)
	recodingNode.AddPushPacketsTo(outputNode)
	logger.Debugf(ctx, "resulting pipeline: %s", inputNode.String())
	logger.Debugf(ctx, "resulting pipeline (for graphviz):\n%s\n", inputNode.DotString(false))

	// start

	errCh := make(chan avpipeline.ErrNode, 10)
	observability.Go(ctx, func() {
		defer cancelFn()
		avpipeline.ServeRecursively(ctx, avpipeline.ServeConfig{
			FrameDrop: *frameDrop,
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
			inputStats := inputNode.GetStats()
			inputStatsJSON, err := json.Marshal(inputStats.FramesWrote)
			assert(ctx, err == nil, err)

			outputStats := outputNode.GetStats()
			outputStatsJSON, err := json.Marshal(outputStats.FramesRead)
			assert(ctx, err == nil, err)

			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		}
	}
}
