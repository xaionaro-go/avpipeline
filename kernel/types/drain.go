package types

import (
	"context"
)

type IsDirtier interface {
	IsDirty(ctx context.Context) bool
}
