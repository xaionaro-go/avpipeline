package safekey

import "context"

type Formatter[K comparable] interface {
	FormatKey(ctx context.Context, key K) string
}

type FormatterFunc[K comparable] func(ctx context.Context, key K) string

func (f FormatterFunc[K]) FormatKey(
	ctx context.Context,
	key K,
) string {
	return f(ctx, key)
}

func Format[K comparable](
	ctx context.Context,
	formatter Formatter[K],
	key K,
) string {
	if formatter == nil {
		return redacted
	}

	return formatter.FormatKey(ctx, key)
}
