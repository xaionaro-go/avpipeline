package condition

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	mathcondition "github.com/xaionaro-go/avpipeline/math/condition"
	"github.com/xaionaro-go/avpipeline/packet"
)

// --- helpers ---

func newPacketWithFlags(t *testing.T, flags astiav.PacketFlags) packet.Input {
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetFlags(flags)

	cp := astiav.AllocCodecParameters()
	cp.SetCodecID(astiav.CodecIDH264)
	si := &packet.StreamInfo{
		CodecParameters: cp,
	}
	return packet.BuildInput(pkt, si)
}

func newPacketWithData(t *testing.T, data []byte) packet.Input {
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	if len(data) > 0 {
		err := pkt.FromData(data)
		require.NoError(t, err)
	}

	si := &packet.StreamInfo{
		CodecParameters: astiav.AllocCodecParameters(),
	}
	return packet.BuildInput(pkt, si)
}

// newMultiStreamCtx creates a format context with numStreams streams and
// returns the format context along with its streams.
func newMultiStreamCtx(t *testing.T, numStreams int) (*astiav.FormatContext, []*astiav.Stream) {
	fmtCtx := astiav.AllocFormatContext()
	t.Cleanup(fmtCtx.Free)

	encoder := astiav.FindEncoderByName("libx264")
	if encoder == nil {
		encoder = astiav.FindEncoderByName("mpeg4")
	}
	require.NotNil(t, encoder, "need at least one video encoder")

	var streams []*astiav.Stream
	for range numStreams {
		stream := fmtCtx.NewStream(encoder)
		require.NotNil(t, stream)
		streams = append(streams, stream)
	}
	return fmtCtx, streams
}

func newPacketWithStreamIndex(t *testing.T, streamIndex int) packet.Input {
	// Create enough streams so that the requested index exists
	_, streams := newMultiStreamCtx(t, streamIndex+1)
	stream := streams[streamIndex]

	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetStreamIndex(streamIndex)

	si := &packet.StreamInfo{
		Stream:          stream,
		CodecParameters: astiav.AllocCodecParameters(),
		StreamIndex:     streamIndex,
	}
	return packet.BuildInput(pkt, si)
}

// --- IsKeyFrame ---

func TestIsKeyFrame_Match_True(t *testing.T) {
	ctx := context.Background()
	cond := IsKeyFrame(true)
	p := newPacketWithFlags(t, astiav.PacketFlags(0).Add(astiav.PacketFlagKey))
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestIsKeyFrame_Match_False(t *testing.T) {
	ctx := context.Background()
	cond := IsKeyFrame(true)
	p := newPacketWithFlags(t, 0)
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestIsKeyFrame_Inverted(t *testing.T) {
	ctx := context.Background()
	cond := IsKeyFrame(false)
	p := newPacketWithFlags(t, astiav.PacketFlags(0).Add(astiav.PacketFlagKey))
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestIsKeyFrame_String(t *testing.T) {
	testifyassert.Equal(t, "IsKeyFrame(true)", IsKeyFrame(true).String())
	testifyassert.Equal(t, "IsKeyFrame(false)", IsKeyFrame(false).String())
}

// --- DataHasPrefix ---

func TestDataHasPrefix_Match(t *testing.T) {
	ctx := context.Background()
	cond := DataHasPrefix([]byte{0x00, 0x00, 0x01})
	p := newPacketWithData(t, []byte{0x00, 0x00, 0x01, 0xFF})
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestDataHasPrefix_NoMatch(t *testing.T) {
	ctx := context.Background()
	cond := DataHasPrefix([]byte{0xFF})
	p := newPacketWithData(t, []byte{0x00, 0x01})
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestDataHasPrefix_String(t *testing.T) {
	testifyassert.Equal(t, "DataHasPrefix(00FF)", DataHasPrefix([]byte{0x00, 0xFF}).String())
}

// --- DataHasSuffix ---

func TestDataHasSuffix_Match(t *testing.T) {
	ctx := context.Background()
	cond := DataHasSuffix([]byte{0xFE, 0xFF})
	p := newPacketWithData(t, []byte{0x00, 0xFE, 0xFF})
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestDataHasSuffix_NoMatch(t *testing.T) {
	ctx := context.Background()
	cond := DataHasSuffix([]byte{0x00})
	p := newPacketWithData(t, []byte{0x00, 0x01})
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestDataHasSuffix_String(t *testing.T) {
	testifyassert.Equal(t, "DataHasSuffix(FF)", DataHasSuffix([]byte{0xFF}).String())
}

// --- AtomicBool ---

func TestAtomicBool_True(t *testing.T) {
	ctx := context.Background()
	b := &atomic.Bool{}
	b.Store(true)
	cond := AtomicBool(b)
	p := newPacketWithFlags(t, 0)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestAtomicBool_False(t *testing.T) {
	ctx := context.Background()
	b := &atomic.Bool{}
	cond := AtomicBool(b)
	p := newPacketWithFlags(t, 0)
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestAtomicBool_Toggle(t *testing.T) {
	ctx := context.Background()
	b := &atomic.Bool{}
	cond := AtomicBool(b)
	p := newPacketWithFlags(t, 0)
	testifyassert.False(t, cond.Match(ctx, p))
	b.Store(true)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestAtomicBool_String(t *testing.T) {
	b := &atomic.Bool{}
	cond := AtomicBool(b)
	testifyassert.Equal(t, "AtomicBool(false)", cond.String())
	b.Store(true)
	testifyassert.Equal(t, "AtomicBool(true)", cond.String())
}

// --- HasPacketFlags ---

func TestHasPacketFlags_Match(t *testing.T) {
	ctx := context.Background()
	cond := HasPacketFlags(astiav.PacketFlagKey)
	p := newPacketWithFlags(t, astiav.PacketFlags(0).Add(astiav.PacketFlagKey))
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestHasPacketFlags_NoMatch(t *testing.T) {
	ctx := context.Background()
	cond := HasPacketFlags(astiav.PacketFlagKey)
	p := newPacketWithFlags(t, 0)
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestHasPacketFlags_NilPacket(t *testing.T) {
	ctx := context.Background()
	cond := HasPacketFlags(astiav.PacketFlagKey)
	p := packet.BuildInput(nil, &packet.StreamInfo{})
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestHasPacketFlags_String(t *testing.T) {
	cond := HasPacketFlags(astiav.PacketFlagKey)
	s := cond.String()
	testifyassert.Contains(t, s, "HasPacketFlags")
}

// --- MediaType ---

func TestMediaType_Match(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeVideo)
	si := &packet.StreamInfo{CodecParameters: cp}
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	p := packet.BuildInput(pkt, si)

	cond := MediaType(astiav.MediaTypeVideo)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestMediaType_NoMatch(t *testing.T) {
	ctx := context.Background()
	cp := astiav.AllocCodecParameters()
	cp.SetMediaType(astiav.MediaTypeAudio)
	si := &packet.StreamInfo{CodecParameters: cp}
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	p := packet.BuildInput(pkt, si)

	cond := MediaType(astiav.MediaTypeVideo)
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestMediaType_String(t *testing.T) {
	cond := MediaType(astiav.MediaTypeVideo)
	testifyassert.Contains(t, cond.String(), "video")
}

// --- StreamIndex ---

func TestStreamIndex_Equal(t *testing.T) {
	ctx := context.Background()
	cond := StreamIndex(mathcondition.GreaterOrEqual[int](0))
	p := newPacketWithStreamIndex(t, 0)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestStreamIndex_NoMatch(t *testing.T) {
	ctx := context.Background()
	cond := StreamIndex(mathcondition.GreaterOrEqual[int](5))
	p := newPacketWithStreamIndex(t, 2)
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestStreamIndex_String(t *testing.T) {
	cond := StreamIndex(mathcondition.GreaterOrEqual[int](3))
	testifyassert.Contains(t, cond.String(), "StreamIndex")
}

// --- Source ---

type mockSource struct {
	name string
}

func (m *mockSource) String() string { return m.name }
func (m *mockSource) WithOutputFormatContext(_ context.Context, _ func(*astiav.FormatContext)) {
}

func TestSource_Match(t *testing.T) {
	ctx := context.Background()
	src := &mockSource{name: "src1"}
	cond := &Source{Source: src}
	si := &packet.StreamInfo{Source: src}
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	p := packet.BuildInput(pkt, si)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestSource_NoMatch(t *testing.T) {
	ctx := context.Background()
	src1 := &mockSource{name: "src1"}
	src2 := &mockSource{name: "src2"}
	cond := &Source{Source: src1}
	si := &packet.StreamInfo{Source: src2}
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	p := packet.BuildInput(pkt, si)
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestSource_String(t *testing.T) {
	src := &mockSource{name: "test-src"}
	cond := &Source{Source: src}
	testifyassert.Equal(t, "SourceIs(test-src)", cond.String())
}

// --- Until ---

func TestUntil_MatchesUntilCondMet(t *testing.T) {
	ctx := context.Background()
	// Until IsKeyFrame(true): pass non-key frames, stop at first key frame
	cond := NewUntil(IsKeyFrame(true))
	nonKey := newPacketWithFlags(t, 0)
	key := newPacketWithFlags(t, astiav.PacketFlags(0).Add(astiav.PacketFlagKey))

	testifyassert.True(t, cond.Match(ctx, nonKey), "should pass non-key frame")
	testifyassert.True(t, cond.Match(ctx, nonKey), "should pass another non-key frame")
	testifyassert.False(t, cond.Match(ctx, key), "should stop at key frame")
	testifyassert.False(t, cond.Match(ctx, nonKey), "should stay false after condition met")
}

func TestUntil_String(t *testing.T) {
	cond := NewUntil(IsKeyFrame(true))
	testifyassert.Equal(t, "Until(IsKeyFrame(true))", cond.String())
}

// --- Switch ---

func TestSwitch_BasicSetGet(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.NextValue.Store(math.MinInt32)

	testifyassert.Equal(t, int32(0), sw.GetValue(ctx))
	require.NoError(t, sw.SetValue(ctx, 1))
	testifyassert.Equal(t, int32(1), sw.GetValue(ctx))
}

func TestSwitch_SetValueWithKeepUnless(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.NextValue.Store(math.MinInt32)

	// KeepUnless delays the actual switch
	sw.SetKeepUnless(IsKeyFrame(true))
	require.NoError(t, sw.SetValue(ctx, 1))
	// current should stay 0 because KeepUnless is set
	testifyassert.Equal(t, int32(0), sw.GetValue(ctx))
	// NextValue should be set
	testifyassert.Equal(t, int32(1), sw.NextValue.Load())
}

func TestSwitch_GetSetKeepUnless(t *testing.T) {
	sw := NewSwitch()
	testifyassert.Nil(t, sw.GetKeepUnless())
	sw.SetKeepUnless(IsKeyFrame(true))
	testifyassert.NotNil(t, sw.GetKeepUnless())
}

func TestSwitch_GetSetOnAfterSwitch(t *testing.T) {
	sw := NewSwitch()
	testifyassert.Nil(t, sw.GetOnAfterSwitch())
	called := false
	sw.SetOnAfterSwitch(func(_ context.Context, _ packet.Input, _, _ int32) {
		called = true
	})
	testifyassert.NotNil(t, sw.GetOnAfterSwitch())
	// call it to verify
	fn := sw.GetOnAfterSwitch()
	fn(context.Background(), packet.Input{}, 0, 1)
	testifyassert.True(t, called)
}

// --- SwitchPacketCondition ---

func TestSwitchPacketCondition_NoNextValue(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(1)
	sw.NextValue.Store(math.MinInt32)

	cond := sw.PacketCondition(1)
	p := newPacketWithFlags(t, 0)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestSwitchPacketCondition_DifferentRequiredValue(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.NextValue.Store(math.MinInt32)

	cond := sw.PacketCondition(1)
	p := newPacketWithFlags(t, 0)
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestSwitchPacketCondition_CommitOnKeyFrame(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.NextValue.Store(math.MinInt32)

	sw.SetKeepUnless(IsKeyFrame(true))
	require.NoError(t, sw.SetValue(ctx, 1))

	cond := sw.PacketCondition(1)
	nonKey := newPacketWithFlags(t, 0)
	key := newPacketWithFlags(t, astiav.PacketFlags(0).Add(astiav.PacketFlagKey))

	// non-key: KeepUnless returns false, so no switch
	testifyassert.False(t, cond.Match(ctx, nonKey))
	testifyassert.Equal(t, int32(0), sw.CurrentValue.Load())

	// key: KeepUnless returns true, commits to next value
	testifyassert.False(t, cond.Match(ctx, key))
	testifyassert.Equal(t, int32(1), sw.CurrentValue.Load())

	// now condition should match (current == required)
	sw.NextValue.Store(math.MinInt32)
	testifyassert.True(t, cond.Match(ctx, nonKey))
}

func TestSwitchPacketCondition_OnAfterSwitchCalled(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(0)
	sw.NextValue.Store(1)

	var calledFrom, calledTo int32
	sw.SetOnAfterSwitch(func(_ context.Context, _ packet.Input, from, to int32) {
		calledFrom = from
		calledTo = to
	})

	cond := sw.PacketCondition(0)
	p := newPacketWithFlags(t, 0)
	cond.Match(ctx, p)

	testifyassert.Equal(t, int32(0), calledFrom)
	testifyassert.Equal(t, int32(1), calledTo)
}

func TestSwitchPacketCondition_SameNextAsCurrent(t *testing.T) {
	ctx := context.Background()
	sw := NewSwitch()
	sw.CurrentValue.Store(1)
	sw.NextValue.Store(1)

	cond := sw.PacketCondition(1)
	p := newPacketWithFlags(t, 0)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestSwitchPacketCondition_String(t *testing.T) {
	sw := NewSwitch()
	sw.CurrentValue.Store(2)
	cond := sw.PacketCondition(2)
	s := cond.String()
	testifyassert.Contains(t, s, "SwitchCondition")
	testifyassert.Contains(t, s, "true")
}

// --- SeenStreamsCount ---

func TestSeenStreamsCount_Basic(t *testing.T) {
	ctx := context.Background()
	cond := SeenStreamCount(mathcondition.GreaterOrEqual[uint](2))

	p0 := newPacketWithStreamIndex(t, 0)
	p1 := newPacketWithStreamIndex(t, 1)

	testifyassert.False(t, cond.Match(ctx, p0), "only 1 stream seen")
	testifyassert.True(t, cond.Match(ctx, p1), "2 streams seen")
	testifyassert.True(t, cond.Match(ctx, p0), "still 2 streams seen")
}

func TestSeenStreamsCount_DuplicateStream(t *testing.T) {
	ctx := context.Background()
	cond := SeenStreamCount(mathcondition.GreaterOrEqual[uint](2))

	p0 := newPacketWithStreamIndex(t, 0)
	testifyassert.False(t, cond.Match(ctx, p0))
	testifyassert.False(t, cond.Match(ctx, p0), "duplicate stream shouldn't increase count")
}

func TestSeenStreamsCount_String(t *testing.T) {
	cond := SeenStreamCount(mathcondition.GreaterOrEqual[uint](2))
	testifyassert.Contains(t, cond.String(), "StreamCountIs")
}

// --- SeenAllStreams ---

type mockSourceWithFmtCtx struct {
	name   string
	fmtCtx *astiav.FormatContext
}

func (m *mockSourceWithFmtCtx) String() string { return m.name }
func (m *mockSourceWithFmtCtx) WithOutputFormatContext(_ context.Context, cb func(*astiav.FormatContext)) {
	cb(m.fmtCtx)
}

func TestSeenAllStreams_Basic(t *testing.T) {
	ctx := context.Background()
	cond := SeenAllStreams()

	fmtCtx, streams := newMultiStreamCtx(t, 2)
	src := &mockSourceWithFmtCtx{name: "src", fmtCtx: fmtCtx}

	makePacket := func(stream *astiav.Stream) packet.Input {
		pkt := astiav.AllocPacket()
		t.Cleanup(pkt.Free)
		pkt.SetStreamIndex(stream.Index())
		si := &packet.StreamInfo{
			Stream: stream,
			Source: src,
		}
		return packet.BuildInput(pkt, si)
	}

	p0 := makePacket(streams[0])
	p1 := makePacket(streams[1])

	testifyassert.False(t, cond.Match(ctx, p0), "only 1 of 2 streams seen")
	testifyassert.True(t, cond.Match(ctx, p1), "both streams seen")
	testifyassert.True(t, cond.Match(ctx, p0), "still both streams seen")
}

func TestSeenAllStreams_String(t *testing.T) {
	cond := SeenAllStreams()
	testifyassert.Equal(t, "StreamAllStreams", cond.String())
}

// --- ContainsFiller additional ---

func TestContainsFiller_NilCodecParams(t *testing.T) {
	ctx := context.Background()
	cond := ContainsFiller(true)
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	si := &packet.StreamInfo{}
	p := packet.BuildInput(pkt, si)
	// nil codec params → containsFiller returns false → cond(true) != false → false
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestContainsFiller_EmptyData(t *testing.T) {
	ctx := context.Background()
	cond := ContainsFiller(true)
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	cp := astiav.AllocCodecParameters()
	cp.SetCodecID(astiav.CodecIDH264)
	si := &packet.StreamInfo{CodecParameters: cp}
	p := packet.BuildInput(pkt, si)
	// empty data → containsFiller returns false
	testifyassert.False(t, cond.Match(ctx, p))
}

func TestContainsFiller_Inverted(t *testing.T) {
	ctx := context.Background()
	cond := ContainsFiller(false)
	// non-filler data → containsFiller=false → false==false → true
	data := []byte{0, 0, 0, 1, 1, 5, 6, 7}
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	err := pkt.FromData(data)
	require.NoError(t, err)
	cp := astiav.AllocCodecParameters()
	cp.SetCodecID(astiav.CodecIDH264)
	si := &packet.StreamInfo{CodecParameters: cp}
	p := packet.BuildInput(pkt, si)
	testifyassert.True(t, cond.Match(ctx, p))
}

func TestContainsFiller_String(t *testing.T) {
	testifyassert.Equal(t, "ContainsFiller(true)", ContainsFiller(true).String())
	testifyassert.Equal(t, "ContainsFiller(false)", ContainsFiller(false).String())
}
