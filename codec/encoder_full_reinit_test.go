package codec

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncoderFull_Reinit_RotatesInitTS verifies that Reinit closes the
// current codec and opens a fresh one. We assert via two independent
// observable side effects:
//  1. InitTS advances (newEncoderFullLocked sets InitTS = time.Now()).
//  2. The encoder is still functional (resolution survives).
func TestEncoderFull_Reinit_RotatesInitTS(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()

	reiniter, ok := enc.(EncoderReiniter)
	require.True(t, ok, "newTestVideoEncoder must return an EncoderReiniter")

	full, ok := enc.(*EncoderFull)
	require.True(t, ok, "newTestVideoEncoder must return *EncoderFull")

	resBefore := enc.GetResolution(ctx)
	require.NotNil(t, resBefore)
	tsBefore := full.GetInitTS()

	require.NoError(t, reiniter.Reinit(ctx))

	tsAfter := full.GetInitTS()
	assert.True(t, tsAfter.After(tsBefore),
		"InitTS must advance after Reinit (before=%v after=%v)", tsBefore, tsAfter)

	resAfter := enc.GetResolution(ctx)
	require.NotNil(t, resAfter)
	assert.Equal(t, *resBefore, *resAfter,
		"resolution must survive Reinit")
}

// TestEncoderFull_Reinit_LockedVariant verifies the locked variant is
// callable from inside LockDo without deadlocking.
func TestEncoderFull_Reinit_LockedVariant(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()

	err := enc.LockDo(ctx, func(ctx context.Context, inner Encoder) error {
		locked, ok := inner.(*EncoderFullLocked)
		require.True(t, ok, "LockDo callback must receive *EncoderFullLocked")
		return locked.Reinit(ctx)
	})
	require.NoError(t, err)
}
