//go:build linux
// +build linux

// output_linux.go provides Linux-specific implementations for the Output kernel.

package kernel

import (
	"context"

	"github.com/xaionaro-go/avpipeline/logger"
	"golang.org/x/sys/unix"
)

func (o *Output) unsafeGetTCPSocketQueue(
	ctx context.Context,
) map[string]uint64 {
	var totalQ, notYetSentQ int
	var errTotal, errNotYetSent error

	if o.rawConn != nil {
		err := o.rawConn.Control(func(fd uintptr) {
			totalQ, errTotal = unix.IoctlGetInt(int(fd), unix.SIOCOUTQ)
			const SIOCOUTQNSD = 0x894B
			notYetSentQ, errNotYetSent = unix.IoctlGetInt(int(fd), SIOCOUTQNSD)
		})
		if err != nil {
			logger.Errorf(ctx, "rawConn.Control failed: %v", err)
			return nil
		}
	} else {
		tcpCtx := o.unsafeGetRawTCPContext(ctx)
		if tcpCtx == nil {
			logger.Errorf(ctx, "unable to get TCP context")
			return nil
		}
		fd := tcpCtx.FileDescriptor()
		if fd < 0 {
			logger.Errorf(ctx, "invalid file descriptor: %d", fd)
			return nil
		}

		totalQ, errTotal = unix.IoctlGetInt(fd, unix.SIOCOUTQ)
		const SIOCOUTQNSD = 0x894B
		notYetSentQ, errNotYetSent = unix.IoctlGetInt(fd, SIOCOUTQNSD)
	}

	if errTotal != nil {
		logger.Errorf(ctx, "cannot get SIOCOUTQ: %v", errTotal)
	}
	if errNotYetSent != nil {
		logger.Errorf(ctx, "cannot get SIOCOUTQNSD: %v", errNotYetSent)
	}

	return map[string]uint64{
		"SentNotACKed": uint64(totalQ - notYetSentQ),
		"NotYetSent":   uint64(notYetSentQ),
	}
}
