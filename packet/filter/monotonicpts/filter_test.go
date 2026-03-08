package monotonicpts

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/packet"
)

type mockSource struct {
	name string
}

func (m *mockSource) String() string { return m.name }
func (m *mockSource) WithOutputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {}

func newTestPacket(t *testing.T, pts, dts int64, streamIndex int, src packet.Source) packet.Input {
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	pkt.SetStreamIndex(streamIndex)
	si := &packet.StreamInfo{
		CodecParameters: astiav.AllocCodecParameters(),
		Source:          src,
		TimeBase:        astiav.NewRational(1, 1000),
	}
	return packet.BuildInput(pkt, si)
}

func TestFilter_MonotonicPTS_Increasing(t *testing.T) {
	ctx := context.Background()
	f := New(false)
	src := &mockSource{name: "src"}

	p1 := newTestPacket(t, 1000, 1000, 0, src)
	p2 := newTestPacket(t, 2000, 2000, 0, src)
	p3 := newTestPacket(t, 3000, 3000, 0, src)

	assert.True(t, f.Match(ctx, p1))
	assert.True(t, f.Match(ctx, p2))
	assert.True(t, f.Match(ctx, p3))
}

func TestFilter_MonotonicPTS_BackwardPTS_ShouldCorrectFalse(t *testing.T) {
	ctx := context.Background()
	f := New(false) // don't correct, just reject
	src := &mockSource{name: "src"}

	p1 := newTestPacket(t, 2000, 2000, 0, src)
	p2 := newTestPacket(t, 1000, 1000, 0, src) // backward

	assert.True(t, f.Match(ctx, p1))
	assert.False(t, f.Match(ctx, p2), "backward PTS should be rejected")
}

func TestFilter_MonotonicPTS_BackwardPTS_ShouldCorrectTrue(t *testing.T) {
	ctx := context.Background()
	f := New(true) // correct backward timestamps
	src := &mockSource{name: "src"}

	p1 := newTestPacket(t, 2000, 2000, 0, src)
	p2 := newTestPacket(t, 1000, 1000, 0, src) // backward

	assert.True(t, f.Match(ctx, p1))
	assert.True(t, f.Match(ctx, p2), "backward PTS should be corrected, not rejected")
	assert.True(t, p2.GetPTS() > 2000, "corrected PTS should be after latest")
}

func TestFilter_PTSLessThanDTS_Rejected(t *testing.T) {
	ctx := context.Background()
	f := New(false)
	src := &mockSource{name: "src"}

	p := newTestPacket(t, 100, 200, 0, src) // PTS < DTS
	assert.False(t, f.Match(ctx, p), "PTS < DTS should be rejected")
}

func TestFilter_NonFirstStream_Passes(t *testing.T) {
	ctx := context.Background()
	f := New(false)
	src := &mockSource{name: "src"}

	p0 := newTestPacket(t, 1000, 1000, 0, src)
	p1 := newTestPacket(t, 500, 500, 1, src)

	assert.True(t, f.Match(ctx, p0))
	assert.True(t, f.Match(ctx, p1), "non-first stream should pass without monotonic check")
}

func TestFilter_String(t *testing.T) {
	f := New(false)
	s := f.String()
	assert.Contains(t, s, "MonotonicPTS")
}
