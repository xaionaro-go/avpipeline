// consumer.go defines the Consumer interface for route destinations.

package router

import (
	"fmt"
)

type Consumer[T any] interface {
	fmt.Stringer
}
