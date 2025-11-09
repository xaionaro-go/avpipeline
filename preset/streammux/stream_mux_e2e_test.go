package streammux_test

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/pkg/runtime"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/preset/streammux"
	streammuxtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

const (
	e2eNvencEnable       = true
	e2eForceTestDraining = false
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func ptr[T any](v T) *T {
	return &v
}

func TestE2E(t *testing.T) {
	loggerLevel := logger.LevelTrace
	runtime.DefaultCallerPCFilter = observability.CallerPCFilter(runtime.DefaultCallerPCFilter)
	l := logrus.Default().WithLevel(loggerLevel)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger {
		return l
	})
	defer belt.Flush(ctx)

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

	for _, fileName := range []string{"video0-1v1a.mov"} {
		t.Run(fileName, func(t *testing.T) {
			input, cancelFn := readInputFromFile(ctx, t, fileName)
			defer cancelFn()

			for _, muxMode := range []streammuxtypes.MuxMode{
				streammuxtypes.MuxModeDifferentOutputsSameTracks,
			} {
				t.Run(fmt.Sprintf("mode=%s", muxMode), func(t *testing.T) {
					for _, codecID := range []astiav.CodecID{astiav.CodecIDH264, astiav.CodecIDHevc} {
						t.Run(fmt.Sprintf("codec=%s", codecID), func(t *testing.T) {
							for i := 0; i < 1; i++ {
								t.Run(fmt.Sprintf("run=%d", i), func(t *testing.T) {
									runTest(ctx, t, input, muxMode, codecID)
								})
							}
						})
					}
				})
			}
		})
	}
}

func readInputFromFile(ctx context.Context, t *testing.T, fileName string) ([]packet.Input, context.CancelFunc) {
	inputKernel := must(kernel.NewInputFromURL(
		ctx,
		path.Join("testdata", fileName),
		secret.New(""),
		kernel.InputConfig{
			KeepOpen: true,
		},
	))

	var input []packet.Input
	ch := make(chan packet.Output)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		for p := range ch {
			input = append(input, packet.BuildInput(p.Packet, p.StreamInfo))
		}
	})
	require.NoError(t, inputKernel.Generate(ctx, ch, nil))
	close(ch)
	wg.Wait()
	require.NotEmpty(t, input)

	return input, func() {
		require.NoError(t, inputKernel.Close(ctx))
	}
}

type decoderOutputFactory struct{}

var _ streammux.SenderFactory = (*decoderOutputFactory)(nil)

func (decoderOutputFactory) NewSender(
	ctx context.Context,
	outputKey streammux.SenderKey,
) (streammux.SendingNode, streammuxtypes.SenderConfig, error) {
	n := node.NewWithCustomDataFromKernel[streammux.OutputCustomData](
		ctx,
		kernel.NewDecoder(ctx, &codec.NaiveDecoderFactory{}),
		processor.DefaultOptionsRecoder()...,
	)
	return n, streammuxtypes.SenderConfig{}, nil
}

func runTest(
	ctx context.Context,
	t *testing.T,
	input []packet.Input,
	muxMode streammuxtypes.MuxMode,
	codecID astiav.CodecID,
) {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	outputFactory := decoderOutputFactory{}

	streamMux := must(streammux.New(
		ctx,
		muxMode,
		ptr(streammux.DefaultAutoBitrateConfig(codecID)),
		outputFactory,
	))

	var vcodecName codectypes.Name
	var hardwareDeviceType codec.HardwareDeviceType
	if e2eNvencEnable {
		vcodecName = codectypes.Name(codecID.String()) + "_nvenc"
		hardwareDeviceType = globaltypes.HardwareDeviceTypeCUDA
	} else {
		vcodecName = "libx264"
	}

	require.NoError(t, streamMux.SwitchToOutputByProps(
		ctx,
		streammuxtypes.SenderProps{
			RecoderConfig: streammuxtypes.RecoderConfig{
				Output: streammuxtypes.RecoderOutputConfig{
					VideoTrackConfigs: []streammuxtypes.OutputVideoTrackConfig{{
						InputTrackIDs:      []int{0, 1, 2, 3, 4, 5, 6, 7},
						OutputTrackIDs:     []int{0},
						CodecName:          vcodecName,
						Resolution:         codectypes.Resolution{Width: 1920, Height: 1080},
						HardwareDeviceType: hardwareDeviceType,
					}},
					AudioTrackConfigs: []streammuxtypes.OutputAudioTrackConfig{{
						InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
						OutputTrackIDs: []int{1},
						CodecName:      "aac",
					}},
				},
			},
		},
	))

	require.NotNil(t, streamMux.GetActiveOutput(ctx))

	errCh := make(chan node.Error, 100)
	observability.Go(ctx, func(ctx context.Context) {
		for err := range errCh {
			require.NoError(t, err)
		}
	})

	defer close(errCh)
	observability.Go(ctx, func(ctx context.Context) {
		defer streamMux.Close(ctx)
		streamMux.Serve(ctx, node.ServeConfig{}, errCh)
	})
	inputCh := streamMux.GetProcessor().InputPacketChan()

	// simple test:

	for _, p := range input {
		mediaType := globaltypes.MediaType(p.GetMediaType())
		pktSize := uint64(p.Packet.Size())
		streamMux.GetCountersPtr().Addressed.Packets.Increment(mediaType, pktSize)
		select {
		case <-ctx.Done():
			streamMux.GetCountersPtr().Missed.Packets.Increment(mediaType, pktSize)
			t.Fatalf("context is closed prematurely: %v", ctx.Err())
		case inputCh <- p:
			streamMux.GetCountersPtr().Received.Packets.Increment(mediaType, pktSize)
		}
	}

	drain := func() {
		ctx, cancelFn := context.WithCancel(ctx)
		defer cancelFn()
		observability.Go(ctx, func(ctx context.Context) {
			_t := time.NewTicker(time.Second)
			defer _t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-_t.C:
				}
				require.False(t, avpipeline.IsDrained(ctx, streamMux))
				require.False(t, streamMux.IsDrained(ctx))
			}
		})
		// simple 'avpipeline.Drain(ctx, nil, streamMux)' should theoretically be sufficient,
		// but we wanna be extra safe, thus we drain&pause, check everything is drained, then unpause again
		avpipeline.Drain(ctx, ptr(true), streamMux)
		avpipeline.Drain(ctx, nil, streamMux)
		avpipeline.SetBlockInput(ctx, false, streamMux)
	}

	if streammux.EnableDraining || e2eForceTestDraining {
		drain()
	}

	for idx, p := range input {
		if idx == len(input)/2 {
			for {
				err := streamMux.SwitchToOutputByProps(
					ctx,
					streammuxtypes.SenderProps{
						RecoderConfig: streammuxtypes.RecoderConfig{
							Output: streammuxtypes.RecoderOutputConfig{
								VideoTrackConfigs: []streammuxtypes.OutputVideoTrackConfig{{
									InputTrackIDs:      []int{0, 1, 2, 3, 4, 5, 6, 7},
									OutputTrackIDs:     []int{0},
									CodecName:          vcodecName,
									Resolution:         codectypes.Resolution{Width: 1280, Height: 720},
									HardwareDeviceType: hardwareDeviceType,
								}},
								AudioTrackConfigs: []streammuxtypes.OutputAudioTrackConfig{{
									InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
									OutputTrackIDs: []int{1},
									CodecName:      "aac",
								}},
							},
						},
					},
				)
				if errors.As(err, &streammux.ErrSwitchAlreadyInProgress{}) {
					logger.Warnf(ctx, "SetRecoderConfig: switch in progress: %v; retrying in 1 second", err)
					time.Sleep(time.Second)
					continue
				}
				require.NoError(t, err)
				break
			}
		}
		mediaType := globaltypes.MediaType(p.GetMediaType())
		pktSize := uint64(p.Packet.Size())
		streamMux.GetCountersPtr().Addressed.Packets.Increment(mediaType, pktSize)
		select {
		case <-ctx.Done():
			streamMux.GetCountersPtr().Missed.Packets.Increment(mediaType, pktSize)
			t.Fatalf("context is closed prematurely: %v", ctx.Err())
		case inputCh <- p:
			streamMux.GetCountersPtr().Received.Packets.Increment(
				globaltypes.MediaType(p.GetMediaType()),
				uint64(p.Packet.Size()),
			)
		}
	}

	if streammux.EnableDraining || e2eForceTestDraining {
		drain()
	}
}
