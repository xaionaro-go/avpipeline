package addpacketflags

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

func newTestPacket(t *testing.T) packet.Input {
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	si := &packet.StreamInfo{
		CodecParameters: astiav.AllocCodecParameters(),
	}
	return packet.BuildInput(pkt, si)
}

func TestFilter_AddsFlag(t *testing.T) {
	ctx := context.Background()
	f := New(astiav.PacketFlagKey, nil)
	p := newTestPacket(t)

	result := f.Match(ctx, p)
	assert.True(t, result)
	assert.True(t, p.Flags().Has(astiav.PacketFlagKey))
}

func TestFilter_ConditionFalse_NoFlag(t *testing.T) {
	ctx := context.Background()
	f := New(astiav.PacketFlagKey, condition.IsKeyFrame(true))
	p := newTestPacket(t) // not a key frame

	result := f.Match(ctx, p)
	assert.True(t, result)
	assert.False(t, p.Flags().Has(astiav.PacketFlagKey), "flag should not be added when condition is false")
}

func TestFilter_ConditionTrue_AddsFlag(t *testing.T) {
	ctx := context.Background()
	f := New(astiav.PacketFlagCorrupt, nil)
	p := newTestPacket(t)

	f.Match(ctx, p)
	assert.True(t, p.Flags().Has(astiav.PacketFlagCorrupt))
}

func TestFilter_FlagAlreadyPresent(t *testing.T) {
	ctx := context.Background()
	f := New(astiav.PacketFlagKey, nil)
	p := newTestPacket(t)
	p.SetFlags(astiav.PacketFlags(0).Add(astiav.PacketFlagKey))

	f.Match(ctx, p)
	assert.True(t, p.Flags().Has(astiav.PacketFlagKey))
}

func TestFilter_String(t *testing.T) {
	f := New(astiav.PacketFlagKey, nil)
	s := f.String()
	assert.Contains(t, s, "AddPacketFlags")
}

func TestFilter_String_WithCondition(t *testing.T) {
	f := New(astiav.PacketFlagKey, condition.IsKeyFrame(true))
	s := f.String()
	assert.Contains(t, s, "AddPacketFlags")
	assert.Contains(t, s, "IsKeyFrame")
}
