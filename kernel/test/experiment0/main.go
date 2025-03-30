// this file borrowed a lot of code from from https://github.com/asticode/go-astiav/blob/eb1dc676fe35e555396afdf73fd4c28b0ddb6118/examples/transcoding/main.go

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/asticode/go-astiav"
	"github.com/asticode/go-astikit"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/logger"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
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

	decoder := kernel.NewDecoder(ctx, codec.NewNaiveDecoderFactory(ctx, 0, "", nil))
	defer decoder.Close(ctx)

	encoder := kernel.NewEncoder(
		ctx,
		codec.NewNaiveEncoderFactory(ctx, "libx264", "aac", 0, "", types.DictionaryItems{
			{Key: "bf", Value: "0"}, // to disable B-frames
		}),
		nil,
	)
	defer encoder.Close(ctx)

	output, err := kernel.NewOutputFromURL(ctx, flag.Arg(1), secret.New(""), kernel.OutputConfig{})
	assert(ctx, err == nil, err)
	defer output.Close(ctx)

	var (
		c                  = astikit.NewCloser()
		inputFormatContext *astiav.FormatContext
		streams            = make(map[int]*stream) // Indexed by input stream index
	)

	// We use an astikit.Closer to free all resources properly
	defer c.Close()

	// Allocate input format context
	if inputFormatContext = astiav.AllocFormatContext(); inputFormatContext == nil {
		panic(errors.New("main: input format context is nil"))
	}
	c.Add(inputFormatContext.Free)

	// Open input
	if err = inputFormatContext.OpenInput(flag.Arg(0), nil, nil); err != nil {
		panic(fmt.Errorf("main: opening input failed: %w", err))
	}
	c.Add(inputFormatContext.CloseInput)

	// Find stream info
	if err = inputFormatContext.FindStreamInfo(nil); err != nil {
		panic(fmt.Errorf("main: finding stream info failed: %w", err))
	}

	// Loop through streams
	for _, is := range inputFormatContext.Streams() {
		// Only process audio or video
		if is.CodecParameters().MediaType() != astiav.MediaTypeAudio &&
			is.CodecParameters().MediaType() != astiav.MediaTypeVideo {
			continue
		}

		// Create stream
		s := &stream{inputStream: is}
		// Store stream
		streams[is.Index()] = s
	}

	pkt := astiav.AllocPacket()
	c.Add(pkt.Free)

	decoderCh := make(chan frame.Output, 10000)
	encoderCh := make(chan packet.Output, 10000)

	// Loop through packets
	for {
		// We use a closure to ease unreferencing the packet
		if stop := func() bool {
			// Read frame
			if err := inputFormatContext.ReadFrame(pkt); err != nil {
				if errors.Is(err, astiav.ErrEof) {
					return true
				}
				log.Fatal(fmt.Errorf("main: reading frame failed: %w", err))
			}

			// Make sure to unreference the packet
			defer pkt.Unref()

			if pkt.StreamIndex() != 0 {
				return false
			}

			// Get stream
			s, ok := streams[pkt.StreamIndex()]
			if !ok {
				return false
			}

			err := decoder.SendInputPacket(ctx, packet.BuildInput(pkt, s.inputStream, inputFormatContext), nil, decoderCh)
			if err != nil {
				panic(err)
			}

			for {
				if stop := func() bool {
					var f frame.Output
					logger.Debugf(ctx, "checking if there is a frame")
					select {
					case f = <-decoderCh:
					default:
						return true
					}

					// Make sure to unreference packet
					defer f.Unref()

					if f.GetTimeBase().Num() == 0 {
						panic(fmt.Errorf("internal error: TimeBase is not set"))
					}

					err := encoder.SendInputFrame(
						ctx,
						frame.Input(f),
						encoderCh,
						nil,
					)
					if err != nil {
						panic(err)
					}

					for {
						if stop := func() bool {
							var pkt packet.Output
							logger.Debugf(ctx, "checking if there is a packet")
							select {
							case pkt = <-encoderCh:
							default:
								return true
							}

							// Make sure to unreference packet
							defer pkt.Unref()

							logger.Debugf(ctx, "starting frame")

							inPkt := packet.BuildInput(
								pkt.Packet, pkt.Stream, pkt.FormatContext,
							)
							err = output.SendInputPacket(ctx, inPkt, nil, nil)
							// Write frame
							if err != nil {
								panic(fmt.Errorf("main: writing frame failed: %w", err))
							}
							return false
						}(); stop {
							break
						}
					}
					return false
				}(); stop {
					break
				}
			}
			return false
		}(); stop {
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

type stream struct {
	inputStream *astiav.Stream
}
