// screen_grab_driver_darwin.go provides the screen grab driver for macOS.

package screencapturer

import (
	"github.com/xaionaro-go/avpipeline/types"
)

const (
	screenGrabDriver = "avfoundation"
)

func getScreenGrabInput(
	params Params,
) string {
	return "0"
}

func inputOptions(
	params Params,
) types.DictionaryItems {
	return types.DictionaryItems{{
		Key:   "f",
		Value: "avfoundation",
	}}
}
