// encoder_full_reinit.go exposes an explicit reinit entry point on the
// full encoder. Internally it forwards to the same reinitEncoder path
// used by SetResolution / Flush-on-no-flush-cap, so observed timings
// match the production reconfig hot path.

package codec

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/xsync"
)

var (
	_ EncoderReiniter = (*EncoderFull)(nil)
	_ EncoderReiniter = (*EncoderFullLocked)(nil)
)

// Reinit closes the current codec instance and opens a fresh one with
// the existing InitParams and Quality. It acquires the encoder lock.
func (e *EncoderFull) Reinit(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Reinit")
	defer func() { logger.Debugf(ctx, "/Reinit: %v", _err) }()
	return xsync.DoA1R1(xsync.WithNoLogging(ctx, true), &e.locker, e.asLocked().Reinit, ctx)
}

// Reinit is the locked-context variant. The caller must already hold
// the encoder lock (e.g. from inside LockDo).
func (e *EncoderFullLocked) Reinit(ctx context.Context) (_err error) {
	logger.Debugf(ctx, "Reinit")
	defer func() { logger.Debugf(ctx, "/Reinit: %v", _err) }()
	return e.reinitEncoder(ctx)
}
