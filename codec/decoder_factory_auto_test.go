package codec

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// makeVideoStream allocates a synthetic video stream backed by a fresh
// FormatContext with the given codec_id. The returned cleanup must run
// to free the FormatContext (and its streams).
func makeVideoStream(
	t *testing.T,
	codecID astiav.CodecID,
) *astiav.Stream {
	t.Helper()
	fmtCtx := astiav.AllocFormatContext()
	require.NotNil(t, fmtCtx)
	t.Cleanup(fmtCtx.Free)
	stream := fmtCtx.NewStream(nil)
	require.NotNil(t, stream)
	cp := stream.CodecParameters()
	require.NotNil(t, cp)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(codecID)
	cp.SetWidth(1920)
	cp.SetHeight(1080)
	return stream
}

// resolveVideoCodecName drives NaiveDecoderFactory.newDecoder up to the
// PreInitFunc hook to capture the resolved CodecName the factory
// produced, before NewDecoder runs. We rewrite CodecParameters' codec_id
// to a synthetic invalid value so the subsequent NewDecoder call fails
// fast without actually opening a hardware decoder.
func resolveVideoCodecName(
	t *testing.T,
	params *NaiveDecoderFactoryParams,
	codecID astiav.CodecID,
) Name {
	t.Helper()
	ctx := context.Background()
	var captured Name
	var capturedSet bool
	userPreInit := params.PreInitFunc
	params.PreInitFunc = func(c context.Context, s *astiav.Stream, in *DecoderInput) {
		if userPreInit != nil {
			userPreInit(c, s, in)
		}
		if !capturedSet {
			captured = in.CodecName
			capturedSet = true
		}
		// Sabotage CodecParameters so findDecoderCodec returns nil for
		// both the explicit-name and id-fallback paths: codec_id=NONE
		// + bogus name → newCodec errors out before any hardware probe.
		in.CodecName = "__abort_test_decoder__"
		in.CodecParameters.SetCodecID(astiav.CodecIDNone)
	}
	f := NewNaiveDecoderFactory(ctx, params)
	stream := makeVideoStream(t, codecID)
	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	// We expect failure (sentinel codec name doesn't exist + codec_id cleared).
	assert.Error(t, err)
	assert.Nil(t, dec)
	require.True(t, capturedSet, "PreInitFunc must have run")
	return captured
}

// TestPreferredHWDecoderName_AV1_NoCUDA validates the no-CUDA fallback
// path: preferredHWDecoderName must return "" when av1_cuvid isn't
// registered. On hosts where cuvid IS registered we skip — the symmetric
// AV1-with-CUDA case is exercised by the auto-select test below.
func TestPreferredHWDecoderName_AV1_NoCUDA(t *testing.T) {
	if astiav.FindDecoderByName("av1_cuvid") != nil {
		t.Skip("av1_cuvid is registered on this host; skipping no-CUDA fallback assertion")
	}
	got := preferredHWDecoderName(context.Background(), astiav.CodecIDAv1, globaltypes.HardwareDeviceTypeCUDA)
	assert.Equal(t, Name(""), got)
}

// TestPreferredHWDecoderName_NoCuvidWrapper pins the FindDecoderByName
// guard for codec_ids whose `_cuvid` variant is not registered in
// upstream FFmpeg (theora has no cuvid wrapper). Independent of host
// CUDA availability — runs unconditionally.
func TestPreferredHWDecoderName_NoCuvidWrapper(t *testing.T) {
	got := preferredHWDecoderName(context.Background(), astiav.CodecIDTheora, globaltypes.HardwareDeviceTypeCUDA)
	assert.Equal(t, Name(""), got)
}

// TestPreferredHWDecoderName_UnregisteredHWVariant_ReturnsEmpty is the
// K-LTH-4 unconditional FindDecoderByName-nil-guard witness. We pick a
// synthetic HardwareDeviceType(0xff) — outside the AVHWDeviceType range
// (existing values are 0x0-0xb in libav's enum, see
// avpipeline/types/hardware_device_type.go) — so HardwareDeviceType.String()
// produces "unknown_FF" and the candidate "av1_unknown_FF" cannot exist in
// any FFmpeg build, present or future. This is more robust than referring
// to a named-but-currently-unregistered backend (e.g. Vulkan): if upstream
// FFmpeg ever ships av1_vulkan, this test would silently false-pass; the
// synthetic sentinel cannot.
func TestPreferredHWDecoderName_UnregisteredHWVariant_ReturnsEmpty(t *testing.T) {
	const syntheticUnregistered = globaltypes.HardwareDeviceType(0xff)
	require.Nil(t, astiav.FindDecoderByName("av1_unknown_FF"),
		"test precondition: synthetic av1_unknown_FF must not be registered")
	got := preferredHWDecoderName(context.Background(), astiav.CodecIDAv1, syntheticUnregistered)
	assert.Equal(t, Name(""), got)
}

// TestPreferredHWDecoderName_AV1_WithCUDA validates the success path:
// when av1_cuvid is registered, the helper returns "av1_cuvid".
func TestPreferredHWDecoderName_AV1_WithCUDA(t *testing.T) {
	if astiav.FindDecoderByName("av1_cuvid") == nil {
		t.Skip("av1_cuvid not registered on this host")
	}
	got := preferredHWDecoderName(context.Background(), astiav.CodecIDAv1, globaltypes.HardwareDeviceTypeCUDA)
	assert.Equal(t, Name("av1_cuvid"), got)
}

// TestPreferredHWDecoderName_HardwareDeviceTypeNone_DefaultsToCUDA pins
// the avd-cascade backward-compat semantic: an unset HardwareDeviceType
// (the default in avd config; hardware_device_type stays commented out)
// must resolve to the cuvid variant. Skips when host lacks av1_cuvid.
func TestPreferredHWDecoderName_HardwareDeviceTypeNone_DefaultsToCUDA(t *testing.T) {
	if astiav.FindDecoderByName("av1_cuvid") == nil {
		t.Skip("av1_cuvid not registered on this host")
	}
	got := preferredHWDecoderName(context.Background(), astiav.CodecIDAv1, globaltypes.HardwareDeviceTypeNone)
	assert.Equal(t, Name("av1_cuvid"), got)
}

// TestNaiveDecoderFactory_EmptyCodecName_AutoSelectsAV1Cuvid verifies the
// end-to-end resolution: empty VideoCodec + AutoSelectHardwareDecoder=true
// + AV1 stream → "av1_cuvid" lands in DecoderInput.CodecName.
func TestNaiveDecoderFactory_EmptyCodecName_AutoSelectsAV1Cuvid(t *testing.T) {
	if astiav.FindDecoderByName("av1_cuvid") == nil {
		t.Skip("av1_cuvid not registered on this host")
	}
	got := resolveVideoCodecName(t, &NaiveDecoderFactoryParams{
		VideoCodec:                "",
		AutoSelectHardwareDecoder: true,
	}, astiav.CodecIDAv1)
	assert.Equal(t, Name("av1_cuvid"), got)
}

// TestNaiveDecoderFactory_ExplicitCodecNameWins falsifies the
// auto-select-overrides-explicit anti-pattern: even with the auto flag
// on, an explicit VideoCodec must pass through unchanged.
func TestNaiveDecoderFactory_ExplicitCodecNameWins(t *testing.T) {
	got := resolveVideoCodecName(t, &NaiveDecoderFactoryParams{
		VideoCodec:                "libdav1d",
		AutoSelectHardwareDecoder: true,
	}, astiav.CodecIDAv1)
	assert.Equal(t, Name("libdav1d"), got)
}

// TestNaiveDecoderFactory_AutoSelectDisabled_EmptyResolvesToDefault pins
// opt-in semantics: empty VideoCodec without the flag must NOT be
// rewritten — the resolved name stays empty so libav's default selection
// applies downstream.
func TestNaiveDecoderFactory_AutoSelectDisabled_EmptyResolvesToDefault(t *testing.T) {
	got := resolveVideoCodecName(t, &NaiveDecoderFactoryParams{
		VideoCodec:                "",
		AutoSelectHardwareDecoder: false,
	}, astiav.CodecIDAv1)
	assert.Equal(t, Name(""), got)
}

// TestNaiveDecoderFactory_AutoSelectHonorsConfiguredHWType pins the
// K-LTH-2 fix: AutoSelectHardwareDecoder must consume the configured
// HardwareDeviceType instead of hardcoding CUDA. With a Vulkan setting
// (no av1_vulkan registered anywhere), the resolved name must be "" so
// libav's default selection takes over — proving the path is no longer
// CUDA-only.
func TestNaiveDecoderFactory_AutoSelectHonorsConfiguredHWType(t *testing.T) {
	require.Nil(t, astiav.FindDecoderByName("av1_vulkan"),
		"test precondition: av1_vulkan must not be registered")
	got := resolveVideoCodecName(t, &NaiveDecoderFactoryParams{
		VideoCodec:                "",
		AutoSelectHardwareDecoder: true,
		HardwareDeviceType:        globaltypes.HardwareDeviceTypeVulkan,
	}, astiav.CodecIDAv1)
	assert.Equal(t, Name(""), got)
}

// TestNaiveDecoderFactory_AudioBranch_Unaffected confirms the new logic
// is video-only: audio decoder resolution is untouched by the flag.
func TestNaiveDecoderFactory_AudioBranch_Unaffected(t *testing.T) {
	ctx := context.Background()
	fmtCtx := astiav.AllocFormatContext()
	require.NotNil(t, fmtCtx)
	t.Cleanup(fmtCtx.Free)
	stream := fmtCtx.NewStream(nil)
	require.NotNil(t, stream)
	cp := stream.CodecParameters()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDAac)

	var captured Name
	var capturedSet bool
	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		AudioCodec:                "",
		AutoSelectHardwareDecoder: true,
		PreInitFunc: func(_ context.Context, _ *astiav.Stream, in *DecoderInput) {
			captured = in.CodecName
			capturedSet = true
			in.CodecName = "__abort_test_decoder__"
			in.CodecParameters.SetCodecID(astiav.CodecIDNone)
		},
	})
	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	assert.Error(t, err)
	assert.Nil(t, dec)
	require.True(t, capturedSet, "PreInitFunc must have run")
	// Audio resolution must remain at the configured AudioCodec ("") —
	// the auto-select flag must not bleed into the audio branch.
	assert.Equal(t, Name(""), captured)
}
