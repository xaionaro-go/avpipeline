// Package voiceanon provides preset constructors for voice anonymization
// kernels with sensible default configurations.
package voiceanon

import (
	"context"
	"sync/atomic"

	voiceanonkernel "github.com/xaionaro-go/avpipeline/kernel/voiceanon"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
)

// NewAnonymizer creates a VoiceAnonymizer kernel with default settings
// (pitch=0.7, formant preservation enabled). Pass enabled=nil for always-on.
func NewAnonymizer(enabled *atomic.Bool) *voiceanonkernel.VoiceAnonymizer {
	va := voiceanonkernel.New(voiceanonkernel.DefaultConfig())
	va.Enabled = enabled
	return va
}

// NewAnonymizerWithPitch creates a VoiceAnonymizer kernel with a custom pitch scale.
func NewAnonymizerWithPitch(enabled *atomic.Bool, pitchScale float64) *voiceanonkernel.VoiceAnonymizer {
	cfg := voiceanonkernel.DefaultConfig()
	cfg.PitchScale = pitchScale
	va := voiceanonkernel.New(cfg)
	va.Enabled = enabled
	return va
}

// NewNode wraps a VoiceAnonymizer kernel in a pipeline node.
func NewNode(
	ctx context.Context,
	va *voiceanonkernel.VoiceAnonymizer,
	procOpts ...processor.Option,
) *node.Node[*processor.FromKernel[*voiceanonkernel.VoiceAnonymizer]] {
	opts := append(processor.DefaultOptionsTranscoder(), procOpts...)
	return node.New(processor.NewFromKernel(ctx, va, opts...))
}
