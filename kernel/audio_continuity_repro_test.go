//go:build test_long

// audio_continuity_repro_test.go reproduces the AAC audio output continuity
// drop seen in production ffstream pipelines (input cont 0.985, output 0.963)
// and isolates whether the deficit happens in the
// transcoder/encoder/output-kernel chain alone.

package kernel_test

import (
	"context"
	"errors"
	"io"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/typesnolibav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
	"github.com/xaionaro-go/avpipeline/packet/condition/extra"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/secret"

	audiotypes "github.com/xaionaro-go/audio/pkg/audio/types"
)

type audioPktSnapshot struct {
	DTS, PTS, Duration int64
	TimeBase           astiav.Rational
}

type audioCapture struct {
	mu   sync.Mutex
	pkts []audioPktSnapshot
}

func (c *audioCapture) String() string                      { return "audioCapture" }
func (c *audioCapture) Match(ctx context.Context, in packet.Input) bool {
	if in.GetMediaType() != astiav.MediaTypeAudio {
		return true
	}
	c.mu.Lock()
	c.pkts = append(c.pkts, audioPktSnapshot{
		DTS:      in.Packet.Dts(),
		PTS:      in.Packet.Pts(),
		Duration: in.Packet.Duration(),
		TimeBase: in.GetTimeBase(),
	})
	c.mu.Unlock()
	return true
}

var _ condition.Condition = (*audioCapture)(nil)


func TestAudioContinuityRepro_AACFLV(t *testing.T) {
	t.Parallel()
	runRepro(t, "baseline", 0, 0)
}

func TestAudioContinuityRepro_AACFLV_Resample48k1ch(t *testing.T) {
	t.Parallel()
	runRepro(t, "ar48000_ac1", 48000, 1)
}

func TestAudioContinuityRepro_AACFLV_Resample48k2ch(t *testing.T) {
	t.Parallel()
	runRepro(t, "ar48000_ac2", 48000, 2)
}

func TestAudioContinuityRepro_AACFLV_Resample44k1ch(t *testing.T) {
	t.Parallel()
	runReproWithInput(t, "ar44100_ac1", "video0-1v1a.mov", 44100, 1)
}

func TestAudioContinuityRepro_AACFLV_FlvInput(t *testing.T) {
	t.Parallel()
	runReproWithInput(t, "flv_in_baseline", "video0-1v1a.flv", 0, 0)
}

func TestAudioContinuityRepro_AACFLV_FlvInputDownmix(t *testing.T) {
	t.Parallel()
	runReproWithInput(t, "flv_in_48k_ac1", "video0-1v1a.flv", 48000, 1)
}

// GapFiller-as-FilterKernel tests omitted: wiring it through Transcoder.SetFilterKernel
// causes empty-frame errors because the GapFiller retains references that are then
// returned to the frame pool by the transcoder loop. This is an orthogonal bug, not the
// audio-continuity issue under investigation.

func runRepro(t *testing.T, label string, sampleRate int, channels int) {
	runReproWithInput(t, label, "video0-1v1a.mov", sampleRate, channels)
}

func runReproWithInput(t *testing.T, label string, inputFile string, sampleRate int, channels int) {
	l := logrus.Default().WithLevel(logger.LevelWarning)
	ctx := logger.CtxWithLogger(context.Background(), l)
	defer belt.Flush(ctx)

	flvPath := path.Join(t.TempDir(), "audio_continuity_repro_"+label+".flv")
	fromURL := path.Join("testdata", inputFile)

	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()

	input, err := kernel.NewInputFromURL(
		ctx,
		fromURL, secret.New(""),
		kernel.InputConfig{
			OnPreClose: kernel.HookFunc(func(ctx context.Context, i typesnolibav.Abstract) error {
				time.Sleep(time.Second)
				return nil
			}),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close(ctx)

	outputQ := extra.NewQuality()
	cap := &audioCapture{}
	combinedFilter := condition.And{outputQ, cap}

	// FLV output (1/1000 timebase) — matches prod
	output, err := kernel.NewOutputFromURL(ctx,
		flvPath, secret.New(""),
		kernel.OutputConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close(ctx)
	output.Filter = combinedFilter

	errCh := make(chan node.Error, 10)
	inputNode := node.NewFromKernel(
		ctx,
		input,
		processor.OptionQueueSizeInput(1),
		processor.OptionQueueSizeOutput(1),
		processor.OptionQueueSizeError(2),
	)
	encParams := &codec.NaiveEncoderFactoryParams{
		VideoCodec: "libx264",
		AudioCodec: "aac",
	}
	if sampleRate > 0 {
		encParams.AudioSampleRate = audiotypes.SampleRate(sampleRate)
	}
	if channels > 0 {
		encParams.AudioChannels = audiotypes.Channel(channels)
	}
	encoderFactory := codec.NewNaiveEncoderFactory(ctx, encParams)
	transcoder, err := kernel.NewTranscoder(
		ctx,
		codec.NewNaiveDecoderFactory(ctx, nil),
		encoderFactory,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer transcoder.Close(ctx)
	transcodingNode := node.NewFromKernel(
		ctx,
		transcoder,
		processor.OptionQueueSizeInput(10),
		processor.OptionQueueSizeOutput(10),
		processor.OptionQueueSizeError(2),
	)

	outputNode := node.NewFromKernel(
		ctx,
		output,
		processor.OptionQueueSizeInput(600),
		processor.OptionQueueSizeOutput(0),
		processor.OptionQueueSizeError(2),
	)

	inputNode.AddPushTo(ctx, transcodingNode)
	transcodingNode.AddPushTo(ctx, outputNode)

	go func() {
		defer cancelFn()
		avpipeline.Serve(ctx, avpipeline.ServeConfig{
			EachNode: node.ServeConfig{
				FrameDropVideo: false,
				FrameDropAudio: false,
			},
		}, errCh, inputNode)
	}()

LOOP:
	for {
		select {
		case <-ctx.Done():
			break LOOP
		case err, ok := <-errCh:
			if !ok {
				break LOOP
			}
			if errors.Is(err.Err, context.Canceled) {
				continue
			}
			if errors.Is(err.Err, io.EOF) {
				continue
			}
			if err.Err != nil {
				t.Fatal(err)
			}
		}
	}

	// Wait briefly for the muxer to flush
	time.Sleep(200 * time.Millisecond)

	getCtx := context.Background()
	outQ, err := outputQ.Measurements.GetQuality(getCtx)
	if err != nil {
		t.Fatalf("output GetQuality: %v", err)
	}
	outAgg := outQ.Aggregate()

	t.Logf("[%s] OUTPUT Audio Continuity=%.6f  FrameRate=%.4f", label, outAgg.Audio.Continuity, outAgg.Audio.FrameRate)
	t.Logf("[%s] OUTPUT Video Continuity=%.6f  FrameRate=%.4f", label, outAgg.Video.Continuity, outAgg.Video.FrameRate)

	// Audio continuity must reach the structural FLV ms-grid floor (~0.985).
	// Below ~0.97 indicates a real packet-timing bug (e.g. the cross-rate
	// resampler regression where output AAC frames inherited the input
	// sample-rate's frame Duration, leaving a per-frame gap proportional to
	// 1 - inRate/outRate).
	const audioContMin = 0.97
	if outAgg.Audio.FrameRate > 0 && outAgg.Audio.Continuity < audioContMin {
		t.Errorf("[%s] OUTPUT Audio Continuity=%.6f below floor %.4f (expected ~0.985); resampler/encoder timestamp bug?",
			label, outAgg.Audio.Continuity, audioContMin)
	}
	for _, sq := range *outQ {
		t.Logf("[%s]   OUT mediaType=%v cont=%.6f fps=%.4f", label, sq.MediaType, sq.Continuity, sq.FrameRate)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if len(cap.pkts) == 0 {
		return
	}
	t.Logf("[%s] AUDIO_PKT_COUNT=%d", label, len(cap.pkts))
	first := cap.pkts[0]
	t.Logf("[%s] timebase=%d/%d", label, first.TimeBase.Num(), first.TimeBase.Den())
	var totalGap, totalOverlap, totalSpan int64
	prevEnd := first.DTS + first.Duration
	gapEvents := 0
	for i := 1; i < len(cap.pkts); i++ {
		p := cap.pkts[i]
		diff := p.DTS - prevEnd
		if diff > 0 {
			totalGap += diff
			gapEvents++
			if gapEvents <= 20 {
				t.Logf("[%s] gap@i=%d  dts=%d prevEnd=%d gap=%d  prevDur=%d curDur=%d",
					label, i, p.DTS, prevEnd, diff, cap.pkts[i-1].Duration, p.Duration)
			}
		} else if diff < 0 {
			totalOverlap += -diff
		}
		totalSpan = (p.DTS + p.Duration) - first.DTS
		prevEnd = p.DTS + p.Duration
	}
	t.Logf("[%s] total_span=%d  total_gap=%d  total_overlap=%d  gap_events=%d  cont_full=%.6f",
		label, totalSpan, totalGap, totalOverlap, gapEvents,
		1.0-float64(totalGap)/float64(totalSpan))
	// Histogram of dts-deltas
	dtsDeltaHist := map[int64]int{}
	for i := 1; i < len(cap.pkts); i++ {
		dtsDeltaHist[cap.pkts[i].DTS-cap.pkts[i-1].DTS]++
	}
	t.Logf("[%s] dts_delta_hist=%v", label, dtsDeltaHist)
	durHist := map[int64]int{}
	for _, p := range cap.pkts {
		durHist[p.Duration]++
	}
	t.Logf("[%s] duration_hist=%v", label, durHist)
}
