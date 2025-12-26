package kernel

import (
	"context"
	"errors"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
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
// 1. Output.SendInputPacket calls Source.WithOutputFormatContext
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

		err = output.SendInputPacket(ctx, inputPkt, nil, nil)
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

		err = output.SendInputPacket(ctx, inputPkt, nil, nil)
		require.NoError(t, err)
	})
}
