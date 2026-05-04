// walk_available_after.go provides the shared "next-available-priority"
// scan used by both error-driven (onInputChainError) and external
// removal-driven (e.g. ffstream.RemoveInput) fallback walks. Single
// SSOT keeps both walks consistent (`!ok`-as-available semantics,
// nil-chain skip, upper bound).

package inputwithfallback

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/availability"
)

// InputChainAvailability is the abstract view of an input chain that
// WalkAvailableAfter needs: an InputFactory pointer (for the optional
// InputFactoryWithAvailability assertion) and a non-nil sentinel.
//
// We model it as an interface rather than the concrete generic
// InputChain[K, DF, C] so the helper is callable from packages that
// hold a slice of *InputChain values without re-importing the generic
// instantiation, AND from ffstream which holds a parallel slice of its
// own *InputChain alias type.
type InputChainAvailability interface {
	// AvailabilityFactory returns the underlying InputFactory typed as
	// any so the helper can perform the optional
	// InputFactoryWithAvailability assertion.
	//
	// Returning the bare pointer (not the typed generic) is intentional:
	// callers in different packages instantiate InputChain at different
	// generic parameters, so a typed accessor would force the helper to
	// be generic too — and a generic helper does not solve the SSOT
	// problem (each instantiation gets its own copy with a chance of
	// drifting). The any return keeps one runtime implementation.
	AvailabilityFactory() any
}

// WalkAvailableAfter scans candidates strictly after `id` in the order
// they appear in `chains` and returns the first index whose chain is
// non-nil AND whose factory either does NOT implement
// InputFactoryWithAvailability or reports HasResources(ctx)==true.
//
// Returns -1 if no such candidate exists.
//
// Semantics MUST match onInputChainError's walk
// (input_with_fallback.go) and ffstream's RemoveInput walk
// (pkg/ffstream/ffstream.go). Specifically:
//
//   - `!ok` on the InputFactoryWithAvailability assertion means the
//     factory has no availability check and is therefore treated as a
//     candidate. This is the legacy dense-walk behavior — a factory
//     opting out of availability semantics keeps the +1 advance.
//   - `nil` chain entries are skipped silently via the factory==nil
//     branch (AvailabilityFactory's nil-receiver guard returns nil
//     when the receiver pointer is nil). The two production callers
//     populate `chains` densely today, but the skip is defensive
//     against future sparse-population schemes (e.g. lazy chain
//     construction at AddFactory time).
//
// Preconditions: caller MUST hold InputChainsLocker (or whatever
// equivalent lock guards `chains` mutation). The helper itself is
// lock-free — it neither reads nor writes any global state — so the
// caller's locking remains the SSOT for chain-slice mutation.
func WalkAvailableAfter[T InputChainAvailability](
	ctx context.Context,
	chains []T,
	id int,
) int {
	candidates := make([]availability.Candidate, len(chains))
	for idx, chain := range chains {
		candidates[idx] = availabilityCandidate(chain)
	}
	candidate, ok := availability.FirstAvailableAfter(ctx, candidates, id)
	if !ok {
		return -1
	}
	return candidate
}

func availabilityCandidate[T InputChainAvailability](
	chain T,
) availability.Candidate {
	// Nil-chain guard: AvailabilityFactory has a nil-receiver guard
	// (see (*InputChain).AvailabilityFactory in input_chain.go) that
	// returns nil when the receiver pointer is nil. The factory==nil
	// check below covers nil chains for all pointer-typed T
	// instantiations.
	//
	// Note: any(chain)==nil cannot be used here as a nil-pointer
	// sentinel — boxing a typed-nil pointer into an interface
	// produces an interface value with a non-nil type descriptor,
	// so the comparison is always false for the production
	// pointer-typed T. Relying on it would be a silent dead check
	// that masks the load-bearing factory-nil guard.
	factory := chain.AvailabilityFactory()
	if factory == nil {
		return availability.Absent()
	}
	if source, ok := factory.(InputFactoryWithAvailability); ok {
		return availability.Present(source)
	}
	return availability.Present(nil)
}
