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
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

func main() {
	flag.Parse()
	ctx := withLogger(context.Background(), logger.LevelTrace)
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	defer belt.Flush(ctx)

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

		// Find decoder
		if s.decCodec = astiav.FindDecoder(is.CodecParameters().CodecID()); s.decCodec == nil {
			panic(errors.New("main: codec is nil"))
		}

		// Allocate codec context
		if s.decCodecContext = astiav.AllocCodecContext(s.decCodec); s.decCodecContext == nil {
			panic(errors.New("main: codec context is nil"))
		}
		c.Add(s.decCodecContext.Free)

		// Update codec context
		if err = is.CodecParameters().ToCodecContext(s.decCodecContext); err != nil {
			panic(fmt.Errorf("main: updating codec context failed: %w", err))
		}

		// Set framerate
		if is.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			s.decCodecContext.SetFramerate(inputFormatContext.GuessFrameRate(is, nil))
		}

		// Open codec context
		if err = s.decCodecContext.Open(s.decCodec, nil); err != nil {
			panic(fmt.Errorf("main: opening codec context failed: %w", err))
		}

		// Set time base
		s.decCodecContext.SetTimeBase(is.TimeBase())

		// Allocate frame
		s.decFrame = astiav.AllocFrame()
		c.Add(s.decFrame.Free)

		// Store stream
		streams[is.Index()] = s
	}

	// Loop through output streams
	for _, s := range streams {
		// Allocate packet
		s.encPkt = astiav.AllocPacket()
		c.Add(s.encPkt.Free)
	}
	// Loop through streams
	for _, is := range inputFormatContext.Streams() {
		// Get stream
		s, ok := streams[is.Index()]
		if !ok {
			continue
		}

		// Create output stream
		if s.outputStream = output.FormatContext.NewStream(nil); s.outputStream == nil {
			panic(errors.New("main: output stream is nil"))
		}

		// Get codec id
		codecID := astiav.CodecIDH264
		if s.decCodecContext.MediaType() == astiav.MediaTypeAudio {
			codecID = astiav.CodecIDAac
		}

		// Find encoder
		if s.encCodec = astiav.FindEncoder(codecID); s.encCodec == nil {
			panic(errors.New("main: codec is nil"))
		}

		// Allocate codec context
		if s.encCodecContext = astiav.AllocCodecContext(s.encCodec); s.encCodecContext == nil {
			panic(errors.New("main: codec context is nil"))
		}
		c.Add(s.encCodecContext.Free)

		// Update codec context
		if s.decCodecContext.MediaType() == astiav.MediaTypeAudio {
			if v := s.encCodec.ChannelLayouts(); len(v) > 0 {
				s.encCodecContext.SetChannelLayout(v[0])
			} else {
				s.encCodecContext.SetChannelLayout(s.decCodecContext.ChannelLayout())
			}
			s.encCodecContext.SetSampleRate(s.decCodecContext.SampleRate())
			if v := s.encCodec.SampleFormats(); len(v) > 0 {
				s.encCodecContext.SetSampleFormat(v[0])
			} else {
				s.encCodecContext.SetSampleFormat(s.decCodecContext.SampleFormat())
			}
			s.encCodecContext.SetTimeBase(astiav.NewRational(1, s.encCodecContext.SampleRate()))
		} else {
			s.encCodecContext.SetHeight(s.decCodecContext.Height())
			if v := s.encCodec.PixelFormats(); len(v) > 0 {
				s.encCodecContext.SetPixelFormat(v[0])
			} else {
				s.encCodecContext.SetPixelFormat(s.decCodecContext.PixelFormat())
			}
			s.encCodecContext.SetSampleAspectRatio(s.decCodecContext.SampleAspectRatio())
			s.encCodecContext.SetTimeBase(s.decCodecContext.TimeBase())
			s.encCodecContext.SetWidth(s.decCodecContext.Width())
		}

		// Open codec context
		if err = s.encCodecContext.Open(s.encCodec, nil); err != nil {
			panic(fmt.Errorf("main: opening codec context failed: %w", err))
		}

		// Update codec parameters
		if err = s.outputStream.CodecParameters().FromCodecContext(s.encCodecContext); err != nil {
			panic(fmt.Errorf("main: updating codec parameters failed: %w", err))
		}

		// Update stream
		s.outputStream.SetTimeBase(s.encCodecContext.TimeBase())
	}

	// Write header
	if err = output.FormatContext.WriteHeader(nil); err != nil {
		panic(fmt.Errorf("main: writing header failed: %w", err))
	}

	pkt := astiav.AllocPacket()
	c.Add(pkt.Free)

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

			// Update packet
			pkt.RescaleTs(s.inputStream.TimeBase(), s.decCodecContext.TimeBase())

			// Send packet
			if err := s.decCodecContext.SendPacket(pkt); err != nil {
				log.Fatal(fmt.Errorf("main: sending packet failed: %w", err))
			}

			// Loop
			for {
				// We use a closure to ease unreferencing the frame
				if stop := func() bool {
					// Receive frame
					if err := s.decCodecContext.ReceiveFrame(s.decFrame); err != nil {
						if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
							return true
						}
						log.Fatal(fmt.Errorf("main: receiving frame failed: %w", err))
					}

					// Make sure to unreference the frame
					defer s.decFrame.Unref()

					// Filter, encode and write frame
					err := func() (err error) {
						// Send frame
						if err = s.encCodecContext.SendFrame(s.decFrame); err != nil {
							err = fmt.Errorf("main: sending frame failed: %w", err)
							return
						}

						// Loop
						for {
							// We use a closure to ease unreferencing the packet
							if stop, err := func() (bool, error) {
								// Receive packet
								if err := s.encCodecContext.ReceivePacket(s.encPkt); err != nil {
									if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
										return true, nil
									}
									return false, fmt.Errorf("main: receiving packet failed: %w", err)
								}

								// Make sure to unreference packet
								defer s.encPkt.Unref()

								// Update pkt
								s.encPkt.SetStreamIndex(s.outputStream.Index())
								s.encPkt.RescaleTs(s.encCodecContext.TimeBase(), s.outputStream.TimeBase())

								err := output.FormatContext.WriteInterleavedFrame(s.encPkt)
								// Write frame
								if err != nil {
									return false, fmt.Errorf("main: writing frame failed: %w", err)
								}
								return false, nil
							}(); err != nil {
								return err
							} else if stop {
								break
							}
						}
						return
					}()
					if err != nil {
						log.Fatal(fmt.Errorf("main: filtering, encoding and writing frame failed: %w", err))
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
	decCodec        *astiav.Codec
	decCodecContext *astiav.CodecContext
	decFrame        *astiav.Frame
	encCodec        *astiav.Codec
	encCodecContext *astiav.CodecContext
	encPkt          *astiav.Packet
	inputStream     *astiav.Stream
	outputStream    *astiav.Stream
}
