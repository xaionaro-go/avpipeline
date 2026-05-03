// encoder_scaler_descriptors_test.go pins the validateScalerDescriptors
// precondition guard. Each test breaks one descriptor in isolation and
// asserts the resulting error names that field — failure-mode
// falsification: replacing the validator body with `return nil` makes
// every test fail.

package kernel

import (
	"errors"
	"strings"
	"testing"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
)

func validResolution() codectypes.Resolution {
	return codectypes.Resolution{Width: 1920, Height: 1920}
}

// TestValidateScalerDescriptors_AllValid pins the GOOD path: NV12 SW
// frames at 1920x1920 on both sides — the canonical camera-active +
// av1_mediacodec pipeline — must validate clean.
func TestValidateScalerDescriptors_AllValid(t *testing.T) {
	err := validateScalerDescriptors(
		validResolution(), astiav.PixelFormatNv12,
		validResolution(), astiav.PixelFormatNv12,
	)
	testifyassert.NoError(t, err)
}

// TestValidateScalerDescriptors_SrcZeroWidth pins the source-side
// "first camera frame race" hypothesis — frame Width=0 before the
// rawvideo decoder populated metadata. sws_getContext returns NULL on
// this; validator must catch it with a named reason.
func TestValidateScalerDescriptors_SrcZeroWidth(t *testing.T) {
	err := validateScalerDescriptors(
		codectypes.Resolution{Width: 0, Height: 1920}, astiav.PixelFormatNv12,
		validResolution(), astiav.PixelFormatNv12,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed), "error must be ErrInvalidScalerDescriptors, got %T: %v", err, err)
	testifyassert.True(t,
		anyReasonContains(typed.Reasons, "source resolution"),
		"reasons must name the source-side offender; got %v", typed.Reasons,
	)
}

// TestValidateScalerDescriptors_SrcZeroHeight mirrors SrcZeroWidth on
// the height axis (different code branch in the validator).
func TestValidateScalerDescriptors_SrcZeroHeight(t *testing.T) {
	err := validateScalerDescriptors(
		codectypes.Resolution{Width: 1920, Height: 0}, astiav.PixelFormatNv12,
		validResolution(), astiav.PixelFormatNv12,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed))
	testifyassert.True(t, anyReasonContains(typed.Reasons, "source resolution"), "got %v", typed.Reasons)
}

// TestValidateScalerDescriptors_DstZeroWidth pins the destination-
// side variant: encoder reports zero output resolution. Possible if
// SenderKey.VideoResolution was zero-initialized by an upstream race.
func TestValidateScalerDescriptors_DstZeroWidth(t *testing.T) {
	err := validateScalerDescriptors(
		validResolution(), astiav.PixelFormatNv12,
		codectypes.Resolution{Width: 0, Height: 1920}, astiav.PixelFormatNv12,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed))
	testifyassert.True(t, anyReasonContains(typed.Reasons, "destination resolution"), "got %v", typed.Reasons)
}

// TestValidateScalerDescriptors_SrcPixFmtNone pins the documented
// hypothesis: AllocFrame'd input.Frame with format unset → AV_PIX_FMT_NONE
// → sws_getContext NULL. Validator must catch this and name source-side.
func TestValidateScalerDescriptors_SrcPixFmtNone(t *testing.T) {
	err := validateScalerDescriptors(
		validResolution(), astiav.PixelFormatNone,
		validResolution(), astiav.PixelFormatNv12,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed))
	testifyassert.True(t, anyReasonContains(typed.Reasons, "source pixel format is AV_PIX_FMT_NONE"), "got %v", typed.Reasons)
}

// TestValidateScalerDescriptors_DstPixFmtNone mirrors the source case
// on the destination side. Documented sws_getContext failure mode.
func TestValidateScalerDescriptors_DstPixFmtNone(t *testing.T) {
	err := validateScalerDescriptors(
		validResolution(), astiav.PixelFormatNv12,
		validResolution(), astiav.PixelFormatNone,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed))
	testifyassert.True(t, anyReasonContains(typed.Reasons, "destination pixel format is AV_PIX_FMT_NONE"), "got %v", typed.Reasons)
}

// TestValidateScalerDescriptors_DstPixFmtMediaCodec pins the
// alternative hypothesis: late-nv12-injection failed to land on the
// encoder's CodecContext, so CodecContext.PixelFormat() and
// ScaledFrame.PixelFormat() are AV_PIX_FMT_MEDIACODEC. libswscale
// cannot produce a HW pixfmt and returns NULL. Validator must catch
// this with a HW-pixfmt reason.
func TestValidateScalerDescriptors_DstPixFmtMediaCodec(t *testing.T) {
	err := validateScalerDescriptors(
		validResolution(), astiav.PixelFormatNv12,
		validResolution(), astiav.PixelFormatMediacodec,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed))
	testifyassert.True(t,
		anyReasonContains(typed.Reasons, "destination pixel format") && anyReasonContains(typed.Reasons, "hardware pixfmt"),
		"got %v", typed.Reasons,
	)
}

// TestValidateScalerDescriptors_SrcPixFmtMediaCodec covers the
// upstream variant: source frame in HW pixfmt (e.g. mediacodec
// decoder feeding the SW scaler). The encoder.go bypass path
// (shouldBypassScaler) is supposed to short-circuit this case
// before prepareScaler — but if it ever doesn't, the validator
// is the second line of defense.
func TestValidateScalerDescriptors_SrcPixFmtMediaCodec(t *testing.T) {
	err := validateScalerDescriptors(
		validResolution(), astiav.PixelFormatMediacodec,
		validResolution(), astiav.PixelFormatNv12,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed))
	testifyassert.True(t,
		anyReasonContains(typed.Reasons, "source pixel format") && anyReasonContains(typed.Reasons, "hardware pixfmt"),
		"got %v", typed.Reasons,
	)
}

// TestValidateScalerDescriptors_MultipleReasons pins that all
// offenders are reported (not just the first), so a single repro
// run names every degenerate field at once.
func TestValidateScalerDescriptors_MultipleReasons(t *testing.T) {
	err := validateScalerDescriptors(
		codectypes.Resolution{Width: 0, Height: 0}, astiav.PixelFormatNone,
		codectypes.Resolution{Width: 0, Height: 0}, astiav.PixelFormatNone,
	)
	require.Error(t, err)
	var typed ErrInvalidScalerDescriptors
	require.True(t, errors.As(err, &typed))
	testifyassert.GreaterOrEqual(t, len(typed.Reasons), 4,
		"every degenerate dimension+pixfmt must be reported, got %v", typed.Reasons,
	)
}

// TestErrInvalidScalerDescriptors_ErrorString pins the format of the
// surfaced error message: callers grep for the descriptor values, so
// a refactor that drops them silently degrades debuggability.
func TestErrInvalidScalerDescriptors_ErrorString(t *testing.T) {
	e := ErrInvalidScalerDescriptors{
		SrcResolution: codectypes.Resolution{Width: 1920, Height: 1920},
		SrcPixFmt:     astiav.PixelFormatNone,
		DstResolution: codectypes.Resolution{Width: 1920, Height: 1920},
		DstPixFmt:     astiav.PixelFormatNv12,
		Reasons:       []string{"source pixel format is AV_PIX_FMT_NONE"},
	}
	msg := e.Error()
	testifyassert.Contains(t, msg, "1920x1920")
	testifyassert.Contains(t, msg, astiav.PixelFormatNone.String())
	testifyassert.Contains(t, msg, astiav.PixelFormatNv12.String())
	testifyassert.Contains(t, msg, "source pixel format is AV_PIX_FMT_NONE")
}

func anyReasonContains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if strings.Contains(h, needle) {
			return true
		}
	}
	return false
}
