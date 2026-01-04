// original_packet_source.go defines the OriginalPacketSourcer interface.

package types

import (
	"github.com/xaionaro-go/avpipeline/packet"
)

// OriginalPacketSourcer is an optional interface that wrapper kernel types
// (like Retryable, Chain, etc.) can implement to expose the actual underlying
// packet source. This is used to get the real packet source kernel instead of
// the wrapper.
type OriginalPacketSourcer interface {
	// OriginalPacketSource returns the underlying packet source, or nil if
	// there is no underlying source available (e.g., kernel not ready).
	OriginalPacketSource() packet.Source
}

// GetOriginalPacketSource recursively unwraps wrapper types to get the actual source.
// Returns nil if any wrapper in the chain returns nil (inner source not available).
func GetOriginalPacketSource(src packet.Source) packet.Source {
	for {
		unwrapper, ok := src.(OriginalPacketSourcer)
		if !ok {
			return src
		}
		inner := unwrapper.OriginalPacketSource()
		if inner == nil {
			return nil
		}
		src = inner
	}
}
