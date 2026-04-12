package libavnolibav

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	libav_proto "github.com/xaionaro-go/avpipeline/protobuf/libav"
)

// --- Linesize ---

func TestLinesizeFromProtobuf(t *testing.T) {
	input := []uint32{100, 200, 300, 400, 500, 600, 700, 800}
	ls, err := LinesizeFromProtobuf(input)
	require.NoError(t, err)
	assert.Equal(t, uint32(100), ls[0])
	assert.Equal(t, uint32(800), ls[7])
}

func TestLinesizeFromProtobuf_InvalidLength(t *testing.T) {
	_, err := LinesizeFromProtobuf([]uint32{1, 2, 3})
	assert.Error(t, err)
}

func TestLinesizeFromGo(t *testing.T) {
	input := [8]int{10, 20, 30, 40, 50, 60, 70, 80}
	ls := LinesizeFromGo(input)
	assert.Equal(t, uint32(10), ls[0])
	assert.Equal(t, uint32(80), ls[7])
}

func TestLinesize_Protobuf(t *testing.T) {
	ls := Linesize{1, 2, 3, 4, 5, 6, 7, 8}
	pb := ls.Protobuf()
	require.Len(t, pb, 8)
	assert.Equal(t, uint32(1), pb[0])
	assert.Equal(t, uint32(8), pb[7])
}

func TestLinesize_Go(t *testing.T) {
	ls := Linesize{10, 20, 30, 40, 50, 60, 70, 80}
	goArr := ls.Go()
	assert.Equal(t, 10, goArr[0])
	assert.Equal(t, 80, goArr[7])
}

func TestLinesize_RoundTrip_ProtobufToGo(t *testing.T) {
	input := []uint32{100, 200, 300, 400, 500, 600, 700, 800}
	ls, err := LinesizeFromProtobuf(input)
	require.NoError(t, err)
	output := ls.Protobuf()
	assert.Equal(t, input, output)
}

func TestLinesize_RoundTrip_GoToProtobuf(t *testing.T) {
	input := [8]int{10, 20, 30, 40, 50, 60, 70, 80}
	ls := LinesizeFromGo(input)
	output := ls.Go()
	assert.Equal(t, input, output)
}

// --- Rational ---

func TestRationalFromProtobuf(t *testing.T) {
	input := &libav_proto.Rational{N: 24000, D: 1001}
	r := RationalFromProtobuf(input)
	require.NotNil(t, r)
	pb := r.Protobuf()
	assert.EqualValues(t, 24000, pb.N)
	assert.EqualValues(t, 1001, pb.D)
}

func TestRationalFromProtobuf_Nil(t *testing.T) {
	r := RationalFromProtobuf(nil)
	assert.Nil(t, r)
}

func TestRational_Protobuf_Nil(t *testing.T) {
	var r *Rational
	assert.Nil(t, r.Protobuf())
}

// --- CodecParameters ---

func TestCodecParametersFromProtobuf(t *testing.T) {
	input := &libav_proto.CodecParameters{
		CodecId: 27,
	}
	cp := CodecParametersFromProtobuf(input)
	require.NotNil(t, cp)
	pb := cp.Protobuf()
	assert.EqualValues(t, 27, pb.CodecId)
}

func TestCodecParametersFromProtobuf_Nil(t *testing.T) {
	cp := CodecParametersFromProtobuf(nil)
	assert.Nil(t, cp)
}

// --- FrameSideData ---

func TestFrameSideDataFromProtobuf(t *testing.T) {
	input := &libav_proto.FrameSideData{
		Type: 1,
		Data: []byte{0x01, 0x02},
	}
	fsd := FrameSideDataFromProtobuf(input)
	require.NotNil(t, fsd)
	pb := fsd.Protobuf()
	assert.EqualValues(t, 1, pb.Type)
	assert.Equal(t, []byte{0x01, 0x02}, pb.Data)
}

func TestFrameSideDataFromProtobuf_Nil(t *testing.T) {
	fsd := FrameSideDataFromProtobuf(nil)
	assert.Nil(t, fsd)
}

// --- PacketSideData ---

func TestPacketSideDataFromProtobuf(t *testing.T) {
	input := &libav_proto.PacketSideData{
		Elements: []*libav_proto.PacketSideDataElement{
			{Type: 5, Data: []byte{0xAA}},
		},
	}
	psd := PacketSideDataFromProtobuf(input)
	require.NotNil(t, psd)
	pb := psd.Protobuf()
	require.Len(t, pb.Elements, 1)
	assert.EqualValues(t, 5, pb.Elements[0].Type)
}

func TestPacketSideDataFromProtobuf_Nil(t *testing.T) {
	psd := PacketSideDataFromProtobuf(nil)
	assert.Nil(t, psd)
}

// --- Stream ---

func TestStreamFromProtobuf(t *testing.T) {
	input := &libav_proto.Stream{
		Index: 1,
	}
	s := StreamFromProtobuf(input)
	require.NotNil(t, s)
	pb := s.Protobuf()
	assert.EqualValues(t, 1, pb.Index)
}

func TestStreamFromProtobuf_Nil(t *testing.T) {
	s := StreamFromProtobuf(nil)
	assert.Nil(t, s)
}

// --- ChannelLayout ---

func TestChannelLayoutFromProtobuf(t *testing.T) {
	input := &libav_proto.ChannelLayout{
		Order:      1,
		NbChannels: 2,
	}
	cl := ChannelLayoutFromProtobuf(input)
	require.NotNil(t, cl)
	pb := cl.Protobuf()
	assert.EqualValues(t, 1, pb.Order)
	assert.EqualValues(t, 2, pb.NbChannels)
}

func TestChannelLayoutFromProtobuf_Nil(t *testing.T) {
	cl := ChannelLayoutFromProtobuf(nil)
	assert.Nil(t, cl)
}

// --- Frame ---

func TestFrameFromProtobuf(t *testing.T) {
	input := &libav_proto.Frame{
		Width:  1920,
		Height: 1080,
	}
	f := FrameFromProtobuf(input)
	require.NotNil(t, f)
	pb := f.Protobuf()
	assert.EqualValues(t, 1920, pb.Width)
	assert.EqualValues(t, 1080, pb.Height)
}

func TestFrameFromProtobuf_Nil(t *testing.T) {
	f := FrameFromProtobuf(nil)
	assert.Nil(t, f)
}

// --- Packet ---

func TestPacketFromProtobuf(t *testing.T) {
	input := &libav_proto.Packet{
		Pts:         12345,
		StreamIndex: 1,
	}
	p := PacketFromProtobuf(input)
	require.NotNil(t, p)
	pb := p.Protobuf()
	assert.EqualValues(t, 12345, pb.Pts)
	assert.EqualValues(t, 1, pb.StreamIndex)
}

func TestPacketFromProtobuf_Nil(t *testing.T) {
	p := PacketFromProtobuf(nil)
	assert.Nil(t, p)
}

// --- FormatContext ---

func TestFormatContextFromProtobuf(t *testing.T) {
	input := &libav_proto.FormatContext{
		Streams: []*libav_proto.Stream{
			{Index: 0},
			{Index: 1},
		},
	}
	fc := FormatContextFromProtobuf(input)
	require.NotNil(t, fc)
	pb := fc.Protobuf()
	require.Len(t, pb.Streams, 2)
}

func TestFormatContextFromProtobuf_Nil(t *testing.T) {
	fc := FormatContextFromProtobuf(nil)
	assert.Nil(t, fc)
}
