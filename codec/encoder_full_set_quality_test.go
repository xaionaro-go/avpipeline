package codec

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/quality"
)

// TestEncoderFull_SetQuality_PersistsAcrossReinit pins the upward-recovery
// contract: after SetQuality(newQ) the encoder must report newQ via
// GetQuality, and the Reinit-rebuilt codec must be opened at newQ's bitrate
// (not the open-time bitrate from InitParams.CodecParameters).
//
// The mediacodec setQuality path historically left e.Quality untouched,
// so reinitEncoder reseeded the new encoder with a stale Quality and the
// reopened codec inherited InitParams.CodecParameters' open-time bitrate.
// On phones this presented as: after a downward Static drive followed by
// an upward Static raise, the encoded bitrate stayed pinned low.
//
// This test runs against the generic path (libx264/mpeg4) which goes
// through setQualityGeneric — exercising the same Reinit contract.
// Falsification: comment out `e.Quality = q` in setQualityGeneric and
// the post-Reinit assertion fails because newEncoderFullLocked is invoked
// with the prior Quality.
func TestEncoderFull_SetQuality_PersistsAcrossReinit(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()

	full, ok := enc.(*EncoderFull)
	require.True(t, ok, "newTestVideoEncoder must return *EncoderFull")

	const targetBitRate = 750_000
	require.NoError(t, enc.SetQuality(ctx, quality.ConstantBitrate(targetBitRate), nil))

	gotQ := enc.GetQuality(ctx)
	require.NotNil(t, gotQ)
	cbr, ok := gotQ.(quality.ConstantBitrate)
	require.True(t, ok, "expected ConstantBitrate, got %T", gotQ)
	assert.Equal(t, quality.ConstantBitrate(targetBitRate), cbr,
		"GetQuality must reflect the most recent SetQuality")

	// e.Quality is the SSOT used by reinitEncoder to seed the rebuilt
	// codec. Direct inspection guards the persistence contract.
	require.NoError(t, full.LockDo(ctx, func(ctx context.Context, inner Encoder) error {
		locked, ok := inner.(*EncoderFullLocked)
		require.True(t, ok)
		ecbr, ok := locked.Quality.(quality.ConstantBitrate)
		require.True(t, ok, "e.Quality must be ConstantBitrate, got %T", locked.Quality)
		assert.Equal(t, quality.ConstantBitrate(targetBitRate), ecbr,
			"e.Quality must be set to the most recent SetQuality target")
		return nil
	}))

	// Reinit rebuilds the codec from e.InitParams + e.Quality. The new
	// encoder's bitrate must reflect the most recent SetQuality target.
	reiniter, ok := enc.(EncoderReiniter)
	require.True(t, ok)
	require.NoError(t, reiniter.Reinit(ctx))

	gotQAfter := enc.GetQuality(ctx)
	require.NotNil(t, gotQAfter, "GetQuality must not be nil after Reinit")
	cbrAfter, ok := gotQAfter.(quality.ConstantBitrate)
	require.True(t, ok, "expected ConstantBitrate after Reinit, got %T", gotQAfter)
	assert.Equal(t, quality.ConstantBitrate(targetBitRate), cbrAfter,
		"after Reinit the rebuilt encoder must report the most recent SetQuality bitrate")
}

// TestEncoderFull_SetQuality_LowToHighRecovery exercises the multi-step
// drive that the wedge regression presented on production phones:
// raise → drop → raise. The reported quality must follow the most recent
// SetQuality call, including across an explicit Reinit (which the
// resolution-change and flush-without-cap paths invoke internally).
func TestEncoderFull_SetQuality_LowToHighRecovery(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()

	const high = 4_000_000
	const low = 500_000

	require.NoError(t, enc.SetQuality(ctx, quality.ConstantBitrate(high), nil))
	require.NoError(t, enc.SetQuality(ctx, quality.ConstantBitrate(low), nil))
	require.NoError(t, enc.SetQuality(ctx, quality.ConstantBitrate(high), nil))

	got := enc.GetQuality(ctx)
	require.NotNil(t, got)
	cbr, ok := got.(quality.ConstantBitrate)
	require.True(t, ok)
	assert.Equal(t, quality.ConstantBitrate(high), cbr,
		"after low→high recovery the encoder must report the high bitrate")

	reiniter, ok := enc.(EncoderReiniter)
	require.True(t, ok)
	require.NoError(t, reiniter.Reinit(ctx))

	gotAfter := enc.GetQuality(ctx)
	require.NotNil(t, gotAfter, "GetQuality must not be nil after Reinit")
	cbrAfter, ok := gotAfter.(quality.ConstantBitrate)
	require.True(t, ok, "expected ConstantBitrate after Reinit, got %T", gotAfter)
	assert.Equal(t, quality.ConstantBitrate(high), cbrAfter,
		"after Reinit the rebuilt encoder must still report the most recent SetQuality bitrate")
}
