// assert.go provides internal assertion helpers for the avpipeline project.

// Package internal provides internal utilities for the avpipeline project.
package internal

import (
	"context"
	"runtime/debug"

	"github.com/xaionaro-go/avpipeline/logger"
)

func Assert(
	ctx context.Context,
	mustBeTrue bool,
	extraArgs ...any,
) {
	if mustBeTrue {
		return
	}

	if len(extraArgs) == 0 {
		logger.Panicf(ctx, "assertion failed:\n%s", debug.Stack())
		return
	}

	logger.Panic(ctx, "assertion failed:\nExtra args:", extraArgs, "\n", string(debug.Stack()))
}

func AssertSoft(
	ctx context.Context,
	mustBeTrue bool,
	extraArgs ...any,
) {
	if mustBeTrue {
		return
	}

	if len(extraArgs) == 0 {
		logger.Warnf(ctx, "soft assertion failed:\n%s", debug.Stack())
		return
	}

	logger.Warn(ctx, "soft assertion failed:\nExtra args:", extraArgs, "\n", string(debug.Stack()))
}
