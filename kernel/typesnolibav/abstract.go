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
