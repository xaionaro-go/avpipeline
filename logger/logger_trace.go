//go:build debug_trace
// +build debug_trace

package logger

import (
	"context"

	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
)

// TraceFields is just a shorthand for LogFields(ctx, logger.LevelTrace, ...)
func TraceFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.TraceFields(ctx, message, fields)
}

// Trace is just a shorthand for Log(ctx, logger.LevelTrace, ...)
func Trace(ctx context.Context, values ...any) {
	logger.Trace(ctx, values...)
}

// Tracef is just a shorthand for Logf(ctx, logger.LevelTrace, ...)
func Tracef(ctx context.Context, format string, args ...any) {
	logger.Tracef(ctx, format, args...)
}
