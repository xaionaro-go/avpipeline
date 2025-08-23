package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
	"golang.org/x/sys/unix"
)

func (r *Output) unsafeGetTCPSocketQueue(
	ctx context.Context,
) map[string]uint64 {
	tcpCtx := r.unsafeGetRawTCPContext(ctx)
	fd := tcpCtx.FileDescriptor()
	if fd < 0 {
		logger.Errorf(ctx, "invalid file descriptor: %d", fd)
		return nil
	}

	// Total bytes in TCP send queue (unsent + sent-not-ACKed)
	var totalQ int
	if v, err := unix.IoctlGetInt(fd, unix.SIOCOUTQ); err == nil {
		totalQ = v
	} else {
		logger.Errorf(ctx, "cannot get SIOCOUTQ: %v", err)
	}

	// Not yet sent
	var notYetSentQ int
	const SIOCOUTQNSD = 0x894B
	if v, err := unix.IoctlGetInt(fd, SIOCOUTQNSD); err == nil {
		notYetSentQ = v
	} else {
		logger.Errorf(ctx, "cannot get SIOCOUTQNSD: %v", err)
	}

	return map[string]uint64{
		"SentNotACKed": uint64(totalQ - notYetSentQ),
		"NotYetSent":   uint64(notYetSentQ),
	}
}
