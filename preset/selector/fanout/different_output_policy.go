package fanout

import (
	"context"
	"errors"
)

var ErrDifferentOutputsNotAllowed = errors.New("different outputs not allowed")

type DifferentOutputPolicy interface {
	AllowsDifferentOutputs(ctx context.Context) bool
}

type DifferentOutputPolicyFunc func(ctx context.Context) bool

func (f DifferentOutputPolicyFunc) AllowsDifferentOutputs(
	ctx context.Context,
) bool {
	return f(ctx)
}

type ModeDifferentOutputPolicy struct {
	mode Mode
}

func NewDifferentOutputPolicy(
	mode Mode,
) *ModeDifferentOutputPolicy {
	return &ModeDifferentOutputPolicy{mode: mode}
}

func (p *ModeDifferentOutputPolicy) AllowsDifferentOutputs(
	context.Context,
) bool {
	switch p.mode {
	case ModeDifferentOutputsSameTracks, ModeDifferentOutputsSameTracksSplitAV:
		return true
	default:
		return false
	}
}
