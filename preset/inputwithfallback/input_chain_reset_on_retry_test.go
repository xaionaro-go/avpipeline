// input_chain_reset_on_retry_test.go: after the upstream Retryable
// reopens its inner kernel on EOF, the downstream Decoder +
// AutoHeaders + Filter Barrier must shed state observed against the
// prior connection (per-stream codec contexts, IsSet flag, ptsBridge
// entries) so video flow is not blocked on every reconnect cycle.

package inputwithfallback

import (
	"context"
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/preset/autoheaders"
)

// TestInputChain_DownstreamResetOnRetry constructs an InputChain via
// the production newInputChain path with a NaiveDecoderFactory so that
// chain.Decoder + chain.AutoHeaders + chain.Filter are all wired. It
// then plants observable state on each downstream kernel and invokes
// the helper that the OnKernelOpen wrapper calls on every reopen.
//
// Pre-fix: helper does not exist (or only resets a subset). Each plant
// survives the call → assertions fail. Post-fix: each plant is cleared.
func TestInputChain_DownstreamResetOnRetry(t *testing.T) {
	ctx := context.Background()

	// NaiveDecoderFactory with empty params is safe to construct
	// without CGO codec init — initialization happens lazily inside
	// NewDecoder per stream, which is never called in this test.
	df := codec.NewNaiveDecoderFactory(ctx, nil)
	factory := &mockInputFactory{
		name:           "test-reset-on-retry",
		decoderFactory: df,
	}

	filterSwitch := stategetter.NewSwitch()
	syncSwitch := stategetter.NewSwitch()

	chain, err := newInputChain[*inputKernel, codec.DecoderFactory, struct{}](
		ctx,
		0, factory,
		filterSwitch.Output(0),
		syncSwitch.Output(0),
		false,
		0, // resetDownstreamKernelsTimeout: 0 → package default
		nil, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close(context.Background()) })

	// Sanity: all downstream kernels are wired.
	require.NotNil(t, chain.Filter, "Filter must be present")
	require.NotNil(t, chain.AutoHeaders, "AutoHeaders must be present")
	require.NotNil(t, chain.Decoder, "Decoder must be present (NaiveDecoderFactory non-nil)")

	// --- Plant observable state on each downstream kernel ---

	// Decoder: insert a sentinel StreamDecoder (Decoder field nil so
	// ResetHard's close-path is skipped, no CGO required). Reset must
	// delete the entry.
	dec := chain.Decoder.Processor.Kernel
	dec.Decoders[42] = &kernel.StreamDecoder{}
	require.Len(t, dec.Decoders, 1, "plant: Decoders[42] must be present")

	// AutoHeaders: drive the handler into "detected" state directly
	// (skip BSF/codec dispatch). Mark IsSet, bump CallCount, and
	// install a sentinel SelectedKernel so we can observe the clear.
	ahKernel := chain.AutoHeaders.Processor.Kernel.(*autoheaders.Kernel)
	ah := ahKernel.Handler
	ah.IsSet = true
	ah.CallCount.Store(7)
	ah.SelectedKernel = &kernel.Passthrough{}
	require.True(t, ah.IsSet, "plant: AutoHeaders IsSet must be true")
	require.NotNil(t, ah.SelectedKernel, "plant: SelectedKernel must be set")

	// --- Drive the reset path that OnKernelOpen would invoke ---
	require.NoError(t, chain.resetDownstreamKernels(ctx))

	// --- Assertions ---

	testifyassert.Empty(t, dec.Decoders,
		"Decoder.Reset must clear per-stream codec contexts so the next "+
			"connection re-derives them against the new stream params")
	testifyassert.False(t, ah.IsSet,
		"AutoHeaders.Reset must clear IsSet so detection reruns on the "+
			"first packet of the new connection")
	testifyassert.Equal(t, uint64(0), ah.CallCount.Load(),
		"AutoHeaders.Reset must zero CallCount so the once-only guard "+
			"does not reject the reopened path")
	testifyassert.Nil(t, ah.SelectedKernel,
		"AutoHeaders.Reset must clear SelectedKernel so the next "+
			"sendInputLocked re-runs detectAppropriateFixerKernel "+
			"against the freshly-observed stream rather than reusing "+
			"the prior connection's BSF selection")
}

// TestInputChain_OnKernelOpen_FirstOpenSkipsReset asserts the
// first-open optimization: the very first Retryable open is virgin —
// downstream kernels have no observed state to shed — so resetting on
// open #1 would be wasted work (and could disturb state planted by
// SetupHooks). Reset only runs from open #2 onward.
//
// Setup: invoke the chain's wrapper OnKernelOpen twice with planted
// state. After call #1, plant must survive. After call #2, plant must
// be cleared.
func TestInputChain_OnKernelOpen_FirstOpenSkipsReset(t *testing.T) {
	ctx := context.Background()

	df := codec.NewNaiveDecoderFactory(ctx, nil)
	factory := &mockInputFactory{
		name:           "test-firstopen-skip",
		decoderFactory: df,
	}
	filterSwitch := stategetter.NewSwitch()
	syncSwitch := stategetter.NewSwitch()
	chain, err := newInputChain[*inputKernel, codec.DecoderFactory, struct{}](
		ctx,
		0, factory,
		filterSwitch.Output(0),
		syncSwitch.Output(0),
		false,
		0, // resetDownstreamKernelsTimeout: 0 → package default
		nil, nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = chain.Close(context.Background()) })

	onKernelOpen := chain.Input.Processor.Kernel.Config.OnKernelOpen
	require.NotNil(t, onKernelOpen, "Retryable must be configured with OnKernelOpen")

	// Plant state and trigger open #1 — must NOT clear (virgin path).
	dec := chain.Decoder.Processor.Kernel
	dec.Decoders[1] = &kernel.StreamDecoder{}
	require.NoError(t, onKernelOpen(ctx, nil))
	testifyassert.Len(t, dec.Decoders, 1,
		"open #1 (virgin) must not call Reset — first-open optimization")

	// Plant again (in case open #1 was buggy and cleared) and trigger
	// open #2 — must clear.
	dec.Decoders[2] = &kernel.StreamDecoder{}
	require.NoError(t, onKernelOpen(ctx, nil))
	testifyassert.Empty(t, dec.Decoders,
		"open #2 (reopen) must call Reset on downstream kernels")
}
