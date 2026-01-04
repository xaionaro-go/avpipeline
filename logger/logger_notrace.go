//go:build !debug_trace
// +build !debug_trace

// logger_notrace.go provides no-op trace logging functions when the debug_trace build tag is not set.

package logger

import (
	"context"

	"github.com/facebookincubator/go-belt/pkg/field"
)

// TraceFields is just a shorthand for LogFields(ctx, logger.LevelTrace, ...)
func TraceFields(ctx context.Context, message string, fields field.AbstractFields) {}

// Trace is just a shorthand for Log(ctx, logger.LevelTrace, ...)
func Trace(ctx context.Context, values ...any) {}

// Tracef is just a shorthand for Logf(ctx, logger.LevelTrace, ...)
func Tracef(ctx context.Context, format string, args ...any) {}
