// output_test.go contains tests for the output kernel.

package kernel

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

// mockPacketSourceNoFormatCtx simulates a packet.Source where WithOutputFormatContext
// doesn't call the callback.
type mockPacketSourceNoFormatCtx struct{}

func (m *mockPacketSourceNoFormatCtx) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	// Intentionally not calling callback
}

func (m *mockPacketSourceNoFormatCtx) String() string {
	return "mockPacketSourceNoFormatCtx"
}

var _ packet.Source = (*mockPacketSourceNoFormatCtx)(nil)

// TestOutput_ErrNoSourceFormatContext tests the scenario where:
// 1. Output.SendInput calls Source.WithOutputFormatContext
// 2. The Source doesn't call the callback
// 3. ErrNoSourceFormatContext is raised
//
// The test verifies that IgnoreNoSourceFormatCtxErrors option handles this gracefully.
func TestOutput_ErrNoSourceFormatContext(t *testing.T) {
	ctx := context.Background()

	t.Run("returns_error_when_ignore_disabled", func(t *testing.T) {
		output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
			IgnoreNoSourceFormatCtxErrors: false,
			CustomOptions: types.DictionaryItems{{
				Key:   "f",
				Value: "null",
			}},
		})
		require.NoError(t, err)
		defer output.Close(ctx)

		pkt := astiav.AllocPacket()
		defer pkt.Free()

		stream := output.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))

		inputPkt := packet.BuildInput(pkt, &packet.StreamInfo{
			Stream: stream,
			Source: &mockPacketSourceNoFormatCtx{},
		})

		err = output.SendInput(ctx, packetorframe.InputUnion{Packet: &inputPkt}, nil)
		require.Error(t, err)
		require.True(t, errors.As(err, &ErrNoSourceFormatContext{}), "got: %v", err)
	})

	t.Run("drops_packet_when_ignore_enabled", func(t *testing.T) {
		output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
			IgnoreNoSourceFormatCtxErrors: true,
			CustomOptions: types.DictionaryItems{{
				Key:   "f",
				Value: "null",
			}},
		})
		require.NoError(t, err)
		defer output.Close(ctx)

		pkt := astiav.AllocPacket()
		defer pkt.Free()

		stream := output.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))

		inputPkt := packet.BuildInput(pkt, &packet.StreamInfo{
			Stream: stream,
			Source: &mockPacketSourceNoFormatCtx{},
		})

		err = output.SendInput(ctx, packetorframe.InputUnion{Packet: &inputPkt}, nil)
		require.NoError(t, err)
	})
}

// TestOutput_DevNull verifies that "/dev/null" as an output URL
// results in FFmpeg's null muxer being used (discarding all output).
func TestOutput_DevNull(t *testing.T) {
	ctx := context.Background()

	output, err := NewOutputFromURL(ctx, "/dev/null", secret.New(""), OutputConfig{})
	require.NoError(t, err)
	defer output.Close(ctx)

	require.Equal(t, "null", output.FormatContext.OutputFormat().Name())
}

// mockPacketSourceContextRespecting simulates a packet.Source that respects
// context cancellation in WithOutputFormatContext.
type mockPacketSourceContextRespecting struct{}

func (m *mockPacketSourceContextRespecting) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	// Respect context cancellation - don't call callback if ctx is done
	select {
	case <-ctx.Done():
		return
	default:
		// In a real implementation, this would call callback with a format context
		// For this test, we simulate the scenario where we respect ctx but still can't provide format context
	}
}

func (m *mockPacketSourceContextRespecting) String() string {
	return "mockPacketSourceContextRespecting"
}

var _ packet.Source = (*mockPacketSourceContextRespecting)(nil)

type mockPacketSourceWithFormatCtx struct {
	fmtCtx *astiav.FormatContext
}

func (m *mockPacketSourceWithFormatCtx) WithOutputFormatContext(
	_ context.Context,
	callback func(*astiav.FormatContext),
) {
	callback(m.fmtCtx)
}

func (m *mockPacketSourceWithFormatCtx) String() string {
	return "mockPacketSourceWithFormatCtx"
}

var _ packet.Source = (*mockPacketSourceWithFormatCtx)(nil)

// TestOutput_ReturnsContextErrorWhenCancelled tests that when context is cancelled
// during WithOutputFormatContext, we return ctx.Err() instead of ErrNoSourceFormatContext
func TestOutput_ReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	output, err := NewOutputFromURL(ctx, "", secret.New(""), OutputConfig{
		IgnoreNoSourceFormatCtxErrors: false,
		CustomOptions: types.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer output.Close(context.Background())

	pkt := astiav.AllocPacket()
	defer pkt.Free()

	stream := output.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))

	inputPkt := packet.BuildInput(pkt, &packet.StreamInfo{
		Stream: stream,
		Source: &mockPacketSourceContextRespecting{},
	})

	// Cancel context before calling SendInput
	cancel()

	err = output.SendInput(ctx, packetorframe.InputUnion{Packet: &inputPkt}, nil)
	require.Error(t, err)
	// Should return context.Canceled, not ErrNoSourceFormatContext
	require.ErrorIs(t, err, context.Canceled, "expected context.Canceled, got: %v", err)
}

func TestOutput_CloseInterruptsAsyncOpenIOContext(t *testing.T) {
	ctx := context.Background()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})

	connCh := make(chan net.Conn, 1)
	acceptErrCh := make(chan error, 1)
	observability.Go(ctx, func(context.Context) {
		conn, err := listener.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		connCh <- conn
	})

	var acceptedConn net.Conn
	t.Cleanup(func() {
		if acceptedConn != nil {
			require.NoError(t, acceptedConn.Close())
		}
	})

	output, err := NewOutputFromURL(
		ctx,
		"rtmp://"+listener.Addr().String()+"/blocked-open",
		secret.New(""),
		OutputConfig{AsyncOpen: true},
	)
	require.NoError(t, err)

	acceptTimer := time.NewTimer(2 * time.Second)
	defer acceptTimer.Stop()
	select {
	case acceptedConn = <-connCh:
	case err := <-acceptErrCh:
		require.NoError(t, err)
	case <-acceptTimer.C:
		t.Fatal("timed out waiting for output OpenIOContext to reach the test listener")
	}

	require.NoError(t, output.Close(ctx))

	closeTimer := time.NewTimer(2 * time.Second)
	defer closeTimer.Stop()
	select {
	case <-output.openFinished:
	case <-closeTimer.C:
		t.Fatal("closing output did not interrupt the pending async OpenIOContext")
	}
	require.Error(t, output.openError)
}

func TestOutput_AsyncOpenSendWaitsForOpen(t *testing.T) {
	ctx := context.Background()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, listener.Close())
	})

	connCh := make(chan net.Conn, 1)
	acceptErrCh := make(chan error, 1)
	observability.Go(ctx, func(context.Context) {
		conn, err := listener.Accept()
		if err != nil {
			acceptErrCh <- err
			return
		}
		connCh <- conn
	})

	var acceptedConn net.Conn
	t.Cleanup(func() {
		if acceptedConn != nil {
			require.NoError(t, acceptedConn.Close())
		}
	})

	output, err := NewOutputFromURL(
		ctx,
		"rtmp://"+listener.Addr().String()+"/blocked-open",
		secret.New(""),
		OutputConfig{
			AsyncOpen: true,
			WaitForOutputStreams: &OutputConfigWaitForOutputStreams{
				MinStreamsVideo: 1,
				Timeout:         time.Second,
			},
		},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, output.Close(context.Background()))
	})

	acceptTimer := time.NewTimer(2 * time.Second)
	defer acceptTimer.Stop()
	select {
	case acceptedConn = <-connCh:
	case err := <-acceptErrCh:
		require.NoError(t, err)
	case <-acceptTimer.C:
		t.Fatal("timed out waiting for output OpenIOContext to reach the test listener")
	}

	src := &mockPacketSourceWithFormatCtx{fmtCtx: astiav.AllocFormatContext()}
	t.Cleanup(src.fmtCtx.Free)
	stream := src.fmtCtx.NewStream(astiav.FindEncoder(astiav.CodecIDH264))
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.CodecParameters().SetCodecID(astiav.CodecIDH264)
	stream.CodecParameters().SetWidth(1920)
	stream.CodecParameters().SetHeight(1080)
	stream.SetTimeBase(astiav.NewRational(1, 90000))

	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	require.NoError(t, pkt.AllocPayload(4))
	pkt.SetStreamIndex(stream.Index())
	pkt.SetPts(0)
	pkt.SetDts(0)
	pkt.SetFlags(astiav.NewPacketFlags(astiav.PacketFlagKey))

	inputPkt := packet.BuildInput(pkt, &packet.StreamInfo{
		Stream: stream,
		Source: src,
	})

	sendCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	err = output.SendInput(sendCtx, packetorframe.InputUnion{Packet: &inputPkt}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, output.headerSent, "SendInput must not write headers before AsyncOpen completes")
}
