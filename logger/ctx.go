package logger

import (
	"context"

	"github.com/facebookincubator/go-belt/tool/logger"
)

func FromCtx(ctx context.Context) logger.Logger {
	return logger.FromCtx(ctx)
}

func CtxWithLogger(ctx context.Context, l logger.Logger) context.Context {
	return logger.CtxWithLogger(ctx, l)
}
