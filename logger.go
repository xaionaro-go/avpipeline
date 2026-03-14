// logger.go provides logging utilities and conversion functions for the avpipeline package.

package avpipeline

import (
	"github.com/asticode/go-astiav"

	"github.com/xaionaro-go/avpipeline/logger"
)

func LogLevelToAstiav(level logger.Level) astiav.LogLevel {
	switch level {
	case logger.LevelUndefined:
		return astiav.LogLevelQuiet
	case logger.LevelPanic:
		return astiav.LogLevelPanic
	case logger.LevelFatal:
		return astiav.LogLevelFatal
	case logger.LevelError:
		return astiav.LogLevelError
	case logger.LevelWarning:
		return astiav.LogLevelWarning
	case logger.LevelInfo:
		return astiav.LogLevelInfo
	case logger.LevelDebug:
		return astiav.LogLevelVerbose
	case logger.LevelTrace:
		return astiav.LogLevelDebug
	}
	return astiav.LogLevelWarning
}

func LogLevelFromAstiav(level astiav.LogLevel) logger.Level {
	switch level {
	case astiav.LogLevelQuiet:
		return logger.LevelUndefined
	case astiav.LogLevelFatal:
		// FFmpeg's AV_LOG_FATAL means "this operation failed", not
		// "the process must exit". Mapping to LevelFatal would cause
		// logrus to call os.Exit(1) on transient decode errors.
		return logger.LevelError
	case astiav.LogLevelPanic:
		return logger.LevelError
	case astiav.LogLevelError:
		return logger.LevelError
	case astiav.LogLevelWarning:
		return logger.LevelWarning
	case astiav.LogLevelInfo:
		return logger.LevelInfo
	case astiav.LogLevelVerbose:
		return logger.LevelDebug
	case astiav.LogLevelDebug:
		return logger.LevelTrace
	}
	return logger.LevelWarning
}
