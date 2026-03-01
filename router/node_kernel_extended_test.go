package router

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

// mockPacketSource implements packet.Source for testing.
type mockPacketSource struct {
	name string
	fc   *astiav.FormatContext
}

func newMockPacketSource(name string) *mockPacketSource {
	return &mockPacketSource{
		name: name,
		fc:   astiav.AllocFormatContext(),
	}
}

func (s *mockPacketSource) String() string {
	return s.name
}

func (s *mockPacketSource) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	callback(s.fc)
}

// Verify mock implements packet.Source.
var _ packet.Source = (*mockPacketSource)(nil)

func TestErrSkip_Error(t *testing.T) {
	err := errSkip{}
	assert.Equal(t, "skip", err.Error())
}

func TestNodeKernel_SendInput_Packet(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	source := newMockPacketSource("test-source")

	// Create a proper format context with a stream so codec params are available.
	fmtCtx := astiav.AllocFormatContext()
	stream := fmtCtx.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	pkt.SetStreamIndex(0)
	pkt.SetPts(100)
	pkt.SetDts(100)

	streamInfo := &packet.StreamInfo{
		Stream:     stream,
		Source:     source,
		TimeBase:   astiav.NewRational(1, 90000),
	}
	input := packet.BuildInput(pkt, streamInfo)
	outputCh := make(chan packetorframe.OutputUnion, 10)

	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, outputCh)
	require.NoError(t, err)

	// An audio packet should pass through (makeTimeMoveOnlyForward skips non-video).
	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Packet, "should receive a packet output")
	case <-time.After(2 * time.Second):
		t.Fatal("expected output but got none")
	}
}

func TestNodeKernel_SendInput_EmptyInput(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	outputCh := make(chan packetorframe.OutputUnion, 10)

	// Neither Packet nor Frame set => should return ErrUnexpectedInputType.
	err = k.SendInput(ctx, packetorframe.InputUnion{}, outputCh)
	assert.Error(t, err)
	assert.ErrorIs(t, err, kerneltypes.ErrUnexpectedInputType{})
}

func TestNodeKernel_SendInput_Frame(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeAudio)

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: codecParams,
		StreamIndex:     0,
		TimeBase:        astiav.NewRational(1, 44100),
	}

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetPts(100)

	input := frame.BuildInput(f, 0, streamInfo)
	outputCh := make(chan packetorframe.OutputUnion, 10)

	err = k.SendInput(ctx, packetorframe.InputUnion{Frame: &input}, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Frame, "should receive a frame output")
	case <-time.After(2 * time.Second):
		t.Fatal("expected output but got none")
	}
}

func TestNodeKernel_SendInput_VideoPacket_NewSourceWithFixPTS(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	source := newMockPacketSource("video-source")

	fmtCtx := astiav.AllocFormatContext()
	stream := fmtCtx.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	pkt.SetStreamIndex(0)
	pkt.SetPts(100)
	pkt.SetDts(100)

	streamInfo := &packet.StreamInfo{
		Stream:     stream,
		Source:     source,
		TimeBase:   astiav.NewRational(1, 90000),
	}
	input := packet.BuildInput(pkt, streamInfo)
	outputCh := make(chan packetorframe.OutputUnion, 10)

	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Packet)
	case <-time.After(2 * time.Second):
		t.Fatal("expected output but got none")
	}
}

func TestNodeKernel_SendInput_VideoPacket_SourceSwitch(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	source1 := newMockPacketSource("source1")
	source2 := newMockPacketSource("source2")

	fmtCtx := astiav.AllocFormatContext()
	stream := fmtCtx.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	outputCh := make(chan packetorframe.OutputUnion, 20)

	// Send first packet from source1.
	pkt1 := astiav.AllocPacket()
	defer pkt1.Free()
	pkt1.SetStreamIndex(0)
	pkt1.SetPts(100)
	pkt1.SetDts(100)

	streamInfo1 := &packet.StreamInfo{
		Stream:   stream,
		Source:   source1,
		TimeBase: astiav.NewRational(1, 90000),
	}
	input1 := packet.BuildInput(pkt1, streamInfo1)
	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input1}, outputCh)
	require.NoError(t, err)
	<-outputCh // drain

	// Send second packet from source2 (source switch => new time shift).
	pkt2 := astiav.AllocPacket()
	defer pkt2.Free()
	pkt2.SetStreamIndex(0)
	pkt2.SetPts(50)
	pkt2.SetDts(50)

	streamInfo2 := &packet.StreamInfo{
		Stream:   stream,
		Source:   source2,
		TimeBase: astiav.NewRational(1, 90000),
	}
	input2 := packet.BuildInput(pkt2, streamInfo2)
	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input2}, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Packet)
	case <-time.After(2 * time.Second):
		t.Fatal("expected output but got none")
	}
}

func TestNodeKernel_SendInput_VideoPacket_DTSGreaterThanPTS(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	source := newMockPacketSource("dts-source")

	fmtCtx := astiav.AllocFormatContext()
	stream := fmtCtx.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	pkt.SetStreamIndex(0)
	pkt.SetPts(100)
	pkt.SetDts(200) // DTS > PTS, should be fixed

	streamInfo := &packet.StreamInfo{
		Stream:   stream,
		Source:   source,
		TimeBase: astiav.NewRational(1, 90000),
	}
	input := packet.BuildInput(pkt, streamInfo)
	outputCh := make(chan packetorframe.OutputUnion, 10)

	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input}, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Packet)
	case <-time.After(2 * time.Second):
		t.Fatal("expected output but got none")
	}
}

func TestNodeKernel_SendInput_VideoPacket_WithoutFixPTS_SkipsOnNewTimeShift(t *testing.T) {
	ctx := context.Background()
	// ShouldFixPTS = false => errSkip when new time shift is needed
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(false))
	require.NoError(t, err)

	source1 := newMockPacketSource("s1")
	source2 := newMockPacketSource("s2")

	fmtCtx := astiav.AllocFormatContext()
	stream := fmtCtx.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	outputCh := make(chan packetorframe.OutputUnion, 20)

	// First packet from source1 at PTS=1000.
	pkt1 := astiav.AllocPacket()
	defer pkt1.Free()
	pkt1.SetStreamIndex(0)
	pkt1.SetPts(1000)
	pkt1.SetDts(1000)

	streamInfo1 := &packet.StreamInfo{
		Stream:   stream,
		Source:   source1,
		TimeBase: astiav.NewRational(1, 90000),
	}
	input1 := packet.BuildInput(pkt1, streamInfo1)
	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input1}, outputCh)
	require.NoError(t, err)
	<-outputCh // drain

	// Second packet from source2 at PTS=50 (source switch), and ShouldFixPTS=false.
	// The new time shift is needed (PTS goes backward), should return errSkip internally
	// and return nil (skip).
	pkt2 := astiav.AllocPacket()
	defer pkt2.Free()
	pkt2.SetStreamIndex(0)
	pkt2.SetPts(50)
	pkt2.SetDts(50)

	streamInfo2 := &packet.StreamInfo{
		Stream:   stream,
		Source:   source2,
		TimeBase: astiav.NewRational(1, 90000),
	}
	input2 := packet.BuildInput(pkt2, streamInfo2)
	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input2}, outputCh)
	require.NoError(t, err)

	// Should be skipped, no output.
	select {
	case <-outputCh:
		t.Fatal("expected packet to be skipped, but got output")
	case <-time.After(100 * time.Millisecond):
		// expected: no output
	}
}

func TestNodeKernel_SendInput_VideoPacket_ExistingSourceNotNewTimeShift(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	source := newMockPacketSource("video-src")

	fmtCtx := astiav.AllocFormatContext()
	stream := fmtCtx.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	outputCh := make(chan packetorframe.OutputUnion, 20)

	// First packet.
	pkt1 := astiav.AllocPacket()
	defer pkt1.Free()
	pkt1.SetStreamIndex(0)
	pkt1.SetPts(100)
	pkt1.SetDts(100)

	streamInfo := &packet.StreamInfo{
		Stream:   stream,
		Source:   source,
		TimeBase: astiav.NewRational(1, 90000),
	}
	input1 := packet.BuildInput(pkt1, streamInfo)
	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input1}, outputCh)
	require.NoError(t, err)
	<-outputCh

	// Second packet from the same source (not new), increasing PTS.
	pkt2 := astiav.AllocPacket()
	defer pkt2.Free()
	pkt2.SetStreamIndex(0)
	pkt2.SetPts(200)
	pkt2.SetDts(200)

	input2 := packet.BuildInput(pkt2, streamInfo)
	err = k.SendInput(ctx, packetorframe.InputUnion{Packet: &input2}, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Packet)
	case <-time.After(2 * time.Second):
		t.Fatal("expected output but got none")
	}
}

func TestNodeKernel_SendInput_ContextCancelled(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	source := newMockPacketSource("src")

	fmtCtx := astiav.AllocFormatContext()
	stream := fmtCtx.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	stream.SetTimeBase(astiav.NewRational(1, 44100))
	stream.SetIndex(0)

	pkt := astiav.AllocPacket()
	defer pkt.Free()
	pkt.SetStreamIndex(0)
	pkt.SetPts(100)
	pkt.SetDts(100)

	streamInfo := &packet.StreamInfo{
		Stream:   stream,
		Source:   source,
		TimeBase: astiav.NewRational(1, 44100),
	}
	input := packet.BuildInput(pkt, streamInfo)

	// Use an unbuffered output channel and a cancelled context.
	outputCh := make(chan packetorframe.OutputUnion) // blocks on send
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // cancel immediately

	err = k.SendInput(cancelCtx, packetorframe.InputUnion{Packet: &input}, outputCh)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNodeKernel_NotifyAboutPacketSource(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	source := newMockPacketSource("notify-source")

	// Add a stream to the source's format context.
	stream := source.fc.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.CodecParameters().SetCodecID(astiav.CodecIDH264)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	err = k.NotifyAboutPacketSource(ctx, source)
	assert.NoError(t, err)

	// Verify output stream was created.
	assert.NotNil(t, k.OutputStreams[0], "output stream should have been created for index 0")
}

func TestNodeKernel_NotifyAboutPacketSource_MultipleStreams(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	source := newMockPacketSource("multi-stream-source")

	// Add two streams.
	stream0 := source.fc.NewStream(nil)
	stream0.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream0.SetTimeBase(astiav.NewRational(1, 90000))
	stream0.SetIndex(0)

	stream1 := source.fc.NewStream(nil)
	stream1.CodecParameters().SetMediaType(astiav.MediaTypeAudio)
	stream1.SetTimeBase(astiav.NewRational(1, 44100))
	stream1.SetIndex(1)

	err = k.NotifyAboutPacketSource(ctx, source)
	assert.NoError(t, err)

	assert.NotNil(t, k.OutputStreams[0])
	assert.NotNil(t, k.OutputStreams[1])
}

func TestNodeKernel_NotifyAboutPacketSource_CalledTwice_ReusesStreams(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	source := newMockPacketSource("reuse-source")

	stream := source.fc.NewStream(nil)
	stream.CodecParameters().SetMediaType(astiav.MediaTypeVideo)
	stream.SetTimeBase(astiav.NewRational(1, 90000))
	stream.SetIndex(0)

	err = k.NotifyAboutPacketSource(ctx, source)
	require.NoError(t, err)

	firstStream := k.OutputStreams[0]
	require.NotNil(t, firstStream)

	// Call again - should reuse the existing stream.
	err = k.NotifyAboutPacketSource(ctx, source)
	require.NoError(t, err)

	assert.Same(t, firstStream, k.OutputStreams[0], "should reuse existing stream")
}

func TestNodeKernel_SendInput_VideoFrame(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeVideo)

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: codecParams,
		StreamIndex:     0,
		TimeBase:        astiav.NewRational(1, 90000),
	}

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetPts(100)

	input := frame.BuildInput(f, 0, streamInfo)
	outputCh := make(chan packetorframe.OutputUnion, 10)

	err = k.SendInput(ctx, packetorframe.InputUnion{Frame: &input}, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Frame)
	case <-time.After(2 * time.Second):
		t.Fatal("expected frame output")
	}
}

func TestNodeKernel_SendInput_Frame_ContextCancelled(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx)
	require.NoError(t, err)

	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeAudio)

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: codecParams,
		StreamIndex:     0,
		TimeBase:        astiav.NewRational(1, 44100),
	}

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetPts(100)

	input := frame.BuildInput(f, 0, streamInfo)

	// Unbuffered channel + cancelled context.
	outputCh := make(chan packetorframe.OutputUnion)
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	err = k.SendInput(cancelCtx, packetorframe.InputUnion{Frame: &input}, outputCh)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNodeKernel_SendInput_VideoFrame_ContextCancelled(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeVideo)

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: codecParams,
		StreamIndex:     0,
		TimeBase:        astiav.NewRational(1, 90000),
	}

	f := astiav.AllocFrame()
	defer f.Free()
	f.SetPts(100)

	input := frame.BuildInput(f, 0, streamInfo)

	// Unbuffered channel + cancelled context.
	outputCh := make(chan packetorframe.OutputUnion)
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	err = k.SendInput(cancelCtx, packetorframe.InputUnion{Frame: &input}, outputCh)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNodeKernel_SendInput_VideoFrame_WithoutFixPTS_Skips(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(false))
	require.NoError(t, err)

	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeVideo)

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: codecParams,
		StreamIndex:     0,
		TimeBase:        astiav.NewRational(1, 90000),
	}

	outputCh := make(chan packetorframe.OutputUnion, 20)

	// First frame with PktDts=0 => new source (DTS==0 triggers setNewTimeShift).
	f1 := astiav.AllocFrame()
	defer f1.Free()
	f1.SetPts(1000)
	f1.SetPktDts(0)
	input1 := frame.BuildInput(f1, 0, streamInfo)
	err = k.SendInput(ctx, packetorframe.InputUnion{Frame: &input1}, outputCh)
	require.NoError(t, err)
	<-outputCh

	// Second frame with PktDts=0 => treated as new source with lower PTS.
	// ShouldFixPTS=false => should be skipped via errSkip.
	f2 := astiav.AllocFrame()
	defer f2.Free()
	f2.SetPts(50)
	f2.SetPktDts(0)
	input2 := frame.BuildInput(f2, 0, streamInfo)
	err = k.SendInput(ctx, packetorframe.InputUnion{Frame: &input2}, outputCh)
	require.NoError(t, err)

	select {
	case <-outputCh:
		t.Fatal("expected frame to be skipped")
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestNodeKernel_SendInput_VideoFrame_SecondFrameWithFixPTS(t *testing.T) {
	ctx := context.Background()
	k, err := NewNodeKernel(ctx, NodeKernelOptionShouldFixPTS(true))
	require.NoError(t, err)

	codecParams := astiav.AllocCodecParameters()
	codecParams.SetMediaType(astiav.MediaTypeVideo)

	streamInfo := &packetorframetypes.StreamInfo{
		CodecParameters: codecParams,
		StreamIndex:     0,
		TimeBase:        astiav.NewRational(1, 90000),
	}

	outputCh := make(chan packetorframe.OutputUnion, 20)

	// First frame with PktDts=0 => treated as new source.
	f1 := astiav.AllocFrame()
	defer f1.Free()
	f1.SetPts(100)
	f1.SetPktDts(0)

	input1 := frame.BuildInput(f1, 0, streamInfo)
	err = k.SendInput(ctx, packetorframe.InputUnion{Frame: &input1}, outputCh)
	require.NoError(t, err)
	<-outputCh

	// Second frame with PktDts=0 again => treated as new source again.
	f2 := astiav.AllocFrame()
	defer f2.Free()
	f2.SetPts(50) // lower PTS, triggers time shift
	f2.SetPktDts(0)

	input2 := frame.BuildInput(f2, 0, streamInfo)
	err = k.SendInput(ctx, packetorframe.InputUnion{Frame: &input2}, outputCh)
	require.NoError(t, err)

	select {
	case out := <-outputCh:
		assert.NotNil(t, out.Frame)
	case <-time.After(2 * time.Second):
		t.Fatal("expected frame output")
	}
}
