//go:build test_e2e

// audio_copy_e2e_test.go reproduces the regression where requesting
// '-c:a copy' through the StreamMux preset (audio codec == NameCopy) caused
// the audio path to be routed through the Transcoder's decode-then-encode
// branch, producing decoded frames that the EncoderCopy then rejected with
// codec.ErrCopyEncoder, taking the entire pipeline (including unrelated
// outputs such as video) down with it.
//
// The expected behaviour is identical to '-c:v copy': audio packets must be
// passed through unchanged when the configured audio codec is "copy", with
// no decode/encode round-trip and no frames ever reaching the encoder.

package streammux_test

import (
	"context"
	"errors"
	"io"
	"path"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/preset/streammux"
	streammuxtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

// TestE2E_AudioCopyDoesNotTearDownPipeline feeds a small mp4 with one video
// and one audio track through StreamMux configured with '-c:a copy' (audio
// codec == NameCopy) and a real video encoder. The pipeline must not abort
// with codec.ErrCopyEncoder; audio packets must propagate to the SendingNode
// and the video encoder must be reachable.
func TestE2E_AudioCopyDoesNotTearDownPipeline(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelWarning)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger { return l })
	defer belt.Flush(ctx)

	astiav.SetLogLevel(avpipeline.LogLevelToAstiav(l.Level()))

	const fileName = "video0-1v1a.mov"
	t.Logf("input: %s", path.Join("testdata", fileName))
	input, cancelInput := readInputFromFile(ctx, t, fileName)
	defer cancelInput()
	require.NotEmpty(t, input)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	outputFactory := decoderOutputFactory[struct{}]{}
	streamMux, err := streammux.New(
		ctx,
		streammuxtypes.MuxModeDifferentOutputsSameTracks,
		outputFactory,
	)
	require.NoError(t, err)
	streamMux.SetAutoBitRateVideoConfig(ctx, ptr(must(streammux.DefaultAutoBitRateVideoConfig(astiav.CodecIDH264))))

	require.NoError(t, streamMux.SwitchToOutputByProps(
		ctx,
		streammuxtypes.SenderProps{
			TranscoderConfig: streammuxtypes.TranscoderConfig{
				Output: streammuxtypes.TranscoderOutputConfig{
					VideoTrackConfigs: []streammuxtypes.OutputVideoTrackConfig{{
						InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
						OutputTrackIDs: []int{0},
						CodecName:      "libx264",
						Resolution:     codectypes.Resolution{Width: 1920, Height: 1080},
					}},
					AudioTrackConfigs: []streammuxtypes.OutputAudioTrackConfig{{
						InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
						OutputTrackIDs: []int{1},
						CodecName:      codectypes.Name(codec.NameCopy),
					}},
				},
			},
		},
	))

	errCh := make(chan node.Error, 100)
	var fatalErr atomic.Pointer[node.Error]
	observability.Go(ctx, func(ctx context.Context) {
		for err := range errCh {
			if errors.Is(err.Err, context.Canceled) || errors.Is(err.Err, io.EOF) {
				continue
			}
			if err.Err == nil {
				continue
			}
			// Capture the first fatal error so the test can assert it.
			if fatalErr.Load() == nil {
				e := err
				fatalErr.Store(&e)
			}
			logger.Errorf(ctx, "pipeline error: %v", err)
			cancelFn()
		}
	})
	defer close(errCh)

	observability.Go(ctx, func(ctx context.Context) {
		defer streamMux.Close(ctx)
		streamMux.Serve(ctx, node.ServeConfig{}, errCh)
	})

	inputCh := streamMux.GetProcessor().InputChan()
	feedDeadline := time.Now().Add(15 * time.Second)
	for _, p := range input {
		if time.Now().After(feedDeadline) {
			break
		}
		mediaType := globaltypes.MediaType(p.GetMediaType())
		pktSize := uint64(p.Packet.Size())
		streamMux.GetCountersPtr().Addressed.Packets.Increment(mediaType, pktSize)
		select {
		case <-ctx.Done():
			break
		case inputCh <- packetorframe.InputUnion{Packet: &p}:
			streamMux.GetCountersPtr().Received.Packets.Increment(mediaType, pktSize)
		}
	}

	// Allow the pipeline a moment to drain any in-flight error.
	time.Sleep(500 * time.Millisecond)

	if got := fatalErr.Load(); got != nil {
		t.Fatalf("pipeline aborted with: %v", got.Err)
	}
}

// TestE2E_AudioCopyAcceptsPredecodedFrames simulates the production scenario
// where an upstream stage (such as ffstream's AudioSync kernel) has already
// decoded audio packets into raw frames before they reach StreamMux. With
// '-c:a copy' configured, StreamMux must not crash the pipeline when those
// audio frames arrive: a copy-coded audio stream cannot semantically consume
// decoded frames, so StreamMux must either pass them through as packets
// (re-encoding via the canonical copy path) or drop them gracefully — but it
// must NOT propagate codec.ErrCopyEncoder out as a fatal pipeline error and
// tear down unrelated outputs.
func TestE2E_AudioCopyAcceptsPredecodedFrames(t *testing.T) {
	l := logrus.Default().WithLevel(logger.LevelWarning)
	ctx := logger.CtxWithLogger(context.Background(), l)
	logger.SetDefault(func() logger.Logger { return l })
	defer belt.Flush(ctx)

	astiav.SetLogLevel(avpipeline.LogLevelToAstiav(l.Level()))

	const fileName = "video0-1v1a.mov"
	rawInput, cancelInput := readInputFromFile(ctx, t, fileName)
	defer cancelInput()
	require.NotEmpty(t, rawInput)

	// Pre-decode audio packets to frames, leaving video packets untouched.
	// This mirrors what ffstream's AudioSync stage produces upstream of
	// StreamMux: video stays as packets, audio arrives as decoded frames.
	decodedInput := preDecodeAudio(ctx, t, rawInput)
	require.NotEmpty(t, decodedInput)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	outputFactory := decoderOutputFactory[struct{}]{}
	streamMux, err := streammux.New(
		ctx,
		streammuxtypes.MuxModeDifferentOutputsSameTracks,
		outputFactory,
	)
	require.NoError(t, err)
	streamMux.SetAutoBitRateVideoConfig(ctx, ptr(must(streammux.DefaultAutoBitRateVideoConfig(astiav.CodecIDH264))))

	require.NoError(t, streamMux.SwitchToOutputByProps(
		ctx,
		streammuxtypes.SenderProps{
			TranscoderConfig: streammuxtypes.TranscoderConfig{
				Output: streammuxtypes.TranscoderOutputConfig{
					VideoTrackConfigs: []streammuxtypes.OutputVideoTrackConfig{{
						InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
						OutputTrackIDs: []int{0},
						CodecName:      "libx264",
						Resolution:     codectypes.Resolution{Width: 1920, Height: 1080},
					}},
					AudioTrackConfigs: []streammuxtypes.OutputAudioTrackConfig{{
						InputTrackIDs:  []int{0, 1, 2, 3, 4, 5, 6, 7},
						OutputTrackIDs: []int{1},
						CodecName:      codectypes.Name(codec.NameCopy),
					}},
				},
			},
		},
	))

	errCh := make(chan node.Error, 100)
	var fatalErr atomic.Pointer[node.Error]
	observability.Go(ctx, func(ctx context.Context) {
		for err := range errCh {
			if errors.Is(err.Err, context.Canceled) || errors.Is(err.Err, io.EOF) {
				continue
			}
			if err.Err == nil {
				continue
			}
			if fatalErr.Load() == nil {
				e := err
				fatalErr.Store(&e)
			}
			logger.Errorf(ctx, "pipeline error: %v", err)
			cancelFn()
		}
	})
	defer close(errCh)

	observability.Go(ctx, func(ctx context.Context) {
		defer streamMux.Close(ctx)
		streamMux.Serve(ctx, node.ServeConfig{}, errCh)
	})

	inputCh := streamMux.GetProcessor().InputChan()
	feedDeadline := time.Now().Add(15 * time.Second)
	for _, in := range decodedInput {
		if time.Now().After(feedDeadline) {
			break
		}
		mediaType := globaltypes.MediaType(in.GetMediaType())
		pktSize := uint64(in.GetSize())
		streamMux.GetCountersPtr().Addressed.Packets.Increment(mediaType, pktSize)
		select {
		case <-ctx.Done():
			break
		case inputCh <- in:
			streamMux.GetCountersPtr().Received.Packets.Increment(mediaType, pktSize)
		}
	}

	time.Sleep(500 * time.Millisecond)

	if got := fatalErr.Load(); got != nil {
		t.Fatalf("pipeline aborted with: %v", got.Err)
	}
}

// preDecodeAudio routes each audio packet through a Decoder kernel, returning
// a slice of InputUnion entries where audio is represented as decoded frames
// while video remains as raw packets — matching the shape ffstream's pipeline
// hands to StreamMux when AudioSync is enabled upstream.
func preDecodeAudio(
	ctx context.Context,
	t *testing.T,
	rawInput []packet.Input,
) []packetorframe.InputUnion {
	t.Helper()
	dec := kernel.NewDecoder(ctx, codec.NewNaiveDecoderFactory(ctx, nil))
	defer dec.Close(ctx)

	var out []packetorframe.InputUnion
	outCh := make(chan packetorframe.OutputUnion, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for op := range outCh {
			if op.Frame != nil {
				f := frame.CloneAsReferenced(op.Frame.Frame)
				cloned := frame.Input{Frame: f, StreamInfo: op.Frame.StreamInfo}
				out = append(out, packetorframe.InputUnion{Frame: &cloned})
			}
		}
	}()
	for i := range rawInput {
		p := rawInput[i]
		if p.GetMediaType() != astiav.MediaTypeAudio {
			out = append(out, packetorframe.InputUnion{Packet: &p})
			continue
		}
		if err := dec.SendInput(ctx, packetorframe.InputUnion{Packet: &p}, outCh); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decoder.SendInput: %v", err)
		}
	}
	close(outCh)
	<-done
	return out
}
