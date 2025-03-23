package kernel

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

type Abstract interface {
	SendInputer
	fmt.Stringer
	types.Closer
	CloseChaner
	Generator
}

type CloseChaner interface {
	CloseChan() <-chan struct{}
}
