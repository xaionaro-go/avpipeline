// from_kernel_caller_ctx_cancel_test.go pins down the goroutine-
// survival contract for NewFromKernel: passing the caller's ctx to
// startProcessing leaks ctx-cancellation into the three long-lived
// processor goroutines (preOutputCh forwarder, readerLoop, Generate).
// When the caller is a request-scoped handler — most concretely
// ffstream's gRPC AddInput RPC, which constructs FromKernel chains via
// preset/inputwithfallback's input_chain factory — gRPC cancels the
// per-call ctx the moment the RPC returns. Without the
// xcontext.DetachDone wrap at from_kernel.go's NewFromKernel, the
// preOutputCh forwarder goroutine sees ctx.Done immediately, runs its
// `defer close(p.OutputCh)`, and downstream
// NodeWithCustomData.Serve returns sendErr(io.EOF) — cascading EOF
// through the entire chain before any frame can flow.
//
// The survival contract this file pins down: cancelling the caller's
// ctx the moment NewFromKernel returns must NOT close p.OutputCh. The
// witness is direct — we read from OutputCh in a goroutine; if it's
// closed (the bug) the read unblocks immediately with ok=false; if
// it's open (the fix) the read blocks indefinitely until the test's
// own Close path tears the processor down.
//
// Falsifier: revert from_kernel.go's NewFromKernel
// `p.startProcessing(xcontext.DetachDone(ctx))` to bare
// `p.startProcessing(ctx)` — this test must fail (outputClosed channel
// fires within the cancel-window because the forwarder goroutine sees
// rpcCtx.Done and runs its deferred close on p.OutputCh).

package processor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFromKernel_StartProcessing_SurvivesCallerCtxCancel(t *testing.T) {
	// daemonCtx is the lifetime of the test — it stays alive so the
	// processor's own goroutines have a parent context to derive from
	// once xcontext.DetachDone severs the cancel-chain. (DetachDone
	// preserves values but stops cancellation propagation.)
	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	defer daemonCancel()

	// rpcCtx mimics gRPC's per-call ctx: cancelled the moment the RPC
	// returns. We construct under rpcCtx, then cancel it before
	// observing whether the spawned goroutines survived.
	rpcCtx, rpcCancel := context.WithCancel(daemonCtx)
	k := &mockKernel{StringVal: "ctx-cancel-survival-kernel"}
	p := NewFromKernel[*mockKernel](rpcCtx, k)
	require.NotNil(t, p)
	rpcCancel()

	// Witness goroutine: closes outputClosed iff p.OutputCh closes.
	// Reading a closed channel returns ok=false immediately; reading an
	// open channel blocks. This is the channel-signal version of "is
	// the forwarder goroutine still alive?" without resorting to
	// polling-with-sleeps.
	outputClosed := make(chan struct{})
	go func() {
		// Drain any output that happens to flow (none expected from a
		// mockKernel with no GenerateFn) until the channel closes.
		for range p.OutputChan() {
		}
		close(outputClosed)
	}()

	t.Cleanup(func() {
		_ = p.Close(context.Background())
		// After Close, OutputCh closes and outputClosed should fire.
		// We don't assert here — the cleanup path exists to free the
		// witness goroutine, not to verify behavior.
	})

	// Pre-fix (bug present): the preOutputCh forwarder goroutine sees
	// rpcCtx.Done immediately and runs `defer close(p.OutputCh)`. The
	// witness goroutine's `for range` returns and closes outputClosed
	// well within the 500ms observation window.
	//
	// Post-fix: rpcCtx-cancel does not propagate (DetachDone). The
	// forwarder waits on its own detached ctx (cancelled only by the
	// processor's closer in t.Cleanup). outputClosed stays open
	// throughout the window.
	const observationWindow = 500 * time.Millisecond
	select {
	case <-outputClosed:
		t.Fatal("p.OutputCh closed after rpcCtx cancel — the FromKernel processor's preOutputCh forwarder goroutine inherited the cancelled rpcCtx and ran its deferred `close(p.OutputCh)` before the processor's own closer fired (xcontext.DetachDone fix at NewFromKernel has regressed)")
	case <-time.After(observationWindow):
		// Pass: forwarder survived the rpcCtx cancel, OutputCh is
		// still open.
	}
}

// TestFromKernel_StartProcessing_OutputCloseOnExplicitClose is the
// dual-sided complement: when the processor is explicitly Close()d,
// p.OutputCh DOES close (proving the witness pattern can detect
// closure when it actually happens — guards against false-positive
// passes where outputClosed would never fire even with the bug).
func TestFromKernel_StartProcessing_OutputCloseOnExplicitClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k := &mockKernel{}
	p := NewFromKernel[*mockKernel](ctx, k)
	require.NotNil(t, p)

	outputClosed := make(chan struct{})
	go func() {
		for range p.OutputChan() {
		}
		close(outputClosed)
	}()

	require.NoError(t, p.Close(ctx))

	select {
	case <-outputClosed:
		// Pass: explicit Close shut down the forwarder, OutputCh
		// closed, witness goroutine returned. Confirms the witness
		// pattern is sensitive enough to detect closure.
	case <-time.After(2 * time.Second):
		t.Fatal("p.OutputCh did not close after explicit Close — the witness pattern is broken or Close has a regression")
	}
}
