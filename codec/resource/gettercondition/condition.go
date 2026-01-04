// condition.go defines the Condition interface for resource getters.

// Package gettercondition provides various conditions for filtering resource getters.
package gettercondition

import (
	"github.com/xaionaro-go/avpipeline/codec/resource"
	"github.com/xaionaro-go/avpipeline/types"
)

type Condition = types.Condition[resource.GetterInput]
