//go:build with_libsrt
// +build with_libsrt

// srt_libsrt.go provides SRT-specific functionality using the libsrt library.

package kernel

import (
	"context"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/libsrt/extras/xastiav"
	"github.com/xaionaro-go/libsrt/threadsafe"
)

func formatContextToSRTSocket(
	ctx context.Context,
	fmtCtx *astiav.FormatContext,
) (_ret *threadsafe.Socket, _err error) {
	logger.Tracef(ctx, "formatContextToSRTSocket")
	defer func() { logger.Tracef(ctx, "/formatContextToSRTSocket: %v %v", _ret, _err) }()

	sockC := xastiav.GetFDFromFormatContext(fmtCtx)
	logger.Debugf(ctx, "SRT file descriptor: %d", sockC)
	sock := threadsafe.SocketFromC(int32(sockC))
	return sock, nil
}
