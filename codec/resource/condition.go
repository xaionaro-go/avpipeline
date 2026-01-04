// condition.go defines the Condition interface and input types for resource getters.

package resource

import (
	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type Condition = globaltypes.Condition[GetterInput]

type GetterInput struct {
	IsEncoder bool
	Params    *astiav.CodecParameters
	TimeBase  astiav.Rational
	Options   []types.Option
}
