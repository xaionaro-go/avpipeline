// intra_only_codec.go implements a Condition that matches packets/frames
// whose codec is in codec.IsIntraOnlyCodec (SSOT for "every packet is a
// keyframe").
//
// Use this instead of OR-ing CodecID(rawvideo) | CodecID(wrapped_avframe)
// at every keep-unless / switch-anchor call site — the single helper
// guarantees streammux, inputwithfallback's InputSwitch, and
// inputwithfallback's Syncer keep-unless can never silently diverge on
// the intra-only set, which is exactly what happened pre-consolidation
// (inputwithfallback omitted CodecIDWrappedAvframe and silently rejected
// lavfi/test-source switch anchors).
//
// See codec/intra_only.go for the case list.

package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// IsIntraOnlyCodec matches inputs whose codec is in codec.IsIntraOnlyCodec
// (rawvideo, wrapped_avframe). nil CodecParameters never match.
type IsIntraOnlyCodec struct{}

var _ Condition = IsIntraOnlyCodec{}

func (IsIntraOnlyCodec) String() string {
	return "IsIntraOnlyCodec"
}

func (IsIntraOnlyCodec) Match(ctx context.Context, in packetorframe.InputUnion) bool {
	cp := in.GetCodecParameters()
	if cp == nil {
		return false
	}
	return codec.IsIntraOnlyCodec(cp.CodecID())
}

// compile-time assertion that the formatted name stays human-readable.
var _ fmt.Stringer = IsIntraOnlyCodec{}
