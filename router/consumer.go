package router

import (
	"fmt"
)

type Consumer[T any] interface {
	fmt.Stringer
}
