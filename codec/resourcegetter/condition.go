package resourcegetter

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Condition = globaltypes.Condition[ConditionInput]

type ConditionInput struct {
	Params   *astiav.CodecParameters
	TimeBase astiav.Rational
	Options  []types.EncoderFactoryOption
}
