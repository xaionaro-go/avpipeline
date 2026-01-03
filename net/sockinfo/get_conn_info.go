package sockinfo

import (
	"context"
	"fmt"
	"syscall"
)

func GetRawConnInfo(
	ctx context.Context,
	rawConn syscall.RawConn,
	net string,
) (SockInfo, error) {
	switch net {
	case "tcp", "tcp4", "tcp6":
		return getRawConnInfoTCP(ctx, rawConn)
	default:
		return nil, ErrNotImplemented{
			Err: fmt.Errorf("raw connection info for network %q is not implemented", net),
		}
	}
}
