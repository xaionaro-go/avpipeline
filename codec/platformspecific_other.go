//go:build !android
// +build !android

package codec

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
)

func (c *Codec) platformSpecificHWSanityChecks(ctx context.Context) {
	logger.Tracef(ctx, "platformSpecificHWSanityChecks")
	defer func() { logger.Tracef(ctx, "/platformSpecificHWSanityChecks") }()
}
