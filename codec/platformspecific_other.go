//go:build !android
// +build !android

// platformspecific_other.go provides stub hardware sanity checks for non-Android platforms.

package codec

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
)

func (c *Codec) platformSpecificHWSanityChecks(ctx context.Context) {
	logger.Tracef(ctx, "platformSpecificHWSanityChecks")
	defer func() { logger.Tracef(ctx, "/platformSpecificHWSanityChecks") }()
}
