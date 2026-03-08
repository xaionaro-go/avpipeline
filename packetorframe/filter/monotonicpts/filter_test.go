package monotonicpts

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type mockSource struct {
	name string
}

func (m *mockSource) String() string { return m.name }
func (m *mockSource) WithOutputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {}

func makeInput(t *testing.T, pts, dts int64, streamIdx int, src packet.Source) packetorframe.InputUnion {
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	pkt.SetStreamIndex(streamIdx)
	si := &packet.StreamInfo{
		CodecParameters: astiav.AllocCodecParameters(),
		Source:          src,
		TimeBase:        astiav.NewRational(1, 1000),
	}
	p := packet.BuildInput(pkt, si)
	return packetorframe.InputUnion{Packet: &p}
}

func TestFilter_IncreasingPTS(t *testing.T) {
	ctx := context.Background()
	f := New(false)
	src := &mockSource{name: "src"}

	in1 := makeInput(t, 1000, 1000, 0, src)
	in2 := makeInput(t, 2000, 2000, 0, src)

	assert.True(t, f.Match(ctx, in1))
	assert.True(t, f.Match(ctx, in2))
}

func TestFilter_BackwardPTS_RejectWhenNotCorrecting(t *testing.T) {
	ctx := context.Background()
	f := New(false)
	src := &mockSource{name: "src"}

	in1 := makeInput(t, 2000, 2000, 0, src)
	in2 := makeInput(t, 1000, 1000, 0, src)

	assert.True(t, f.Match(ctx, in1))
	assert.False(t, f.Match(ctx, in2))
}

func TestFilter_BackwardPTS_CorrectWhenEnabled(t *testing.T) {
	ctx := context.Background()
	f := New(true)
	src := &mockSource{name: "src"}

	in1 := makeInput(t, 2000, 2000, 0, src)
	in2 := makeInput(t, 1000, 1000, 0, src)

	assert.True(t, f.Match(ctx, in1))
	assert.True(t, f.Match(ctx, in2))
}

func TestFilter_PTSLessThanDTS(t *testing.T) {
	ctx := context.Background()
	f := New(false)
	src := &mockSource{name: "src"}

	in := makeInput(t, 100, 200, 0, src) // PTS < DTS
	assert.False(t, f.Match(ctx, in))
}

func TestFilter_NonFirstStream(t *testing.T) {
	ctx := context.Background()
	f := New(false)
	src := &mockSource{name: "src"}

	in1 := makeInput(t, 1000, 1000, 0, src) // first stream, sets baseline
	in2 := makeInput(t, 500, 500, 1, src)    // non-first, passes without check

	assert.True(t, f.Match(ctx, in1))
	assert.True(t, f.Match(ctx, in2))
}

func TestFilter_ShouldCorrect_AppliesShift(t *testing.T) {
	ctx := context.Background()
	f := New(true)
	src := &mockSource{name: "src"}

	// First packet sets the baseline at PTS=2000
	in1 := makeInput(t, 2000, 2000, 0, src)
	assert.True(t, f.Match(ctx, in1))

	// Second packet has backward PTS=1000, gets corrected and shift stored
	in2 := makeInput(t, 1000, 1000, 0, src)
	assert.True(t, f.Match(ctx, in2))

	// Third packet also from same source, should have the shift applied
	in3 := makeInput(t, 1500, 1500, 0, src)
	assert.True(t, f.Match(ctx, in3))
	// PTS should be shifted forward by the stored shift amount
	assert.True(t, in3.GetPTS() > 1500, "PTS should be shifted forward")
}

func TestFilter_String(t *testing.T) {
	f := New(false)
	s := f.String()
	assert.Contains(t, s, "MonotonicPTS")
}
