package filter

import (
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/avpipeline/types"
)

type Node interface {
	GetProcessor() processor.Abstract
}

type Input[T any] struct {
	Destination Node
	Input       T
}

type Condition[T any] = types.Condition[Input[T]]
