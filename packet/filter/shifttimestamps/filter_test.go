package shifttimestamps

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packet/condition"
)

func newTestPacket(t *testing.T, pts, dts int64) packet.Input {
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	si := &packet.StreamInfo{
		CodecParameters: astiav.AllocCodecParameters(),
	}
	return packet.BuildInput(pkt, si)
}

func TestFilter_ShiftBoth(t *testing.T) {
	ctx := context.Background()
	f := New(100, nil)
	p := newTestPacket(t, 1000, 900)

	result := f.Match(ctx, p)
	assert.True(t, result, "always returns true")
	assert.Equal(t, int64(1100), p.GetPTS())
	assert.Equal(t, int64(1000), p.GetDTS())
}

func TestFilter_NegativeOffset(t *testing.T) {
	ctx := context.Background()
	f := New(-50, nil)
	p := newTestPacket(t, 200, 190)

	f.Match(ctx, p)
	assert.Equal(t, int64(150), p.GetPTS())
	assert.Equal(t, int64(140), p.GetDTS())
}

func TestFilter_NoPTSValue(t *testing.T) {
	ctx := context.Background()
	f := New(100, nil)
	p := newTestPacket(t, astiav.NoPtsValue, astiav.NoPtsValue)

	f.Match(ctx, p)
	assert.Equal(t, astiav.NoPtsValue, p.GetPTS(), "NoPTS should remain unchanged")
	assert.Equal(t, astiav.NoPtsValue, p.GetDTS(), "NoDTS should remain unchanged")
}

func TestFilter_ConditionFalse_NoShift(t *testing.T) {
	ctx := context.Background()
	cond := condition.IsKeyFrame(true)
	f := New(100, cond)

	// non-key packet, condition returns false → no shift
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(500)
	pkt.SetDts(490)
	si := &packet.StreamInfo{CodecParameters: astiav.AllocCodecParameters()}
	p := packet.BuildInput(pkt, si)

	result := f.Match(ctx, p)
	assert.True(t, result)
	assert.Equal(t, int64(500), p.GetPTS(), "should not shift when condition is false")
	assert.Equal(t, int64(490), p.GetDTS(), "should not shift when condition is false")
}

func TestFilter_ConditionTrue_Shifts(t *testing.T) {
	ctx := context.Background()
	cond := condition.IsKeyFrame(true)
	f := New(100, cond)

	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(500)
	pkt.SetDts(490)
	pkt.SetFlags(astiav.PacketFlags(0).Add(astiav.PacketFlagKey))
	si := &packet.StreamInfo{CodecParameters: astiav.AllocCodecParameters()}
	p := packet.BuildInput(pkt, si)

	result := f.Match(ctx, p)
	assert.True(t, result)
	assert.Equal(t, int64(600), p.GetPTS())
	assert.Equal(t, int64(590), p.GetDTS())
}

func TestFilter_ZeroOffset(t *testing.T) {
	ctx := context.Background()
	f := New(0, nil)
	p := newTestPacket(t, 500, 490)

	f.Match(ctx, p)
	assert.Equal(t, int64(500), p.GetPTS())
	assert.Equal(t, int64(490), p.GetDTS())
}

func TestFilter_String(t *testing.T) {
	f := New(100, nil)
	s := f.String()
	assert.Contains(t, s, "ShiftTimestamps")
}

func TestFilter_String_WithCondition(t *testing.T) {
	f := New(100, condition.IsKeyFrame(true))
	s := f.String()
	assert.Contains(t, s, "ShiftTimestamps")
	assert.Contains(t, s, "IsKeyFrame")
}

func TestFilter_OnlyPTSValid(t *testing.T) {
	ctx := context.Background()
	f := New(50, nil)

	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(200)
	pkt.SetDts(astiav.NoPtsValue)
	si := &packet.StreamInfo{CodecParameters: astiav.AllocCodecParameters()}
	p := packet.BuildInput(pkt, si)

	require.True(t, f.Match(ctx, p))
	assert.Equal(t, int64(250), p.GetPTS())
	assert.Equal(t, astiav.NoPtsValue, p.GetDTS())
}

func TestFilter_OnlyDTSValid(t *testing.T) {
	ctx := context.Background()
	f := New(50, nil)

	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(astiav.NoPtsValue)
	pkt.SetDts(200)
	si := &packet.StreamInfo{CodecParameters: astiav.AllocCodecParameters()}
	p := packet.BuildInput(pkt, si)

	require.True(t, f.Match(ctx, p))
	assert.Equal(t, astiav.NoPtsValue, p.GetPTS())
	assert.Equal(t, int64(250), p.GetDTS())
}
