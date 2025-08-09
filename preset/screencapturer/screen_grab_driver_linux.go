package screencapturer

import (
	"fmt"
	"os"

	"github.com/xaionaro-go/avpipeline/types"
)

func getScreenGrabInput(
	params Params,
) string {
	return fmt.Sprintf(
		"%s+%d,%d",
		os.Getenv("DISPLAY"),
		params.Area.Min.X, params.Area.Min.Y,
	)
}

func inputOptions(
	params Params,
) types.DictionaryItems {
	return types.DictionaryItems{
		{
			Key:   "f",
			Value: "x11grab", // TODO: add support of kmsgrab
		},
		{
			Key:   "video_size",
			Value: fmt.Sprintf("%dx%d", params.Area.Dx(), params.Area.Dy()),
		},
	}
}
