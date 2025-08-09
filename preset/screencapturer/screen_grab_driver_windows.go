package screencapturer

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

func getScreenGrabInput(
	params Params,
) string {
	return fmt.Sprintf(
		"desktop",
	)
}

func inputOptions(
	params Params,
) types.DictionaryItems {
	return types.DictionaryItems{
		{
			Key:   "f",
			Value: "gdigrab", // TODO(performance): replace with dshow or even better: the "ddagrab" filter.
		},
		{
			Key:   "offset_x",
			Value: fmt.Sprintf("%d", params.Area.Min.X),
		},
		{
			Key:   "offset_y",
			Value: fmt.Sprintf("%d", params.Area.Min.X),
		},
		{
			Key:   "video_size",
			Value: fmt.Sprintf("%dx%d", params.Area.Dx(), params.Area.Dy()),
		},
	}
}
