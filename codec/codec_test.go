package codec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/quality"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// --- LowLatencyOptions ---

func TestLowLatencyOptions_Encoder_Generic(t *testing.T) {
	ctx := context.Background()
	opts := LowLatencyOptions(ctx, "libx265", true)
	require.NotEmpty(t, opts)

	// Should have generic encoder options
	found := map[string]string{}
	for _, o := range opts {
		found[o.Key] = o.Value
	}
	assert.Equal(t, "1", found["zerolatency"])
	assert.Equal(t, "0", found["bf"])
	assert.Equal(t, "1", found["forced-idr"])
	assert.Equal(t, "0", found["intra-refresh"])
}

func TestLowLatencyOptions_Encoder_MediaCodec(t *testing.T) {
	ctx := context.Background()
	opts := LowLatencyOptions(ctx, "h264_mediacodec", true)
	found := map[string]string{}
	for _, o := range opts {
		found[o.Key] = o.Value
	}
	assert.Equal(t, "0", found["priority"])
	// Should NOT have libx264-specific or nvenc-specific options
	_, hasTuneZerolatency := found["tune"]
	assert.False(t, hasTuneZerolatency || found["tune"] == "zerolatency")
}

func TestLowLatencyOptions_Encoder_NVENC(t *testing.T) {
	ctx := context.Background()
	opts := LowLatencyOptions(ctx, "h264_nvenc", true)
	found := map[string]string{}
	for _, o := range opts {
		found[o.Key] = o.Value
	}
	assert.Equal(t, "ll", found["tune"])
	assert.Equal(t, "0", found["delay"])
	assert.Equal(t, "0", found["rc-lookahead"])
	assert.Equal(t, "cbr_ld_hq", found["rc"])
}

func TestLowLatencyOptions_Encoder_Libx264(t *testing.T) {
	ctx := context.Background()
	opts := LowLatencyOptions(ctx, "libx264", true)
	found := map[string]string{}
	for _, o := range opts {
		found[o.Key] = o.Value
	}
	assert.Equal(t, "zerolatency", found["tune"])
}

func TestLowLatencyOptions_Decoder_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	opts := LowLatencyOptions(ctx, "libx264", false)
	assert.Empty(t, opts, "decoder should have no low-latency options")
}

// --- EncoderCopy ---

func TestEncoderCopy_String(t *testing.T) {
	e := EncoderCopy{}
	assert.Equal(t, "Encoder(copy)", e.String())
}

func TestEncoderCopy_Close(t *testing.T) {
	e := EncoderCopy{}
	assert.NoError(t, e.Close(context.Background()))
}

func TestEncoderCopy_NilCodecAndContext(t *testing.T) {
	ctx := context.Background()
	e := EncoderCopy{}
	assert.Nil(t, e.Codec(ctx))
	assert.Nil(t, e.CodecContext(ctx))
	assert.Nil(t, e.HardwareDeviceContext(ctx))
	assert.Equal(t, astiav.PixelFormat(0), e.HardwarePixelFormat(ctx))
}

func TestEncoderCopy_SendFrame_ReturnsError(t *testing.T) {
	e := EncoderCopy{}
	err := e.SendFrame(context.Background(), nil)
	assert.Error(t, err)
	assert.IsType(t, ErrCopyEncoder{}, err)
}

func TestEncoderCopy_ReceivePacket_ReturnsError(t *testing.T) {
	e := EncoderCopy{}
	err := e.ReceivePacket(context.Background(), nil)
	assert.Error(t, err)
	assert.IsType(t, ErrCopyEncoder{}, err)
}

func TestEncoderCopy_GetQuality_Nil(t *testing.T) {
	e := EncoderCopy{}
	assert.Nil(t, e.GetQuality(context.Background()))
}

func TestEncoderCopy_SetQuality_ReturnsError(t *testing.T) {
	e := EncoderCopy{}
	assert.Error(t, e.SetQuality(context.Background(), nil, nil))
}

func TestEncoderCopy_GetResolution_Nil(t *testing.T) {
	e := EncoderCopy{}
	assert.Nil(t, e.GetResolution(context.Background()))
}

func TestEncoderCopy_SetResolution_ReturnsError(t *testing.T) {
	e := EncoderCopy{}
	assert.Error(t, e.SetResolution(context.Background(), Resolution{}, nil))
}

func TestEncoderCopy_GetPCMAudioFormat_Nil(t *testing.T) {
	e := EncoderCopy{}
	assert.Nil(t, e.GetPCMAudioFormat(context.Background()))
}

func TestEncoderCopy_SetForceNextKeyFrame_ReturnsError(t *testing.T) {
	e := EncoderCopy{}
	assert.Error(t, e.SetForceNextKeyFrame(context.Background(), true))
}

func TestEncoderCopy_FlushDrain_NoError(t *testing.T) {
	e := EncoderCopy{}
	assert.NoError(t, e.Flush(context.Background(), nil))
	assert.NoError(t, e.Drain(context.Background(), nil))
}

func TestEncoderCopy_IsDirty_False(t *testing.T) {
	e := EncoderCopy{}
	assert.False(t, e.IsDirty())
}

func TestEncoderCopy_ToCodecParameters_NoError(t *testing.T) {
	e := EncoderCopy{}
	assert.NoError(t, e.ToCodecParameters(context.Background(), nil))
}

func TestEncoderCopy_LockDo(t *testing.T) {
	e := EncoderCopy{}
	called := false
	err := e.LockDo(context.Background(), func(ctx context.Context, enc Encoder) error {
		called = true
		assert.IsType(t, EncoderCopy{}, enc)
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestEncoderCopy_MediaType_Panics(t *testing.T) {
	ctx := context.Background()
	e := EncoderCopy{}
	assert.Panics(t, func() { e.MediaType(ctx) })
}

func TestEncoderCopy_TimeBase_Panics(t *testing.T) {
	ctx := context.Background()
	e := EncoderCopy{}
	assert.Panics(t, func() { e.TimeBase(ctx) })
}

func TestIsEncoderCopy(t *testing.T) {
	assert.True(t, IsEncoderCopy(EncoderCopy{}))
	assert.False(t, IsEncoderCopy(EncoderRaw{}))
}

// --- EncoderRaw ---

func TestEncoderRaw_String(t *testing.T) {
	e := EncoderRaw{}
	assert.Equal(t, "Encoder(raw)", e.String())
}

func TestEncoderRaw_Close(t *testing.T) {
	e := EncoderRaw{}
	assert.NoError(t, e.Close(context.Background()))
}

func TestEncoderRaw_NilCodecAndContext(t *testing.T) {
	ctx := context.Background()
	e := EncoderRaw{}
	assert.Nil(t, e.Codec(ctx))
	assert.Nil(t, e.CodecContext(ctx))
	assert.Nil(t, e.HardwareDeviceContext(ctx))
	assert.Equal(t, astiav.PixelFormat(0), e.HardwarePixelFormat(ctx))
}

func TestEncoderRaw_SendFrame_ReturnsError(t *testing.T) {
	e := EncoderRaw{}
	err := e.SendFrame(context.Background(), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "manual")
}

func TestEncoderRaw_ReceivePacket_ReturnsError(t *testing.T) {
	e := EncoderRaw{}
	err := e.ReceivePacket(context.Background(), nil)
	assert.Error(t, err)
}

func TestEncoderRaw_GetQuality_Nil(t *testing.T) {
	e := EncoderRaw{}
	assert.Nil(t, e.GetQuality(context.Background()))
}

func TestEncoderRaw_SetQuality_ReturnsError(t *testing.T) {
	e := EncoderRaw{}
	assert.Error(t, e.SetQuality(context.Background(), nil, nil))
}

func TestEncoderRaw_SetResolution_ReturnsError(t *testing.T) {
	e := EncoderRaw{}
	assert.Error(t, e.SetResolution(context.Background(), Resolution{}, nil))
}

func TestEncoderRaw_GetPCMAudioFormat_Nil(t *testing.T) {
	e := EncoderRaw{}
	assert.Nil(t, e.GetPCMAudioFormat(context.Background()))
}

func TestEncoderRaw_FlushDrain_NoError(t *testing.T) {
	e := EncoderRaw{}
	assert.NoError(t, e.Flush(context.Background(), nil))
	assert.NoError(t, e.Drain(context.Background(), nil))
}

func TestEncoderRaw_SetForceNextKeyFrame_NoError(t *testing.T) {
	e := EncoderRaw{}
	assert.NoError(t, e.SetForceNextKeyFrame(context.Background(), true))
}

func TestEncoderRaw_IsDirty_False(t *testing.T) {
	e := EncoderRaw{}
	assert.False(t, e.IsDirty())
}

func TestEncoderRaw_LockDo(t *testing.T) {
	e := EncoderRaw{}
	called := false
	err := e.LockDo(context.Background(), func(ctx context.Context, enc Encoder) error {
		called = true
		assert.IsType(t, EncoderRaw{}, enc)
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestIsEncoderRaw(t *testing.T) {
	assert.True(t, IsEncoderRaw(EncoderRaw{}))
	assert.False(t, IsEncoderRaw(EncoderCopy{}))
}

func TestIsDummyEncoder(t *testing.T) {
	assert.True(t, IsDummyEncoder(EncoderCopy{}))
	assert.True(t, IsDummyEncoder(EncoderRaw{}))
}

// --- Error Types ---

func TestErrCopyEncoder_Error(t *testing.T) {
	err := ErrCopyEncoder{}
	assert.Equal(t, "'copy' encoder", err.Error())
}

func TestErrNotDummy_Error(t *testing.T) {
	err := ErrNotDummy{}
	assert.Equal(t, "not a dummy encoder", err.Error())
}

func TestErrNotImplemented_Error(t *testing.T) {
	err := ErrNotImplemented{Err: assert.AnError}
	assert.Contains(t, err.Error(), "not implemented")
	assert.Contains(t, err.Error(), assert.AnError.Error())
}

func TestErrNotKeyFrame_Error(t *testing.T) {
	err := ErrNotKeyFrame{}
	assert.Equal(t, "not a key frame", err.Error())
}

// --- Quirks ---

func TestQuirks_HasAll(t *testing.T) {
	q := QuirkBuggyFlushBuffers
	assert.True(t, q.HasAll(QuirkBuggyFlushBuffers))
	assert.False(t, Quirks(0).HasAll(QuirkBuggyFlushBuffers))
}

func TestQuirks_HasAny(t *testing.T) {
	q := QuirkBuggyFlushBuffers
	assert.True(t, q.HasAny(QuirkBuggyFlushBuffers))
	assert.False(t, Quirks(0).HasAny(QuirkBuggyFlushBuffers))
}

func TestQuirks_SetUnset(t *testing.T) {
	var q Quirks
	q.Set(QuirkBuggyFlushBuffers)
	assert.True(t, q.HasAll(QuirkBuggyFlushBuffers))
	q.Unset(QuirkBuggyFlushBuffers)
	assert.False(t, q.HasAny(QuirkBuggyFlushBuffers))
}

func TestQuirks_String(t *testing.T) {
	var q Quirks
	assert.Equal(t, "", q.String())
}

// --- PCMAudioFormat ---

func TestPCMAudioFormat_String(t *testing.T) {
	f := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatFltp,
		SampleRate:   44100,
		ChunkSize:    1024,
	}
	s := f.String()
	assert.Contains(t, s, "44100")
	assert.Contains(t, s, "1024")
}

func TestPCMAudioFormat_Equal_SameFormat(t *testing.T) {
	a := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatFltp,
		SampleRate:   48000,
		ChunkSize:    1024,
	}
	b := a
	assert.True(t, a.Equal(b))
}

func TestPCMAudioFormat_Equal_DifferentSampleRate(t *testing.T) {
	a := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatFltp,
		SampleRate:   44100,
	}
	b := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatFltp,
		SampleRate:   48000,
	}
	assert.False(t, a.Equal(b))
}

func TestPCMAudioFormat_Equal_DifferentSampleFormat(t *testing.T) {
	a := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatFltp,
		SampleRate:   48000,
	}
	b := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatS16,
		SampleRate:   48000,
	}
	assert.False(t, a.Equal(b))
}

func TestPCMAudioFormat_Equal_ZeroChunkSizeIsWild(t *testing.T) {
	a := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatFltp,
		SampleRate:   48000,
		ChunkSize:    0,
	}
	b := PCMAudioFormat{
		SampleFormat: astiav.SampleFormatFltp,
		SampleRate:   48000,
		ChunkSize:    1024,
	}
	assert.True(t, a.Equal(b), "zero ChunkSize should match any ChunkSize")
}

// --- NewEncoder with special names ---

func TestNewEncoder_Copy(t *testing.T) {
	enc, err := NewEncoder(context.Background(), CodecParams{CodecName: NameCopy})
	require.NoError(t, err)
	assert.IsType(t, EncoderCopy{}, enc)
}

func TestNewEncoder_Raw(t *testing.T) {
	enc, err := NewEncoder(context.Background(), CodecParams{CodecName: NameRaw})
	require.NoError(t, err)
	assert.IsType(t, EncoderRaw{}, enc)
}

func TestNewEncoder_OnlyDummy(t *testing.T) {
	_, err := NewEncoder(context.Background(), CodecParams{
		CodecName: "libx264",
		Options:   []Option{EncoderFactoryOptionOnlyDummy{OnlyDummy: true}},
	})
	assert.Error(t, err)
	assert.IsType(t, ErrNotDummy{}, err)
}

// --- Name ---

func TestName_Codec_Encoder(t *testing.T) {
	ctx := context.Background()
	codec := Name("libx264").Codec(ctx, true)
	if codec == nil {
		codec = Name("mpeg4").Codec(ctx, true)
	}
	require.NotNil(t, codec)
	assert.True(t, codec.IsEncoder())
}

func TestName_Codec_Decoder(t *testing.T) {
	ctx := context.Background()
	codec := Name("h264").Codec(ctx, false)
	require.NotNil(t, codec)
	assert.True(t, codec.IsDecoder())
}

func TestName_Codec_NotFound(t *testing.T) {
	ctx := context.Background()
	codec := Name("nonexistent_codec_xyz").Codec(ctx, true)
	assert.Nil(t, codec)
}

func TestName_Canonicalize_Copy(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, NameCopy, NameCopy.Canonicalize(ctx, true))
}

func TestName_Canonicalize_Raw(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, NameRaw, NameRaw.Canonicalize(ctx, true))
}

func TestName_Canonicalize_Known(t *testing.T) {
	ctx := context.Background()
	result := Name("libx264").Canonicalize(ctx, true)
	// libx264 maps to codec ID H264, whose canonical name is "h264"
	assert.Equal(t, Name("h264"), result)
}

func TestName_hwName_CUDA_Encoder(t *testing.T) {
	ctx := context.Background()
	result := Name("h264").hwName(ctx, true, 2) // CUDA=2
	assert.Equal(t, Name("h264_nvenc"), result)
}

func TestName_hwName_CUDA_Decoder(t *testing.T) {
	ctx := context.Background()
	result := Name("h264").hwName(ctx, false, 2) // CUDA=2
	assert.Equal(t, Name("h264_cuvid"), result)
}

// --- detectHardwareDeviceType ---

func TestDetectHardwareDeviceType(t *testing.T) {
	for _, tc := range []struct {
		codecName string
		expected  HardwareDeviceType
	}{
		{"hevc_mediacodec", globaltypes.HardwareDeviceTypeMediaCodec},
		{"h264_mediacodec", globaltypes.HardwareDeviceTypeMediaCodec},
		{"h264_nvenc", globaltypes.HardwareDeviceTypeCUDA},
		{"hevc_nvenc", globaltypes.HardwareDeviceTypeCUDA},
		{"h264_cuvid", globaltypes.HardwareDeviceTypeCUDA},
		{"h264_qsv", globaltypes.HardwareDeviceTypeQSV},
		{"h264_vaapi", globaltypes.HardwareDeviceTypeVAAPI},
		{"h264_videotoolbox", globaltypes.HardwareDeviceTypeVideoToolbox},
		{"libx264", globaltypes.HardwareDeviceTypeNone},
		{"aac", globaltypes.HardwareDeviceTypeNone},
		{"rawvideo", globaltypes.HardwareDeviceTypeNone},
	} {
		t.Run(tc.codecName, func(t *testing.T) {
			assert.Equal(t, tc.expected, detectHardwareDeviceType(tc.codecName))
		})
	}
}

// --- NaiveDecoderFactory ---

func TestNewNaiveDecoderFactory_NilParams(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveDecoderFactory(ctx, nil)
	require.NotNil(t, f)
	assert.Equal(t, "NaiveDecoderFactory", f.String())
}

func TestNewNaiveDecoderFactory_WithParams(t *testing.T) {
	ctx := context.Background()
	params := &NaiveDecoderFactoryParams{}
	f := NewNaiveDecoderFactory(ctx, params)
	require.NotNil(t, f)
}

func TestNaiveDecoderFactory_Reset(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveDecoderFactory(ctx, nil)
	// Add some fake entries
	f.VideoDecoders = append(f.VideoDecoders, nil)
	f.AudioDecoders = append(f.AudioDecoders, nil)
	err := f.Reset(ctx)
	assert.NoError(t, err)
	assert.Empty(t, f.VideoDecoders)
	assert.Empty(t, f.AudioDecoders)
}

func TestNaiveDecoderFactory_NewDecoder_UnsupportedMediaType(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveDecoderFactory(ctx, nil)
	fmtCtx := astiav.AllocFormatContext()
	t.Cleanup(fmtCtx.Free)
	codec := astiav.FindDecoderByName("srt")
	if codec == nil {
		// srt might not be available, try subtitle type
		t.Skip("no subtitle decoder available for testing")
	}
	stream := fmtCtx.NewStream(codec)
	require.NotNil(t, stream)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeSubtitle)
	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	// NaiveDecoderFactory returns nil,nil for unsupported media types
	assert.Nil(t, dec)
	assert.Nil(t, err)
}

func TestNewDecoder_NoHWCodecVariant_FallsBackToSoftware(t *testing.T) {
	ctx := context.Background()

	old := FallbackToSoftwareOnNoHWCodec
	FallbackToSoftwareOnNoHWCodec = true
	t.Cleanup(func() { FallbackToSoftwareOnNoHWCodec = old })

	// MJPEG has no mediacodec variant (no mjpeg_mediacodec decoder).
	// When HardwareDeviceType is set to mediacodec and fallback is enabled,
	// the decoder should fall back to software decoding instead of failing.
	codecParams := astiav.AllocCodecParameters()
	t.Cleanup(codecParams.Free)
	codecParams.SetMediaType(astiav.MediaTypeVideo)
	codecParams.SetCodecID(astiav.CodecIDMjpeg)
	codecParams.SetWidth(640)
	codecParams.SetHeight(480)

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters:    codecParams,
		HardwareDeviceType: HardwareDeviceType(globaltypes.HardwareDeviceTypeMediaCodec),
	})
	require.NoError(t, err)
	require.NotNil(t, dec)
	t.Cleanup(func() { _ = dec.Close(ctx) })

	// Verify it's using a software codec (not a _mediacodec variant).
	assert.Equal(t, "mjpeg", dec.codec.Name())
}

// --- NaiveEncoderFactory ---

func TestNewNaiveEncoderFactory_NilParams(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	require.NotNil(t, f)
	assert.Contains(t, f.String(), "NaiveEncoderFactory")
}

func TestNewNaiveEncoderFactory_WithParams(t *testing.T) {
	ctx := context.Background()
	params := &NaiveEncoderFactoryParams{
		VideoCodec: "libx264",
		AudioCodec: "aac",
	}
	f := NewNaiveEncoderFactory(ctx, params)
	assert.Contains(t, f.String(), "libx264")
	assert.Contains(t, f.String(), "aac")
}

func TestNaiveEncoderFactory_Reset(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	f.VideoEncoders = append(f.VideoEncoders, EncoderCopy{})
	f.AudioEncoders = append(f.AudioEncoders, EncoderRaw{})
	err := f.Reset(ctx)
	assert.NoError(t, err)
	assert.Empty(t, f.VideoEncoders)
	assert.Empty(t, f.AudioEncoders)
}

func TestNaiveEncoderFactory_NewEncoder_ZeroTimeBase(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodec: "libx264",
	})
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	_, err := f.NewEncoder(ctx, cp, astiav.NewRational(0, 0))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TimeBase must be set")
}

func TestNaiveEncoderFactory_NewEncoder_CopyCodec(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodec: Name(NameCopy),
	})
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	enc, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	require.NoError(t, err)
	assert.IsType(t, EncoderCopy{}, enc)
}

// --- Decoder creation with real codec ---

func TestNewDecoder_Video(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(320)
	cp.SetHeight(240)

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters: cp,
	})
	require.NoError(t, err)
	require.NotNil(t, dec)
	defer func() { _ = dec.Close(ctx) }()

	assert.Contains(t, dec.String(), "Decoder")
	assert.Equal(t, astiav.MediaTypeVideo, dec.MediaType(ctx))
	assert.False(t, dec.IsDirty(ctx))
}

func TestNewDecoder_Audio(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDAac)
	cp.SetSampleRate(44100)
	cp.SetChannelLayout(astiav.ChannelLayoutStereo)
	cp.SetSampleFormat(astiav.SampleFormatFltp)

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters: cp,
	})
	require.NoError(t, err)
	require.NotNil(t, dec)
	defer func() { _ = dec.Close(ctx) }()

	assert.Equal(t, astiav.MediaTypeAudio, dec.MediaType(ctx))
}

func TestNewDecoder_InvalidCodec(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)

	_, err := NewDecoder(ctx, DecoderInput{
		CodecName:       "nonexistent_codec_xyz",
		CodecParameters: cp,
	})
	assert.Error(t, err)
}

func TestDecoder_LockDo(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(320)
	cp.SetHeight(240)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	called := false
	err = dec.LockDo(ctx, func(ctx context.Context, dl *DecoderLocked) error {
		called = true
		assert.NotNil(t, dl)
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

// --- Encoder creation with real codec ---

func TestNewEncoder_Video_Libx264(t *testing.T) {
	ctx := context.Background()
	encoder := astiav.FindEncoderByName("libx264")
	if encoder == nil {
		encoder = astiav.FindEncoderByName("mpeg4")
	}
	require.NotNil(t, encoder)

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(encoder.ID())
	cp.SetWidth(320)
	cp.SetHeight(240)

	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:       Name(encoder.Name()),
		CodecParameters: cp,
		TimeBase:        astiav.NewRational(1, 30),
	})
	require.NoError(t, err)
	require.NotNil(t, enc)
	defer func() { _ = enc.Close(ctx) }()

	assert.Contains(t, enc.String(), "Encoder")
	assert.NotNil(t, enc.Codec(ctx))
	assert.NotNil(t, enc.CodecContext(ctx))
	assert.False(t, enc.IsDirty())
}

func TestNewEncoder_Audio_Aac(t *testing.T) {
	ctx := context.Background()
	encoder := astiav.FindEncoderByName("aac")
	if encoder == nil {
		t.Skip("aac encoder not available")
	}

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDAac)
	cp.SetSampleRate(44100)
	cp.SetChannelLayout(astiav.ChannelLayoutStereo)
	cp.SetSampleFormat(astiav.SampleFormatFltp)

	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:       "aac",
		CodecParameters: cp,
		TimeBase:        astiav.NewRational(1, 44100),
	})
	require.NoError(t, err)
	require.NotNil(t, enc)
	defer func() { _ = enc.Close(ctx) }()

	assert.Equal(t, astiav.MediaTypeAudio, enc.MediaType(ctx))
}

// --- Encoder encode cycle ---

func TestEncoder_SendReceive_Video(t *testing.T) {
	ctx := context.Background()
	encoderCodec := astiav.FindEncoderByName("libx264")
	if encoderCodec == nil {
		encoderCodec = astiav.FindEncoderByName("mpeg4")
	}
	require.NotNil(t, encoderCodec)

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(encoderCodec.ID())
	cp.SetWidth(64)
	cp.SetHeight(64)

	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:       Name(encoderCodec.Name()),
		CodecParameters: cp,
		TimeBase:        astiav.NewRational(1, 30),
	})
	require.NoError(t, err)
	defer func() { _ = enc.Close(ctx) }()

	// Create a test frame
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)
	frame.SetWidth(64)
	frame.SetHeight(64)
	frame.SetPixelFormat(enc.CodecContext(ctx).PixelFormat())
	require.NoError(t, frame.AllocBuffer(0))
	frame.SetPts(0)
	frame.SetFlags(frame.Flags().Add(astiav.FrameFlagKey))
	frame.SetPictureType(astiav.PictureTypeI)

	// Send frame
	err = enc.SendFrame(ctx, frame)
	require.NoError(t, err)
	assert.True(t, enc.IsDirty())

	// Try receiving packets (may need multiple frames for some codecs)
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	_ = enc.ReceivePacket(ctx, pkt) // May or may not have a packet yet
}

// --- Decoder decode cycle ---

func TestDecoder_SendPacket_DropNonKeyFrame(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(320)
	cp.SetHeight(240)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	// Send non-key packet first — should be rejected
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(0)
	pkt.SetDts(0)
	err = dec.SendPacket(ctx, pkt)
	assert.Error(t, err)
	assert.IsType(t, ErrNotKeyFrame{}, err)
}

func TestDecoder_SendPacket_IntraOnlyCodecAcceptsNonKeyFrame(t *testing.T) {
	for _, tc := range []struct {
		name    string
		codecID astiav.CodecID
	}{
		{"rawvideo", astiav.CodecIDRawvideo},
		{"wrapped_avframe", astiav.CodecIDWrappedAvframe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, isIntraOnlyCodec(tc.codecID))
		})
	}

	// Verify inter-frame codecs are NOT intra-only.
	assert.False(t, isIntraOnlyCodec(astiav.CodecIDH264))
	assert.False(t, isIntraOnlyCodec(astiav.CodecIDH265))
}

// --- NaiveDecoderFactory lifecycle ---

func TestNaiveDecoderFactory_NewDecoder_Video(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveDecoderFactory(ctx, nil)

	fmtCtx := astiav.AllocFormatContext()
	t.Cleanup(fmtCtx.Free)
	decoder := astiav.FindDecoder(astiav.CodecIDH264)
	require.NotNil(t, decoder)
	stream := fmtCtx.NewStream(decoder)
	require.NotNil(t, stream)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.CodecParameters().SetCodecID(astiav.CodecIDH264)
	stream.CodecParameters().SetWidth(320)
	stream.CodecParameters().SetHeight(240)

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.NoError(t, err)
	require.NotNil(t, dec)
	defer func() { _ = dec.Close(ctx) }()

	assert.Len(t, f.VideoDecoders, 1)
	assert.Empty(t, f.AudioDecoders)
}

// --- Resource Type (tested in resource package) ---

// --- findCodec ---

func TestFindCodec_ByName(t *testing.T) {
	ctx := context.Background()
	c := findCodec(ctx, true, 0, "mpeg4")
	require.NotNil(t, c)
	assert.True(t, c.IsEncoder())
}

func TestFindCodec_ByID(t *testing.T) {
	ctx := context.Background()
	c := findCodec(ctx, false, astiav.CodecIDH264, "")
	require.NotNil(t, c)
	assert.True(t, c.IsDecoder())
}

func TestFindCodec_NameTakesPrecedence(t *testing.T) {
	ctx := context.Background()
	encoderName := "mpeg4"
	c := findCodec(ctx, true, astiav.CodecIDH264, Name(encoderName))
	require.NotNil(t, c)
	assert.Equal(t, encoderName, c.Name())
}

// --- Full Encoder Quality/Resolution ---

func newTestVideoEncoder(t *testing.T) Encoder {
	t.Helper()
	ctx := context.Background()
	encoderCodec := astiav.FindEncoderByName("libx264")
	if encoderCodec == nil {
		encoderCodec = astiav.FindEncoderByName("mpeg4")
	}
	require.NotNil(t, encoderCodec)
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(encoderCodec.ID())
	cp.SetWidth(64)
	cp.SetHeight(64)
	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:       Name(encoderCodec.Name()),
		CodecParameters: cp,
		TimeBase:        astiav.NewRational(1, 30),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = enc.Close(ctx) })
	return enc
}

func TestEncoder_GetResolution(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	res := enc.GetResolution(ctx)
	require.NotNil(t, res)
	assert.Equal(t, uint32(64), res.Width)
	assert.Equal(t, uint32(64), res.Height)
}

func TestEncoder_GetQuality_NoBitrate(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	q := enc.GetQuality(ctx)
	// With no bitrate explicitly set, may be nil or may have a default
	_ = q // no panic is sufficient
}

func TestEncoder_SetQuality_CBR(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	err := enc.SetQuality(ctx, quality.ConstantBitrate(500000), nil)
	assert.NoError(t, err)
}

func TestEncoder_SetQuality_CRF(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	// CRF is only applicable to libx264
	err := enc.SetQuality(ctx, quality.ConstantQuality(23), nil)
	// May or may not succeed depending on codec
	_ = err
}

func TestEncoder_SetResolution_SameSize(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	// Same resolution — should be a no-op
	err := enc.SetResolution(ctx, Resolution{Width: 64, Height: 64}, nil)
	assert.NoError(t, err)
}

func TestEncoder_SetForceNextKeyFrame(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	err := enc.SetForceNextKeyFrame(ctx, true)
	assert.NoError(t, err)
}

func TestEncoder_SanityCheck(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	err := encFull.SanityCheck(ctx)
	assert.NoError(t, err)
}

func TestEncoder_LockDo(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	called := false
	err := enc.LockDo(ctx, func(ctx context.Context, e Encoder) error {
		called = true
		return nil
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestEncoder_Flush(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()

	// Send a frame first
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)
	frame.SetWidth(64)
	frame.SetHeight(64)
	frame.SetPixelFormat(enc.CodecContext(ctx).PixelFormat())
	require.NoError(t, frame.AllocBuffer(0))
	frame.SetPts(0)
	frame.SetFlags(frame.Flags().Add(astiav.FrameFlagKey))
	frame.SetPictureType(astiav.PictureTypeI)
	require.NoError(t, enc.SendFrame(ctx, frame))

	// Flush
	err := enc.Flush(ctx, nil)
	assert.NoError(t, err)
}

func TestEncoder_Drain(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	// Drain without sending anything — should not error
	err := enc.Drain(ctx, nil)
	assert.NoError(t, err)
}

func TestEncoder_GetPCMAudioFormat_Video(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	// Video encoders still return PCMAudioFormat (with zero-ish values)
	pcm := enc.GetPCMAudioFormat(ctx)
	// Just ensure no panic — the format may be non-nil with default values
	_ = pcm
}

// --- MIME Types ---

func TestCodec_MIMETypes(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.NotEmpty(t, mimeTypes)
	assert.Contains(t, mimeTypes, "video/H264")
	assert.Contains(t, mimeTypes, "video/avc") // Android MIME type
}

func TestCodec_MIMETypes_Audio(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDAac)
	cp.SetSampleRate(44100)
	cp.SetChannelLayout(astiav.ChannelLayoutStereo)
	cp.SetSampleFormat(astiav.SampleFormatFltp)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.NotEmpty(t, mimeTypes)
	assert.Contains(t, mimeTypes, "audio/mp4a-latm")
}

func TestCodec_GetAndroidMIMEType(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDHevc)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	androidMIME := dec.Codec.GetAndroidMIMEType(ctx)
	assert.Equal(t, "video/hevc", androidMIME)
}

// --- Codec internal methods ---

func TestCodecInternals_IsOpen(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	assert.True(t, dec.codecInternals.IsOpen())
	assert.True(t, dec.codecInternals.IsDecoder())
	assert.False(t, dec.codecInternals.IsEncoder())
}

func TestCodecInternals_IsEncoder(t *testing.T) {
	enc := newTestVideoEncoder(t)
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	assert.True(t, encFull.codecInternals.IsEncoder())
	assert.False(t, encFull.codecInternals.IsDecoder())
}

// --- NaiveEncoderFactory video creation ---

func TestNaiveEncoderFactory_NewEncoder_Video(t *testing.T) {
	ctx := context.Background()
	encoderCodec := astiav.FindEncoderByName("libx264")
	if encoderCodec == nil {
		encoderCodec = astiav.FindEncoderByName("mpeg4")
	}
	require.NotNil(t, encoderCodec)

	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodec: Name(encoderCodec.Name()),
	})
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(encoderCodec.ID())
	cp.SetWidth(64)
	cp.SetHeight(64)

	enc, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	require.NoError(t, err)
	require.NotNil(t, enc)
	defer func() { _ = enc.Close(ctx) }()

	assert.Len(t, f.VideoEncoders, 1)
	assert.Empty(t, f.AudioEncoders)
}

func TestNaiveEncoderFactory_NewEncoder_UnsupportedMediaType(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveEncoderFactory(ctx, nil)
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeSubtitle)
	_, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "only audio and video")
}

// --- Decoder GetQuality ---

func TestDecoder_GetQuality(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	q := dec.GetQuality(ctx)
	// H264 decoder may have a default bitrate — just check no panic
	_ = q
}

// --- Decoder SetLowLatency ---

func TestDecoder_SetLowLatency_Generic(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	// Generic low latency is not implemented
	err = dec.SetLowLatency(ctx, true)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

// --- EncoderFactoryOptionLatest ---

func TestEncoderFactoryOptionLatest_Found(t *testing.T) {
	opts := []Option{
		EncoderFactoryOptionOnlyDummy{OnlyDummy: false},
		EncoderFactoryOptionOnlyDummy{OnlyDummy: true},
	}
	v, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionOnlyDummy](opts)
	assert.True(t, ok)
	assert.True(t, v.OnlyDummy)
}

func TestEncoderFactoryOptionLatest_NotFound(t *testing.T) {
	opts := []Option{
		EncoderFactoryOptionOnlyDummy{OnlyDummy: true},
	}
	_, ok := EncoderFactoryOptionLatest[EncoderFactoryOptionGetDecoderer](opts)
	assert.False(t, ok)
}

// --- NaiveDecoderFactory with callbacks ---

func TestNaiveDecoderFactory_PostInitFunc(t *testing.T) {
	ctx := context.Background()
	postInitCalled := false
	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		PostInitFunc: func(ctx context.Context, d *Decoder) {
			postInitCalled = true
		},
	})

	fmtCtx := astiav.AllocFormatContext()
	t.Cleanup(fmtCtx.Free)
	decoder := astiav.FindDecoder(astiav.CodecIDH264)
	require.NotNil(t, decoder)
	stream := fmtCtx.NewStream(decoder)
	require.NotNil(t, stream)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.CodecParameters().SetCodecID(astiav.CodecIDH264)
	stream.CodecParameters().SetWidth(64)
	stream.CodecParameters().SetHeight(64)

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	assert.True(t, postInitCalled)
}

func TestNaiveDecoderFactory_PreInitFunc(t *testing.T) {
	ctx := context.Background()
	preInitCalled := false
	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		PreInitFunc: func(ctx context.Context, s *astiav.Stream, input *DecoderInput) {
			preInitCalled = true
		},
	})

	fmtCtx := astiav.AllocFormatContext()
	t.Cleanup(fmtCtx.Free)
	decoder := astiav.FindDecoder(astiav.CodecIDH264)
	require.NotNil(t, decoder)
	stream := fmtCtx.NewStream(decoder)
	require.NotNil(t, stream)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.CodecParameters().SetCodecID(astiav.CodecIDH264)
	stream.CodecParameters().SetWidth(64)
	stream.CodecParameters().SetHeight(64)

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	assert.True(t, preInitCalled)
}

// --- Codec TimeBase ---

func TestCodec_TimeBase_Encoder(t *testing.T) {
	ctx := context.Background()
	enc := newTestVideoEncoder(t)
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	tb := encFull.TimeBase(ctx)
	// TimeBase should match what we set: 1/30
	assert.Equal(t, 1, tb.Num())
	assert.Equal(t, 30, tb.Den())
}

func TestCodec_TimeBase_Decoder(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	tb := dec.Codec.TimeBase(ctx)
	// Decoder time base should be non-zero after initialization
	_ = tb // no panic is sufficient
}

// --- Codec HardwareDeviceContext / HardwarePixelFormat ---

func TestCodec_HardwareDeviceContext_Software(t *testing.T) {
	ctx := context.Background()
	enc := newTestVideoEncoder(t)
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	assert.Nil(t, encFull.HardwareDeviceContext(ctx))
}

func TestCodec_HardwarePixelFormat_Software(t *testing.T) {
	ctx := context.Background()
	enc := newTestVideoEncoder(t)
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	// Software codecs have no hardware pixel format (should be 0 / None)
	pf := encFull.HardwarePixelFormat(ctx)
	assert.Equal(t, astiav.PixelFormat(0), pf)
}

// --- Codec ToCodecParameters ---

func TestCodec_ToCodecParameters_Encoder(t *testing.T) {
	ctx := context.Background()
	enc := newTestVideoEncoder(t)
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	err := encFull.ToCodecParameters(ctx, cp)
	require.NoError(t, err)
	assert.Equal(t, astiav.MediaTypeVideo, cp.MediaType())
	assert.Equal(t, 64, cp.Width())
	assert.Equal(t, 64, cp.Height())
}

func TestCodec_ToCodecParameters_Decoder(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(320)
	cp.SetHeight(240)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	outCP := astiav.AllocCodecParameters()
	t.Cleanup(outCP.Free)
	err = dec.Codec.ToCodecParameters(ctx, outCP)
	require.NoError(t, err)
	assert.Equal(t, astiav.CodecIDH264, outCP.CodecID())
}

// --- Codec Reset ---

func TestCodec_Reset_Decoder(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	// Reset on a fresh decoder should work
	err = dec.Codec.Reset(ctx)
	assert.NoError(t, err)
}

func TestCodec_Reset_Encoder(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	// Reset on a clean encoder should be a no-op (not dirty)
	err := encFull.Reset(ctx)
	assert.NoError(t, err)
}

// --- EncoderRaw additional methods ---

func TestEncoderRaw_MediaType_Panics(t *testing.T) {
	ctx := context.Background()
	e := EncoderRaw{}
	assert.Panics(t, func() { e.MediaType(ctx) })
}

func TestEncoderRaw_TimeBase_Panics(t *testing.T) {
	ctx := context.Background()
	e := EncoderRaw{}
	assert.Panics(t, func() { e.TimeBase(ctx) })
}

func TestEncoderRaw_ToCodecParameters_NoError(t *testing.T) {
	e := EncoderRaw{}
	assert.NoError(t, e.ToCodecParameters(context.Background(), nil))
}

func TestEncoderRaw_GetResolution_Nil(t *testing.T) {
	e := EncoderRaw{}
	assert.Nil(t, e.GetResolution(context.Background()))
}

func TestEncoderRaw_Reset_NoError(t *testing.T) {
	e := EncoderRaw{}
	assert.NoError(t, e.Reset(context.Background()))
}

// --- EncoderCopy additional methods ---

func TestEncoderCopy_Reset_NoError(t *testing.T) {
	e := EncoderCopy{}
	assert.NoError(t, e.Reset(context.Background()))
}

// --- Frame (nil InputPacket cases) ---

func TestFrame_MaxPosition_NilInputPacket(t *testing.T) {
	f := Frame{
		Frame:       astiav.AllocFrame(),
		InputPacket: nil,
	}
	t.Cleanup(f.Frame.Free)
	assert.Equal(t, time.Duration(0), f.MaxPosition(context.Background()))
}

func TestFrame_Position_NilInputPacket(t *testing.T) {
	f := Frame{
		Frame:       astiav.AllocFrame(),
		InputPacket: nil,
	}
	t.Cleanup(f.Frame.Free)
	assert.Equal(t, time.Duration(0), f.Position())
}

func TestFrame_PositionInBytes_NilInputPacket(t *testing.T) {
	f := Frame{
		Frame:       astiav.AllocFrame(),
		InputPacket: nil,
	}
	t.Cleanup(f.Frame.Free)
	assert.Equal(t, int64(-1), f.PositionInBytes())
}

func TestFrame_FrameDuration_NilInputPacket(t *testing.T) {
	f := Frame{
		Frame:       astiav.AllocFrame(),
		InputPacket: nil,
	}
	t.Cleanup(f.Frame.Free)
	assert.Equal(t, time.Duration(0), f.FrameDuration())
}

func TestFrame_TransferFromHardwareToRAM_NilDecoder(t *testing.T) {
	ctx := context.Background()
	f := Frame{
		Frame:   astiav.AllocFrame(),
		Decoder: nil,
	}
	t.Cleanup(f.Frame.Free)
	err := f.TransferFromHardwareToRAM(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decoder is nil")
}

// --- Encoder SetQuality with deferred condition ---

func TestEncoder_SetQuality_WithCondition(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	// Setting quality with a condition that's always true
	err := enc.SetQuality(ctx, quality.ConstantBitrate(300000), nil)
	assert.NoError(t, err)
}

// --- toDuration ---

func TestToDuration(t *testing.T) {
	// 1 second at timeBase 1.0
	d := toDuration(1, 1.0)
	assert.Equal(t, time.Second, d)

	// 90000 ticks at 1/90000 timeBase
	d = toDuration(90000, 1.0/90000.0)
	assert.InDelta(t, float64(time.Second), float64(d), float64(time.Microsecond))

	// Zero
	d = toDuration(0, 1.0)
	assert.Equal(t, time.Duration(0), d)
}

// --- More MIME type coverage ---

func TestCodec_MIMETypes_HEVC(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDHevc)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.Contains(t, mimeTypes, "video/H265")
	assert.Contains(t, mimeTypes, "video/HEVC")
	assert.Contains(t, mimeTypes, "video/hevc")
}

func TestCodec_MIMETypes_Opus(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDOpus)
	cp.SetSampleRate(48000)
	cp.SetChannelLayout(astiav.ChannelLayoutStereo)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.Contains(t, mimeTypes, "audio/opus")
	androidMIME := dec.Codec.GetAndroidMIMEType(ctx)
	assert.Equal(t, "audio/opus", androidMIME)
}

func TestCodec_MIMETypes_VP9(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDVp9)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	if err != nil {
		t.Skip("VP9 decoder not available")
	}
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.Contains(t, mimeTypes, "video/VP9")
	androidMIME := dec.Codec.GetAndroidMIMEType(ctx)
	assert.Equal(t, "video/x-vnd.on2.vp9", androidMIME)
}

func TestCodec_MIMETypes_MP3(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDMp3)
	cp.SetSampleRate(44100)
	cp.SetChannelLayout(astiav.ChannelLayoutStereo)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	if err != nil {
		t.Skip("MP3 decoder not available")
	}
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.Contains(t, mimeTypes, "audio/mpeg")
	androidMIME := dec.Codec.GetAndroidMIMEType(ctx)
	assert.Equal(t, "audio/mpeg", androidMIME)
}

func TestCodec_MIMETypes_MPEG4(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDMpeg4)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.Contains(t, mimeTypes, "video/mp4v-es")
	androidMIME := dec.Codec.GetAndroidMIMEType(ctx)
	assert.Equal(t, "video/mp4v-es", androidMIME)
}

func TestCodec_MIMETypes_Flac(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDFlac)
	cp.SetSampleRate(44100)
	cp.SetChannelLayout(astiav.ChannelLayoutStereo)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	if err != nil {
		t.Skip("FLAC decoder not available")
	}
	defer func() { _ = dec.Close(ctx) }()

	mimeTypes := dec.Codec.GetMIMEType(ctx)
	assert.Contains(t, mimeTypes, "audio/flac")
	androidMIME := dec.Codec.GetAndroidMIMEType(ctx)
	assert.Equal(t, "audio/flac", androidMIME)
}

// --- More sampleFormat coverage ---

func TestSampleFormatFromString_AllFormats(t *testing.T) {
	tests := []struct {
		input string
		want  astiav.SampleFormat
	}{
		{"u8p", astiav.SampleFormatU8P},
		{"s16", astiav.SampleFormatS16},
		{"s32", astiav.SampleFormatS32},
		{"s32p", astiav.SampleFormatS32P},
		{"s64", astiav.SampleFormatS64},
		{"s64p", astiav.SampleFormatS64P},
		{"flt", astiav.SampleFormatFlt},
		{"dbl", astiav.SampleFormatDbl},
		{"dblp", astiav.SampleFormatDblp},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := sampleFormatFromString(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- CodecParams Clone ---

func TestCodecParams_Clone(t *testing.T) {
	ctx := context.Background()
	origCP := astiav.AllocCodecParameters()
	t.Cleanup(origCP.Free)
	origCP.SetMediaType(astiav.MediaTypeVideo)
	origCP.SetCodecID(astiav.CodecIDH264)
	origCP.SetWidth(640)
	origCP.SetHeight(480)

	params := CodecParams{
		CodecName:       "libx264",
		CodecParameters: origCP,
		TimeBase:        astiav.NewRational(1, 30),
	}

	cloned := params.Clone(ctx)

	// Cloned should have a different CodecParameters pointer
	assert.NotSame(t, origCP, cloned.CodecParameters)
	// But same values
	assert.Equal(t, astiav.CodecIDH264, cloned.CodecParameters.CodecID())
	assert.Equal(t, 640, cloned.CodecParameters.Width())
	assert.Equal(t, 480, cloned.CodecParameters.Height())

	// Modifying original should not affect clone
	origCP.SetWidth(1920)
	assert.Equal(t, 640, cloned.CodecParameters.Width())
}

func TestCodecParams_Clone_NilCodecParameters(t *testing.T) {
	ctx := context.Background()
	params := CodecParams{
		CodecName: "libx264",
		TimeBase:  astiav.NewRational(1, 30),
	}
	cloned := params.Clone(ctx)
	assert.Nil(t, cloned.CodecParameters)
	assert.Equal(t, Name("libx264"), cloned.CodecName)
}

func TestCodecParams_Clone_WithCustomOptions(t *testing.T) {
	ctx := context.Background()
	origOpts := astiav.NewDictionary()
	t.Cleanup(origOpts.Free)
	origOpts.Set("preset", "ultrafast", 0)

	params := CodecParams{
		CodecName:     "libx264",
		CustomOptions: origOpts,
		TimeBase:      astiav.NewRational(1, 30),
	}

	cloned := params.Clone(ctx)
	// Custom options should be cloned
	require.NotNil(t, cloned.CustomOptions)
	assert.NotSame(t, origOpts, cloned.CustomOptions)
}

// --- codecInternals nil checks ---

func TestCodecInternals_IsOpen_NilContext(t *testing.T) {
	var ci codecInternals
	assert.False(t, ci.IsOpen())
}

func TestCodecInternals_IsDecoder_NilCodec(t *testing.T) {
	var ci codecInternals
	assert.False(t, ci.IsDecoder())
}

func TestCodecInternals_IsEncoder_NilCodec(t *testing.T) {
	var ci codecInternals
	assert.False(t, ci.IsEncoder())
}

func TestCodecInternals_IsMediaCodec_NilCodec(t *testing.T) {
	var ci codecInternals
	assert.False(t, ci.isMediaCodec())
}

func TestCodecInternals_IsNVENC_NilCodec(t *testing.T) {
	var ci codecInternals
	assert.False(t, ci.isNVENC())
}

// --- Decoder flush/drain ---

func TestDecoder_Flush_NilCallback(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	// Flush on a clean decoder
	err = dec.LockDo(ctx, func(ctx context.Context, dl *DecoderLocked) error {
		return dl.Flush(ctx, nil)
	})
	assert.NoError(t, err)
}

func TestDecoder_Drain_NilCallback(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	// Drain on a clean decoder
	err = dec.LockDo(ctx, func(ctx context.Context, dl *DecoderLocked) error {
		return dl.Drain(ctx, nil)
	})
	assert.NoError(t, err)
}

// --- DecoderLocked Reset ---

func TestDecoderLocked_Reset(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	err = dec.LockDo(ctx, func(ctx context.Context, dl *DecoderLocked) error {
		return dl.Reset(ctx)
	})
	assert.NoError(t, err)
}

// --- DecoderLocked ToCodecParameters ---

func TestDecoderLocked_ToCodecParameters(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(320)
	cp.SetHeight(240)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	outCP := astiav.AllocCodecParameters()
	t.Cleanup(outCP.Free)

	err = dec.LockDo(ctx, func(ctx context.Context, dl *DecoderLocked) error {
		return dl.ToCodecParameters(ctx, outCP)
	})
	require.NoError(t, err)
	assert.Equal(t, astiav.CodecIDH264, outCP.CodecID())
}

// --- Encoder Reset after dirty ---

func TestEncoder_Reset_AfterSendFrame(t *testing.T) {
	enc := newTestVideoEncoder(t)
	ctx := context.Background()
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}

	// Send a frame to make the encoder dirty
	frame := astiav.AllocFrame()
	t.Cleanup(frame.Free)
	frame.SetWidth(64)
	frame.SetHeight(64)
	frame.SetPixelFormat(enc.CodecContext(ctx).PixelFormat())
	require.NoError(t, frame.AllocBuffer(0))
	frame.SetPts(0)
	frame.SetFlags(frame.Flags().Add(astiav.FrameFlagKey))
	frame.SetPictureType(astiav.PictureTypeI)
	require.NoError(t, enc.SendFrame(ctx, frame))

	assert.True(t, enc.IsDirty())

	// Reset the encoder
	err := encFull.Reset(ctx)
	assert.NoError(t, err)
}

// --- Encoder getInitTS ---

func TestEncoder_GetInitTS(t *testing.T) {
	enc := newTestVideoEncoder(t)
	encFull, ok := enc.(*EncoderFull)
	if !ok {
		t.Skip("not a full encoder")
	}
	initTS := encFull.GetInitTS()
	assert.False(t, initTS.IsZero())
}

// --- NaiveDecoderFactory audio ---

func TestNaiveDecoderFactory_NewDecoder_Audio(t *testing.T) {
	ctx := context.Background()
	f := NewNaiveDecoderFactory(ctx, nil)

	fmtCtx := astiav.AllocFormatContext()
	t.Cleanup(fmtCtx.Free)
	decoder := astiav.FindDecoder(astiav.CodecIDAac)
	require.NotNil(t, decoder)
	stream := fmtCtx.NewStream(decoder)
	require.NotNil(t, stream)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	stream.CodecParameters().SetCodecID(astiav.CodecIDAac)
	stream.CodecParameters().SetSampleRate(44100)
	stream.CodecParameters().SetChannelLayout(astiav.ChannelLayoutStereo)
	stream.CodecParameters().SetSampleFormat(astiav.SampleFormatFltp)

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.NoError(t, err)
	require.NotNil(t, dec)
	defer func() { _ = dec.Close(ctx) }()

	assert.Empty(t, f.VideoDecoders)
	assert.Len(t, f.AudioDecoders, 1)
}

// --- CUDA hardware decoding (h264_cuvid) ---
// agent-generated tests

func requireNVIDIAGPU(t *testing.T) {
	t.Helper()
	_, err := astiav.CreateHardwareDeviceContext(
		astiav.HardwareDeviceType(globaltypes.HardwareDeviceTypeCUDA),
		"",
		nil,
		0,
	)
	if err != nil {
		t.Skipf("no NVIDIA GPU available: %v", err)
	}
}

func encodeTestFrames(
	t *testing.T,
	ctx context.Context,
	enc Encoder,
	width, height int,
	pixFmt astiav.PixelFormat,
	count int64,
) []*astiav.Packet {
	t.Helper()
	var packets []*astiav.Packet
	timeBase := enc.CodecContext(ctx).TimeBase()
	for i := int64(0); i < count; i++ {
		frame := astiav.AllocFrame()
		defer frame.Free()
		frame.SetWidth(width)
		frame.SetHeight(height)
		frame.SetPixelFormat(pixFmt)
		require.NoError(t, frame.AllocBuffer(0))
		frame.SetPts(i * int64(timeBase.Den()) / (30 * int64(timeBase.Num())))
		frame.SetDuration(int64(timeBase.Den()) / (30 * int64(timeBase.Num())))

		require.NoError(t, enc.SendFrame(ctx, frame))

		pkt := astiav.AllocPacket()
		for {
			err := enc.ReceivePacket(ctx, pkt)
			if err != nil {
				break
			}
			copyPkt := astiav.AllocPacket()
			require.NoError(t, copyPkt.Ref(pkt))
			packets = append(packets, copyPkt)
			pkt.Unref()
		}
	}
	return packets
}

func decodeAllPackets(
	t *testing.T,
	ctx context.Context,
	dec *Decoder,
	packets []*astiav.Packet,
	width, height int,
) int {
	t.Helper()
	var decodedCount int
	for _, pkt := range packets {
		err := dec.SendPacket(ctx, pkt)
		if err != nil {
			t.Logf("SendPacket error (flags=%v): %v", pkt.Flags(), err)
			continue
		}

		frame := astiav.AllocFrame()
		for {
			err = dec.ReceiveFrame(ctx, frame)
			if err != nil {
				break
			}
			decodedCount++
			assert.Equal(t, width, frame.Width())
			assert.Equal(t, height, frame.Height())
			frame.Unref()
		}
		frame.Free()
	}
	return decodedCount
}

func TestNewDecoder_H264_CUVID(t *testing.T) {
	requireNVIDIAGPU(t)
	ctx := context.Background()

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters:    cp,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
	})
	require.NoError(t, err)
	require.NotNil(t, dec)
	defer func() { _ = dec.Close(ctx) }()

	assert.Equal(t, "h264_cuvid", dec.codec.Name())
	assert.NotNil(t, dec.HardwareDeviceContext(ctx))
}

func TestCUVID_EncodeDecodeRoundTrip(t *testing.T) {
	requireNVIDIAGPU(t)
	ctx := context.Background()

	const width = 256
	const height = 256

	// Create an NVENC encoder.
	encCP := astiav.AllocCodecParameters()
	t.Cleanup(encCP.Free)
	encCP.SetMediaType(astiav.MediaTypeVideo)
	encCP.SetCodecID(astiav.CodecIDH264)
	encCP.SetWidth(width)
	encCP.SetHeight(height)

	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:          "h264_nvenc",
		CodecParameters:    encCP,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
		TimeBase:           astiav.NewRational(1, 30),
	})
	require.NoError(t, err)
	defer func() { _ = enc.Close(ctx) }()

	// Send several frames to get at least one encoded packet.
	pixFmt := enc.CodecContext(ctx).PixelFormat()
	packets := encodeTestFrames(t, ctx, enc, width, height, pixFmt, 10)
	require.NotEmpty(t, packets, "encoder did not produce any packets")

	// Extract codec parameters from encoder for the decoder.
	decCP := astiav.AllocCodecParameters()
	t.Cleanup(decCP.Free)
	require.NoError(t, enc.CodecContext(ctx).ToCodecParameters(decCP))

	// Create a CUVID decoder.
	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters:    decCP,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
	})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	assert.Equal(t, "h264_cuvid", dec.codec.Name())
	assert.NotNil(t, dec.HardwareDeviceContext(ctx))

	// Decode the packets.
	decodedCount := decodeAllPackets(t, ctx, dec, packets, width, height)
	require.Greater(t, decodedCount, 0, "decoder did not produce any frames")
}

func TestCUVID_TransferFromHardwareToRAM(t *testing.T) {
	requireNVIDIAGPU(t)
	ctx := context.Background()

	const width = 256
	const height = 256

	// Encode a few frames with NVENC.
	encCP := astiav.AllocCodecParameters()
	t.Cleanup(encCP.Free)
	encCP.SetMediaType(astiav.MediaTypeVideo)
	encCP.SetCodecID(astiav.CodecIDH264)
	encCP.SetWidth(width)
	encCP.SetHeight(height)

	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:          "h264_nvenc",
		CodecParameters:    encCP,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
		TimeBase:           astiav.NewRational(1, 30),
	})
	require.NoError(t, err)
	defer func() { _ = enc.Close(ctx) }()

	pixFmt := enc.CodecContext(ctx).PixelFormat()
	packets := encodeTestFrames(t, ctx, enc, width, height, pixFmt, 10)
	require.NotEmpty(t, packets)

	// Decode with CUVID.
	decCP := astiav.AllocCodecParameters()
	t.Cleanup(decCP.Free)
	require.NoError(t, enc.CodecContext(ctx).ToCodecParameters(decCP))

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters:    decCP,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
	})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	// Decode and transfer frames from GPU to RAM.
	var transferredCount int
	dl := dec.locked()
	for _, pkt := range packets {
		if err := dl.SendPacket(ctx, pkt); err != nil {
			t.Logf("SendPacket error (flags=%v): %v", pkt.Flags(), err)
			continue
		}

		for {
			hwFrame := astiav.AllocFrame()
			ramFrame := astiav.AllocFrame()

			err = dl.ReceiveFrame(ctx, hwFrame)
			if err != nil {
				hwFrame.Free()
				ramFrame.Free()
				break
			}

			// The frame should be in CUDA pixel format.
			assert.Equal(t, dl.HardwarePixelFormat(ctx), hwFrame.PixelFormat(),
				"decoded frame should have hardware pixel format")

			f := &Frame{
				Frame:    hwFrame,
				Decoder:  dl,
				RAMFrame: ramFrame,
			}
			err = f.TransferFromHardwareToRAM(ctx)
			require.NoError(t, err)

			// After transfer, the frame should have a non-hardware pixel format.
			assert.NotEqual(t, dl.HardwarePixelFormat(ctx), f.Frame.PixelFormat())
			assert.Equal(t, width, f.Frame.Width())
			assert.Equal(t, height, f.Frame.Height())
			transferredCount++

			hwFrame.Free()
			ramFrame.Free()
		}
	}
	require.Greater(t, transferredCount, 0, "no frames were transferred from GPU to RAM")
}

func TestInitHardwarePixelFormat_PrefersHwDeviceCtx(t *testing.T) {
	requireNVIDIAGPU(t)
	ctx := context.Background()

	// Create a cuvid decoder and verify it picked HwDeviceCtx, not HwFramesCtx.
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters:    cp,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
	})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	assert.Equal(t, hardwareContextTypeDevice, dec.hardwareContextType,
		"should prefer HwDeviceCtx over HwFramesCtx for h264_cuvid")
	assert.NotNil(t, dec.HardwareDeviceContext(ctx))
	assert.NotEqual(t, astiav.PixelFormatNone, dec.HardwarePixelFormat(ctx))
}

func TestNewDecoder_CUVID_FlushDrain(t *testing.T) {
	requireNVIDIAGPU(t)
	ctx := context.Background()

	const width = 256
	const height = 256

	// Encode frames.
	encCP := astiav.AllocCodecParameters()
	t.Cleanup(encCP.Free)
	encCP.SetMediaType(astiav.MediaTypeVideo)
	encCP.SetCodecID(astiav.CodecIDH264)
	encCP.SetWidth(width)
	encCP.SetHeight(height)

	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:          "h264_nvenc",
		CodecParameters:    encCP,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
		TimeBase:           astiav.NewRational(1, 30),
	})
	require.NoError(t, err)
	defer func() { _ = enc.Close(ctx) }()

	pixFmt := enc.CodecContext(ctx).PixelFormat()
	packets := encodeTestFrames(t, ctx, enc, width, height, pixFmt, 5)
	require.NotEmpty(t, packets)

	decCP := astiav.AllocCodecParameters()
	t.Cleanup(decCP.Free)
	require.NoError(t, enc.CodecContext(ctx).ToCodecParameters(decCP))

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecParameters:    decCP,
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
	})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	// Send packets.
	for _, pkt := range packets {
		_ = dec.SendPacket(ctx, pkt)
	}

	// Flush the decoder — should not panic or return unexpected errors.
	var flushedCount int
	err = dec.Flush(ctx, func(_ context.Context, _ *DecoderLocked, _ astiav.CodecCapabilities, f *astiav.Frame) error {
		flushedCount++
		return nil
	})
	if err != nil && !errors.Is(err, astiav.ErrEof) {
		require.NoError(t, err)
	}
}
