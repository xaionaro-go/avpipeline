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
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/spf13/pflag"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	packetfiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetfilter/condition"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	packetorframecondition "github.com/xaionaro-go/avpipeline/packetorframe/condition"
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
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

	if *netPprofAddr != "" {
		observability.Go(ctx, func(context.Context) { l.Error(http.ListenAndServe(*netPprofAddr, nil)) })
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
	var transcoders []*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]
	for _, vcodec := range *videoCodecs {
		hwDeviceName := codec.HardwareDeviceName(*hwDeviceName)
		encoderFactory := codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			VideoCodec:         codec.Name(vcodec),
			AudioCodec:         codec.NameCopy,
			HardwareDeviceType: 0,
			HardwareDeviceName: hwDeviceName,
		})
		transcoder, err := kernel.NewTranscoder(
			ctx,
			codec.NewNaiveDecoderFactory(ctx, &codec.NaiveDecoderFactoryParams{
				HardwareDeviceName: hwDeviceName,
			}),
			encoderFactory,
			nil,
		)
		if err != nil {
			l.Fatal(err)
		}
		defer transcoder.Close(ctx)
		l.Debugf("initialized a transcoder to %s (hwdev:%s)...", *videoCodecs, hwDeviceName)
		transcoders = append(transcoders, transcoder)
	}
	var transcodingNode node.Abstract
	var sw *kernel.Switch[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]
	switch len(transcoders) {
	case 0:
	case 1:
		transcodingNode = node.NewFromKernel(
			ctx,
			transcoders[0],
			processor.DefaultOptionsTranscoder()...,
		)
	default:
		sw = kernel.NewSwitch(transcoders...)
		sw.SetVerifySwitchOutput(packetorframecondition.And{
			packetorframecondition.MediaType(astiav.MediaTypeVideo),
			packetorframecondition.IsKeyFrame(true),
		})
		transcodingNode = node.NewFromKernel(
			ctx,
			sw,
			processor.DefaultOptionsTranscoder()...,
		)
	}
	if transcodingNode != nil {
		inputNode.AddPushTo(ctx, transcodingNode)
		finalNode = transcodingNode

		if len(*alternateBitrate) >= 2 || len(*videoCodecs) >= 2 {
			transcodingNode.SetInputFilter(ctx,
				packetorframefiltercondition.PacketFilter{
					Condition: packetfiltercondition.Packet{
						condition.Function(sillyAlternationsJustForDemonstration(
							sw,
							*alternateBitrate,
						)),
					},
				},
			)
		}
	}
	finalNode.AddPushTo(ctx, node.New(output))
	pushTos := finalNode.GetPushTos(ctx)
	assert(ctx, len(pushTos) == 1, len(pushTos))

	l.Debugf("resulting pipeline: %s", inputNode.String())
	l.Debugf("resulting pipeline (for graphviz):\n%s\n", inputNode.DotString(false))

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
			inputStats := inputNode.GetCountersPtr()
			inputStatsJSON, err := json.Marshal(inputStats.Sent.Packets.ToStats())
			if err != nil {
				l.Fatal(err)
			}
			outputStats := pushTos[0].Node.GetCountersPtr()
			outputStatsJSON, err := json.Marshal(outputStats.Received.Packets.ToStats())
			if err != nil {
				l.Fatal(err)
			}
			fmt.Printf("input:%s -> output:%s\n", inputStatsJSON, outputStatsJSON)
		}
	}
}
