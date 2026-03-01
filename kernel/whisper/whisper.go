// Package whisper provides a kernel that performs speech-to-text transcription
// using FFmpeg 8's whisper audio filter (backed by whisper.cpp).
//
// The kernel wraps an AVFilterGraph containing the whisper filter. Audio frames
// are processed through the filter, and transcription results are extracted from
// output frame metadata (lavfi.whisper.text) and attached as PipelineSideData.
//
// Requires FFmpeg compiled with --enable-whisper and whisper.cpp >= 1.7.5.
package whisper

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// Whisper is a kernel that performs speech-to-text transcription using
// FFmpeg's whisper audio filter. It wraps an AVFilterGraph and extracts
// transcription results from output frame metadata as PipelineSideData.
type Whisper struct {
	*closuresignaler.ClosureSignaler
	Config      WhisperConfig
	filterGraph *kernel.AVFilterGraph
}

var _ kernel.Abstract = (*Whisper)(nil)

// New creates a new Whisper kernel. The codecParameters must describe the input
// audio stream; the filter graph handles format conversion (to FLT/mono/16kHz)
// automatically.
func New(
	ctx context.Context,
	config *WhisperConfig,
	streamIndex int,
	codecParameters *astiav.CodecParameters,
	timeBase astiav.Rational,
) (*Whisper, error) {
	if config == nil {
		return nil, fmt.Errorf("whisper config is nil")
	}
	if config.Model == "" {
		return nil, fmt.Errorf("whisper model path is required")
	}

	filterStr := config.FilterString()
	trackConfig := map[int]avfilter.TrackConfig{
		streamIndex: {
			Filters:         []string{filterStr},
			CodecParameters: codecParameters,
			TimeBase:        timeBase,
		},
	}

	graph, err := avfilter.NewGraph(ctx, trackConfig, "")
	if err != nil {
		return nil, fmt.Errorf("unable to create whisper filter graph: %w", err)
	}

	return &Whisper{
		ClosureSignaler: closuresignaler.New(),
		Config:          *config,
		filterGraph:     kernel.NewAVFilterGraph(ctx, graph),
	}, nil
}

func (w *Whisper) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(w)
}

func (w *Whisper) String() string {
	return fmt.Sprintf("Whisper(model=%s, lang=%s)", w.Config.Model, w.Config.Language)
}

func (w *Whisper) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_, frameInput := input.Unwrap()
	if frameInput == nil {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	if frameInput.GetMediaType() != astiav.MediaTypeAudio {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	interceptCh := make(chan packetorframe.OutputUnion, 16)

	if err := w.filterGraph.SendInput(ctx, input, interceptCh); err != nil {
		return err
	}

	for {
		select {
		case out := <-interceptCh:
			if out.Frame != nil {
				extractWhisperMetadata(out.Frame)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case outputCh <- out:
			}
		default:
			return nil
		}
	}
}

const (
	metadataKeyText     = "lavfi.whisper.text"
	metadataKeyDuration = "lavfi.whisper.duration"
)

func extractWhisperMetadata(output *frame.Output) {
	if output.Frame == nil {
		return
	}

	md := output.Frame.Metadata()
	if md == nil {
		return
	}

	entry := md.Get(metadataKeyText, nil, astiav.NewDictionaryFlags())
	if entry == nil {
		return
	}

	text := entry.Value()
	if text == "" {
		return
	}

	result := &WhisperResult{
		Text: text,
	}

	durEntry := md.Get(metadataKeyDuration, nil, astiav.NewDictionaryFlags())
	if durEntry != nil {
		if ms, err := strconv.ParseFloat(durEntry.Value(), 64); err == nil {
			result.Duration = time.Duration(ms * float64(time.Millisecond))
		}
	}

	output.AddPipelineSideData(result)
}

func (w *Whisper) Close(ctx context.Context) error {
	w.ClosureSignaler.Close(ctx)
	if w.filterGraph != nil {
		return w.filterGraph.Close(ctx)
	}
	return nil
}

func (w *Whisper) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}
