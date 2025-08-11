package logger

import (
	"github.com/facebookincubator/go-belt/tool/logger"
)

type Level = logger.Level

const (
	// LevelUndefined is the erroneous value of log-level which corresponds
	// to zero-value.
	LevelUndefined = logger.LevelUndefined

	// LevelFatal will report about Fatalf-s only.
	LevelFatal = logger.LevelFatal

	// LevelPanic will report about Panicf-s and Fatalf-s only.
	LevelPanic = logger.LevelPanic

	// LevelError will report about Errorf-s, Panicf-s, ...
	LevelError = logger.LevelError

	// LevelWarning will report about Warningf-s, Errorf-s, ...
	LevelWarning = logger.LevelWarning

	// LevelInfo will report about Infof-s, Warningf-s, ...
	LevelInfo = logger.LevelInfo

	// LevelDebug will report about Debugf-s, Infof-s, ...
	LevelDebug = logger.LevelDebug

	// LevelTrace will report about Tracef-s, Debugf-s, ...
	LevelTrace = logger.LevelTrace
)
