package resourcegetter

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Condition = globaltypes.Condition[Input]

type Input struct {
	Params   *astiav.CodecParameters
	TimeBase astiav.Rational
	Options  []types.EncoderFactoryOption
}
