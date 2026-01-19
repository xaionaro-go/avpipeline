package kernel

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

func TestInput_DisplayRotation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use lavfi testsrc to have a video stream without needing a physical file
	urlString := "testsrc=duration=1"
	authKey := secret.New("")
	cfg := InputConfig{
		CustomOptions: types.DictionaryItems{
			{Key: "f", Value: "lavfi"},
			{Key: "display_rotation", Value: "90"},
		},
	}

	input, err := NewInputFromURL(ctx, urlString, authKey, cfg)
	require.NoError(t, err)
	defer input.Close(ctx)

	foundVideoStream := false
	for _, stream := range input.FormatContext.Streams() {
		if stream.CodecParameters().MediaType() == astiav.MediaTypeVideo {
			foundVideoStream = true
			dm, ok := stream.SideData().DisplayMatrix().Get()
			require.True(t, ok, "Display matrix should be present")
			require.Equal(t, 90.0, dm.Rotation(), "Rotation should be 90 degrees")
		}
	}
	require.True(t, foundVideoStream, "Should have found at least one video stream")
}
