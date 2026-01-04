// condition.go defines the Condition interface for filtering inputs to nodes.

// Package filter provides various filters for node inputs.
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
