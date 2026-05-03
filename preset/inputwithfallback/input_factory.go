// input_factory.go defines the interface for creating input kernels and decoders.

package inputwithfallback

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec"
)

type InputFactory[K InputKernel, DF codec.DecoderFactory, C any] interface {
	fmt.Stringer
	NewInput(
		ctx context.Context,
		chain *InputChain[K, DF, C],
	) (K, error)
	NewDecoderFactory(
		ctx context.Context,
		chain *InputChain[K, DF, C],
	) (DF, error)
}

// InputFactoryWithAvailability is an optional interface implemented by
// InputFactory values that know whether their chain currently holds
// any input resources. The fallback walk in
// InputWithFallback.onInputChainError consults HasResources when
// looking for the next chain to switch to: chains whose factory
// reports HasResources=false are SKIPPED, and the walk continues to
// the next occupied chain.
//
// Without this interface, the fallback walk advances by +1
// sequentially. That works for dense chain layouts but races itself
// for sparse layouts (e.g. chain 0 occupied, chains 1..9 empty,
// chain 10 occupied): each empty chain forces a switch attempt that
// contends the switching latch (procN) and serializes with the next
// step's onInputChainError invocation, producing the
// "another switch is in progress (procN: N), cannot switch to N+1"
// error class and never reaching the occupied chain at the far end.
//
// Implementations are expected to be cheap and side-effect-free —
// HasResources is invoked under InputChainsLocker on the error path.
//
// Production caller: InputWithFallback.onInputChainError's fallback-
// walk skipper. The fallback-walk skipper is load-bearing for sparse
// chain layouts (procN-latch race avoidance) — do NOT remove this
// interface without first confirming the caller has been migrated.
type InputFactoryWithAvailability interface {
	HasResources(ctx context.Context) bool
}
