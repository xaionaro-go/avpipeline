// error_handler.go defines the ErrorHandler interface.

package types

import (
	"context"
)

type ErrorHandler interface {
	HandleError(ctx context.Context, err error) error
}
