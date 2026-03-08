package sort

import (
	gosort "sort"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// --- OrderedStreams ---

func TestOrderedStreams_Sort(t *testing.T) {
	streams := OrderedStreams{
		{Order: 3},
		{Order: 1},
		{Order: 2},
	}
	gosort.Sort(streams)
	assert.Equal(t, int64(1), streams[0].Order)
	assert.Equal(t, int64(2), streams[1].Order)
	assert.Equal(t, int64(3), streams[2].Order)
}

func TestOrderedStreams_Empty(t *testing.T) {
	streams := OrderedStreams{}
	gosort.Sort(streams)
	assert.Len(t, streams, 0)
}

func TestOrderedStreams_AlreadySorted(t *testing.T) {
	streams := OrderedStreams{
		{Order: 1},
		{Order: 2},
		{Order: 3},
	}
	gosort.Sort(streams)
	assert.Equal(t, int64(1), streams[0].Order)
	assert.Equal(t, int64(3), streams[2].Order)
}

func TestOrderedStreams_Reverse(t *testing.T) {
	streams := OrderedStreams{
		{Order: 100},
		{Order: 50},
		{Order: -10},
	}
	gosort.Sort(streams)
	assert.Equal(t, int64(-10), streams[0].Order)
	assert.Equal(t, int64(100), streams[2].Order)
}

// --- AbstractPacketOrFrames ---

func TestAbstractPacketOrFrames_Sort(t *testing.T) {
	pkt1 := astiav.AllocPacket()
	t.Cleanup(pkt1.Free)
	pkt1.SetPts(300)
	pkt2 := astiav.AllocPacket()
	t.Cleanup(pkt2.Free)
	pkt2.SetPts(100)
	pkt3 := astiav.AllocPacket()
	t.Cleanup(pkt3.Free)
	pkt3.SetPts(200)

	si := &packet.StreamInfo{CodecParameters: astiav.AllocCodecParameters()}
	p1 := packet.BuildInput(pkt1, si)
	p2 := packet.BuildInput(pkt2, si)
	p3 := packet.BuildInput(pkt3, si)
	items := AbstractPacketOrFrames{&p1, &p2, &p3}
	gosort.Sort(items)
	assert.Equal(t, int64(100), items[0].GetPTS())
	assert.Equal(t, int64(200), items[1].GetPTS())
	assert.Equal(t, int64(300), items[2].GetPTS())
}

// --- PacketOrFrames ---

func TestPacketOrFrames_Sort(t *testing.T) {
	pkt1 := astiav.AllocPacket()
	t.Cleanup(pkt1.Free)
	pkt1.SetPts(300)
	pkt2 := astiav.AllocPacket()
	t.Cleanup(pkt2.Free)
	pkt2.SetPts(100)
	pkt3 := astiav.AllocPacket()
	t.Cleanup(pkt3.Free)
	pkt3.SetPts(200)

	si := &packet.StreamInfo{CodecParameters: astiav.AllocCodecParameters()}
	items := PacketOrFrames[packet.Input, *packet.Input]{
		packet.BuildInput(pkt1, si),
		packet.BuildInput(pkt2, si),
		packet.BuildInput(pkt3, si),
	}
	gosort.Sort(items)
	assert.Equal(t, int64(100), (*packet.Input)(&items[0]).GetPTS())
	assert.Equal(t, int64(200), (*packet.Input)(&items[1]).GetPTS())
	assert.Equal(t, int64(300), (*packet.Input)(&items[2]).GetPTS())
}

// --- InputPacketOrFrameUnionsByPTS ---

func makePacketInput(t *testing.T, pts, dts int64) packetorframe.InputUnion {
	pkt := astiav.AllocPacket()
	t.Cleanup(pkt.Free)
	pkt.SetPts(pts)
	pkt.SetDts(dts)
	si := &packet.StreamInfo{
		CodecParameters: astiav.AllocCodecParameters(),
	}
	p := packet.BuildInput(pkt, si)
	return packetorframe.InputUnion{Packet: &p}
}

func TestInputPacketOrFrameUnionsByPTS_Sort(t *testing.T) {
	items := InputPacketOrFrameUnionsByPTS{
		makePacketInput(t, 300, 300),
		makePacketInput(t, 100, 100),
		makePacketInput(t, 200, 200),
	}
	gosort.Sort(items)
	assert.Equal(t, int64(100), items[0].GetPTS())
	assert.Equal(t, int64(200), items[1].GetPTS())
	assert.Equal(t, int64(300), items[2].GetPTS())
}

func TestInputPacketOrFrameUnionsByPTS_Empty(t *testing.T) {
	items := InputPacketOrFrameUnionsByPTS{}
	gosort.Sort(items)
	require.Len(t, items, 0)
}

// --- InputPacketOrFrameUnionsByDTS ---

func TestInputPacketOrFrameUnionsByDTS_Sort(t *testing.T) {
	items := InputPacketOrFrameUnionsByDTS{
		makePacketInput(t, 300, 350),
		makePacketInput(t, 100, 50),
		makePacketInput(t, 200, 250),
	}
	gosort.Sort(items)
	assert.Equal(t, int64(50), items[0].GetDTS())
	assert.Equal(t, int64(250), items[1].GetDTS())
	assert.Equal(t, int64(350), items[2].GetDTS())
}
