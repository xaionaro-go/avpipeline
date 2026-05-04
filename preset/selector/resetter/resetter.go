package resetter

import "context"

// Resetter clears state for a reusable downstream component.
type Resetter interface {
	Reset(ctx context.Context) error
}
