package kernel

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/sockopt"
)

func (i *Input) UnsafeSetRecvBufferSize(
	ctx context.Context,
	size uint,
) (_err error) {
	logger.Debugf(ctx, "UnsafeSetRecvBufferSize(ctx, %d)", size)
	defer func() { logger.Debugf(ctx, "/UnsafeSetRecvBufferSize(ctx, %d): %v", size, _err) }()

	fd, err := i.UnsafeGetFileDescriptor(ctx)
	if err != nil {
		return fmt.Errorf("unable to get file descriptor: %w", err)
	}

	err = sockopt.SetReadBuffer(fd, int(size))
	if err != nil {
		return fmt.Errorf("unable to set the buffer size of file descriptor %d to %d", fd, int(size))
	}

	return nil
}

var _ GetInternalQueueSizer = (*Retry[Abstract])(nil)

func (i *Input) UnsafeGetRawAVIOContext(
	ctx context.Context,
) *avcommon.AVIOContext {
	return i.UnsafeGetRawAVFormatContext(ctx).Pb()
}

func (i *Input) UnsafeGetRawAVFormatContext(
	ctx context.Context,
) *avcommon.AVFormatContext {
	return avcommon.WrapAVFormatContext(xastiav.CFromAVFormatContext(i.FormatContext))
}

func (i *Input) UnsafeGetFileDescriptor(
	ctx context.Context,
) (_ret int, _err error) {
	logger.Debugf(ctx, "UnsafeGetFileDescriptor")
	defer func() { logger.Debugf(ctx, "/UnsafeGetFileDescriptor: %v %v", _ret, _err) }()
	switch i.URLParsed.Scheme {
	case "rtmp":
		if tcpCtx := i.UnsafeGetRawTCPContext(ctx); tcpCtx != nil {
			return tcpCtx.FileDescriptor(), nil
		}
		return 0, fmt.Errorf("unable to get an RTMP context")
	case "srt":
		return formatContextToSRTFD(ctx, i.FormatContext)
	}
	return 0, fmt.Errorf("do not know how to obtain the file descriptor of the Input for network scheme '%s'", i.URLParsed.Scheme)
}

func (i *Input) UnsafeGetRawURLContext(
	ctx context.Context,
) *avcommon.URLContext {
	avioCtx := i.UnsafeGetRawAVIOContext(ctx)
	return avcommon.WrapURLContext(avioCtx.Opaque())
}

func (i *Input) UnsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	switch i.URLParsed.Scheme {
	case "rtmp", "rtmps":
		if rtmp := i.UnsafeGetRawRTMPContext(ctx); rtmp != nil {
			return rtmp.TCPContext()
		}
		return nil
	default:
		logger.Debugf(ctx, "getting the the TCP docket from '%s' (yet?)", i.URLParsed.Scheme)
		return nil
	}
}

func (i *Input) UnsafeGetRawRTMPContext(
	ctx context.Context,
) *avcommon.RTMPContext {
	urlCtx := i.UnsafeGetRawURLContext(ctx)
	return avcommon.WrapRTMPContext(urlCtx.PrivData())
}
