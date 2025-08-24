package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec/resourcegetter"
)

type Static bool

var _ Condition = (Static)(false)

func (v Static) String() string {
	return fmt.Sprintf("%t", v)
}

func (v Static) Match(context.Context, resourcegetter.ConditionInput) bool {
	return (bool)(v)
}
