package types

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec"
)

type OutputKey struct {
	AudioCodec codec.Name
	VideoCodec codec.Name
	Resolution codec.Resolution
}

func (k OutputKey) String() string {
	return fmt.Sprintf("%s/%dx%d;%s", k.VideoCodec, k.Resolution.Width, k.Resolution.Height, k.AudioCodec)
}
