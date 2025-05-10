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
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

func main() {
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "syntax: %s <URL-from> <URL-to>\n", os.Args[0])
		pflag.PrintDefaults()
	}

	loggerLevel := logger.LevelWarning
	pflag.Var(&loggerLevel, "log-level", "Log level")
	netPprofAddr := pflag.String("net-pprof-listen-addr", "", "an address to listen for incoming net/pprof connections")
	videoCodecs := pflag.StringSlice("vcodec", nil, "")
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
	input, err := processor.NewInputFromURL(ctx, fromURL, secret.New(""), kernel.InputConfig{})
	if err != nil {
		l.Fatal(err)
	}
	defer input.Close(ctx)

	l.Debugf("opening '%s' as the output...", toURL)
	output, err := processor.NewOutputFromURL(
		ctx,
		toURL, secret.New(""),
		kernel.OutputConfig{},
	)
	if err != nil {
		l.Fatal(err)
	}
	defer output.Close(ctx)

	errCh := make(chan node.Error, 10)
	inputNode := node.New(input)
	var finalNode node.Abstract
	finalNode = inputNode
	var recoders []*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]
	for _, vcodec := range *videoCodecs {
		hwDeviceName := codec.HardwareDeviceName(*hwDeviceName)
		encoderFactory := codec.NewNaiveEncoderFactory(ctx, vcodec, "copy", 0, hwDeviceName, nil, nil)
		recoder, err := kernel.NewRecoder(
			ctx,
			codec.NewNaiveDecoderFactory(ctx, 0, hwDeviceName, nil, nil),
			encoderFactory,
			nil,
		)
		if err != nil {
			l.Fatal(err)
		}
		defer recoder.Close(ctx)
		l.Debugf("initialized a recoder to %s (hwdev:%s)...", *videoCodecs, hwDeviceName)
		recoders = append(recoders, recoder)
	}
	var recodingNode node.Abstract
	var sw *kernel.Switch[*kernel.Recoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]
	switch len(recoders) {
	case 0:
	case 1:
		recodingNode = node.NewFromKernel(
			ctx,
			recoders[0],
			processor.DefaultOptionsRecoder()...,
		)
	default:
		sw = kernel.NewSwitch(recoders...)
		sw.SetVerifySwitchOutput(condition.And{
			condition.MediaType(astiav.MediaTypeVideo),
			condition.IsKeyFrame(true),
		})
		recodingNode = node.NewFromKernel(
			ctx,
			sw,
			processor.DefaultOptionsRecoder()...,
		)
	}
	if recodingNode != nil {
		inputNode.PushPacketsTos.Add(recodingNode)
		finalNode = recodingNode

		if len(*alternateBitrate) >= 2 || len(*videoCodecs) >= 2 {
			recodingNode.SetInputPacketCondition(
				condition.Function(sillyAlternationsJustForDemonstration(
					sw,
					*alternateBitrate,
				)),
			)
		}
	}
	finalNode.AddPushPacketsTo(node.New(output))
	assert(ctx, len(finalNode.GetPushPacketsTos()) == 1, len(finalNode.GetPushPacketsTos()))

	l.Debugf("resulting pipeline: %s", inputNode.String())
	l.Debugf("resulting pipeline (for graphviz):\n%s\n", inputNode.DotString(false))

	observability.Go(ctx, func() {
		defer cancelFn()
		avpipeline.Serve(ctx, avpipeline.ServeConfig{
			EachNode: node.ServeConfig{
				FrameDrop: *frameDrop,
			},
		}, errCh, inputNode)
	})

	statusTicker := time.NewTicker(time.Second)
	defer statusTicker.Stop()
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
			outputStats := finalNode.GetPushPacketsTos()[0].Node.GetStatistics()
			outputStatsJSON, err := json.Marshal(outputStats)
			if err != nil {
				l.Fatal(err)
			}
			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		}
	}
}
