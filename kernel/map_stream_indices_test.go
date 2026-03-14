// map_stream_indices_test.go tests MapStreamIndices kernel.
// Agent-generated tests.

package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	packetorframetypes "github.com/xaionaro-go/avpipeline/packetorframe/types"
)

type fixedAssigner struct {
	OutputIndex int
}

func (a *fixedAssigner) StreamIndexAssign(
	_ context.Context,
	_ packetorframe.InputUnion,
) ([]int, error) {
	return []int{a.OutputIndex}, nil
}

func TestMapStreamIndices_DoesNotMutateSourceStreamInfo(t *testing.T) {
	ctx := context.Background()

	cp := astiav.AllocCodecParameters()
	defer cp.Free()
	cp.SetMediaType(astiav.MediaTypeAudio)
	cp.SetCodecID(astiav.CodecIDPcmS16Le)
	cp.SetSampleRate(48000)
	cp.SetChannelLayout(astiav.ChannelLayoutMono)

	sourceStreamInfo := &packetorframetypes.StreamInfo{
		Source:          &Dummy{},
		CodecParameters: cp,
		StreamIndex:     0,
		StreamsCount:    1,
		TimeBase:        astiav.NewRational(1, 48000),
	}

	m := NewMapStreamIndices(ctx, &fixedAssigner{OutputIndex: 5})
	defer m.Close(ctx)

	outputCh := make(chan packetorframe.OutputUnion, 10)

	// Send first frame.
	f1 := astiav.AllocFrame()
	defer f1.Free()
	f1.SetNbSamples(1024)
	f1.SetSampleRate(48000)
	f1.SetChannelLayout(astiav.ChannelLayoutMono)
	f1.SetSampleFormat(astiav.SampleFormatS16)
	input1 := packetorframe.InputUnion{
		Frame: ptr(frame.BuildInput(f1, 0, sourceStreamInfo)),
	}
	err := m.SendInput(ctx, input1, outputCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 0, sourceStreamInfo.StreamIndex,
		"source StreamInfo.StreamIndex must not be mutated by MapStreamIndices")

	// Send second frame — should still hit cache (StreamIndex unchanged).
	f2 := astiav.AllocFrame()
	defer f2.Free()
	f2.SetNbSamples(1024)
	f2.SetSampleRate(48000)
	f2.SetChannelLayout(astiav.ChannelLayoutMono)
	f2.SetSampleFormat(astiav.SampleFormatS16)
	input2 := packetorframe.InputUnion{
		Frame: ptr(frame.BuildInput(f2, 0, sourceStreamInfo)),
	}
	err = m.SendInput(ctx, input2, outputCh)
	require.NoError(t, err)
	testifyassert.Equal(t, 0, sourceStreamInfo.StreamIndex,
		"source StreamInfo.StreamIndex must not be mutated after second frame")

	// Verify both outputs have the mapped stream index.
	testifyassert.Len(t, outputCh, 2)
	out1 := <-outputCh
	out2 := <-outputCh
	testifyassert.Equal(t, 5, out1.GetStreamIndex())
	testifyassert.Equal(t, 5, out2.GetStreamIndex())

	// Verify only one output stream was created (no index leak).
	testifyassert.Len(t, m.outputStreams, 1)
}
