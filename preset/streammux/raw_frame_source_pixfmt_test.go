// raw_frame_source_pixfmt_test.go pins the contract of
// forceRawFrameSourceMediaCodecPixFmt: pix_fmt=nv12 is appended only when
// RawFrameSource is enabled, the encoder request targets MediaCodec, and no
// explicit pix_fmt is already present. Otherwise the input slice is returned
// unchanged.
// This is the unit-level proof of the silent-consume fix.

package streammux

import (
	"context"
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// hasPixFmt reports whether opts contains a pix_fmt entry with the given value.
func hasPixFmtValue(opts globaltypes.DictionaryItems, value string) bool {
	v := opts.GetFirst("pix_fmt")
	if v == nil {
		return false
	}
	return *v == value
}

// hasAnyPixFmt reports whether opts contains any pix_fmt entry.
func hasAnyPixFmt(opts globaltypes.DictionaryItems) bool {
	return opts.GetFirst("pix_fmt") != nil
}

func TestRawFrameSourceMediaCodecPixFmt_UsesNV12UploadForAndroidCamera(t *testing.T) {
	testifyassert.Equal(t, "nv12", rawFrameSourceMediaCodecPixFmt,
		"raw android_camera frames may be NV21; MediaCodec upload pix_fmt must use NV12 so the scaler swaps VU to UV")
	testifyassert.NotEqual(t, "yuv420p", rawFrameSourceMediaCodecPixFmt,
		"planar yuv420p upload produced green/magenta vertical striping on Pixel 8a AV1 MediaCodec")
	testifyassert.NotEqual(t, "nv21", rawFrameSourceMediaCodecPixFmt,
		"MediaCodec encoders in the pinned FFmpeg source do not advertise NV21")
}

// TestForceRawFrameSourceMediaCodecPixFmt_RawFrameSource_MediaCodec_InjectsNV12
// is the GOOD-side: the camera+MediaCodec combination must inject pix_fmt=nv12.
func TestForceRawFrameSourceMediaCodecPixFmt_RawFrameSource_MediaCodec_InjectsNV12(t *testing.T) {
	ctx := context.Background()
	in := globaltypes.DictionaryItems{
		{Key: "forced-idr", Value: "1"},
	}
	out := forceRawFrameSourceMediaCodecPixFmt(
		ctx, in, true, types.HardwareDeviceType(globaltypes.HardwareDeviceTypeMediaCodec),
		codec.Name("av1_mediacodec"),
	)
	testifyassert.True(t, hasPixFmtValue(out, rawFrameSourceMediaCodecPixFmt),
		"raw-frame source + MediaCodec must inject pix_fmt=%s; got %#v", rawFrameSourceMediaCodecPixFmt, out)
	// existing options must be preserved
	testifyassert.Equal(t, "1", *out.GetFirst("forced-idr"),
		"original options must be preserved; got %#v", out)
}

// TestForceRawFrameSourceMediaCodecPixFmt_RawFrameSource_False_NoOp pins the
// BAD-side: with RawFrameSource=false, no injection happens regardless of HW
// device type. This protects pre-existing surface-passthrough decoder paths
// from being silently broken by an over-eager fix.
func TestForceRawFrameSourceMediaCodecPixFmt_RawFrameSource_False_NoOp(t *testing.T) {
	ctx := context.Background()
	in := globaltypes.DictionaryItems{
		{Key: "forced-idr", Value: "1"},
	}
	out := forceRawFrameSourceMediaCodecPixFmt(
		ctx, in, false, types.HardwareDeviceType(globaltypes.HardwareDeviceTypeMediaCodec),
		codec.Name("av1_mediacodec"),
	)
	testifyassert.False(t, hasAnyPixFmt(out),
		"RawFrameSource=false must NOT inject pix_fmt; got %#v", out)
	testifyassert.Equal(t, in, out, "slice must be returned unchanged")
}

// TestForceRawFrameSourceMediaCodecPixFmt_NonMediaCodec_NoOp pins the
// BAD-side: when the encoder is not a MediaCodec encoder, no injection
// happens even with RawFrameSource=true. Other HW encoders (NVENC, VAAPI)
// have their own pix_fmt logic that must not be perturbed.
func TestForceRawFrameSourceMediaCodecPixFmt_NonMediaCodec_NoOp(t *testing.T) {
	ctx := context.Background()
	in := globaltypes.DictionaryItems{
		{Key: "forced-idr", Value: "1"},
	}
	for _, hwType := range []types.HardwareDeviceType{
		types.HardwareDeviceType(globaltypes.HardwareDeviceTypeNone),
		types.HardwareDeviceType(globaltypes.HardwareDeviceTypeCUDA),
		types.HardwareDeviceType(globaltypes.HardwareDeviceTypeVAAPI),
	} {
		out := forceRawFrameSourceMediaCodecPixFmt(ctx, in, true, hwType, codec.Name("h264"))
		testifyassert.False(t, hasAnyPixFmt(out),
			"non-MediaCodec hwType=%s must NOT inject pix_fmt; got %#v", hwType, out)
	}
}

// TestForceRawFrameSourceMediaCodecPixFmt_ExplicitPixFmt_Preserved pins the
// caller-override contract: if the caller already provided a pix_fmt, the
// helper must not overwrite it. This lets operators pin any known-good
// supported format for a specific device.
func TestForceRawFrameSourceMediaCodecPixFmt_ExplicitPixFmt_Preserved(t *testing.T) {
	ctx := context.Background()
	in := globaltypes.DictionaryItems{
		{Key: "pix_fmt", Value: "yuv420p"},
	}
	out := forceRawFrameSourceMediaCodecPixFmt(
		ctx, in, true, types.HardwareDeviceType(globaltypes.HardwareDeviceTypeMediaCodec),
		codec.Name("av1_mediacodec"),
	)
	testifyassert.Equal(t, "yuv420p", *out.GetFirst("pix_fmt"),
		"explicit pix_fmt must NOT be overridden; got %#v", out)
	// no duplicate entry was appended
	count := 0
	for _, item := range out {
		if item.Key == "pix_fmt" {
			count++
		}
	}
	testifyassert.Equal(t, 1, count,
		"there must be exactly one pix_fmt entry; got %d", count)
}

// TestStreamMux_RawFrameSource_PropagatesToOutput pins the
// StreamMux-level inheritance: when StreamMux.RawFrameSource is set,
// getOrCreateOutputLocked must initialize the new Output with
// RawFrameSource=true even when the caller passes no options. This is
// the integration point that ffstream uses (it sets the flag once at
// Start() based on whether any input resource is a raw-frame source).
func TestStreamMux_RawFrameSource_PropagatesToOutput(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	mux.RawFrameSource.Set()

	out, isNew, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	require.True(t, isNew)
	require.NotNil(t, out)
	testifyassert.True(t, out.RawFrameSource.Load(),
		"StreamMux.RawFrameSource=true must propagate to new Output.RawFrameSource")
}

// TestStreamMux_SetRawFrameSource_LateInjectsExistingFactory is the
// production trigger: an Output is created (with RawFrameSource=false at
// Start time, when only the URL fallback has been registered as an
// input) and its encoder factory has its VideoOptions configured by
// reconfigureEncoder; THEN wingout's gRPC AddInput hot-adds the camera
// at priority 0 and ffstream calls SetRawFrameSource(true). The fix
// must retroactively patch the live encoder factory's VideoOptions
// Dictionary so the lazily-opened MediaCodec encoder takes the SW
// pix_fmt branch. Without this test the original false-then-true gap
// (silent-consume on the actual production layout) regresses
// silently — unit-level scaffolding for the deployment-time witness.
func TestStreamMux_SetRawFrameSource_LateInjectsExistingFactory(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	require.False(t, mux.RawFrameSource.Load(), "sanity")

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	require.False(t, out.RawFrameSource.Load(), "sanity: created with flag off")
	// pre-condition: the encoder factory has no pix_fmt yet
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeMediaCodec

	mux.SetRawFrameSource(ctx, true)

	testifyassert.True(t, out.RawFrameSource.Load(),
		"SetRawFrameSource(true) must propagate to existing Output")
	require.NotNil(t, enc.VideoOptions,
		"VideoOptions Dictionary must be allocated by late injection")
	v := enc.VideoOptions.Get("pix_fmt", nil, 0)
	require.NotNil(t, v, "pix_fmt must be set by late injection")
	testifyassert.Equal(t, rawFrameSourceMediaCodecPixFmt, v.Value(),
		"late-injected pix_fmt must be the raw-frame MediaCodec default")
}

func TestStreamMux_SetRawFrameSource_LateInjectsExistingFactoryByMediaCodecName(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	require.False(t, mux.RawFrameSource.Load(), "sanity")

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	require.False(t, out.RawFrameSource.Load(), "sanity: created with flag off")

	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeNone
	require.Equal(t, codec.Name("av1_mediacodec"), enc.VideoCodec)

	mux.SetRawFrameSource(ctx, true)

	require.NotNil(t, enc.VideoOptions,
		"MediaCodec-name encoder must allocate VideoOptions even when HardwareDeviceType is unset")
	v := enc.VideoOptions.Get("pix_fmt", nil, 0)
	require.NotNil(t, v,
		"MediaCodec-name encoder must receive late pix_fmt injection even when HardwareDeviceType is unset")
	testifyassert.Equal(t, rawFrameSourceMediaCodecPixFmt, v.Value(),
		"late-injected MediaCodec-name pix_fmt must be the raw-frame MediaCodec default")
}

func TestOutputReconfigureEncoder_RawFrameSourceMediaCodecNameInjectsNV12(t *testing.T) {
	ctx := context.Background()
	encoderFactory := codec.NewNaiveEncoderFactory(ctx, nil)
	output := newOutputWithEncoderFactory(ctx, encoderFactory)
	output.RawFrameSource.Set()
	cfg := types.TranscoderConfig{
		Output: types.TranscoderOutputConfig{
			VideoTrackConfigs: []types.OutputVideoTrackConfig{{
				CodecName: "av1_mediacodec",
				Resolution: codectypes.Resolution{
					Width:  1920,
					Height: 1920,
				},
			}},
		},
	}

	_, err := output.reconfigureEncoder(ctx, cfg)
	require.NoError(t, err)

	require.NotNil(t, encoderFactory.VideoOptions,
		"raw-frame MediaCodec-name encoder must allocate VideoOptions")
	v := encoderFactory.VideoOptions.Get("pix_fmt", nil, 0)
	require.NotNil(t, v,
		"raw-frame MediaCodec-name encoder must inject pix_fmt even when HardwareDeviceType is unset")
	testifyassert.Equal(t, rawFrameSourceMediaCodecPixFmt, v.Value(),
		"raw-frame MediaCodec-name encoder must use NV12 upload")
}

// TestStreamMux_SetRawFrameSource_NonMediaCodecOutput is the BAD-side
// of the late-injection path: an existing Output whose encoder is NOT
// MediaCodec must NOT have pix_fmt mutated when the flag flips on.
// Other HW encoders (NVENC, VAAPI) negotiate pix_fmt via different
// mechanisms and a stray raw-frame MediaCodec pix_fmt would break them.
func TestStreamMux_SetRawFrameSource_NonMediaCodecOutput(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "h264_nvenc",
	})
	require.NoError(t, err)
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeCUDA

	mux.SetRawFrameSource(ctx, true)

	if enc.VideoOptions == nil {
		// nothing to check — nil dictionary is the expected no-op outcome
		return
	}
	testifyassert.Nil(t, enc.VideoOptions.Get("pix_fmt", nil, 0),
		"non-MediaCodec encoder must NOT have pix_fmt injected")
}

// TestStreamMux_RawFrameSource_DefaultDoesNotPropagate is the BAD-side:
// the default (RawFrameSource not set) must NOT silently flip Output's
// flag, otherwise existing transcoding-from-decoder pipelines would gain
// the camera-only fix and break their surface-passthrough optimisation.
func TestStreamMux_RawFrameSource_DefaultDoesNotPropagate(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	require.False(t, mux.RawFrameSource.Load(), "sanity: default must be false")

	out, isNew, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	require.True(t, isNew)
	testifyassert.False(t, out.RawFrameSource.Load(),
		"default StreamMux must keep new Output.RawFrameSource=false")
}

// TestStreamMux_SetRawFrameSource_StickyTrue_FalseIsNoOp pins the
// sticky-true contract: once SetRawFrameSource(true) has been called,
// a subsequent SetRawFrameSource(false) MUST NOT clear the flag and
// MUST NOT mutate any Output's RawFrameSource flag. The previous
// implementation called RawFrameSource.Store(false) unconditionally,
// which contradicted the docstring's sticky-true claim and could be
// triggered by ffstream.Start running after a hot-add at priority 0
// had already latched the flag true. (Aspect A retro audit Finding 1.)
func TestStreamMux_SetRawFrameSource_StickyTrue_FalseIsNoOp(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	require.False(t, mux.RawFrameSource.Load(), "sanity: default false")

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeMediaCodec

	// latch the flag true (the production sequence: hot-add of camera
	// hits this path before ffstream's cold-boot Start re-applies it)
	mux.SetRawFrameSource(ctx, true)
	require.True(t, mux.RawFrameSource.Load(), "sanity: after true call")
	require.True(t, out.RawFrameSource.Load(), "sanity: per-output mirrored")

	// false call must be a no-op on the StreamMux flag
	mux.SetRawFrameSource(ctx, false)
	testifyassert.True(t, mux.RawFrameSource.Load(),
		"SetRawFrameSource(false) must NOT clear sticky-true flag")
	testifyassert.True(t, out.RawFrameSource.Load(),
		"SetRawFrameSource(false) must NOT clear per-output RawFrameSource flag")
}

// TestStreamMux_SetRawFrameSource_FalseFromInitialFalse_DoesNotMutateOutputs
// pins that even when the flag is false (never latched), a false call
// must not run the per-Output Range mutation loop — verified indirectly
// by checking that no Output's encoder factory grows a pix_fmt entry.
// This guards against a regression where the false call accidentally
// re-engaged the retroactive injection path.
func TestStreamMux_SetRawFrameSource_FalseFromInitialFalse_DoesNotMutateOutputs(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)
	require.False(t, mux.RawFrameSource.Load(), "sanity")

	out, _, err := mux.GetOrCreateOutput(ctx, SenderKey{
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		VideoCodec:      "av1_mediacodec",
	})
	require.NoError(t, err)
	enc := out.TranscoderNode.Processor.Kernel.EncoderFactory
	enc.HardwareDeviceType = globaltypes.HardwareDeviceTypeMediaCodec

	mux.SetRawFrameSource(ctx, false)

	testifyassert.False(t, mux.RawFrameSource.Load(),
		"false call from false start must keep flag false")
	testifyassert.False(t, out.RawFrameSource.Load(),
		"false call must not toggle per-output flag")
	if enc.VideoOptions != nil {
		testifyassert.Nil(t, enc.VideoOptions.Get("pix_fmt", nil, 0),
			"false call must NOT inject pix_fmt into encoder factory")
	}
}

// TestOptionRawFrameSource_Apply pins the option-pattern contract:
// OptionRawFrameSource(true) sets initOutputConfig.RawFrameSource=true; the
// false variant leaves it unchanged from the zero value.
func TestOptionRawFrameSource_Apply(t *testing.T) {
	cfgTrue := InitOutputOptions{OptionRawFrameSource(true)}.config()
	testifyassert.True(t, cfgTrue.RawFrameSource,
		"OptionRawFrameSource(true) must set RawFrameSource=true")

	cfgFalse := InitOutputOptions{OptionRawFrameSource(false)}.config()
	testifyassert.False(t, cfgFalse.RawFrameSource,
		"OptionRawFrameSource(false) must keep RawFrameSource=false")

	cfgZero := InitOutputOptions{}.config()
	testifyassert.False(t, cfgZero.RawFrameSource,
		"absence of OptionRawFrameSource must default RawFrameSource=false")
}
