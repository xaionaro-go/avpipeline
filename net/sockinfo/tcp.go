package sockinfo

import (
	"context"
	"fmt"
	"syscall"
	"time"
	_ "unsafe"

	"github.com/xaionaro-go/avpipeline/net/raw"
	tcpinfo "github.com/xaionaro-go/tcp/info"
)

type TCP tcpinfo.Info

var _ SockInfo = (*TCP)(nil)

func (s *TCP) GetRTT() (time.Duration, error) {
	return s.RTT, nil
}

func getRawConnInfoTCP(
	_ context.Context,
	rawConn syscall.RawConn,
) (SockInfo, error) {
	opt, err := raw.GetOption(rawConn, (*tcpinfo.Info)(nil))
	if err != nil {
		return nil, fmt.Errorf("unable to get TCP info: %w", err)
	}
	tcpInfo, ok := opt.(*tcpinfo.Info)
	if !ok {
		return nil, fmt.Errorf("unexpected option type: %T", opt)
	}

	return (*TCP)(tcpInfo), nil
}
