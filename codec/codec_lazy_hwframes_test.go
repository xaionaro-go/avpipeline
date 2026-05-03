// codec_lazy_hwframes_test.go pins the contract of EnsureLazyHardwareFramesContext
// (mediacodec Surface-mode decoder hw_frames_ctx allocation).
//
// The behavioural pins:
//   - Returns an error when no hardware device context is configured (a SW
//     codec cannot lazily allocate an HFC).
//   - Returns an error when no hardware pixel format is configured.
//   - Returns an error on invalid dimensions.
//   - LazyHardwareFramesContext returns nil before any successful allocation.
//
// Live-allocation paths (CAS install, CAS race, freed-at-close) require a real
// HW device context and are exercised by the encoder integration tests on the
// phone (kernel/encoder.go's getScaledFrame mediacodec branch); unit-mocking
// AVHWDeviceContext is not viable.

package codec

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
)

func TestCodec_EnsureLazyHardwareFramesContext_NoHWDevice_Errors(t *testing.T) {
	ctx := context.Background()
	c := &Codec{
		codecInternals: &codecInternals{},
	}
	hfc, err := c.EnsureLazyHardwareFramesContext(ctx, 1920, 1920, astiav.PixelFormatNv12, 0)
	assert.Nil(t, hfc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no hardware device context")
}

func TestCodec_LazyHardwareFramesContext_NilBeforeAllocation(t *testing.T) {
	c := &Codec{
		codecInternals: &codecInternals{},
	}
	assert.Nil(t, c.LazyHardwareFramesContext())
}

func TestCodec_EnsureLazyHardwareFramesContext_ReturnsExistingHFC(t *testing.T) {
	// When c.hardwareFramesContext is already populated (init-time allocation
	// path, e.g. cuvid via initHardwareFramesContext), the lazy method must
	// short-circuit and return the existing pointer without allocating.
	ctx := context.Background()
	existing := &astiav.HardwareFramesContext{} // sentinel
	c := &Codec{
		codecInternals: &codecInternals{
			hardwareFramesContext: existing,
		},
	}
	got, err := c.EnsureLazyHardwareFramesContext(ctx, 1920, 1920, astiav.PixelFormatNv12, 8)
	assert.NoError(t, err)
	assert.Same(t, existing, got)
	// Lazy slot is untouched.
	assert.Nil(t, c.LazyHardwareFramesContext())
}
