// avsynccondition.go adapts a *kernel.AVSync to the
// packetorframefiltercondition.Condition interface so it can be
// installed as a PushTo Condition on a node edge.

// Package avsynccondition adapts kernel.AVSync to the
// packetorframefilter Condition interface.
//
// AVSync is structurally a kernel — kernel.Abstract — but the desired
// wiring on the avd side installs it as a PushTo Condition on the edge
// feeding the per-forwarding Output, so observation+mutation runs as a
// side-effect of edge evaluation without inserting another node into
// the chain. The kernel.Abstract conformance stays vestigial for
// type-compat with existing FilterKernelFactory paths but is unused
// here.
//
// The kernel package itself cannot import
// node/filter/packetorframefilter/condition because that package
// transitively imports node/filter -> processor -> kernel — an import
// cycle. This thin sibling package owns the adapter.
package avsynccondition

import (
	"context"

	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
)

// Condition installs a *kernel.AVSync as a
// packetorframefiltercondition.Condition.
//
// Match always returns true — the predicate role is
// observation+mutation, not filtering. Match calls
// AVSync.ApplyAndObserve on a non-empty packet input, which mutates
// PTS/DTS in place via the union's pointer-typed *packet.Input.
type Condition struct {
	AVSync *kernel.AVSync
}

var _ packetorframefiltercondition.Condition = (*Condition)(nil)
var _ kerneltypes.Resetter = (*Condition)(nil)

func New(s *kernel.AVSync) *Condition {
	return &Condition{AVSync: s}
}

// String delegates to the wrapped AVSync's String — the package name
// already says "AVSync condition", so no extra prefix is needed.
func (c *Condition) String() string {
	return c.AVSync.String()
}

// Reset clears the wrapped AVSync's per-stream observation state. It
// is invoked by chain teardown paths so the next chain starts with a
// clean max-PTS baseline. Operator-configured offsets on the wrapped
// AVSync are preserved by AVSync.Reset.
func (c *Condition) Reset(ctx context.Context) error {
	return c.AVSync.Reset(ctx)
}

// Match observes and mutates the inbound packet via AVSync, then
// returns true. Frames and empty unions are no-ops (still return true).
func (c *Condition) Match(
	ctx context.Context,
	in packetorframefiltercondition.Input,
) bool {
	// Skip empty unions and frames: AVSync only operates on packets.
	// (Frames have no PTS in the AVSync model — the kernel's media-type
	// guard would reject them anyway, but we filter early to avoid the
	// nil-Get() dispatch panic.)
	if in.Input.Packet == nil {
		return true
	}
	c.AVSync.ApplyAndObserve(ctx, &in.Input)
	return true
}
