package member

import "context"

type LiveKeyFunc[K comparable, M any] func(ctx context.Context, value M) (K, error)
