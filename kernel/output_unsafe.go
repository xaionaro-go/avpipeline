// output_unsafe.go provides methods for accessing underlying network connections of an Output kernel.

package kernel

import (
	"context"
	"fmt"
	"io"
	"net"
	"runtime/debug"
	"slices"
	"syscall"
	"time"

	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/net/raw"
	"github.com/xaionaro-go/xsync"
)

type ctxKeyBypassIsOpenCheckT struct{}

var ctxKeyBypassIsOpenCheck = ctxKeyBypassIsOpenCheckT{}

func (o *Output) UnsafeWithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) (_err error) {
	logger.Debugf(ctx, "UnsafeWithNetworkConn")
	defer func() { logger.Debugf(ctx, "/UnsafeWithNetworkConn: %v", _err) }()
	return xsync.DoA2R1(ctx, &o.formatContextLocker, o.unsafeWithNetworkConn, ctx, callback)
}

func (o *Output) verifyOpen(ctx context.Context) error {
	select {
	case <-o.openFinished:
		if o.openError != nil {
			return fmt.Errorf("output opening errored: %w", o.openError)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-o.CloseChan():
		return io.EOF
	default:
		return fmt.Errorf("not opened, yet")
	}
}

func (o *Output) unsafeWithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) (_err error) {
	if _, ok := ctx.Value(ctxKeyBypassIsOpenCheck).(struct{}); !ok {
		logger.Tracef(ctx, "checking whether the output is opened")
		if err := o.verifyOpen(ctx); err != nil {
			return fmt.Errorf("output is not opened: %w", err)
		}
	}

	fd, err := o.unsafeGetFileDescriptor(ctx)
	if err != nil {
		return fmt.Errorf("unable to get file descriptor: %w", err)
	}

	if o.URLParsed.Scheme == "srt" {
		return ErrNotImplemented{
			Err: fmt.Errorf("SRT is not supported for net.Conn wrapping yet"),
		}
	}

	logger.Tracef(ctx, "obtained file descriptor %d", fd)

	err = raw.WithTCPConnFromFD(ctx, fd, func(conn net.Conn) error {
		return callback(ctx, conn)
	})
	if err != nil {
		return fmt.Errorf("WithTCPConnFromFD(ctx, %d, ...) failed: %w", fd, err)
	}
	return nil
}

func (o *Output) UnsafeWithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) (_err error) {
	logger.Debugf(ctx, "UnsafeWithRawNetworkConn")
	defer func() { logger.Debugf(ctx, "/UnsafeWithRawNetworkConn: %v", _err) }()
	return xsync.DoA2R1(ctx, &o.formatContextLocker, o.unsafeWithRawNetworkConn, ctx, callback)
}

func (o *Output) unsafeWithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) (_err error) {
	return o.unsafeWithNetworkConn(ctx, func(ctx context.Context, conn net.Conn) error {
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
	_ GetInternalQueueSizer = (*Output)(nil)
	_ GetNetConner          = (*Output)(nil)
	_ GetSyscallRawConner   = (*Output)(nil)
)

// GetInternalQueueSize returns the size of internal queues used by the output.
//
// Warning! The implementation intrudes into private structures, which is unsafe.
func (o *Output) GetInternalQueueSize(
	ctx context.Context,
) (_ret map[string]uint64) {
	defer func() { logger.Tracef(ctx, "GetInternalQueueSize: %#+v", _ret) }()
	defer func() {
		if rec := recover(); rec != nil {
			logger.Debugf(ctx, "panic recovered in %s in GetInternalQueueSize: %v\n%s", o, rec, debug.Stack())
		}
	}()
	return xsync.DoA1R1(ctx, &o.formatContextLocker, o.unsafeGetInternalQueueSize, ctx)
}

func (o *Output) unsafeGetInternalQueueSize(
	ctx context.Context,
) map[string]uint64 {
	if _, ok := ctx.Value(ctxKeyBypassIsOpenCheck).(struct{}); !ok {
		logger.Tracef(ctx, "checking whether the output is opened")
		if err := o.verifyOpen(ctx); err != nil {
			logger.Debugf(ctx, "output is not opened: %v", err)
			return nil
		}
	}

	if o.proxy != nil {
		logger.Debugf(ctx, "getting the internal queue size from the proxy is not implemented, yet")
		return nil
	}

	switch o.URLParsed.Scheme {
	case "rtmp", "rtmps":
		return o.getInternalQueueSizeRTMP(ctx)
	default:
		logger.Debugf(ctx, "getting the internal queue size from '%s' is not implemented, yet", o.URLParsed.Scheme)
		return nil // not implemented, yet
	}
}

func (o *Output) UnsafeGetRawAVIOContext(
	ctx context.Context,
) *avcommon.AVIOContext {
	return xsync.DoA1R1(context.Background(), &o.formatContextLocker, o.unsafeGetRawAVIOContext, ctx)
}

func (o *Output) unsafeGetRawAVIOContext(
	ctx context.Context,
) *avcommon.AVIOContext {
	return o.unsafeGetRawAVFormatContext(ctx).Pb()
}

func (o *Output) UnsafeGetRawAVFormatContext(
	ctx context.Context,
) *avcommon.AVFormatContext {
	return xsync.DoA1R1(context.Background(), &o.formatContextLocker, o.unsafeGetRawAVFormatContext, ctx)
}

func (o *Output) unsafeGetRawAVFormatContext(
	ctx context.Context,
) *avcommon.AVFormatContext {
	return avcommon.WrapAVFormatContext(xastiav.CFromAVFormatContext(o.FormatContext))
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

func (o *Output) UnsafeGetRawURLContext(
	ctx context.Context,
) *avcommon.URLContext {
	return xsync.DoA1R1(ctx, &o.formatContextLocker, o.unsafeGetRawURLContext, ctx)
}

func (o *Output) unsafeGetRawURLContext(
	ctx context.Context,
) *avcommon.URLContext {
	avioCtx := o.unsafeGetRawAVIOContext(ctx)
	return avcommon.WrapURLContext(avioCtx.Opaque())
}

func (o *Output) UnsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	return xsync.DoA1R1(ctx, &o.formatContextLocker, o.unsafeGetRawTCPContext, ctx)
}

func (o *Output) unsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	switch o.URLParsed.Scheme {
	case "rtmp", "rtmps":
		if rtmp := o.unsafeGetRawRTMPContext(ctx); rtmp != nil {
			return rtmp.TCPContext()
		}
		return nil
	default:
		logger.Debugf(ctx, "getting the the TCP docket from '%s' (yet?)", o.URLParsed.Scheme)
		return nil
	}
}

func (o *Output) UnsafeGetRawRTMPContext(
	ctx context.Context,
) *avcommon.RTMPContext {
	return xsync.DoA1R1(ctx, &o.formatContextLocker, o.unsafeGetRawRTMPContext, ctx)
}

func (o *Output) unsafeGetRawRTMPContext(
	ctx context.Context,
) *avcommon.RTMPContext {
	urlCtx := o.unsafeGetRawURLContext(ctx)
	return avcommon.WrapRTMPContext(urlCtx.PrivData())
}

func (o *Output) getInternalQueueSizeRTMP(
	ctx context.Context,
) (_ret map[string]uint64) {
	defer func() { logger.Tracef(ctx, "getInternalQueueSizeRTMP: %#+v", _ret) }()

	avioCtx := o.unsafeGetRawAVIOContext(ctx)
	avioBytes := avioCtx.Buffer()
	result := map[string]uint64{
		"AVIOBuffered": uint64(len(avioBytes)),
	}

	tcpQueue := o.unsafeGetTCPSocketQueue(ctx)
	for k, v := range tcpQueue {
		result["TCP:"+k] = v
	}

	return result
}

var _ kerneltypes.UnsafeGetOldestDTSInTheQueuer = (*Output)(nil)

func (o *Output) UnsafeGetOldestDTSInTheQueue(
	ctx context.Context,
) (_ret time.Duration, _err error) {
	logger.Tracef(ctx, "UnsafeGetOldestDTSInTheQueue[%s]", o.URL)
	defer func() { logger.Tracef(ctx, "/UnsafeGetOldestDTSInTheQueue[%s]: %v %v", o.URL, _ret, _err) }()

	outTSs := o.outTSs.GetAll()

	queueSize := o.GetInternalQueueSize(ctx)
	if queueSize == nil {
		return 0, fmt.Errorf("unable to get the internal queue size")
	}

	var queueSizeTotal uint64
	for _, v := range queueSize {
		queueSizeTotal += v
	}
	logger.Tracef(ctx, "queueSizeTotal == %d", queueSizeTotal)

	if queueSizeTotal == 0 {
		for _, outTS := range slices.Backward(outTSs) {
			if outTS.DTS > 0 {
				return outTS.DTS, nil
			}
		}
		return 0, nil
	}

	accountedQueueSize := uint64(0)
	for idx, outTS := range slices.Backward(outTSs) {
		accountedQueueSize += outTS.PacketSize
		if accountedQueueSize >= queueSizeTotal && outTS.DTS > 0 {
			dts := outTS.DTS
			pts := outTS.PTS
			logger.Tracef(ctx, "accountedQueueSize == %d (/%d) at idx %d (/%d); dts == %v; pts == %v", accountedQueueSize, queueSizeTotal, len(outTSs)-1-idx, len(outTSs), dts, pts)
			return dts, nil
		}
	}

	if accountedQueueSize == 0 {
		return 0, fmt.Errorf("no information about sent packets' DTSs")
	}

	var oldestDTS, earliestDTS time.Duration
	for _, outTS := range outTSs {
		if outTS.DTS > 0 {
			oldestDTS = outTS.DTS
			break
		}
	}
	for _, outTS := range slices.Backward(outTSs) {
		if outTS.DTS > 0 {
			earliestDTS = outTS.DTS
			break
		}
	}

	knownLatency := earliestDTS - oldestDTS
	k := float64(queueSizeTotal) / float64(accountedQueueSize)
	estimatedLatency := time.Duration(float64(knownLatency) * k)
	logger.Tracef(ctx, "oldestDTS=%v earliestDTS=%v knownLatency=%v accountedQueueSize=%d k=%v estimatedLatency=%v", oldestDTS, earliestDTS, knownLatency, accountedQueueSize, k, estimatedLatency)

	if estimatedLatency < 0 {
		return 0, fmt.Errorf("calculated negative estimated latency: %v (%v * %v)", estimatedLatency, knownLatency, k)
	}

	return earliestDTS - estimatedLatency, ErrApproximateValue{}
}

type ErrApproximateValue struct{}

func (e ErrApproximateValue) Error() string {
	return "approximate value"
}
