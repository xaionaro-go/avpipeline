// closer.go defines the Closer interface.

package types

import (
	"context"
)

type Closer interface {
	Close(context.Context) error
}
