package kernel

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"time"

	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
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
) (_ret map[string]uint64) {
	defer func() { logger.Tracef(ctx, "GetInternalQueueSize: %#+v", _ret) }()
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
		logger.Debugf(ctx, "getting the internal queue size from '%s' is not implemented, yet", r.URLParsed.Scheme)
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
) (_ret map[string]uint64) {
	defer func() { logger.Tracef(ctx, "getInternalQueueSizeRTMP: %#+v", _ret) }()

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

var _ kerneltypes.UnsafeGetOldestDTSInTheQueuer = (*Output)(nil)

func (r *Output) UnsafeGetOldestDTSInTheQueue(
	ctx context.Context,
) (_ret time.Duration, _err error) {
	logger.Tracef(ctx, "UnsafeGetOldestDTSInTheQueue[%s]", r.URL)
	defer func() { logger.Tracef(ctx, "/UnsafeGetOldestDTSInTheQueue[%s]: %v %v", r.URL, _ret, _err) }()

	outTSs := r.outTSs.GetAll()

	queueSize := r.GetInternalQueueSize(ctx)
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
