package kernel

import (
	"context"
	"fmt"
	"io"
	"net"
	"syscall"

	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/net/raw"
	"github.com/xaionaro-go/sockopt"
	"github.com/xaionaro-go/xsync"
)

func (i *Input) UnsafeWithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) (_err error) {
	logger.Debugf(ctx, "UnsafeWithNetworkConn")
	defer func() { logger.Debugf(ctx, "/UnsafeWithNetworkConn: %v", _err) }()
	return xsync.DoA2R1(ctx, &i.FormatContextLocker, i.unsafeWithNetworkConn, ctx, callback)
}

func (i *Input) unsafeWithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) (_err error) {
	if _, ok := ctx.Value(ctxKeyBypassIsOpenCheck).(struct{}); !ok {
		logger.Tracef(ctx, "checking whether the input is opened")
		select {
		case <-i.openFinished:
			if i.openError != nil {
				return fmt.Errorf("input is not opened successfully: %w", i.openError)
			}
		case <-ctx.Done():
			return ctx.Err()
		case <-i.CloseChan():
			return io.EOF
		default:
			return fmt.Errorf("input is not opened, yet")
		}
	}

	fd, err := i.unsafeGetFileDescriptor(ctx)
	if err != nil {
		return fmt.Errorf("unable to get file descriptor: %w", err)
	}

	return raw.WithTCPConnFromFD(ctx, fd, func(conn net.Conn) error {
		return callback(ctx, conn)
	})
}

func (i *Input) UnsafeWithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) (_err error) {
	logger.Debugf(ctx, "UnsafeWithRawNetworkConn")
	defer func() { logger.Debugf(ctx, "/UnsafeWithRawNetworkConn: %v", _err) }()
	return xsync.DoA2R1(ctx, &i.FormatContextLocker, i.unsafeWithRawNetworkConn, ctx, callback)
}

func (i *Input) unsafeWithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) (_err error) {
	return i.unsafeWithNetworkConn(ctx, func(ctx context.Context, conn net.Conn) error {
		rawConner, ok := conn.(syscall.Conn)
		if !ok {
			return fmt.Errorf("unable to get syscall.Conn from net.Conn (%T)", conn)
		}
		rawConn, err := rawConner.SyscallConn()
		if err != nil {
			return fmt.Errorf("unable to get RawConn from syscall.Conn: %w", err)
		}
		networkName := "tcp"
		if conn.RemoteAddr() != nil {
			networkName = conn.RemoteAddr().Network()
		}
		return callback(ctx, rawConn, networkName)
	})
}

var (
	_ GetNetConner        = (*Input)(nil)
	_ GetSyscallRawConner = (*Input)(nil)
)

func (i *Input) UnsafeSetRecvBufferSize(
	ctx context.Context,
	size uint,
) (_err error) {
	logger.Debugf(ctx, "UnsafeSetRecvBufferSize(ctx, %d)", size)
	defer func() { logger.Debugf(ctx, "/UnsafeSetRecvBufferSize(ctx, %d): %v", size, _err) }()
	return xsync.DoA2R1(ctx, &i.FormatContextLocker, i.unsafeSetRecvBufferSize, ctx, size)
}

func (i *Input) unsafeSetRecvBufferSize(
	ctx context.Context,
	size uint,
) (_err error) {
	fd, err := i.unsafeGetFileDescriptor(ctx)
	if err != nil {
		return fmt.Errorf("unable to get file descriptor: %w", err)
	}

	err = sockopt.SetReadBuffer(fd, int(size))
	if err != nil {
		return fmt.Errorf("unable to set the buffer size of file descriptor %d to %d", fd, int(size))
	}

	return nil
}

func (i *Input) UnsafeGetRawAVIOContext(
	ctx context.Context,
) *avcommon.AVIOContext {
	return xsync.DoA1R1(ctx, &i.FormatContextLocker, i.unsafeGetRawAVIOContext, ctx)
}

func (i *Input) unsafeGetRawAVIOContext(
	ctx context.Context,
) *avcommon.AVIOContext {
	return i.unsafeGetRawAVFormatContext(ctx).Pb()
}

func (i *Input) UnsafeGetRawAVFormatContext(
	ctx context.Context,
) *avcommon.AVFormatContext {
	return xsync.DoA1R1(ctx, &i.FormatContextLocker, i.unsafeGetRawAVFormatContext, ctx)
}

func (i *Input) unsafeGetRawAVFormatContext(
	ctx context.Context,
) *avcommon.AVFormatContext {
	return avcommon.WrapAVFormatContext(xastiav.CFromAVFormatContext(i.FormatContext))
}

func (i *Input) UnsafeGetFileDescriptor(
	ctx context.Context,
) (_ret int, _err error) {
	logger.Debugf(ctx, "UnsafeGetFileDescriptor")
	defer func() { logger.Debugf(ctx, "/UnsafeGetFileDescriptor: %v %v", _ret, _err) }()
	return xsync.DoA1R2(ctx, &i.FormatContextLocker, i.unsafeGetFileDescriptor, ctx)
}

func (i *Input) unsafeGetFileDescriptor(
	ctx context.Context,
) (_ret int, _err error) {
	switch i.URLParsed.Scheme {
	case "rtmp":
		if tcpCtx := i.unsafeGetRawTCPContext(ctx); tcpCtx != nil {
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
	return xsync.DoA1R1(ctx, &i.FormatContextLocker, i.unsafeGetRawURLContext, ctx)
}

func (i *Input) unsafeGetRawURLContext(
	ctx context.Context,
) *avcommon.URLContext {
	avioCtx := i.unsafeGetRawAVIOContext(ctx)
	return avcommon.WrapURLContext(avioCtx.Opaque())
}

func (i *Input) UnsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	return xsync.DoA1R1(ctx, &i.FormatContextLocker, i.unsafeGetRawTCPContext, ctx)
}

func (i *Input) unsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	switch i.URLParsed.Scheme {
	case "rtmp", "rtmps":
		if rtmp := i.unsafeGetRawRTMPContext(ctx); rtmp != nil {
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
	return xsync.DoA1R1(ctx, &i.FormatContextLocker, i.unsafeGetRawRTMPContext, ctx)
}

func (i *Input) unsafeGetRawRTMPContext(
	ctx context.Context,
) *avcommon.RTMPContext {
	urlCtx := i.unsafeGetRawURLContext(ctx)
	return avcommon.WrapRTMPContext(urlCtx.PrivData())
}
