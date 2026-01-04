// udp.go provides general UDP-related helper functions.

package kernel

import (
	"context"
	"fmt"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avcommon"
	xastiav "github.com/xaionaro-go/avcommon/astiav"
)

/*
// See file libavformat/udp.c in the ffmpeg source code
typedef struct UDPContext {
    const void *class;
    int udp_fd;
} UDPContext;
*/
import "C"

func formatContextToUDPFD(
	ctx context.Context,
	fmtCtx *astiav.FormatContext,
) (int, error) {
	f := avcommon.WrapAVFormatContext(xastiav.CFromAVFormatContext(fmtCtx))
	avioCtx := f.Pb()
	if avioCtx == nil {
		return 0, fmt.Errorf("avioCtx is nil")
	}
	urlCtx := avcommon.WrapURLContext(avioCtx.Opaque())
	if urlCtx == nil {
		return 0, fmt.Errorf("urlCtx is nil")
	}
	if urlCtx.Protocol().Name() != "udp" {
		return 0, fmt.Errorf("expected protocol 'udp', but got '%s'", urlCtx.Protocol().Name())
	}
	privData := urlCtx.PrivData()
	if privData == nil {
		return 0, fmt.Errorf("privData is nil")
	}
	udpCtx := (*C.UDPContext)(privData.UnsafePointer())
	return int(udpCtx.udp_fd), nil
}

func (n *netConn) unsafeGetRawUDPContext(
	ctx context.Context,
) *C.UDPContext {
	urlCtx := n.unsafeGetRawURLContext(ctx)
	if urlCtx == nil {
		return nil
	}
	privData := urlCtx.PrivData()
	if privData == nil {
		return nil
	}
	return (*C.UDPContext)(privData.UnsafePointer())
}

func (n *netConn) GetRawUDPContext(
	ctx context.Context,
) *C.UDPContext {
	n.locker.ManualRLock(ctx)
	defer n.locker.ManualRUnlock(ctx)
	return n.unsafeGetRawUDPContext(ctx)
}
