package kernel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"syscall"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
	"github.com/xaionaro-go/avpipeline/logger"
	netraw "github.com/xaionaro-go/avpipeline/net/raw"
	tcpopt "github.com/xaionaro-go/tcp/opt"
	"github.com/xaionaro-go/xsync"
)

type netConn struct {
	locker       xsync.RWMutex
	netConn      net.Conn
	netFile      *os.File
	rawConn      syscall.RawConn
	networkName  string
	protocolName string
	avioCtx      *avcommon.AVIOContext
}

func (n *netConn) Init(
	ctx context.Context,
	formatContext *astiav.FormatContext,
) {
	n.locker.ManualLock(ctx)
	defer n.locker.ManualUnlock(ctx)

	n.avioCtx = n.unsafeGetRawAVIOContext(ctx, formatContext)
	urlCtx := n.unsafeGetRawURLContext(ctx)
	if urlCtx == nil {
		return
	}
	n.protocolName = urlCtx.Protocol().Name()

	switch n.protocolName {
	case "rtmp", "rtmps", "srt", "libsrt", "udp":
	default:
		return
	}

	fd, err := n.unsafeGetFileDescriptor(ctx, formatContext)
	if err != nil {
		logger.Debugf(ctx, "unable to get FD: %v", err)
		return
	}

	conn, f, err := netraw.ConnFromFD(ctx, fd)
	if err != nil {
		logger.Debugf(ctx, "unable to get net.Conn from FD %d: %v", fd, err)
		return
	}

	n.netConn = conn
	n.netFile = f
	switch n.protocolName {
	case "rtmp", "rtmps":
		n.networkName = "tcp"
	case "srt", "libsrt", "udp":
		n.networkName = "udp"
	default:
		logger.Errorf(ctx, "unknown protocol '%s'", n.protocolName)
	}
	if conn.RemoteAddr() != nil {
		n.networkName = conn.RemoteAddr().Network()
	}
	if rawConner, ok := conn.(syscall.Conn); ok {
		rawConn, err := rawConner.SyscallConn()
		if err == nil {
			n.rawConn = rawConn
		}
	}

	if err := n.verify(ctx); err != nil {
		logger.Debugf(ctx, "verification failed: %v", err)
		n.closeLocked(ctx)
	}
}

func (n *netConn) verify(context.Context) error {
	if n.netConn == nil {
		return fmt.Errorf("netConn is nil")
	}
	if n.rawConn == nil {
		return fmt.Errorf("rawConn is nil")
	}
	if n.avioCtx == nil {
		return fmt.Errorf("avioCtx is nil")
	}

	if n.avioCtx.Opaque() == nil {
		return fmt.Errorf("avioCtx.Opaque() is nil")
	}

	if n.netConn.LocalAddr() == nil {
		return fmt.Errorf("local address is nil")
	}

	var verifyErr error
	err := n.rawConn.Control(func(fd uintptr) {
		soType, err := syscall.GetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_TYPE)
		if err != nil {
			verifyErr = fmt.Errorf("getsockopt(SO_TYPE) failed: %w", err)
			return
		}

		switch n.protocolName {
		case "rtmp", "rtmps":
			if soType != syscall.SOCK_STREAM {
				verifyErr = fmt.Errorf("expected SOCK_STREAM for protocol %s, but got %d", n.protocolName, soType)
			}
		case "srt", "libsrt", "udp":
			if soType != syscall.SOCK_DGRAM {
				verifyErr = fmt.Errorf("expected SOCK_DGRAM for protocol %s, but got %d", n.protocolName, soType)
			}
		}
	})
	if err != nil {
		return fmt.Errorf("rawConn.Control failed: %w", err)
	}
	if verifyErr != nil {
		return verifyErr
	}

	return nil
}

func (n *netConn) closeLocked(context.Context) error {
	var result []error
	if n.netFile != nil {
		if err := n.netFile.Close(); err != nil {
			result = append(result, fmt.Errorf("unable to close the network file: %w", err))
		}
		n.netFile = nil
	}
	n.netConn = nil
	n.rawConn = nil
	return errors.Join(result...)
}

func (n *netConn) Close(ctx context.Context) error {
	n.locker.ManualLock(ctx)
	defer n.locker.ManualUnlock(ctx)
	return n.closeLocked(ctx)
}

func (n *netConn) withNetworkConnLocked(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) error {
	if n.netConn == nil {
		return fmt.Errorf("network connection is not available")
	}

	return callback(ctx, n.netConn)
}

func (n *netConn) WithNetworkConn(
	ctx context.Context,
	callback func(context.Context, net.Conn) error,
) error {
	n.locker.ManualRLock(ctx)
	defer n.locker.ManualRUnlock(ctx)
	return n.withNetworkConnLocked(ctx, callback)
}

func (n *netConn) withRawNetworkConnLocked(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) error {
	if n.rawConn == nil {
		return fmt.Errorf("raw network connection is not available")
	}

	return callback(ctx, n.rawConn, n.networkName)
}

func (n *netConn) WithRawNetworkConn(
	ctx context.Context,
	callback func(context.Context, syscall.RawConn, string) error,
) error {
	n.locker.ManualRLock(ctx)
	defer n.locker.ManualRUnlock(ctx)
	return n.withRawNetworkConnLocked(ctx, callback)
}

func (n *netConn) unsafeGetInternalQueueSizeRTMP(
	ctx context.Context,
) (_ret map[string]uint64) {
	if n.avioCtx == nil {
		return nil
	}
	avioBytes := n.avioCtx.Buffer()
	result := map[string]uint64{
		"AVIOBuffered": uint64(len(avioBytes)),
	}

	tcpQueue := n.getTCPSocketQueueLocked(ctx)
	for k, v := range tcpQueue {
		result["TCP:"+k] = v
	}

	return result
}

func (n *netConn) unsafeGetInternalQueueSizeUDP(
	ctx context.Context,
) (_ret map[string]uint64) {
	if n.avioCtx == nil {
		return nil
	}
	avioBytes := n.avioCtx.Buffer()
	result := map[string]uint64{
		"AVIOBuffered": uint64(len(avioBytes)),
	}

	udpQueue := n.getTCPSocketQueueLocked(ctx)
	for k, v := range udpQueue {
		result["UDP:"+k] = v
	}

	return result
}

func (n *netConn) getInternalQueueSizeLocked(
	ctx context.Context,
) map[string]uint64 {
	switch n.protocolName {
	case "rtmp", "rtmps":
		return n.unsafeGetInternalQueueSizeRTMP(ctx)
	case "udp":
		return n.unsafeGetInternalQueueSizeUDP(ctx)
	default:
		logger.Debugf(ctx, "getting the internal queue size from '%s' is not implemented, yet", n.protocolName)
		return nil
	}
}

func (n *netConn) GetInternalQueueSize(
	ctx context.Context,
) map[string]uint64 {
	n.locker.ManualRLock(ctx)
	defer n.locker.ManualRUnlock(ctx)
	return n.getInternalQueueSizeLocked(ctx)
}

func (n *netConn) unsafeGetFileDescriptor(
	ctx context.Context,
	formatContext *astiav.FormatContext,
) (int, error) {
	urlCtx := n.unsafeGetRawURLContext(ctx)
	if urlCtx == nil {
		return 0, fmt.Errorf("unable to get URLContext")
	}
	protocolName := urlCtx.Protocol().Name()

	var fd int
	var err error
	switch protocolName {
	case "rtmp", "rtmps":
		if tcpCtx := n.unsafeGetRawTCPContext(ctx); tcpCtx != nil {
			fd = tcpCtx.FileDescriptor()
		} else {
			return 0, fmt.Errorf("unable to get an RTMP context (protocol: %s)", protocolName)
		}
	case "srt", "libsrt":
		fd, err = formatContextToSRTFD(ctx, formatContext)
	case "udp":
		fd, err = formatContextToUDPFD(ctx, formatContext)
	default:
		return 0, fmt.Errorf("do not know how to obtain the file descriptor for network protocol '%s'", protocolName)
	}
	if err != nil {
		return 0, err
	}
	if fd < 0 {
		return 0, fmt.Errorf("invalid file descriptor: %d", fd)
	}
	return fd, nil
}

func (n *netConn) unsafeGetRawAVIOContext(
	_ context.Context,
	formatContext *astiav.FormatContext,
) *avcommon.AVIOContext {
	return avcommon.WrapAVFormatContext(xastiav.CFromAVFormatContext(formatContext)).Pb()
}

func (n *netConn) unsafeGetRawURLContext(
	_ context.Context,
) *avcommon.URLContext {
	if n.avioCtx == nil {
		return nil
	}
	return avcommon.WrapURLContext(n.avioCtx.Opaque())
}

func (n *netConn) unsafeGetRawTCPContext(
	ctx context.Context,
) *avcommon.TCPContext {
	switch n.protocolName {
	case "rtmp", "rtmps":
		if rtmp := n.unsafeGetRawRTMPContext(ctx); rtmp != nil {
			return rtmp.TCPContext()
		}
		return nil
	case "udp", "srt", "libsrt":
		return nil
	default:
		logger.Debugf(ctx, "getting the the TCP docket from '%s' (yet?)", n.protocolName)
		return nil
	}
}

func (n *netConn) unsafeGetRawRTMPContext(
	ctx context.Context,
) *avcommon.RTMPContext {
	urlCtx := n.unsafeGetRawURLContext(ctx)
	if urlCtx == nil {
		return nil
	}
	return avcommon.WrapRTMPContext(urlCtx.PrivData())
}

func (n *netConn) setRecvBufferSizeLocked(
	ctx context.Context,
	size uint,
) (_err error) {
	return n.withRawNetworkConnLocked(
		ctx,
		func(ctx context.Context, rawConn syscall.RawConn, _ string) error {
			return netraw.SetTCPSockOption(ctx, rawConn, tcpopt.ReceiveBuffer(size))
		},
	)
}

func (n *netConn) SetRecvBufferSize(
	ctx context.Context,
	size uint,
) (_err error) {
	n.locker.ManualRLock(ctx)
	defer n.locker.ManualRUnlock(ctx)
	return n.setRecvBufferSizeLocked(ctx, size)
}

func (n *netConn) setSendBufferSizeLocked(
	ctx context.Context,
	size uint,
) (_err error) {
	return n.withRawNetworkConnLocked(
		ctx,
		func(ctx context.Context, rawConn syscall.RawConn, _ string) error {
			return netraw.SetTCPSockOption(ctx, rawConn, tcpopt.SendBuffer(size))
		},
	)
}

func (n *netConn) SetSendBufferSize(
	ctx context.Context,
	size uint,
) (_err error) {
	n.locker.ManualRLock(ctx)
	defer n.locker.ManualRUnlock(ctx)
	return n.setSendBufferSizeLocked(ctx, size)
}

func (n *netConn) getTCPSocketQueueLocked(
	ctx context.Context,
) map[string]uint64 {
	return getTCPSocketQueueOS(ctx, n.rawConn)
}

func (n *netConn) getOldestDTSInTheQueueLocked(
	ctx context.Context,
	url string,
	outTSs []outTS,
) (_ret time.Duration, _err error) {
	logger.Tracef(ctx, "GetOldestDTSInTheQueue[%s]", url)
	defer func() { logger.Tracef(ctx, "/GetOldestDTSInTheQueue[%s]: %v %v", url, _ret, _err) }()

	queueSize := n.getInternalQueueSizeLocked(ctx)
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

func (n *netConn) GetOldestDTSInTheQueue(
	ctx context.Context,
	url string,
	outTSs []outTS,
) (_ret time.Duration, _err error) {
	n.locker.ManualRLock(ctx)
	defer n.locker.ManualRUnlock(ctx)
	return n.getOldestDTSInTheQueueLocked(ctx, url, outTSs)
}

type ErrApproximateValue struct{}

func (e ErrApproximateValue) Error() string {
	return "approximate value"
}
