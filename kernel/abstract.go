package kernel

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
)

type Abstract interface {
	SendInputer
	fmt.Stringer
	Closer
	CloseChaner
	Generator

	GetOutputFormatContext(ctx context.Context) *astiav.FormatContext
}

type CloseChaner interface {
	CloseChan() <-chan struct{}
}
