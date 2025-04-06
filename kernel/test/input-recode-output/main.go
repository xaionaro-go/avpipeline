package main

import (
	"context"
	"flag"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

func main() {
	flag.Parse()
	ctx := withLogger(context.Background(), logger.LevelTrace)
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	defer belt.Flush(ctx)

	input, err := kernel.NewInputFromURL(ctx, flag.Arg(0), secret.New(""), kernel.InputConfig{})
	assert(ctx, err == nil, err)
	defer input.Close(ctx)

	recoder, err := kernel.NewRecoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, 0, "", nil, nil),
		codec.NewNaiveEncoderFactory(ctx, "libx264", "aac", 0, "", types.DictionaryItems{
			{Key: "bf", Value: "0"}, // to disable B-frames
		}, nil),
		nil,
	)
	assert(ctx, err == nil, err)
	defer recoder.Close(ctx)

	output, err := kernel.NewOutputFromURL(ctx, flag.Arg(1), secret.New(""), kernel.OutputConfig{})
	assert(ctx, err == nil, err)
	defer output.Close(ctx)

	inputCh := make(chan packet.Output, 1000)
	observability.Go(ctx, func() {
		defer func() {
			close(inputCh)
		}()
		err := input.Generate(ctx, inputCh, nil)
		assert(ctx, err == nil, err)
	})

	outputCh := make(chan packet.Output, 1000)
	for pkt := range inputCh {
		assert(ctx, pkt.FormatContext != nil)
		err := recoder.SendInputPacket(ctx, packet.BuildInput(
			pkt.Packet,
			pkt.Stream,
			pkt.FormatContext,
		), outputCh, nil)
		assert(ctx, err == nil, err)

		for {
			select {
			case pkt, ok := <-outputCh:
				if !ok {
					break
				}
				err := output.SendInputPacket(ctx, packet.BuildInput(
					pkt.Packet,
					pkt.Stream,
					pkt.FormatContext,
				), nil, nil)
				assert(ctx, err == nil, err)
				continue
			default:
			}
			break
		}
	}
}

func withLogger(ctx context.Context, loggerLevel logger.Level) context.Context {
	runtime.DefaultCallerPCFilter = observability.CallerPCFilter(runtime.DefaultCallerPCFilter)
	l := logrus.Default().WithLevel(loggerLevel)
	ctx = logger.CtxWithLogger(context.Background(), l)
	logger.Default = func() logger.Logger {
		return l
	}

	astiav.SetLogLevel(avpipeline.LogLevelToAstiav(l.Level()))
	astiav.SetLogCallback(func(c astiav.Classer, level astiav.LogLevel, fmt, msg string) {
		var cs string
		if c != nil {
			if cl := c.Class(); cl != nil {
				cs = " - class: " + cl.String()
			}
		}
		logger.Logf(ctx,
			avpipeline.LogLevelFromAstiav(level),
			"%s%s",
			strings.TrimSpace(msg), cs,
		)
	})

	return ctx
}
