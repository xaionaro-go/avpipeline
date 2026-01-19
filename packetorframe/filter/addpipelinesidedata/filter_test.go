// filter_test.go provides tests for the addpipelinesidedata filter.

package addpipelinesidedata

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

func TestAddSideData(t *testing.T) {
	ctx := context.Background()
	type CustomData struct {
		Value string
	}
	data := CustomData{Value: "test"}
	f := New(data)

	t.Run("packet", func(t *testing.T) {
		pkt := &packet.Input{
			StreamInfo: &packetorframetypes.StreamInfo{},
		}
		in := packetorframe.InputUnion{
			Packet: pkt,
		}

		require.True(t, f.Match(ctx, in))
		require.True(t, in.GetPipelineSideData().Contains(data))
	})

	t.Run("frame", func(t *testing.T) {
		fInput := &frame.Input{
			StreamInfo: &packetorframetypes.StreamInfo{},
		}
		in := packetorframe.InputUnion{
			Frame: fInput,
		}

		require.True(t, f.Match(ctx, in))
		require.True(t, in.GetPipelineSideData().Contains(data))
	})
}
