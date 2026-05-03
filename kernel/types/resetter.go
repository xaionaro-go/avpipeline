// resetter.go defines the Resetter interface for stateful objects whose
// per-stream observation/derived state must be invalidated when the
// chain that produced those observations tears down.

package types

import "context"

// Resetter is implemented by stateful objects (kernels, conditions) that
// hold per-stream observation state requiring invalidation when the
// chain that produced those observations tears down. Reset must NOT
// clear operator-configured state (offsets, thresholds, enables) —
// only observed/derived state.
type Resetter interface {
	Reset(ctx context.Context) error
}
