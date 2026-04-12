//go:build test_e2e

package kernel_test

import (
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/facebookincubator/go-belt"
	"github.com/facebookincubator/go-belt/tool/logger/implementation/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/extradata"
	"github.com/xaionaro-go/avpipeline/logger"
)

func requireAndroidMediaCodec(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "android" {
		t.Skip("android-only test")
	}
	if os.Getenv("MEDIACODEC_E2E") != "1" {
		t.Skip("set MEDIACODEC_E2E=1 to run")
	}
	c := astiav.FindEncoderByName("h264_mediacodec")
	if c == nil {
		t.Skip("h264_mediacodec encoder not available")
	}
	d := astiav.FindDecoderByName("h264_mediacodec")
	if d == nil {
		t.Skip("h264_mediacodec decoder not available")
	}

	// Verify MediaCodec actually works on this device.
	probe, err := codec.NewEncoder(context.Background(), codec.CodecParams{
		CodecName: "h264_mediacodec",
		CodecParameters: func() *astiav.CodecParameters {
			cp := astiav.AllocCodecParameters()
			cp.SetMediaType(astiav.MediaTypeVideo)
			cp.SetCodecID(astiav.CodecIDH264)
			cp.SetWidth(128)
			cp.SetHeight(128)
			return cp
		}(),
		TimeBase: astiav.NewRational(1, 30),
	})
	if err != nil {
		t.Skipf("MediaCodec not functional on this device: %v", err)
	}
	_ = probe.Close(context.Background())
}

// encodeTestFrames creates a MediaCodec encoder, encodes some frames, and returns
// the codec parameters (with extradata) and encoded packets. This is used by
// decoder tests because MediaCodec decoders require H.264 extradata (SPS/PPS).
func encodeTestFrames(ctx context.Context, t *testing.T, width, height int) (*astiav.CodecParameters, []*astiav.Packet, codec.Encoder) {
	t.Helper()

	encCP := astiav.AllocCodecParameters()
	t.Cleanup(encCP.Free)
	encCP.SetMediaType(astiav.MediaTypeVideo)
	encCP.SetCodecID(astiav.CodecIDH264)
	encCP.SetWidth(width)
	encCP.SetHeight(height)

	enc, err := codec.NewEncoder(ctx, codec.CodecParams{
		CodecName:       "h264_mediacodec",
		CodecParameters: encCP,
		TimeBase:        astiav.NewRational(1, 30),
	})
	require.NoError(t, err)

	pixFmt := enc.CodecContext(ctx).PixelFormat()
	timeBase := enc.CodecContext(ctx).TimeBase()
	var packets []*astiav.Packet
	for i := int64(0); i < 10; i++ {
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
			if err := enc.ReceivePacket(ctx, pkt); err != nil {
				break
			}
			copyPkt := astiav.AllocPacket()
			require.NoError(t, copyPkt.Ref(pkt))
			packets = append(packets, copyPkt)
			pkt.Unref()
		}
	}
	require.NotEmpty(t, packets, "encoder did not produce any packets")

	// Extract codec parameters for decoders.
	decCP := astiav.AllocCodecParameters()
	t.Cleanup(decCP.Free)
	require.NoError(t, enc.CodecContext(ctx).ToCodecParameters(decCP))
	t.Logf("codec parameters from ToCodecParameters: extradata_size=%d", len(decCP.ExtraData()))

	// The encoder uses CodecFlag2LocalHeader (for streaming), so SPS/PPS are
	// in-band in the packets rather than in the codec context extradata.
	// Extract SPS/PPS from the encoded packets to construct extradata.
	if len(decCP.ExtraData()) == 0 {
		ed := extractH264Extradata(t, packets)
		require.NotEmpty(t, ed, "could not extract H.264 extradata (SPS/PPS) from encoded packets")
		decCP.SetExtraData(ed)
		t.Logf("extracted extradata from packets: %d bytes", len(ed))
	}

	return decCP, packets, enc
}

// extractH264Extradata scans encoded packets for SPS and PPS NAL units
// and returns them as Annex-B formatted extradata suitable for SetExtraData.
func extractH264Extradata(t *testing.T, packets []*astiav.Packet) []byte {
	t.Helper()
	var sps, pps []byte
	for _, pkt := range packets {
		nalus := extradata.SplitAnnexB(pkt.Data())
		for _, nalu := range nalus {
			if len(nalu) == 0 {
				continue
			}
			nalType := extradata.H264NalUnitType(nalu[0] & 0x1F)
			switch nalType {
			case extradata.H264NalUnitTypeSPS:
				if sps == nil {
					sps = append([]byte(nil), nalu...)
					t.Logf("found SPS: %d bytes", len(sps))
				}
			case extradata.H264NalUnitTypePPS:
				if pps == nil {
					pps = append([]byte(nil), nalu...)
					t.Logf("found PPS: %d bytes", len(pps))
				}
			}
		}
		if sps != nil && pps != nil {
			break
		}
	}
	if sps == nil || pps == nil {
		return nil
	}
	// Build Annex-B extradata: start_code + SPS + start_code + PPS
	startCode := []byte{0x00, 0x00, 0x00, 0x01}
	ed := make([]byte, 0, len(startCode)*2+len(sps)+len(pps))
	ed = append(ed, startCode...)
	ed = append(ed, sps...)
	ed = append(ed, startCode...)
	ed = append(ed, pps...)
	return ed
}

// TestMediaCodec_AutoDetectHWDevice verifies that creating a MediaCodec decoder
// without explicit HardwareDeviceType auto-detects it and creates a HW device context.
func TestMediaCodec_AutoDetectHWDevice(t *testing.T) {
	requireAndroidMediaCodec(t)

	l := logrus.Default().WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	defer belt.Flush(ctx)

	// Encode frames to get codec parameters with extradata (SPS/PPS).
	// MediaCodec decoder requires extradata to initialize.
	decCP, _, enc := encodeTestFrames(ctx, t, 256, 256)
	defer func() { _ = enc.Close(ctx) }()

	t.Logf("encoder pixel format: %s", enc.CodecContext(ctx).PixelFormat())
	assert.NotNil(t, enc.HardwareDeviceContext(ctx),
		"MediaCodec encoder should auto-detect hardware device type")

	// No HardwareDeviceType set — auto-detection should kick in.
	dec, err := codec.NewDecoder(ctx, codec.DecoderInput{
		CodecParameters: decCP,
		CodecName:       "h264_mediacodec",
	})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	assert.NotNil(t, dec.HardwareDeviceContext(ctx),
		"MediaCodec decoder should auto-detect hardware device type and create HW device context")
	t.Logf("decoder pixel format: %s", dec.CodecContext(ctx).PixelFormat())
}

// TestMediaCodec_EncodeDecodeRoundTrip encodes frames with h264_mediacodec,
// then decodes them with h264_mediacodec and verifies the decoded frames
// have the correct dimensions.
func TestMediaCodec_EncodeDecodeRoundTrip(t *testing.T) {
	requireAndroidMediaCodec(t)

	l := logrus.Default().WithLevel(logger.LevelTrace)
	ctx := logger.CtxWithLogger(context.Background(), l)
	defer belt.Flush(ctx)

	const width = 256
	const height = 256

	decCP, packets, enc := encodeTestFrames(ctx, t, width, height)
	defer func() { _ = enc.Close(ctx) }()
	t.Logf("encoded %d packets", len(packets))

	// Create a MediaCodec decoder (no explicit HardwareDeviceType).
	dec, err := codec.NewDecoder(ctx, codec.DecoderInput{
		CodecParameters: decCP,
		CodecName:       "h264_mediacodec",
	})
	require.NoError(t, err)
	defer func() { _ = dec.Close(ctx) }()

	assert.NotNil(t, dec.HardwareDeviceContext(ctx),
		"MediaCodec decoder should have auto-detected HW device context")

	// Decode.
	var decodedCount int
	for _, pkt := range packets {
		err := dec.SendPacket(ctx, pkt)
		if err != nil {
			t.Logf("SendPacket error: %v", err)
			continue
		}
		for {
			frame := astiav.AllocFrame()
			err = dec.ReceiveFrame(ctx, frame)
			if err != nil {
				frame.Free()
				break
			}
			decodedCount++
			assert.Equal(t, width, frame.Width())
			assert.Equal(t, height, frame.Height())
			t.Logf("decoded frame #%d: %dx%d pixel_format=%s",
				decodedCount, frame.Width(), frame.Height(), frame.PixelFormat())
			frame.Free()
		}
	}
	require.Greater(t, decodedCount, 0, "decoder did not produce any frames")
	t.Logf("decoded %d frames total", decodedCount)
}
