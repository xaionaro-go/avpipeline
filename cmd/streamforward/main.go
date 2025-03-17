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
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/quality"
	"github.com/xaionaro-go/observability"
)

func main() {
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: %s <URL-from> <URL-to>\n", os.Args[0])
		pflag.PrintDefaults()
	}

	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	netPprofAddr := pflag.String("net-pprof-listen-addr", "", "an address to listen for incoming net/pprof connections")
	videoCodec := pflag.String("vcodec", "copy", "")
	hwDeviceName := pflag.String("hwdevice", "", "")
	frameDrop := pflag.Bool("framedrop", false, "")
	alternateBitrate := pflag.IntSlice("silly-stuff-alternate-bitrate", nil, "")

	pflag.Parse()
	if len(pflag.Args()) != 2 {
		pflag.Usage()
		os.Exit(1)
	}

	runtime.DefaultCallerPCFilter = observability.CallerPCFilter(runtime.DefaultCallerPCFilter)
	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	ctx, cancelFn := context.WithCancel(ctx)
	logger.Default = func() logger.Logger {
		return l
	}
	defer belt.Flush(ctx)

	if *netPprofAddr != "" {
		observability.Go(ctx, func() { l.Error(http.ListenAndServe(*netPprofAddr, nil)) })
	}

	fromURL := pflag.Arg(0)
	toURL := pflag.Arg(1)

	astiav.SetLogLevel(avpipeline.LogLevelToAstiav(l.Level()))
	astiav.SetLogCallback(func(c astiav.Classer, level astiav.LogLevel, fmt, msg string) {
		var cs string
		if c != nil {
			if cl := c.Class(); cl != nil {
				cs = " - class: " + cl.String()
			}
		}
		l.Logf(
			avpipeline.LogLevelFromAstiav(level),
			"%s%s",
			strings.TrimSpace(msg), cs,
		)
	})

	l.Debugf("opening '%s' as the input...", fromURL)
	input, err := avpipeline.NewInputFromURL(ctx, fromURL, "", avpipeline.InputConfig{})
	if err != nil {
		l.Fatal(err)
	}
	defer input.Close()

	l.Debugf("opening '%s' as the output...", toURL)
	output, err := avpipeline.NewOutputFromURL(
		ctx,
		toURL, "",
		avpipeline.OutputConfig{},
	)
	if err != nil {
		l.Fatal(err)
	}
	defer output.Close()

	errCh := make(chan avpipeline.ErrPipeline, 10)
	inputNode := avpipeline.NewPipelineNode(input)
	finalNode := inputNode
	var encoderFactory *avpipeline.NaiveEncoderFactory
	if *videoCodec != "copy" {
		hwDeviceName := avpipeline.HardwareDeviceName(*hwDeviceName)
		encoderFactory = avpipeline.NewNaiveEncoderFactory(*videoCodec, "copy", 0, hwDeviceName)
		recoder, err := avpipeline.NewRecoder(
			ctx,
			avpipeline.NewNaiveDecoderFactory(0, hwDeviceName),
			encoderFactory,
			nil,
		)
		if err != nil {
			l.Fatal(err)
		}
		defer recoder.Close()
		l.Debugf("initialized a recoder to %s (hwdev:%s)...", *videoCodec, hwDeviceName)
		recodingNode := avpipeline.NewPipelineNode(recoder)
		inputNode.PushTo.Add(recodingNode)
		finalNode = recodingNode
	}
	finalNode.PushTo.Add(avpipeline.NewPipelineNode(output))
	assert(ctx, len(finalNode.PushTo) == 1, len(finalNode.PushTo))

	pipeline := inputNode
	l.Debugf("resulting pipeline: %s", pipeline.String())
	l.Debugf("resulting pipeline (for graphviz):\n%s\n", pipeline.DotString(false))

	observability.Go(ctx, func() {
		defer cancelFn()
		pipeline.Serve(ctx, avpipeline.PipelineServeConfig{
			FrameDrop: *frameDrop,
		}, errCh)
	})

	bitrateIdx := 0
	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()
	codecTicker := time.NewTimer(3 * time.Second)
	for {
		select {
		case <-ctx.Done():
			l.Infof("finished")
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
				l.Fatal(err)
				return
			}
		case <-statusTicker.C:
			inputStats := inputNode.GetStats()
			inputStatsJSON, err := json.Marshal(inputStats)
			if err != nil {
				l.Fatal(err)
			}
			outputStats := finalNode.PushTo[0].GetStats()
			outputStatsJSON, err := json.Marshal(outputStats)
			if err != nil {
				l.Fatal(err)
			}
			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		case <-codecTicker.C:
			if encoderFactory == nil {
				logger.Debugf(ctx, "encoderFactory == nil")
				continue
			}
			if len(*alternateBitrate) == 0 {
				logger.Debugf(ctx, "len(*alternateBitrate) == 0")
				continue
			}
			nextBitrate := (*alternateBitrate)[bitrateIdx]
			bitrateIdx++
			if bitrateIdx >= len(*alternateBitrate) {
				bitrateIdx = bitrateIdx % len(*alternateBitrate)
			}
			encoderFactory.Locker.Do(ctx, func() {
				for _, encoder := range encoderFactory.VideoEncoders {
					err := encoder.SetQuality(ctx, quality.ConstantBitrate(nextBitrate))
					if err != nil {
						logger.Errorf(ctx, "SetQuality errored: %v", err)
						continue
					}
				}
			})
			fmt.Printf("changed the video bitrate to %d\n", nextBitrate)
		}
	}
}
