// Package kernel provides the core functionality for the audio/video pipeline.
// This file defines abstract types and interfaces used throughout the kernel.
package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/kernel/typesnolibav"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type Abstract = types.Abstract

type Flusher interface {
	IsDirty(ctx context.Context) bool
	Flush(
		ctx context.Context,
		outputCh chan<- packetorframe.OutputUnion,
	) error
}

type HookFunc = typesnolibav.HookFunc
