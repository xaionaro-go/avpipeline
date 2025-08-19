package types

import (
	"github.com/xaionaro-go/avpipeline/codec"
)

type OutputKey struct {
	AudioCodec string
	VideoCodec string
	Resolution codec.Resolution
}
