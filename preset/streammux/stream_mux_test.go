// stream_mux_test.go tests the stream muxer.
package streammux

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/node"
)

func TestStreamMuxNodes(t *testing.T) {
	mux := &StreamMux[struct{}]{}
	v := reflect.ValueOf(mux).Elem()

	var expectedValues []node.Abstract
	for i := range v.NumField() {
		fT := v.Type().Field(i)
		fTT := fT.Type
		if !fTT.Implements(reflect.TypeOf((*node.Abstract)(nil)).Elem()) {
			continue
		}

		fV := v.Field(i)
		if fV.Interface() != nil {
			expectedValues = append(expectedValues, fV.Interface().(node.Abstract))
		}
	}

	// we have zero outputs, so there be only the global streammux nodes:
	require.Equal(t, expectedValues, mux.Nodes(context.Background()))
}

func TestSetPreferredOutputsSplitAVRejectsIncompletePlanWithoutPartialSwitch(t *testing.T) {
	mux, ctx := newStreamMuxForEvictTest(t)

	const originalVideoID OutputID = 7
	const nextVideoID OutputID = 11

	videoKey := SenderKey{
		VideoCodec:      codectypes.Name("av1"),
		VideoResolution: codectypes.Resolution{Width: 1280, Height: 720},
	}
	audioKey := SenderKey{
		AudioCodec:      codectypes.Name("aac"),
		AudioSampleRate: audio.SampleRate(48_000),
	}
	combinedKey := SenderKey{
		VideoCodec:      videoKey.VideoCodec,
		VideoResolution: videoKey.VideoResolution,
		AudioCodec:      audioKey.AudioCodec,
		AudioSampleRate: audioKey.AudioSampleRate,
	}
	videoOutput := newOutputForInputForTest(t, ctx, mux.InputVideoOnly, nextVideoID, videoKey)
	mux.Outputs.Store(nextVideoID, videoOutput)
	mux.OutputsMap.Store(videoOutput.StorageKey(), videoOutput)
	mux.InputVideoOnly.OutputSwitch.CurrentValue.Store(int32(originalVideoID))
	mux.InputVideoOnly.OutputSyncer.CurrentValue.Store(int32(originalVideoID))

	err := mux.setPreferredOutputs(ctx, combinedKey)

	require.Error(t, err)
	require.Equal(t, int32(originalVideoID), mux.InputVideoOnly.OutputSwitch.CurrentValue.Load(),
		"invalid SplitAV route plans must be rejected before switching any route")
	require.Equal(t, int32(originalVideoID), mux.InputVideoOnly.OutputSyncer.CurrentValue.Load(),
		"invalid SplitAV route plans must not sync a partially switched route")
	require.Equal(t, int32(math.MinInt32), mux.InputVideoOnly.OutputSwitch.NextValue.Load(),
		"invalid SplitAV route plans must not leave a pending partial switch")
}
