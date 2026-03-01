package voiceanon

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/audio"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// WhitelistingConfig configures the WhitelistingVoiceAnonymizer kernel.
type WhitelistingConfig struct {
	// AnonymizationConfig configures the pitch-shift anonymization filter.
	AnonymizationConfig Config

	// BufferSamples is the number of audio samples to buffer before
	// making a speaker identification decision. At 16kHz this is roughly:
	//   16000 = 1 second, 32000 = 2 seconds.
	// Larger values improve identification accuracy but increase latency.
	// Default: 16000 (1 second).
	BufferSamples int
}

// DefaultWhitelistingConfig returns a WhitelistingConfig with sensible defaults.
func DefaultWhitelistingConfig() WhitelistingConfig {
	return WhitelistingConfig{
		AnonymizationConfig: DefaultConfig(),
		BufferSamples:       16000,
	}
}

// WhitelistingVoiceAnonymizer wraps a VoiceAnonymizer with speaker
// identification. Whitelisted speakers pass through unchanged; unknown
// speakers are anonymized via the rubberband filter.
//
// Audio frames are buffered until enough samples are collected for speaker
// identification. Once the speaker is identified (or not), buffered frames
// are either passed through or anonymized in a batch.
type WhitelistingVoiceAnonymizer struct {
	*closuresignaler.ClosureSignaler
	Enabled *atomic.Bool // nil = always on; false = passthrough

	config      WhitelistingConfig
	speakerID   SpeakerIdentifier
	filterGraph *kernel.AVFilterGraph

	initOnce sync.Once
	initErr  error

	// Buffering state for speaker identification.
	bufferedFrames  []bufferedFrame
	bufferedSamples []float32
	sampleRate      int
}

type bufferedFrame struct {
	input packetorframe.InputUnion
}

var _ kernel.Abstract = (*WhitelistingVoiceAnonymizer)(nil)

// NewWhitelisting creates a WhitelistingVoiceAnonymizer with the given
// speaker identifier. The speaker identifier should already have speakers
// registered via Register().
func NewWhitelisting(cfg WhitelistingConfig, speakerID SpeakerIdentifier) *WhitelistingVoiceAnonymizer {
	if cfg.BufferSamples <= 0 {
		cfg.BufferSamples = 16000
	}
	if cfg.AnonymizationConfig.PitchScale == 0 {
		cfg.AnonymizationConfig.PitchScale = 0.7
	}
	return &WhitelistingVoiceAnonymizer{
		ClosureSignaler: closuresignaler.New(),
		config:          cfg,
		speakerID:       speakerID,
	}
}

func (w *WhitelistingVoiceAnonymizer) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(w)
}

func (w *WhitelistingVoiceAnonymizer) String() string {
	return fmt.Sprintf("WhitelistingVoiceAnonymizer(pitch=%g, speakers=%v)",
		w.config.AnonymizationConfig.PitchScale, w.speakerID.Speakers())
}

func (w *WhitelistingVoiceAnonymizer) initFilterGraph(
	ctx context.Context,
	streamIndex int,
	codecParameters *astiav.CodecParameters,
	timeBase astiav.Rational,
) error {
	filterStr := w.config.AnonymizationConfig.FilterString()
	filterComplex := fmt.Sprintf("[in%d]%s[out%d]", streamIndex, filterStr, streamIndex)
	trackConfig := map[int]avfilter.TrackConfig{
		streamIndex: {
			CodecParameters: codecParameters,
			TimeBase:        timeBase,
		},
	}

	graph, err := avfilter.NewGraph(ctx, trackConfig, filterComplex)
	if err != nil {
		return fmt.Errorf("unable to create rubberband filter graph: %w", err)
	}

	w.filterGraph = kernel.NewAVFilterGraph(ctx, graph)
	return nil
}

func (w *WhitelistingVoiceAnonymizer) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_, frameInput := input.Unwrap()
	if frameInput == nil {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Passthrough when disabled.
	if w.Enabled != nil && !w.Enabled.Load() {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Passthrough non-audio frames.
	if frameInput.GetMediaType() != astiav.MediaTypeAudio {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Lazy-init the filter graph.
	w.initOnce.Do(func() {
		w.initErr = w.initFilterGraph(
			ctx,
			frameInput.GetStreamIndex(),
			frameInput.GetCodecParameters(),
			frameInput.GetTimeBase(),
		)
		if w.initErr == nil {
			w.sampleRate = frameInput.GetCodecParameters().SampleRate()
		}
	})
	if w.initErr != nil {
		return w.initErr
	}

	// Extract mono float32 samples for speaker identification.
	samples, err := extractFloat32Samples(frameInput.Frame)
	if err != nil {
		// If we can't extract samples (unusual format), anonymize by default.
		return w.filterGraph.SendInput(ctx, input, outputCh)
	}

	// Buffer the frame and its samples.
	w.bufferedFrames = append(w.bufferedFrames, bufferedFrame{input: input})
	w.bufferedSamples = append(w.bufferedSamples, samples...)

	// Check if we have enough samples for identification.
	if len(w.bufferedSamples) < w.config.BufferSamples {
		return nil
	}

	return w.flushBuffer(ctx, outputCh)
}

func (w *WhitelistingVoiceAnonymizer) flushBuffer(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	defer func() {
		w.bufferedFrames = w.bufferedFrames[:0]
		w.bufferedSamples = w.bufferedSamples[:0]
	}()

	// Identify speaker.
	speakerName, err := w.speakerID.Identify(w.bufferedSamples, w.sampleRate)
	if err != nil {
		// On identification error, anonymize as a safety measure.
		speakerName = ""
	}

	isWhitelisted := speakerName != ""

	for _, bf := range w.bufferedFrames {
		if isWhitelisted {
			// Known speaker: pass through unchanged.
			outputCh <- bf.input.CloneAsReferencedOutput()
		} else {
			// Unknown speaker: anonymize.
			if err := w.filterGraph.SendInput(ctx, bf.input, outputCh); err != nil {
				return err
			}
		}
	}
	return nil
}

// extractFloat32Samples extracts mono float32 samples from an audio frame.
func extractFloat32Samples(f *astiav.Frame) ([]float32, error) {
	if f == nil {
		return nil, fmt.Errorf("nil frame")
	}

	samples, err := audio.ExtractSamples(f, 0)
	if err != nil {
		return nil, err
	}

	result := make([]float32, len(samples))
	for i, s := range samples {
		result[i] = float32(s)
	}
	return result, nil
}

func (w *WhitelistingVoiceAnonymizer) Close(ctx context.Context) error {
	w.ClosureSignaler.Close(ctx)

	// Flush remaining buffered frames (anonymize them since we don't
	// have enough data for identification).
	if len(w.bufferedFrames) > 0 && w.filterGraph != nil {
		outputCh := make(chan packetorframe.OutputUnion, len(w.bufferedFrames)*2)
		for _, bf := range w.bufferedFrames {
			_ = w.filterGraph.SendInput(ctx, bf.input, outputCh)
		}
		// Discard outputs during close.
		close(outputCh)
		w.bufferedFrames = nil
		w.bufferedSamples = nil
	}

	if w.filterGraph != nil {
		if err := w.filterGraph.Close(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (w *WhitelistingVoiceAnonymizer) Generate(
	_ context.Context,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}

// SpeakerID returns the associated SpeakerIdentifier.
func (w *WhitelistingVoiceAnonymizer) SpeakerID() SpeakerIdentifier {
	return w.speakerID
}

// BufferedSampleCount returns the number of samples currently buffered
// awaiting speaker identification.
func (w *WhitelistingVoiceAnonymizer) BufferedSampleCount() int {
	return len(w.bufferedSamples)
}

