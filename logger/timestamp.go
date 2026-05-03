// timestamp.go provides helpers for emitting wall-clock timestamps inside log
// messages. It exists so codec/decoder lifecycle log lines (MediaCodec
// open/close/reinit) carry millisecond-precision timestamps regardless of the
// outer logger's TimestampFormat — making reconfig pause budgets measurable
// from logs alone.

package logger

import "time"

// NowMS returns the current wall-clock time in Unix milliseconds.
//
// It is intended for inlining into log format strings at lifecycle boundaries
// where the outer logger's timestamp granularity (e.g. integer seconds) is
// insufficient for measuring sub-second pauses. Example:
//
//	logger.Debugf(ctx, "reinitEncoder ts_ms=%d", logger.NowMS())
func NowMS() int64 {
	return time.Now().UnixMilli()
}
