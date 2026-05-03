// output_late_stream_test.go contains tests verifying that adding a new
// output stream after the muxer header was already written is rejected
// with ErrLateStreamAddition (instead of silently producing a SIGFPE
// inside av_interleaved_write_frame).

package kernel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

// mockMutablePacketSource is a packet.Source backed by an
// astiav.FormatContext that can have streams added by the test after
// the Output kernel has already received earlier packets.
type mockMutablePacketSource struct {
	fmtCtx *astiav.FormatContext
}

func newMockMutablePacketSource() *mockMutablePacketSource {
	return &mockMutablePacketSource{
		fmtCtx: astiav.AllocFormatContext(),
	}
}

func (m *mockMutablePacketSource) WithOutputFormatContext(
	_ context.Context,
	cb func(*astiav.FormatContext),
) {
	cb(m.fmtCtx)
}

func (m *mockMutablePacketSource) String() string { return "mockMutablePacketSource" }

func (m *mockMutablePacketSource) addStream(codecID astiav.CodecID) *astiav.Stream {
	s := m.fmtCtx.NewStream(astiav.FindEncoder(codecID))
	return s
}

var _ packet.Source = (*mockMutablePacketSource)(nil)

// TestOutput_NotifyAboutPacketSource_LateStream verifies that when
// NotifyAboutPacketSource is called with a new stream after the muxer
// header has been written, ErrLateStreamAddition is returned (instead
// of the new stream being silently preallocated, which would later
// SIGFPE inside av_interleaved_write_frame).
func TestOutput_NotifyAboutPacketSource_LateStream(t *testing.T) {
	ctx := context.Background()

	output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: types.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer output.Close(ctx)

	src := newMockMutablePacketSource()
	defer src.fmtCtx.Free()

	// Initial notify with one stream succeeds.
	first := src.addStream(astiav.CodecIDH264)
	require.NoError(t, output.NotifyAboutPacketSource(ctx, src))

	// Manually mark stream #0 as registered, then mark the muxer as
	// having written its header. (preallocateOutputStream only inserts
	// into OutputStreams when the codec's MediaType is recognized;
	// stubbed encoders may report MediaTypeUnknown.)
	output.OutputStreams[first.Index()] = nil
	output.headerSent = true

	// Add a second stream to the source AFTER headerSent. The next
	// notify must surface ErrLateStreamAddition for the new stream
	// rather than silently preallocate it. Stream #0 must NOT cause
	// ErrLateStreamAddition because it was already registered.
	newStream := src.addStream(astiav.CodecIDAac)
	err = output.NotifyAboutPacketSource(ctx, src)
	require.Error(t, err)

	var lateErr ErrLateStreamAddition
	require.True(t, errors.As(err, &lateErr),
		"expected ErrLateStreamAddition, got: %v", err)
	require.Equal(t, newStream.Index(), lateErr.StreamIndex)
}

// TestOutput_GetOutputStream_LateStream verifies that the
// internal getOutputStream path rejects late stream additions
// once the header has been written, returning the typed error.
func TestOutput_GetOutputStream_LateStream(t *testing.T) {
	ctx := context.Background()

	output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: types.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer output.Close(ctx)

	src := newMockMutablePacketSource()
	defer src.fmtCtx.Free()

	stream := src.addStream(astiav.CodecIDH264)

	// Mark the muxer as having written its header before any output
	// stream entry exists for `stream`.
	output.headerSent = true

	_, err = output.getOutputStream(ctx, src, stream, src.fmtCtx)
	require.Error(t, err)
	var lateErr ErrLateStreamAddition
	require.True(t, errors.As(err, &lateErr),
		"expected ErrLateStreamAddition, got: %v", err)
	require.Equal(t, stream.Index(), lateErr.StreamIndex)
}

// configureSampleH264Stream populates an astiav.Stream with a minimal
// valid set of codec parameters so that the output kernel's audio
// sample_rate / video time_base guards in configureOutputStream do
// not reject it.
func configureSampleH264Stream(s *astiav.Stream) {
	cp := s.CodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(1920)
	cp.SetHeight(1080)
	s.SetTimeBase(astiav.NewRational(1, 90000))
}

func configureSampleAACStream(s *astiav.Stream) {
	cp := s.CodecParameters()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDAac)
	cp.SetSampleRate(48000)
	cp.SetChannelLayout(astiav.ChannelLayoutStereo)
	s.SetTimeBase(astiav.NewRational(1, 48000))
}

func buildAudioPacket(t *testing.T, src *mockMutablePacketSource, stream *astiav.Stream) packet.Input {
	t.Helper()
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetStreamIndex(stream.Index())
	// A minimal payload so the muxer code path that inspects packet
	// data does not panic. We never actually write to the muxer in
	// the timeout-fired branch (no header has been written yet).
	require.NoError(t, pkt.AllocPayload(4))
	pkt.SetDts(0)
	pkt.SetPts(0)
	return packet.BuildInput(pkt, &packet.StreamInfo{
		Stream: stream,
		Source: src,
	})
}

func buildVideoPacket(t *testing.T, src *mockMutablePacketSource, stream *astiav.Stream, isKey bool) packet.Input {
	t.Helper()
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetStreamIndex(stream.Index())
	require.NoError(t, pkt.AllocPayload(4))
	pkt.SetDts(0)
	pkt.SetPts(0)
	if isKey {
		pkt.SetFlags(astiav.NewPacketFlags(astiav.PacketFlagKey))
	}
	return packet.BuildInput(pkt, &packet.StreamInfo{
		Stream: stream,
		Source: src,
	})
}

// TestOutput_BoundedGating_TimeoutFires verifies that when the
// configured Min* stream counts have not been satisfied within
// WaitForOutputStreams.Timeout, send() commits to writing the header
// with whatever streams are registered. After the commit, a stream
// arriving late is rejected with ErrLateStreamAddition.
func TestOutput_BoundedGating_TimeoutFires(t *testing.T) {
	ctx := context.Background()

	output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: types.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
		WaitForOutputStreams: &OutputConfigWaitForOutputStreams{
			MinStreamsVideo: 1,
			MinStreamsAudio: 1,
			Timeout:         50 * time.Millisecond,
		},
	})
	require.NoError(t, err)
	defer output.Close(ctx)

	src := newMockMutablePacketSource()
	defer src.fmtCtx.Free()

	// Source advertises only an audio stream; video is "still being
	// parsed" by the demuxer and has not yet appeared.
	audioStream := src.addStream(astiav.CodecIDAac)
	configureSampleAACStream(audioStream)

	// First audio packet — should be buffered (waiting for video).
	audioPkt := buildAudioPacket(t, src, audioStream)
	err = output.SendInput(ctx, packetorframe.InputUnion{Packet: &audioPkt}, nil)
	require.NoError(t, err)
	require.False(t, output.headerSent, "header must not be written while waiting for video")
	require.False(t, output.sendingAllowed, "sendingAllowed must be false while waiting")
	require.False(t, output.pendingPacketsDeadline.IsZero(),
		"deadline must be armed once a packet is buffered")

	// Force the deadline into the past so the next send commits.
	output.pendingPacketsDeadline = time.Now().Add(-time.Second)

	audioPkt2 := buildAudioPacket(t, src, audioStream)
	err = output.SendInput(ctx, packetorframe.InputUnion{Packet: &audioPkt2}, nil)
	// Writing the null muxer header may fail because the audio codec
	// parameters are only minimally populated, but what we care about
	// is that the kernel committed (headerSent = true) rather than
	// returning nil and continuing to wait.
	require.True(t, output.sendingAllowed,
		"timeout must release the gate; got sendingAllowed=false (err=%v)", err)
	require.True(t, output.headerSent,
		"timeout must trigger WriteHeader; got headerSent=false (err=%v)", err)

	// A late video stream now appears — must be rejected with the
	// typed error rather than silently registered.
	videoStream := src.addStream(astiav.CodecIDH264)
	configureSampleH264Stream(videoStream)

	_, getErr := output.getOutputStream(ctx, src, videoStream, src.fmtCtx)
	require.Error(t, getErr)
	var lateErr ErrLateStreamAddition
	require.True(t, errors.As(getErr, &lateErr),
		"expected ErrLateStreamAddition for late video stream, got: %v", getErr)
	require.Equal(t, videoStream.Index(), lateErr.StreamIndex)
}

// TestOutput_BoundedGating_StreamsArriveBeforeTimeout verifies that
// when both required streams register before the timeout elapses,
// WriteHeader fires once the threshold is met (not before). No typed
// error is surfaced and no late-stream rejection happens.
func TestOutput_BoundedGating_StreamsArriveBeforeTimeout(t *testing.T) {
	ctx := context.Background()

	output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: types.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
		WaitForOutputStreams: &OutputConfigWaitForOutputStreams{
			MinStreamsVideo: 1,
			MinStreamsAudio: 1,
			// Plenty of headroom so the test isn't time-sensitive.
			Timeout: 10 * time.Second,
		},
	})
	require.NoError(t, err)
	defer output.Close(ctx)

	src := newMockMutablePacketSource()
	defer src.fmtCtx.Free()

	// Initially only audio is visible.
	audioStream := src.addStream(astiav.CodecIDAac)
	configureSampleAACStream(audioStream)

	audioPkt := buildAudioPacket(t, src, audioStream)
	err = output.SendInput(ctx, packetorframe.InputUnion{Packet: &audioPkt}, nil)
	require.NoError(t, err)
	require.False(t, output.headerSent,
		"header must NOT be written while video is still missing")
	require.False(t, output.sendingAllowed,
		"sendingAllowed must remain false while threshold is unmet")
	require.False(t, output.pendingPacketsDeadline.IsZero(),
		"deadline armed on first buffered packet")

	// Now the video stream appears (well within the 10s timeout).
	videoStream := src.addStream(astiav.CodecIDH264)
	configureSampleH264Stream(videoStream)

	videoPkt := buildVideoPacket(t, src, videoStream, true)
	err = output.SendInput(ctx, packetorframe.InputUnion{Packet: &videoPkt}, nil)
	// Header may still fail to actually write to the null muxer for
	// the same minimal-codec-params reason as above; the assertion
	// that matters here is that the kernel committed *because both
	// streams registered*, not because the timeout elapsed.
	require.True(t, output.sendingAllowed,
		"both streams registered before timeout; gate must release")
	require.True(t, output.headerSent,
		"WriteHeader must fire once both streams registered (err=%v)", err)
	require.False(t, time.Now().After(output.pendingPacketsDeadline),
		"deadline must not have elapsed in this run")

	// Both streams must be present in the output.
	require.NotNil(t, output.OutputStreams[audioStream.Index()],
		"audio output stream must be registered")
	require.NotNil(t, output.OutputStreams[videoStream.Index()],
		"video output stream must be registered")
}

// TestOutput_BoundedGating_DefaultTimeoutApplied verifies that when
// WaitForOutputStreams.Timeout is left at its zero value the kernel
// substitutes defaultWaitForOutputStreamsTimeout instead of waiting
// indefinitely (which would deadlock late-stream-addition scenarios).
func TestOutput_BoundedGating_DefaultTimeoutApplied(t *testing.T) {
	ctx := context.Background()

	output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		CustomOptions: types.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
		// Timeout deliberately left at zero.
		WaitForOutputStreams: &OutputConfigWaitForOutputStreams{},
	})
	require.NoError(t, err)
	defer output.Close(ctx)

	require.Equal(t, defaultWaitForOutputStreamsTimeout,
		output.Config.WaitForOutputStreams.Timeout,
		"zero Timeout must be replaced by the package default")
}

// TestMuxerAllowsLateStreamAddition_AllRefuse verifies the safe-by-
// default policy: every container we currently target refuses late
// stream addition. If a future change flips one of these to true,
// this test acts as a tripwire to ensure the change is intentional.
func TestMuxerAllowsLateStreamAddition_AllRefuse(t *testing.T) {
	for _, name := range []string{
		"flv", "mpegts", "matroska", "webm", "mp4", "mov", "hls", "m3u8",
		"unknown-future-format",
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, muxerAllowsLateStreamAddition(name),
				"muxer %q must refuse late stream addition until libavformat grows runtime PMT/Tracks update support", name)
		})
	}
}
