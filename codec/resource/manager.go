// manager.go defines the ResourceManager interface for managing codec resources.

// Package resource provides utilities for managing codec resources like hardware devices.
package resource

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/types"
)

type ResourceManager interface {
	GetReusabler
	FreeUnneededer
}

type GetReusabler interface {
	GetReusable(
		ctx context.Context,
		isEncoder bool,
		params *astiav.CodecParameters,
		timeBase astiav.Rational,
		opts ...types.Option,
	) *Resources
}

type FreeUnneededer interface {
	FreeUnneeded(
		ctx context.Context,
		resourceType Type,
		codec *astiav.Codec,
		opts ...types.Option,
	) uint
}
