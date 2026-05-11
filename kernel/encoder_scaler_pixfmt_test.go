// encoder_scaler_pixfmt_test.go covers the HW-pixfmt scaler-destination
// rule: when the encoder reports a hardware pixel format (e.g.
// mediacodec / cuda), the scaler-destination ScaledFrame must NOT be
// allocated in that hwaccel pixfmt — av_frame_get_buffer fails with
// EINVAL on hwaccel formats. The tests pin
// selectScaledFramePixelFormat (extracted from prepareScaler) as a
// pure helper.
package kernel

import (
	"testing"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	testifyrequire "github.com/stretchr/testify/require"
)

// allocHWFramesCtxOrSkip returns a freshly-allocated
// HardwareFramesContext with the given hw and sw pixel formats. It
// requires a real HardwareDeviceContext to allocate the buffer; if
// no MediaCodec/CUDA/VAAPI device is available on this host (typical
// for CI / dev workstation), the test is skipped instead of failing.
//
// The returned hwfc is initialized only enough to expose
// HardwarePixelFormat() / SoftwarePixelFormat() -- av_hwframe_ctx_init
// is intentionally NOT called because that requires kernel-level
// device support we don't have in unit tests.
func allocHWFramesCtxOrSkip(t *testing.T, hwFmt, swFmt astiav.PixelFormat) *astiav.HardwareFramesContext {
	t.Helper()

	// Try mediacodec first, then cuda, then vaapi. Any one is enough
	// to exercise SoftwarePixelFormat.
	candidates := []astiav.HardwareDeviceType{
		astiav.HardwareDeviceTypeMediaCodec,
		astiav.HardwareDeviceTypeCUDA,
		astiav.HardwareDeviceTypeVAAPI,
	}

	for _, dt := range candidates {
		hdc, err := astiav.CreateHardwareDeviceContext(dt, "", nil, 0)
		if err != nil || hdc == nil {
			continue
		}
		hwfc := astiav.AllocHardwareFramesContext(hdc)
		if hwfc == nil {
			hdc.Free()
			continue
		}
		hwfc.SetWidth(1280)
		hwfc.SetHeight(720)
		hwfc.SetHardwarePixelFormat(hwFmt)
		hwfc.SetSoftwarePixelFormat(swFmt)
		// Don't Initialize -- that needs real device backing. The
		// getters work on the configured fields directly.
		t.Cleanup(func() {
			hwfc.Free()
			hdc.Free()
		})
		return hwfc
	}

	t.Skip("no usable hardware device context available on this host")
	return nil
}

// TestSelectScaledFramePixelFormat_SWFormatPassthrough verifies that
// software pixel formats are passed through unchanged: when the
// encoder is in HwDeviceCtx mode (or is a pure SW encoder), its
// codecContext.PixelFormat() is already a SW format -- the scaler
// must target that exact format so the encoder can consume the
// frame directly.
func TestSelectScaledFramePixelFormat_SWFormatPassthrough(t *testing.T) {
	t.Parallel()

	cases := []astiav.PixelFormat{
		astiav.PixelFormatNv12,
		astiav.PixelFormatYuv420P,
		astiav.PixelFormatYuv444P,
		astiav.PixelFormatRgba,
	}

	for _, pf := range cases {
		got := selectScaledFramePixelFormat(pf, nil)
		testifyassert.Equalf(t, pf, got,
			"SW pixfmt %s must be passed through unchanged regardless of hw_frames_ctx; got %s",
			pf, got)
	}
}

// TestSelectScaledFramePixelFormat_HWFormatNoHWFramesCtx exercises
// the fallback branch: the encoder reports a HW pixfmt but no
// hw_frames_ctx getter is available (encoder not yet fully
// initialized, or HardwareFramesContext returned nil). In that case
// the helper must fall back to NV12 -- the universal SW upload
// format for mediacodec / NVENC / VAAPI / Videotoolbox.
//
// BEFORE the fix, the hot path would set HW pixfmt on a SW frame and
// av_frame_get_buffer returned EINVAL. AFTER the fix, the scaler
// targets NV12 and the encoder uploads NV12 -> HW in SendFrame.
func TestSelectScaledFramePixelFormat_HWFormatNoHWFramesCtx(t *testing.T) {
	t.Parallel()

	hwFmts := []astiav.PixelFormat{
		astiav.PixelFormatMediacodec,
		astiav.PixelFormatCuda,
		astiav.PixelFormatVaapi,
		astiav.PixelFormatVideotoolbox,
	}

	for _, hw := range hwFmts {
		got := selectScaledFramePixelFormat(hw, nil)
		testifyassert.Equalf(t, fallbackScaledFrameSWPixelFormat, got,
			"HW pixfmt %s with nil hw_frames_ctx must fall back to %s; got %s",
			hw, fallbackScaledFrameSWPixelFormat, got)
		testifyassert.Falsef(t, isHardwarePixelFormat(got),
			"selected pixfmt for HW input %s must be a SW format (av_frame_get_buffer would EINVAL otherwise); got %s",
			hw, got)
	}
}

// TestSelectScaledFramePixelFormat_HWFormatWithHWFramesCtx covers
// the encoder-fully-initialized path: the hw_frames_ctx exposes its
// configured sw_format (NV12 for mediacodec, NV12 for nvenc on most
// builds), and the scaler should target THAT format so the encoder's
// SW->HW upload doesn't need to do an extra colorspace conversion.
//
// Skipped automatically if no HW device is available on the host.
func TestSelectScaledFramePixelFormat_HWFormatWithHWFramesCtx(t *testing.T) {
	// Intentionally NOT t.Parallel(): allocHWFramesCtxOrSkip touches
	// CGO HardwareDeviceContext globals (e.g. CUDA driver state) that
	// race with other parallel kernel-package tests. Run serially.
	hwfc := allocHWFramesCtxOrSkip(t, astiav.PixelFormatCuda, astiav.PixelFormatNv12)

	got := selectScaledFramePixelFormat(astiav.PixelFormatCuda, hwfc)
	testifyassert.Equal(t, astiav.PixelFormatNv12, got,
		"HW pixfmt with hw_frames_ctx.sw_format=NV12 must select NV12")
	testifyassert.False(t, isHardwarePixelFormat(got),
		"selected pixfmt must be a SW format")
}

// TestSelectScaledFramePixelFormat_HWFormatWithCorruptHWFramesCtxFallsBack
// verifies the defensive branch: if hw_frames_ctx exposes a HW or
// None sw_format (not a real configuration, but a degraded state we
// must survive), the helper falls back to NV12 rather than producing
// a HW frame.
func TestSelectScaledFramePixelFormat_HWFormatWithCorruptHWFramesCtxFallsBack(t *testing.T) {
	// Intentionally NOT t.Parallel(): allocHWFramesCtxOrSkip touches
	// CGO HardwareDeviceContext globals.
	// HardwareFramesContext exposes only setters/getters for the
	// fields we care about; a zero-value instance with a non-nil c
	// pointer would crash on .data() deref. Use a real allocation but
	// set sw_format to None (default).
	hwfc := allocHWFramesCtxOrSkip(t, astiav.PixelFormatCuda, astiav.PixelFormatNone)
	testifyrequire.Equal(t, astiav.PixelFormatNone, hwfc.SoftwarePixelFormat(),
		"precondition: sw_format must be None for this case")

	got := selectScaledFramePixelFormat(astiav.PixelFormatCuda, hwfc)
	testifyassert.Equal(t, fallbackScaledFrameSWPixelFormat, got,
		"HW pixfmt + sw_format=None must fall back to NV12")
}

// TestIsHardwarePixelFormat verifies the hwaccel-flag detection used
// to gate the HW->SW substitution. A regression here would cause us
// to either treat SW pixfmts as HW (over-substitution, breaks SW
// encoders) or HW pixfmts as SW (the original EINVAL surface).
func TestIsHardwarePixelFormat(t *testing.T) {
	t.Parallel()

	swFmts := []astiav.PixelFormat{
		astiav.PixelFormatNv12,
		astiav.PixelFormatYuv420P,
		astiav.PixelFormatYuv444P,
		astiav.PixelFormatRgba,
	}
	for _, pf := range swFmts {
		testifyassert.Falsef(t, isHardwarePixelFormat(pf),
			"%s must NOT be reported as a HW pixfmt", pf)
	}

	hwFmts := []astiav.PixelFormat{
		astiav.PixelFormatMediacodec,
		astiav.PixelFormatCuda,
		astiav.PixelFormatVaapi,
		astiav.PixelFormatVideotoolbox,
		astiav.PixelFormatDrmPrime,
		astiav.PixelFormatVdpau,
		astiav.PixelFormatQsv,
	}
	for _, pf := range hwFmts {
		testifyassert.Truef(t, isHardwarePixelFormat(pf),
			"%s MUST be reported as a HW pixfmt (HWACCEL flag in descriptor)", pf)
	}
}

// TestShouldBypassScaler_HWInputHWEncoder_SameResolution_BypassesScaler
// pins the steady-state HW->HW passthrough path.
//
// Setup: encoder advertises a hwaccel pixfmt (mediacodec / cuda)
// because it was opened in HwFramesCtx mode, and the upstream decoder
// emits frames in the SAME hwaccel pixfmt (e.g. mediacodec decoder ->
// mediacodec encoder pipeline on Android). Frame dimensions match the
// encoder's target resolution exactly.
//
// Required behaviour: bypass MUST be taken -- SendFrame forwards the
// HW frame to the encoder natively. If bypass is missed, the caller
// runs libswscale on a hwaccel source, which fails ("Unsupported input
// pixel format") and the upstream queue fills up until the pipeline
// stalls.
func TestShouldBypassScaler_HWInputHWEncoder_SameResolution_BypassesScaler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		hwPixFmt astiav.PixelFormat
	}{
		{"mediacodec", astiav.PixelFormatMediacodec},
		{"cuda", astiav.PixelFormatCuda},
		{"vaapi", astiav.PixelFormatVaapi},
		{"videotoolbox", astiav.PixelFormatVideotoolbox},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// hwFramesCtx is nil here -- mirrors the real encoder hot
			// path during steady-state operation when the encoder reports
			// its hwaccel pixfmt but HardwareFramesContext access at the
			// codec.Encoder layer hasn't been threaded through yet (or
			// returns nil for mediacodec encoders that own the surface
			// internally rather than via hw_frames_ctx).
			got := shouldBypassScaler(
				tc.hwPixFmt, 1280, 720,
				tc.hwPixFmt, 1280, 720,
				nil,
			)
			testifyassert.Truef(t, got,
				"steady-state HW->HW passthrough must bypass the scaler "+
					"(input=%s/1280x720, encoder=%s/1280x720, hwfc=nil); "+
					"bypass=false would push a hwaccel frame into libswscale and stall",
				tc.hwPixFmt, tc.hwPixFmt)
		})
	}
}

// TestShouldBypassScaler_SWInputSWEncoder_SameResolution_BypassesScaler
// pins the SW success path: SW input matching the SW upload
// format selected by selectScaledFramePixelFormat must still bypass.
func TestShouldBypassScaler_SWInputSWEncoder_SameResolution_BypassesScaler(t *testing.T) {
	t.Parallel()

	cases := []astiav.PixelFormat{
		astiav.PixelFormatNv12,
		astiav.PixelFormatYuv420P,
	}
	for _, pf := range cases {
		got := shouldBypassScaler(pf, 1280, 720, pf, 1280, 720, nil)
		testifyassert.Truef(t, got,
			"SW->SW passthrough must bypass scaler (pixfmt=%s)", pf)
	}
}

// TestShouldBypassScaler_SWInputHWEncoder_NV12Upload_BypassesScaler:
// SW NV12 input feeding a HW encoder with no hw_frames_ctx attached
// must bypass (the encoder uploads NV12->HW in SendFrame).
func TestShouldBypassScaler_SWInputHWEncoder_NV12Upload_BypassesScaler(t *testing.T) {
	t.Parallel()

	got := shouldBypassScaler(
		astiav.PixelFormatNv12, 1280, 720,
		astiav.PixelFormatMediacodec, 1280, 720,
		nil,
	)
	testifyassert.True(t, got,
		"SW NV12 input matching the upload-fallback format must bypass scaler")
}

// TestShouldBypassScaler_DimensionMismatch_RunsScaler ensures the
// dimension check is enforced -- a resolution change must NOT bypass.
func TestShouldBypassScaler_DimensionMismatch_RunsScaler(t *testing.T) {
	t.Parallel()

	got := shouldBypassScaler(
		astiav.PixelFormatNv12, 1920, 1080,
		astiav.PixelFormatNv12, 1280, 720,
		nil,
	)
	testifyassert.False(t, got, "dimension mismatch must run scaler")
}

// TestShouldBypassScaler_PixelFormatMismatch_RunsScaler ensures the
// pixfmt check is enforced -- a SW input that doesn't match either the
// upload format or the encoder pixfmt must run the scaler.
func TestShouldBypassScaler_PixelFormatMismatch_RunsScaler(t *testing.T) {
	t.Parallel()

	got := shouldBypassScaler(
		astiav.PixelFormatYuv444P, 1280, 720,
		astiav.PixelFormatMediacodec, 1280, 720,
		nil,
	)
	testifyassert.False(t, got,
		"YUV444P != NV12 upload format and != MEDIACODEC -> scaler must run")
}

func TestShouldBypassScaler_NV21InputNV12Encoder_RunsScaler(t *testing.T) {
	t.Parallel()

	got := shouldBypassScaler(
		astiav.PixelFormatNv21, 1920, 1920,
		astiav.PixelFormatNv12, 1920, 1920,
		nil,
	)
	testifyassert.False(t, got,
		"NV21 camera frames must run scaler before NV12 MediaCodec upload so VU chroma becomes UV")
}

func TestSoftwareScale_NV21ToNV12SwapsVUToUVWithoutPlanarSplit(t *testing.T) {
	t.Parallel()

	const (
		width  = 4
		height = 4
		align  = 1
	)

	src := astiav.AllocFrame()
	testifyrequire.NotNil(t, src)
	defer src.Free()
	src.SetWidth(width)
	src.SetHeight(height)
	src.SetPixelFormat(astiav.PixelFormatNv21)
	testifyrequire.NoError(t, src.AllocBuffer(align))

	yPlane := []byte{
		0x10, 0x11, 0x12, 0x13,
		0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1a, 0x1b,
		0x1c, 0x1d, 0x1e, 0x1f,
	}
	nv21Chroma := []byte{
		0xc8, 0x20, 0xc9, 0x21,
		0xca, 0x22, 0xcb, 0x23,
	}
	testifyrequire.NoError(t, src.Data().SetBytes(append(yPlane, nv21Chroma...), align))

	dst := astiav.AllocFrame()
	testifyrequire.NotNil(t, dst)
	defer dst.Free()
	dst.SetWidth(width)
	dst.SetHeight(height)
	dst.SetPixelFormat(astiav.PixelFormatNv12)
	testifyrequire.NoError(t, dst.AllocBuffer(align))

	sws, err := astiav.CreateSoftwareScaleContext(
		width, height, astiav.PixelFormatNv21,
		width, height, astiav.PixelFormatNv12,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagPoint),
	)
	testifyrequire.NoError(t, err)
	defer sws.Free()

	testifyrequire.NoError(t, sws.ScaleFrame(src, dst))
	got, err := dst.Data().Bytes(align)
	testifyrequire.NoError(t, err)

	expected := append([]byte{}, yPlane...)
	expected = append(expected,
		0x20, 0xc8, 0x21, 0xc9,
		0x22, 0xca, 0x23, 0xcb,
	)
	testifyassert.Equal(t, expected, got,
		"NV21 VU bytes must become NV12 UV bytes before MediaCodec upload")
}

// TestShouldBypassScaler_HWInputHWEncoder_DimMismatch_BypassesScaler
// pins the prod DJI scenario: decoder post-crop dims (1920x1072)
// differ from the encoder's launcher-configured target (1920x1080),
// but BOTH sides are MEDIACODEC. The Android Surface composer absorbs
// producer/consumer dim mismatch via the shared
// dev_ctx->native_window, so HW->HW passthrough is safe.
//
// Required behaviour: bypass MUST be taken — running libswscale on a
// hwaccel source pixfmt fails outright, and falling through to
// av_hwframe_transfer_data against the mediacodec hwctx returns
// ENOSYS (FFmpeg's hwcontext_mediacodec.c does not implement
// frames_get_buffer). Either path wedges the encoder.
//
// The HW->HW arm must fire BEFORE the dim-equality early-return —
// otherwise the dim mismatch falls through to "run scaler" and the
// pipeline stalls.
func TestShouldBypassScaler_HWInputHWEncoder_DimMismatch_BypassesScaler(t *testing.T) {
	t.Parallel()

	got := shouldBypassScaler(
		astiav.PixelFormatMediacodec, 1920, 1072,
		astiav.PixelFormatMediacodec, 1920, 1080,
		nil,
	)
	testifyassert.True(t, got,
		"HW->HW passthrough must bypass scaler even with dim mismatch "+
			"(input=MEDIACODEC/1920x1072, encoder=MEDIACODEC/1920x1080); "+
			"Surface composer absorbs the mismatch — bypass=false leads to "+
			"libswscale ENOSYS or transfer_data ENOSYS")
}

func TestShouldBypassScaler_HWInputHWEncoder_CUDADimMismatch_RunsScaler(t *testing.T) {
	t.Parallel()

	got := shouldBypassScaler(
		astiav.PixelFormatCuda, 1920, 1072,
		astiav.PixelFormatCuda, 1920, 1080,
		nil,
	)
	testifyassert.False(t, got,
		"CUDA HW->HW passthrough must not bypass scaler on dim mismatch without a backend-specific proof")
}

// TestShouldBypassScaler_SWInputHWEncoder_DimMismatch_RunsScaler is the
// regression check paired with TestShouldBypassScaler_HWInputHWEncoder_DimMismatch_BypassesScaler:
// when the input pixfmt is a SW format (NV12), the dim-mismatch arm
// MUST still fall through to "run scaler" — only HW->HW input gets the
// Surface-composer dim absorption.
func TestShouldBypassScaler_SWInputHWEncoder_DimMismatch_RunsScaler(t *testing.T) {
	t.Parallel()

	got := shouldBypassScaler(
		astiav.PixelFormatNv12, 1920, 1072,
		astiav.PixelFormatMediacodec, 1920, 1080,
		nil,
	)
	testifyassert.False(t, got,
		"SW NV12 input with dim mismatch must run scaler "+
			"(no Surface-composer absorption for SW->HW upload path)")
}

// TestScaledFrame_AllocBufferFailsForHWPixelFormat is the empirical
// reproduction of the EINVAL: it asserts the libavutil contract that
// av_frame_get_buffer cannot allocate for hwaccel pixfmts. This is
// the underlying constraint the helper exists to respect.
//
// If this ever stops failing (e.g. libavutil gains a SW-fallback for
// hwaccel formats), the helper's design assumption changes -- update
// it deliberately rather than letting drift go unnoticed.
//
// The test uses only public astiav API; if libavutil ever introduces
// SW-fallback allocation for hwaccel formats, update the helper's
// design assumption deliberately rather than letting drift go
// unnoticed.
func TestScaledFrame_AllocBufferFailsForHWPixelFormat(t *testing.T) {
	t.Parallel()

	hwFmts := []astiav.PixelFormat{
		astiav.PixelFormatMediacodec,
		astiav.PixelFormatCuda,
	}

	for _, hwFmt := range hwFmts {
		f := astiav.AllocFrame()
		f.SetWidth(1280)
		f.SetHeight(720)
		f.SetPixelFormat(hwFmt)
		err := f.AllocBuffer(0)
		f.Free()
		testifyassert.Errorf(t, err,
			"av_frame_get_buffer MUST fail for hwaccel pixfmt %s -- this is the EINVAL surface",
			hwFmt)
	}
}
