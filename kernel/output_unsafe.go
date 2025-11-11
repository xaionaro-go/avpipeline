package kernel

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/sockopt"
	"github.com/xaionaro-go/xsync"
)

func (o *Output) UnsafeSetSendBufferSize(
	ctx context.Context,
	size uint,
) (_err error) {
	logger.Debugf(ctx, "UnsafeSetSendBufferSize(ctx, %d)", size)
	defer func() { logger.Debugf(ctx, "/UnsafeSetSendBufferSize(ctx, %d): %v", size, _err) }()

	fd, err := o.UnsafeGetFileDescriptor(ctx)
	if err != nil {
		return fmt.Errorf("unable to get file descriptor: %w", err)
	}

	err = sockopt.SetWriteBuffer(fd, int(size))
	if err != nil {
		return fmt.Errorf("unable to set the buffer size of file descriptor %d to %d", fd, int(size))
	}

	return nil
}

func (o *Output) UnsafeSetLinger(
	ctx context.Context,
	onOff int32,
	linger int32,
) (_err error) {
	logger.Debugf(ctx, "UnsafeSetLinger(ctx, %d, %d)", onOff, linger)
	defer func() { logger.Debugf(ctx, "/UnsafeSetLinger(ctx, %d, %d): %v", onOff, linger, _err) }()

	fd, err := o.UnsafeGetFileDescriptor(ctx)
	if err != nil {
		return fmt.Errorf("unable to get file descriptor: %w", err)
	}

	err = sockSetLinger(fd, onOff, linger)
	if err != nil {
		return fmt.Errorf("unable to set the linger of file descriptor %d to %d/%d: %w", fd, onOff, linger, err)
	}

	return nil
}

var _ GetInternalQueueSizer = (*Output)(nil)

// Warning! The implementation intrudes into private structures, which is unsafe.
func (r *Output) GetInternalQueueSize(
	ctx context.Context,
) map[string]uint64 {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Debugf(ctx, "panic recovered in %s in GetInternalQueueSize: %v\n%s", r, rec, debug.Stack())
		}
	}()
	return xsync.DoA1R1(ctx, &r.formatContextLocker, r.unsafeGetInternalQueueSize, ctx)
}

func (r *Output) unsafeGetInternalQueueSize(
	ctx context.Context,
) map[string]uint64 {
	if r.proxy != nil {
		logger.Debugf(ctx, "getting the internal queue size from the proxy is not implemented, yet")
		return nil
	}
	switch r.URLParsed.Scheme {
	case "rtmp", "rtmps":
		return r.getInternalQueueSizeRTMP(ctx)
	default:
		logger.Debugf(ctx, "getting the internal queue size from '%s', yet", r.URLParsed.Scheme)
		return nil // not implemented, yet
	}
}

func (r *Output) UnsafeGetRawAVIOContext(
	ctx context.Context,
) *avcommon.AVIOContext {
	return xsync.DoA1R1(context.Background(), &r.formatContextLocker, r.unsafeGetRawAVIOContext, ctx)
}

func (r *Output) unsafeGetRawAVIOContext(
	ctx context.Context,
) *avcommon.AVIOContext {
	return r.unsafeGetRawAVFormatContext(ctx).Pb()
}

func (r *Output) UnsafeGetRawAVFormatContext(
	ctx context.Context,
) *avcommon.AVFormatContext {
	return xsync.DoA1R1(context.Background(), &r.formatContextLocker, r.unsafeGetRawAVFormatContext, ctx)
}

func (r *Output) unsafeGetRawAVFormatContext(
	ctx context.Context,
) *avcommon.AVFormatContext {
	return avcommon.WrapAVFormatContext(xastiav.CFromAVFormatContext(r.FormatContext))
}

func (o *Output) UnsafeGetFileDescriptor(
	ctx context.Context,
) (_ret int, _err error) {
	logger.Debugf(ctx, "UnsafeGetFileDescriptor")
	defer func() { logger.Debugf(ctx, "/UnsafeGetFileDescriptor: %v %v", _ret, _err) }()
	return xsync.DoA1R2(ctx, &o.formatContextLocker, o.unsafeGetFileDescriptor, ctx)
}

func (o *Output) unsafeGetFileDescriptor(
	ctx context.Context,
) (_ret int, _err error) {
	switch o.URLParsed.Scheme {
	case "rtmp":
		if tcpCtx := o.unsafeGetRawTCPContext(ctx); tcpCtx != nil {
			return tcpCtx.FileDescriptor(), nil
		}
		return 0, fmt.Errorf("unable to get an RTMP context")
	case "srt":
		return formatContextToSRTFD(ctx, o.FormatContext)
	}
	return 0, fmt.Errorf("do not know how to obtain the file descriptor of the output for network scheme '%s'", o.URLParsed.Scheme)
}

func (r *Output) UnsafeGetRawURLContext(
	ctx context.Context,
) *avcommon.URLContext {
	return xsync.DoA1R1(ctx, &r.formatContextLocker, r.unsafeGetRawURLContext, ctx)
}

func (r *Output) unsafeGetRawURLContext(
	ctx context.Context,
) *avcommon.URLContext {
	avioCtx := r.unsafeGetRawAVIOContext(ctx)
	return avcommon.WrapURLContext(avioCtx.Opaque())
}

func (r *Output) UnsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	return xsync.DoA1R1(ctx, &r.formatContextLocker, r.unsafeGetRawTCPContext, ctx)
}

func (r *Output) unsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	switch r.URLParsed.Scheme {
	case "rtmp", "rtmps":
		if rtmp := r.unsafeGetRawRTMPContext(ctx); rtmp != nil {
			return rtmp.TCPContext()
		}
		return nil
	default:
		logger.Debugf(ctx, "getting the the TCP docket from '%s' (yet?)", r.URLParsed.Scheme)
		return nil
	}
}

func (r *Output) UnsafeGetRawRTMPContext(
	ctx context.Context,
) *avcommon.RTMPContext {
	return xsync.DoA1R1(ctx, &r.formatContextLocker, r.unsafeGetRawRTMPContext, ctx)
}

func (r *Output) unsafeGetRawRTMPContext(
	ctx context.Context,
) *avcommon.RTMPContext {
	urlCtx := r.unsafeGetRawURLContext(ctx)
	return avcommon.WrapRTMPContext(urlCtx.PrivData())
}

func (r *Output) getInternalQueueSizeRTMP(
	ctx context.Context,
) map[string]uint64 {

	avioCtx := r.unsafeGetRawAVIOContext(ctx)
	avioBytes := avioCtx.Buffer()
	result := map[string]uint64{
		"AVIOBuffered": uint64(len(avioBytes)),
	}

	tcpQueue := r.unsafeGetTCPSocketQueue(ctx)
	for k, v := range tcpQueue {
		result["TCP:"+k] = v
	}

	return result
}
