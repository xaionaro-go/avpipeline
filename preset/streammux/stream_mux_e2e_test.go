package streammux_test

import (
	"context"
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
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

const (
	e2eNvencEnable = true
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
							for i := 0; i < 10; i++ {
								t.Run(fmt.Sprintf("run%d", i), func(t *testing.T) {
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

var _ streammux.OutputFactory = (*decoderOutputFactory)(nil)

func (decoderOutputFactory) NewOutput(
	ctx context.Context,
	outputKey streammux.OutputKey,
) (node.Abstract, streammux.OutputConfig, error) {
	n := node.NewFromKernel(
		ctx,
		kernel.NewDecoder(ctx, &codec.NaiveDecoderFactory{}),
		processor.DefaultOptionsRecoder()...,
	)
	return n, streammux.OutputConfig{}, nil
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

	vcodecName := codectypes.Name(codecID.String())
	if e2eNvencEnable {
		vcodecName += "_nvenc"
	}

	require.NoError(t, streamMux.SetRecoderConfig(ctx, streammuxtypes.RecoderConfig{
		AudioTrackConfigs: []streammuxtypes.AudioTrackConfig{{
			InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
			OutputTrackIDs: []int{0},
			CodecName:      "aac",
		}},
		VideoTrackConfigs: []streammuxtypes.VideoTrackConfig{{
			InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
			OutputTrackIDs: []int{0},
			CodecName:      vcodecName,
			Resolution:     codectypes.Resolution{Width: 1920, Height: 1080},
		}},
	}))

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

	// simple test:

	inputCh := streamMux.GetProcessor().InputPacketChan()
	for _, p := range input {
		select {
		case <-ctx.Done():
			require.NoError(t, ctx.Err())
		case inputCh <- p:
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

	drain()
}
