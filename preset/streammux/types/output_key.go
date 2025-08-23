package types

import (
	"fmt"

	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
)

type OutputKey struct {
	AudioCodec codectypes.Name
	VideoCodec codectypes.Name
	Resolution codectypes.Resolution
}

func (k OutputKey) String() string {
	return fmt.Sprintf("%s/%dx%d;%s", k.VideoCodec, k.Resolution.Width, k.Resolution.Height, k.AudioCodec)
}
