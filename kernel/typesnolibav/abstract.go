// Package typesnolibav provides core abstractions for the avpipeline kernel that are independent of libav (FFmpeg) specific types.
//
// abstract.go defines the Abstract interface, which serves as the base contract for components in this package.
package typesnolibav

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

type Abstract interface {
	fmt.Stringer
	types.Closer
	types.GetObjectIDer
	CloseChaner
}
