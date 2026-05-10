// sending_node.go defines the interface for sending nodes in the stream muxer.

package streammux

import (
	"context"

	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

type SendingNode[C any] interface {
	node.Abstract
	SetCustomData(v OutputCustomData[C])
	GetCustomData() OutputCustomData[C]
}

type SetDropOnCloser interface {
	SetDropOnClose(ctx context.Context, v bool) error
}

type SenderFactory[C any] interface {
	NewSender(
		ctx context.Context,
		senderKey SenderKey,
	) (SendingNode[C], types.SenderConfig, error)
}

// SenderURLPreviewer is the optional capability a SenderFactory may
// implement to expose the URL it would generate for a given senderKey
// WITHOUT actually constructing a sender. The streammux Reuse path
// uses this to detect URL drift between the existing reused output's
// URL and the URL the factory would now produce — when they differ,
// the existing output is torn down so the next CreationActionCreate
// path picks up the new URL via NewSender.
//
// Without this capability, SetOutputURL+SwitchOutputByProps with the
// same codec props (same senderKey) takes the Reuse path and silently
// retains the old sender URL — see Task #174 RC analysis.
//
// Implementations may return ("", nil) when the URL cannot be
// previewed (e.g. template not yet configured); callers treat empty
// URL as "drift detection not applicable" and fall through to the
// regular Reuse path.
type SenderURLPreviewer interface {
	URLForKey(
		ctx context.Context,
		senderKey SenderKey,
	) (string, error)
}

type ErrNoSetDropOnClose struct{}

func (e ErrNoSetDropOnClose) Error() string {
	return "sending node does not implement SetDropOnCloser"
}

func sendingNodeSetDropOnClose[C any](
	ctx context.Context,
	sendingNode SendingNode[C],
	v bool,
) error {
	s, ok := sendingNode.(SetDropOnCloser)
	if !ok {
		return ErrNoSetDropOnClose{}
	}
	return s.SetDropOnClose(ctx, v)
}
