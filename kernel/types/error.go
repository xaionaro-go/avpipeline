// error.go defines common error types for kernels.

package types

import (
	"github.com/xaionaro-go/avpipeline/kernel/typesnolibav"
)

type (
	ErrNotImplemented      = typesnolibav.ErrNotImplemented
	ErrUnexpectedInputType = typesnolibav.ErrUnexpectedInputType
)
