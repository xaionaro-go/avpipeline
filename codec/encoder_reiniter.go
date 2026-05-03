// encoder_reiniter.go defines the optional EncoderReiniter capability
// for encoders that can be torn down and reinitialized in place.

package codec

import "context"

// EncoderReiniter is implemented by encoders that support an explicit,
// idempotent close+reopen of the underlying codec context.
//
// Reinit closes the current codec instance and opens a fresh one with
// the same InitParams and Quality. It is intended for instrumented
// canary measurement of the reconfig pause, and as a building block
// for higher-level operations that already implicitly reinitialize
// (SetResolution / SetQuality with a codec change).
//
// Dummy encoders (copy / raw) deliberately do not implement this
// interface — they have no codec context to reopen.
type EncoderReiniter interface {
	Reinit(ctx context.Context) error
}
