package gettercondition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec/resource"
)

type Function func(context.Context, resource.GetterInput) bool

var _ Condition = (Function)(nil)

func (fn Function) String() string {
	return fmt.Sprintf("<custom_function:%p>", fn)
}

func (fn Function) Match(ctx context.Context, f resource.GetterInput) bool {
	return fn(ctx, f)
}
