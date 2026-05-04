package safekey_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
)

type panicStringerKey struct{}

func (panicStringerKey) String() string {
	panic("raw key formatting must not be used")
}

func TestNilFormatterRedactsKeyWithoutRawFormatting(t *testing.T) {
	require.Equal(t, "<redacted>", safekey.Format[panicStringerKey](context.Background(), nil, panicStringerKey{}))
}

func TestRedactedFormatterRedactsKey(t *testing.T) {
	formatter := safekey.RedactedFormatter[string]()

	require.Equal(t, "<redacted>", formatter.FormatKey(context.Background(), "raw-value"))
	require.Equal(t, "<redacted>", safekey.Format(context.Background(), formatter, "raw-value"))
}

func TestFormatterFuncFormatsKey(t *testing.T) {
	formatter := safekey.FormatterFunc[string](func(ctx context.Context, key string) string {
		require.NotNil(t, ctx)

		return "safe:" + key
	})

	require.Equal(t, "safe:public", formatter.FormatKey(context.Background(), "public"))
	require.Equal(t, "safe:public", safekey.Format(context.Background(), formatter, "public"))
}
