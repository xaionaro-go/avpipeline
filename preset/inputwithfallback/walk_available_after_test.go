// walk_available_after_test.go pins the SSOT next-available-priority
// scan contract against the two production caller patterns
// (onInputChainError + ffstream.RemoveInput).

package inputwithfallback

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeChain implements InputChainAvailability for the helper's
// generic constraint. The factory field is returned verbatim from
// AvailabilityFactory so tests can inject typed-nil and concrete
// (with/without InputFactoryWithAvailability) values.
type fakeChain struct {
	factory any
}

func (f *fakeChain) AvailabilityFactory() any {
	if f == nil {
		return nil
	}
	return f.factory
}

// fakeFactoryWithAvailability satisfies InputFactoryWithAvailability
// with an injectable HasResources verdict. Used to exercise the
// optional-interface branch.
type fakeFactoryWithAvailability struct {
	available bool
}

func (f *fakeFactoryWithAvailability) HasResources(_ context.Context) bool {
	return f.available
}

// fakeFactoryWithoutAvailability does NOT implement
// InputFactoryWithAvailability — used to verify the legacy `!ok`-as-
// available semantics is preserved.
type fakeFactoryWithoutAvailability struct{}

// TestWalkAvailableAfter_SkipsUnavailable pins that a candidate whose
// factory implements InputFactoryWithAvailability and reports
// HasResources=false is skipped, and the next available factory is
// returned.
func TestWalkAvailableAfter_SkipsUnavailable(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithAvailability{available: true}}, // id 0 — caller starts walk AFTER this
		{factory: &fakeFactoryWithAvailability{available: false}},
		{factory: &fakeFactoryWithAvailability{available: false}},
		{factory: &fakeFactoryWithAvailability{available: true}},
	}
	require.Equal(t, 3, WalkAvailableAfter(ctx, chains, 0))
}

// TestWalkAvailableAfter_NoOptInIsAvailable pins the legacy
// dense-walk behavior: factories that do NOT implement
// InputFactoryWithAvailability are treated as candidates (the +1
// advance under the original SSOT). Falsifier: if the helper required
// the opt-in interface it would skip these and return -1.
func TestWalkAvailableAfter_NoOptInIsAvailable(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithoutAvailability{}},
		{factory: &fakeFactoryWithoutAvailability{}},
	}
	require.Equal(t, 1, WalkAvailableAfter(ctx, chains, 0))
}

// TestWalkAvailableAfter_NilChainSkipped pins the defensive
// nil-chain skip — the production callers populate the slice
// densely today, but a future sparse-population scheme must not
// crash the helper.
//
// Mechanism: this test passes via the factory==nil branch in
// WalkAvailableAfter. fakeChain.AvailabilityFactory has a
// nil-receiver guard (mirroring (*InputChain).AvailabilityFactory)
// that returns nil when the receiver pointer is nil — the helper
// then `continue`s without dereferencing.
//
// Falsifier: if fakeChain's nil-receiver guard (or the production
// (*InputChain).AvailabilityFactory's guard) were removed,
// AvailabilityFactory would dereference the nil receiver and the
// test would panic before returning 2. Equivalently, a panicking
// chain entry between the nil and the available one would surface
// if the helper visited the panicking entry.
func TestWalkAvailableAfter_NilChainSkipped(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithAvailability{available: true}},
		nil, // sparse slot
		{factory: &fakeFactoryWithAvailability{available: true}},
	}
	require.Equal(t, 2, WalkAvailableAfter(ctx, chains, 0))
}

// TestWalkAvailableAfter_NilFactorySkipped pins that a chain whose
// AvailabilityFactory() returns nil is treated as unavailable.
// Without this, an InputChainsLocker-protected factory swap would
// momentarily expose a nil factory and the walk would crash on the
// type assertion.
func TestWalkAvailableAfter_NilFactorySkipped(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithAvailability{available: true}},
		{factory: nil},
		{factory: &fakeFactoryWithAvailability{available: true}},
	}
	require.Equal(t, 2, WalkAvailableAfter(ctx, chains, 0))
}

// TestWalkAvailableAfter_NoFallback pins that a walk past the last
// chain returns -1 — the caller signals "stay put, idle the active
// chain" on this verdict.
func TestWalkAvailableAfter_NoFallback(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithAvailability{available: true}},
		{factory: &fakeFactoryWithAvailability{available: false}},
	}
	require.Equal(t, -1, WalkAvailableAfter(ctx, chains, 0))
}

// TestWalkAvailableAfter_StartsStrictlyAfter pins that the walk
// starts at id+1, not at id. Falsifier: if the helper started at id
// the walk on a 1-chain slice with id=0 and HasResources=false would
// return 0 instead of -1.
func TestWalkAvailableAfter_StartsStrictlyAfter(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithAvailability{available: false}},
	}
	require.Equal(t, -1, WalkAvailableAfter(ctx, chains, 0))
}

// TestWalkAvailableAfter_OutOfRangeStartID pins that an id past the
// end of the slice produces -1 without indexing past the slice.
func TestWalkAvailableAfter_OutOfRangeStartID(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithAvailability{available: true}},
	}
	require.Equal(t, -1, WalkAvailableAfter(ctx, chains, 5))
	require.Equal(t, -1, WalkAvailableAfter(ctx, chains, 0))
}

// TestWalkAvailableAfter_EmptySlice pins behavior on an empty slice.
func TestWalkAvailableAfter_EmptySlice(t *testing.T) {
	ctx := context.Background()
	require.Equal(t, -1, WalkAvailableAfter(ctx, []*fakeChain{}, 0))
	require.Equal(t, -1, WalkAvailableAfter[*fakeChain](ctx, nil, 0))
}

// TestWalkAvailableAfter_AllUnavailable pins that a slice of all-empty
// factories returns -1 — the caller's fallback verdict relies on this
// to "stay put" rather than auto-switch to a useless candidate.
func TestWalkAvailableAfter_AllUnavailable(t *testing.T) {
	ctx := context.Background()
	chains := []*fakeChain{
		{factory: &fakeFactoryWithAvailability{available: true}},
		{factory: &fakeFactoryWithAvailability{available: false}},
		{factory: &fakeFactoryWithAvailability{available: false}},
		{factory: &fakeFactoryWithAvailability{available: false}},
	}
	require.Equal(t, -1, WalkAvailableAfter(ctx, chains, 0))
}
