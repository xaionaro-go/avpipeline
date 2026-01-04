// output_test.go contains tests for the output kernel.

package kernel

import (
	"context"
	"errors"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
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
