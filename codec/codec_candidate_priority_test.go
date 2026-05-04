package codec

import (
	"context"
	"errors"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func TestNaiveDecoderFactory_VideoCodecNamesFallsBackToSecondCandidate(t *testing.T) {
	ctx := context.Background()
	stream := makeVideoStream(t, astiav.CodecIDH264)

	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		VideoCodecs: []Name{"__missing_decoder_for_priority_test__", "h264"},
	})

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.NoError(t, err)
	require.NotNil(t, dec)
	t.Cleanup(func() { _ = dec.Close(ctx) })

	assert.Equal(t, Name("h264"), dec.InitParams.CodecName)
	assert.Equal(t, "h264", dec.codec.Name())
}

func TestNaiveDecoderFactory_CodecNamesAllMissingReturnsTypedCandidateErrors(t *testing.T) {
	ctx := context.Background()
	stream := makeVideoStream(t, astiav.CodecIDH264)

	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		VideoCodecs: []Name{
			"__missing_decoder_candidate_a__",
			"__missing_decoder_candidate_b__",
		},
	})

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.Error(t, err)
	assert.Nil(t, dec)

	var codecNotFound ErrCodecNotFound
	require.True(t, errors.As(err, &codecNotFound))
	assert.Equal(t, Name("__missing_decoder_candidate_a__"), codecNotFound.CodecName)
	assert.Contains(t, err.Error(), "__missing_decoder_candidate_b__")
}

func TestNaiveDecoderFactory_VideoCodecNamesClearHardwareForSoftwareCandidate(t *testing.T) {
	ctx := context.Background()
	stream := makeVideoStream(t, astiav.CodecIDH264)

	var captured HardwareDeviceType
	var capturedSet bool
	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		VideoCodecs:        []Name{"h264"},
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
		PreInitFunc: func(_ context.Context, _ *astiav.Stream, in *DecoderInput) {
			captured = in.HardwareDeviceType
			capturedSet = true
			in.CodecName = "__abort_after_capture__"
			in.CodecParameters.SetCodecID(astiav.CodecIDNone)
		},
	})

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.Error(t, err)
	assert.Nil(t, dec)
	require.True(t, capturedSet)
	assert.Equal(t, globaltypes.HardwareDeviceTypeNone, globaltypes.HardwareDeviceType(captured))
}

func TestNaiveDecoderFactory_VideoCodecNamesCloneEmptyOptionsPerCandidate(t *testing.T) {
	ctx := context.Background()
	opts := astiav.NewDictionary()
	t.Cleanup(opts.Free)

	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		VideoCodecs:  []Name{"h264", "h264"},
		VideoOptions: opts,
	})

	candidates, err := f.videoDecoderCandidates(ctx, astiav.CodecIDH264)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.NotNil(t, candidates[0].CustomOptions)
	require.NotNil(t, candidates[1].CustomOptions)

	require.NoError(t, candidates[0].CustomOptions.Set("gpu", "0", 0))
	assert.Nil(t, candidates[1].CustomOptions.Get("gpu", nil, 0))
	assert.Nil(t, opts.Get("gpu", nil, 0))
}

func TestNaiveDecoderFactory_FallbackCandidateSeesCleanEmptyOptionsAfterMutation(t *testing.T) {
	ctx := context.Background()
	stream := makeVideoStream(t, astiav.CodecIDH264)
	opts := astiav.NewDictionary()
	t.Cleanup(opts.Free)

	var attempts int
	f := NewNaiveDecoderFactory(ctx, &NaiveDecoderFactoryParams{
		VideoCodecs:  []Name{"h264", "h264"},
		VideoOptions: opts,
		PreInitFunc: func(_ context.Context, _ *astiav.Stream, in *DecoderInput) {
			attempts++
			require.NotNil(t, in.CustomOptions)
			switch attempts {
			case 1:
				require.NoError(t, in.CustomOptions.Set("gpu", "0", 0))
				in.HardwareDeviceType = HardwareDeviceType(0xff)
			case 2:
				assert.Nil(t, in.CustomOptions.Get("gpu", nil, 0))
				assert.Nil(t, opts.Get("gpu", nil, 0))
			default:
				t.Fatalf("unexpected decoder attempt %d", attempts)
			}
		},
	})

	dec, err := f.NewDecoder(ctx, nil, stream, nil)
	require.NoError(t, err)
	require.NotNil(t, dec)
	t.Cleanup(func() { _ = dec.Close(ctx) })

	assert.Equal(t, 2, attempts)
}

func TestNaiveEncoderFactory_VideoCodecNamesFallsBackToSecondCandidate(t *testing.T) {
	ctx := context.Background()
	encoderCodec := requireTestVideoEncoderCodec(t)
	cp := makeEncoderVideoCodecParameters(t, encoderCodec.ID(), 64, 64)

	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodecs: []Name{"__missing_encoder_for_priority_test__", Name(encoderCodec.Name())},
	})

	enc, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	require.NoError(t, err)
	require.NotNil(t, enc)
	t.Cleanup(func() { _ = enc.Close(ctx) })

	codecFull, ok := enc.(*EncoderFull)
	require.True(t, ok)
	assert.Equal(t, Name(encoderCodec.Name()), codecFull.InitParams.CodecName)
	assert.Equal(t, encoderCodec.Name(), enc.Codec(ctx).Name())
}

func TestNaiveEncoderFactory_VideoCodecNamesClearHardwareForSoftwareCandidate(t *testing.T) {
	ctx := context.Background()
	encoderCodec := requireTestVideoEncoderCodec(t)
	cp := makeEncoderVideoCodecParameters(t, encoderCodec.ID(), 64, 64)

	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodecs:        []Name{Name(encoderCodec.Name())},
		HardwareDeviceType: globaltypes.HardwareDeviceTypeCUDA,
	})

	enc, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	require.NoError(t, err)
	require.NotNil(t, enc)
	t.Cleanup(func() { _ = enc.Close(ctx) })

	codecFull, ok := enc.(*EncoderFull)
	require.True(t, ok)
	assert.Equal(t, globaltypes.HardwareDeviceTypeNone, globaltypes.HardwareDeviceType(codecFull.InitParams.HardwareDeviceType))
}

func TestNaiveEncoderFactory_VideoCodecNamesCloneEmptyOptionsPerCandidate(t *testing.T) {
	ctx := context.Background()
	opts := astiav.NewDictionary()
	t.Cleanup(opts.Free)

	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodecs:  []Name{"libx264", "libx264"},
		VideoOptions: opts,
	})

	candidates, err := f.videoEncoderCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.NotNil(t, candidates[0].CustomOptions)
	require.NotNil(t, candidates[1].CustomOptions)

	require.NoError(t, candidates[0].CustomOptions.Set("gpu", "0", 0))
	assert.Nil(t, candidates[1].CustomOptions.Get("gpu", nil, 0))
	assert.Nil(t, opts.Get("gpu", nil, 0))
}

func TestNaiveEncoderFactory_VideoCodecNamesRejectMixedCodecIDs(t *testing.T) {
	ctx := context.Background()
	first, second := requireDistinctVideoEncoderCodecs(t)
	cp := makeEncoderVideoCodecParameters(t, first.ID(), 64, 64)

	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodecs: []Name{Name(first.Name()), Name(second.Name())},
	})

	enc, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	require.Error(t, err)
	assert.Nil(t, enc)

	var mismatch ErrCodecCandidateMismatch
	require.True(t, errors.As(err, &mismatch))
	assert.Equal(t, astiav.MediaTypeVideo, mismatch.MediaType)
	assert.Equal(t, first.ID(), mismatch.ExpectedCodecID)
	assert.Equal(t, second.ID(), mismatch.ActualCodecID)
}

func TestNaiveEncoderFactory_VideoCodecNamesFallsBackAfterOpenFailure(t *testing.T) {
	ctx := context.Background()
	hwCodec := astiav.FindEncoderByName("h264_nvenc")
	swCodec := astiav.FindEncoderByName("libx264")
	if hwCodec == nil || swCodec == nil {
		t.Skip("h264_nvenc and libx264 encoders are required for this fallback witness")
	}
	if hwCodec.ID() != swCodec.ID() {
		t.Fatalf("test setup expected h264_nvenc and libx264 to share a codec ID")
	}

	cp := makeEncoderVideoCodecParameters(t, swCodec.ID(), 64, 64)
	probe := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodecs: []Name{Name(hwCodec.Name())},
	})
	probeEnc, probeErr := probe.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	if probeErr == nil {
		require.NoError(t, probeEnc.Close(ctx))
		t.Skip("h264_nvenc opens on this host, so this host cannot force the open-failure fallback")
	}
	var openErr ErrCodecOpen
	var hardwareErr ErrHardwareUnavailable
	if !errors.As(probeErr, &openErr) && !errors.As(probeErr, &hardwareErr) {
		t.Skipf("h264_nvenc failed before the retryable open/hardware path: %v", probeErr)
	}

	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodecs: []Name{Name(hwCodec.Name()), Name(swCodec.Name())},
	})

	enc, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	require.NoError(t, err)
	require.NotNil(t, enc)
	t.Cleanup(func() { _ = enc.Close(ctx) })

	codecFull, ok := enc.(*EncoderFull)
	require.True(t, ok)
	assert.Equal(t, Name(swCodec.Name()), codecFull.InitParams.CodecName)
}

func TestNaiveEncoderFactory_FailedCandidateDoesNotMutateFactoryResolution(t *testing.T) {
	ctx := context.Background()
	encoderCodec := requireTestVideoEncoderCodec(t)
	cp := makeEncoderVideoCodecParameters(t, encoderCodec.ID(), 720, 1280)
	res := Resolution{Width: 1280, Height: 720}

	f := NewNaiveEncoderFactory(ctx, &NaiveEncoderFactoryParams{
		VideoCodecs:     []Name{"__missing_encoder_for_resolution_test__", Name(encoderCodec.Name())},
		VideoResolution: &res,
	})

	enc, err := f.NewEncoder(ctx, cp, astiav.NewRational(1, 30))
	require.NoError(t, err)
	require.NotNil(t, enc)
	t.Cleanup(func() { _ = enc.Close(ctx) })

	require.NotNil(t, f.VideoResolution)
	assert.Equal(t, Resolution{Width: 1280, Height: 720}, *f.VideoResolution)
}

func TestNewEncoder_CodecOpenFailureIsTyped(t *testing.T) {
	ctx := context.Background()
	encoderCodec := astiav.FindEncoderByName("libx264")
	if encoderCodec == nil {
		t.Skip("libx264 encoder is not registered on this host")
	}
	cp := makeEncoderVideoCodecParameters(t, encoderCodec.ID(), 0, 0)

	enc, err := NewEncoder(ctx, CodecParams{
		CodecName:       Name(encoderCodec.Name()),
		CodecParameters: cp,
		TimeBase:        astiav.NewRational(1, 30),
	})
	require.Error(t, err)
	assert.Nil(t, enc)

	var openErr ErrCodecOpen
	require.True(t, errors.As(err, &openErr))
	assert.True(t, openErr.IsEncoder)
	assert.Equal(t, Name(encoderCodec.Name()), openErr.CodecName)
}

func TestNewDecoder_HardwareUnavailableIsTyped(t *testing.T) {
	ctx := context.Background()
	if astiav.FindDecoderByName("h264") == nil {
		t.Skip("h264 decoder is not registered on this host")
	}
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(64)
	cp.SetHeight(64)

	dec, err := NewDecoder(ctx, DecoderInput{
		CodecName:          "h264",
		CodecParameters:    cp,
		HardwareDeviceType: HardwareDeviceType(0xff),
	})
	require.Error(t, err)
	assert.Nil(t, dec)

	var hardwareErr ErrHardwareUnavailable
	require.True(t, errors.As(err, &hardwareErr))
	assert.False(t, hardwareErr.IsEncoder)
	assert.Equal(t, Name("h264"), hardwareErr.CodecName)
	assert.Equal(t, HardwareDeviceType(0xff), hardwareErr.HardwareDeviceType)
}

func requireTestVideoEncoderCodec(t *testing.T) *astiav.Codec {
	t.Helper()
	for _, name := range []string{"libx264", "mpeg4"} {
		if c := astiav.FindEncoderByName(name); c != nil {
			return c
		}
	}
	t.Skip("no test video encoder is registered")
	return nil
}

func requireDistinctVideoEncoderCodecs(t *testing.T) (*astiav.Codec, *astiav.Codec) {
	t.Helper()
	var first *astiav.Codec
	for _, name := range []string{"libx264", "mpeg4", "libaom-av1", "libx265"} {
		c := astiav.FindEncoderByName(name)
		if c == nil {
			continue
		}
		if first == nil {
			first = c
			continue
		}
		if first.ID() != c.ID() {
			return first, c
		}
	}
	t.Skip("no pair of distinct video encoder codec IDs is registered")
	return nil, nil
}

func makeEncoderVideoCodecParameters(
	t *testing.T,
	codecID astiav.CodecID,
	width, height int,
) *astiav.CodecParameters {
	t.Helper()
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(codecID)
	cp.SetWidth(width)
	cp.SetHeight(height)
	cp.SetPixelFormat(astiav.PixelFormatYuv420P)
	cp.SetFrameRate(astiav.NewRational(30, 1))
	return cp
}
