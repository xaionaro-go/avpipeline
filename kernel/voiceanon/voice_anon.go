package voiceanon

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// VoiceAnonymizer is a kernel that pitch-shifts audio to anonymize speakers.
// It wraps an FFmpeg rubberband filter graph. The filter graph is lazily
// initialized on the first audio frame (to auto-detect sample format).
type VoiceAnonymizer struct {
	*closuresignaler.ClosureSignaler
	Enabled *atomic.Bool // nil = always on; false = passthrough

	config      Config
	filterGraph *kernel.AVFilterGraph
	initOnce    sync.Once
	initErr     error
}

var _ kernel.Abstract = (*VoiceAnonymizer)(nil)

// New creates a new VoiceAnonymizer kernel.
func New(cfg Config) *VoiceAnonymizer {
	if cfg.PitchScale == 0 {
		cfg.PitchScale = 0.7
	}
	return &VoiceAnonymizer{
		ClosureSignaler: closuresignaler.New(),
		config:          cfg,
	}
}

func (v *VoiceAnonymizer) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(v)
}

func (v *VoiceAnonymizer) String() string {
	return fmt.Sprintf("VoiceAnonymizer(pitch=%g)", v.config.PitchScale)
}

func (v *VoiceAnonymizer) initFilterGraph(
	ctx context.Context,
	streamIndex int,
	codecParameters *astiav.CodecParameters,
	timeBase astiav.Rational,
) error {
	filterStr := v.config.FilterString()
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

	v.filterGraph = kernel.NewAVFilterGraph(ctx, graph)
	return nil
}

func (v *VoiceAnonymizer) SendInput(
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
	if v.Enabled != nil && !v.Enabled.Load() {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Passthrough non-audio frames.
	if frameInput.GetMediaType() != astiav.MediaTypeAudio {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Lazy-init the filter graph on the first audio frame.
	v.initOnce.Do(func() {
		v.initErr = v.initFilterGraph(
			ctx,
			frameInput.GetStreamIndex(),
			frameInput.GetCodecParameters(),
			frameInput.GetTimeBase(),
		)
	})
	if v.initErr != nil {
		return v.initErr
	}

	// Delegate to the AVFilterGraph.
	return v.filterGraph.SendInput(ctx, input, outputCh)
}

func (v *VoiceAnonymizer) Close(ctx context.Context) error {
	v.ClosureSignaler.Close(ctx)
	if v.filterGraph != nil {
		return v.filterGraph.Close(ctx)
	}
	return nil
}

func (v *VoiceAnonymizer) Generate(
	_ context.Context,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}

// Config returns the kernel's configuration.
func (v *VoiceAnonymizer) Config() Config {
	return v.config
}

// FilterGraph returns the underlying AVFilterGraph, or nil if not yet initialized.
func (v *VoiceAnonymizer) FilterGraph() *kernel.AVFilterGraph {
	return v.filterGraph
}
