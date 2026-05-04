package safekey

import "context"

const redacted = "<redacted>"

type redactedFormatter[K comparable] struct{}

func RedactedFormatter[K comparable]() Formatter[K] {
	return redactedFormatter[K]{}
}

func (redactedFormatter[K]) FormatKey(
	context.Context,
	K,
) string {
	return redacted
}
