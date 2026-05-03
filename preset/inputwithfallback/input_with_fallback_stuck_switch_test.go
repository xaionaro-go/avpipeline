// input_with_fallback_stuck_switch_test.go regresses the
// switchingProcN-leak bug: when the InputSyncer's KeepUnless predicate
// never returns true post-switch (stale priority chain), the gated
// decrement was skipped and switchingProcN stuck above zero, so every
// subsequent SetValue was rejected by the OnSwitchRequest gate with
// "another switch is in progress (procN: N)" until process restart.
//
// The fix introduces a generation counter on the OnBeforeSwitch →
// InputSyncer-KeepUnless cycle: each new switch claims a fresh
// generation, superseding any prior in-flight cycle whose KeepUnless
// never matched. This test drives KeepUnless to return false 100×, then
// invokes a fresh OnSwitchRequest and asserts switchingProcN converges
// to zero — both the synchronous error-path and the asynchronous
// counter convergence are deterministic (no time.Sleep racing).

package inputwithfallback

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

func TestInputWithFallback_StuckSwitchRecovers(t *testing.T) {
	ctx := context.Background()
	f0 := &mockInputFactory{name: "primary"}
	f1 := &mockInputFactory{name: "fallback1"}
	f2 := &mockInputFactory{name: "fallback2"}
	iwf := newTestIWF(t, f0, f1, f2)
	iwf.InputSwitch.CurrentValue.Store(0)

	// Phase 1: drive the lifecycle that commitToNextValue runs inside
	// CommitMutex when InputSwitch.KeepUnless allows a 0 → 1 transition.
	// OnBeforeSwitch claims the syncer reservation; OnAfterSwitch primes
	// syncingSince and starts the InputSyncer's wait window.
	onBefore := iwf.InputSwitch.GetOnBeforeSwitch()
	require.NotNil(t, onBefore)
	onBefore(ctx, packetorframe.InputUnion{}, 0, 1)

	onAfter := iwf.InputSwitch.GetOnAfterSwitch()
	require.NotNil(t, onAfter)
	onAfter(ctx, packetorframe.InputUnion{}, 0, 1)

	// Phase 2: drive InputSyncer's KeepUnless to return false 100× —
	// stale-priority-chain semantics where no packet ever satisfies the
	// predicate. Each false return leaves switchingProcN held above zero.
	keepUnless := iwf.InputSyncer.GetKeepUnless()
	require.NotNil(t, keepUnless)
	in := packetorframe.InputUnion{
		Packet: &packet.Input{StreamInfo: &packetorframetypes.StreamInfo{}},
	}
	for n := 0; n < 100; n++ {
		require.False(t, keepUnless.Match(ctx, in), "iteration %d", n)
	}
	require.NotZero(t, iwf.switchingProcN.Load(),
		"leak invariant: OnBeforeSwitch's reservation must be live before recovery")

	// Phase 3: a fresh switch request must recover synchronously.
	// Pre-fix: OnSwitchRequest's "another switch is in progress" gate
	// rejects because the stuck reservation is still on switchingProcN.
	// Post-fix: OnSwitchRequest releases the stale syncer cycle first
	// (via syncingGen.Swap(0) and switchingProcN.Add(-1)), so the gate
	// sees a clean state and the new attempt succeeds.
	onSwitchReq := iwf.InputSwitch.GetOnSwitchRequest()
	require.NotNil(t, onSwitchReq)
	// Use to=999: the spawned async goroutine returns immediately when
	// getInputChainByID misses, so its deferred decrement runs without
	// touching real Pause/Unpause paths. This keeps the convergence
	// assertion below independent of retry-kernel async work.
	require.NoError(t, onSwitchReq(ctx, packetorframe.InputUnion{}, 999))

	// Phase 4: switchingProcN must converge to 0. Recovery released the
	// stuck reservation synchronously; the brief async work spawned by
	// OnSwitchRequest self-decrements via defer. Eventually polls a
	// deterministic condition; if the leak regresses, the poll reaches
	// the deadline and the test fails.
	require.Eventually(t, func() bool {
		return iwf.switchingProcN.Load() == 0
	}, 2*time.Second, time.Millisecond,
		"switchingProcN failed to converge to 0; leak regressed")
}
