//go:build linux
// +build linux

package kernel

import (
	"context"
	"syscall"

	"github.com/xaionaro-go/avpipeline/logger"
	"golang.org/x/sys/unix"
)

func getTCPSocketQueueOS(
	ctx context.Context,
	rawConn syscall.RawConn,
) map[string]uint64 {
	var totalQ, notYetSentQ int
	var errTotal, errNotYetSent error

	if rawConn == nil {
		logger.Errorf(ctx, "rawConn is nil")
		return nil
	}

	err := rawConn.Control(func(fd uintptr) {
		totalQ, errTotal = unix.IoctlGetInt(int(fd), unix.SIOCOUTQ)
		const SIOCOUTQNSD = 0x894B
		notYetSentQ, errNotYetSent = unix.IoctlGetInt(int(fd), SIOCOUTQNSD)
	})
	if err != nil {
		logger.Errorf(ctx, "rawConn.Control failed: %v", err)
		return nil
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
