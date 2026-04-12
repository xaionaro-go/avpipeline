// channel_layout.go provides helpers for converting between channel counts and layouts.

package codec

import (
	"fmt"

	"github.com/asticode/go-astiav"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
)

func channelLayoutFromCount(channels audio.Channel) (astiav.ChannelLayout, error) {
	switch channels {
	case 1:
		return astiav.ChannelLayoutMono, nil
	case 2:
		return astiav.ChannelLayoutStereo, nil
	default:
		return astiav.ChannelLayout{}, fmt.Errorf("unsupported channel count %d: only 1 (mono) and 2 (stereo) are supported", channels)
	}
}
