// net_conn_test.go covers the post-close contract of netConn.

package kernel

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"

	testassert "github.com/stretchr/testify/assert"
	testrequire "github.com/stretchr/testify/require"
)

// fakeRawConn is a syscall.RawConn placeholder for unit tests; only its
// non-nil-ness matters for the netConn.closeLocked behavior under test.
type fakeRawConn struct{}

func (fakeRawConn) Control(func(uintptr)) error { return nil }
func (fakeRawConn) Read(func(uintptr) bool) error {
	return errors.New("not implemented")
}
func (fakeRawConn) Write(func(uintptr) bool) error {
	return errors.New("not implemented")
}

// TestNetConnCloseClearsBorrowedHandles asserts that after Close,
// netConn returns ErrNoNetworkConn / ErrNoRawNetworkConn (not io.EOF)
// to its public probes — the contract the autobitrate handler relies
// on to classify steady-state probes of stopped outputs as benign.
//
// netFile is left nil on purpose: in production it is a dup'd fd whose
// closure is exercised by the kernel-level integration tests; the
// post-close error contract under test here is independent of the file
// having ever been opened (the rawConn nil-check fires first in
// withRawNetworkConnLocked).
func TestNetConnCloseClearsBorrowedHandles(t *testing.T) {
	ctx := context.Background()

	n := &netConn{
		netConn:      &net.TCPConn{},
		rawConn:      fakeRawConn{},
		networkName:  "tcp",
		protocolName: "rtmp",
	}

	testrequire.NoError(t, n.Close(ctx))

	testassert.Nil(t, n.netConn, "netConn must be cleared post-close")
	testassert.Nil(t, n.rawConn, "rawConn must be cleared post-close")
	testassert.Nil(t, n.avioCtx, "avioCtx must be cleared post-close")

	errNet := n.WithNetworkConn(ctx, func(context.Context, net.Conn) error {
		t.Fatal("callback must not be invoked after Close")
		return nil
	})
	testassert.ErrorAs(t, errNet, &ErrNoNetworkConn{},
		"WithNetworkConn must surface ErrNoNetworkConn (not io.EOF) after Close")

	errRaw := n.WithRawNetworkConn(ctx, func(context.Context, syscall.RawConn, string) error {
		t.Fatal("callback must not be invoked after Close")
		return nil
	})
	testassert.ErrorAs(t, errRaw, &ErrNoRawNetworkConn{},
		"WithRawNetworkConn must surface ErrNoRawNetworkConn (not io.EOF) after Close")

	// Idempotent: a second Close is a no-op and does not panic on the
	// already-cleared fields.
	testrequire.NoError(t, n.Close(ctx))
}
