package gettercondition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec/resource"
)

type Static bool

var _ Condition = (Static)(false)

func (v Static) String() string {
	return fmt.Sprintf("%t", v)
}

func (v Static) Match(context.Context, resource.GetterInput) bool {
	return (bool)(v)
}
