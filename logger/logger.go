// logger.go provides logging utilities and type aliases for the avpipeline project.

// Package logger provides logging utilities for the avpipeline project.
package logger

import (
	"context"

	"github.com/facebookincubator/go-belt/pkg/field"
	"github.com/facebookincubator/go-belt/tool/logger"
)

// Logger is just a type-alias for logger.Logger for convenience.
type Logger = logger.Logger

func SetDefault(defaultLogger func() Logger) {
	logger.Default = defaultLogger
}

// DebugFields is just a shorthand for LogFields(ctx, logger.LevelDebug, ...)
func DebugFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.DebugFields(ctx, message, fields)
}

// InfoFields is just a shorthand for LogFields(ctx, logger.LevelInfo, ...)
func InfoFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.InfoFields(ctx, message, fields)
}

// WarnFields is just a shorthand for LogFields(ctx, logger.LevelWarn, ...)
func WarnFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.WarnFields(ctx, message, fields)
}

// ErrorFields is just a shorthand for LogFields(ctx, logger.LevelError, ...)
func ErrorFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.ErrorFields(ctx, message, fields)
}

// PanicFields is just a shorthand for LogFields(ctx, logger.LevelPanic, ...)
//
// Be aware: Panic level also triggers a `panic`.
func PanicFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.PanicFields(ctx, message, fields)
}

// FatalFields is just a shorthand for LogFields(ctx, logger.LevelFatal, ...)
//
// Be aware: Panic level also triggers an `os.Exit`.
func FatalFields(ctx context.Context, message string, fields field.AbstractFields) {
	logger.FatalFields(ctx, message, fields)
}

// Debug is just a shorthand for Log(ctx, logger.LevelDebug, ...)
func Debug(ctx context.Context, values ...any) {
	logger.Debug(ctx, values...)
}

// Info is just a shorthand for Log(ctx, logger.LevelInfo, ...)
func Info(ctx context.Context, values ...any) {
	logger.Info(ctx, values...)
}

// Warn is just a shorthand for Log(ctx, logger.LevelWarn, ...)
func Warn(ctx context.Context, values ...any) {
	logger.Warn(ctx, values...)
}

// Error is just a shorthand for Log(ctx, logger.LevelError, ...)
func Error(ctx context.Context, values ...any) {
	logger.Error(ctx, values...)
}

// Panic is just a shorthand for Log(ctx, logger.LevelPanic, ...)
//
// Be aware: Panic level also triggers a `panic`.
func Panic(ctx context.Context, values ...any) {
	logger.Panic(ctx, values...)
}

// Fatal is just a shorthand for Log(logger.LevelFatal, ...)
//
// Be aware: Fatal level also triggers an `os.Exit`.
func Fatal(ctx context.Context, values ...any) {
	logger.Fatal(ctx, values...)
}

// Debugf is just a shorthand for Logf(ctx, logger.LevelDebug, ...)
func Debugf(ctx context.Context, format string, args ...any) {
	logger.Debugf(ctx, format, args...)
}

// Infof is just a shorthand for Logf(ctx, logger.LevelInfo, ...)
func Infof(ctx context.Context, format string, args ...any) {
	logger.Infof(ctx, format, args...)
}

// Warnf is just a shorthand for Logf(ctx, logger.LevelWarn, ...)
func Warnf(ctx context.Context, format string, args ...any) {
	logger.Warnf(ctx, format, args...)
}

// Errorf is just a shorthand for Logf(ctx, logger.LevelError, ...)
func Errorf(ctx context.Context, format string, args ...any) {
	logger.Errorf(ctx, format, args...)
}

// Panicf is just a shorthand for Logf(ctx, logger.LevelPanic, ...)
//
// Be aware: Panic level also triggers a `panic`.
func Panicf(ctx context.Context, format string, args ...any) {
	logger.Panicf(ctx, format, args...)
}

// Fatalf is just a shorthand for Logf(ctx, logger.LevelFatal, ...)
//
// Be aware: Fatal level also triggers an `os.Exit`.
func Fatalf(ctx context.Context, format string, args ...any) {
	logger.Fatalf(ctx, format, args...)
}

// Logf logs an unstructured message. All contextual structured
// fields are also logged.
//
// This method exists mostly for convenience, for people who
// has not got used to proper structured logging, yet.
// See `LogFields` and `Log`. If one have variables they want to
// log, it is better for scalable observability to log them
// as structured values, instead of injecting them into a
// non-structured string.
func Logf(ctx context.Context, level logger.Level, format string, args ...any) {
	logger.Logf(ctx, level, format, args...)
}
