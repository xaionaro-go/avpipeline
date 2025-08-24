package frame

import (
	"github.com/xaionaro-go/avpipeline/codec"
)

type Source interface {
	codec.FrameSource
}
