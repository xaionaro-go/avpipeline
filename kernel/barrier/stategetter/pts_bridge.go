// pts_bridge.go implements a per-chain PTS offset bridge that runs inside
// SwitchOutput.GetState when SwitchFlagBridgePTSAcrossChains is set.
//
// Motivation: an InputSwitch routes packets from one of several upstream
// chains. Different chains may live in unrelated PTS clock domains
// (e.g. an rtmp upstream that has been streaming for 10 minutes vs a
// freshly-opened camera+mic on a shared monotonic epoch). When the active
// chain flips, the raw PTS sequence at the Switch's output suddenly jumps —
// often by hundreds of seconds. Players freeze on such jumps; MonotonicPTS
// downstream only absorbs the small-PTS direction. The bridge restores
// continuity by, on each chain change, computing
//
//	offset_new = lastEmittedPTS_old - firstObservedPTS_new + 1 tick
//
// per-stream (keyed by media type) and applying it to all subsequent packets
// from the new chain until the next switch.
//
// All arithmetic is done in integer ticks via astiav.RescaleQ to avoid the
// float-precision loss that an intermediate time.Duration round-trip would
// introduce on common video timebases (e.g. 1/90000, 1/44100).

package stategetter

import (
	"sync"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// streamKey is the identity used to match a stream across chains. Media type
// is the right join key here — the routing target is "video continues video,
// audio continues audio" — independent of the upstream's local stream index.
// Falling back to the local index when MediaType is Unknown keeps synthetic
// or codec-unannounced packets working in isolation per chain.
type streamKey struct {
	mediaType   astiav.MediaType
	streamIndex int // only consulted when mediaType == MediaTypeUnknown
}

func keyOf(in packetorframe.InputUnion) streamKey {
	mt := in.GetMediaType()
	k := streamKey{mediaType: mt}
	if mt == astiav.MediaTypeUnknown {
		k.streamIndex = in.GetStreamIndex()
	}
	return k
}

// chainStreamKey identifies (chain, stream) — needed because each chain owns
// its own offset and its own last-emitted bookkeeping per stream.
type chainStreamKey struct {
	chainID int32
	stream  streamKey
}

// lastEmitted captures the post-bridge PTS most recently produced for a
// (chain, stream), in the chain's native timebase. Storing the tb alongside
// the integer tick count lets cross-chain rebase use astiav.RescaleQ for
// lossless conversion when the new chain happens to use a different tb.
type lastEmitted struct {
	ticks int64
	tb    astiav.Rational
}

// chainOffset is the per-(chain, stream) PTS shift, in the new chain's tb,
// bound on the first observed packet after a chain change. Once set it is
// applied verbatim to subsequent packets from that chain until the next
// chain change for this stream.
type chainOffset struct {
	ticks int64
	tb    astiav.Rational // tb the offset was computed in
	bound bool
}

// ptsBridge holds the cross-chain offset state. All access goes through mu;
// the bridge runs on the SendInput goroutine of the upstream node and the
// SwitchOutput.GetState callsite, which are concurrent for sibling chains.
type ptsBridge struct {
	mu sync.Mutex

	lastEmitted        map[chainStreamKey]lastEmitted
	offset             map[chainStreamKey]chainOffset
	lastChainForStream map[streamKey]int32
}

func newPTSBridge() *ptsBridge {
	return &ptsBridge{
		lastEmitted:        make(map[chainStreamKey]lastEmitted),
		offset:             make(map[chainStreamKey]chainOffset),
		lastChainForStream: make(map[streamKey]int32),
	}
}

// apply mutates in.PTS / DTS to maintain a monotonic, gap-free continuation
// across chain changes. Returns silently for packets that have no PTS
// (NoPtsValue) or no usable timebase — those carry no temporal information
// and bind no offset.
func (b *ptsBridge) apply(chainID int32, in packetorframe.InputUnion) {
	ptsRaw := in.GetPTS()
	if ptsRaw == astiav.NoPtsValue {
		return
	}
	tb := in.GetTimeBase()
	if tb.Num() == 0 || tb.Den() == 0 {
		return
	}

	stream := keyOf(in)
	csk := chainStreamKey{chainID: chainID, stream: stream}

	b.mu.Lock()
	defer b.mu.Unlock()

	prevChain, hasPrevChain := b.lastChainForStream[stream]
	off := b.offset[csk]

	if hasPrevChain && prevChain != chainID && !off.bound {
		// chain changed for this stream — bind a fresh offset.
		oldKey := chainStreamKey{chainID: prevChain, stream: stream}
		if last, ok := b.lastEmitted[oldKey]; ok {
			lastInCurTB := last.ticks
			if last.tb != tb {
				lastInCurTB = astiav.RescaleQ(last.ticks, last.tb, tb)
			}
			off = chainOffset{
				ticks: lastInCurTB - ptsRaw + 1,
				tb:    tb,
				bound: true,
			}
			b.offset[csk] = off
		}
		// stale-offset cleanup: any other (chainID, stream') entries left over
		// from a previous activation of this chain are no longer guaranteed
		// monotonic vs the rest of the pipeline; force re-bind by deleting.
		// Skip the entry we just set above and any stream' for which this
		// chain is still the "last emitter" (offset still fresh).
		for k := range b.offset {
			if k.chainID != chainID || k == csk {
				continue
			}
			if lc, ok := b.lastChainForStream[k.stream]; ok && lc == chainID {
				continue
			}
			delete(b.offset, k)
		}
	}

	if off.bound {
		// rescale offset to current tb if the chain has heterogeneous tbs.
		shift := off.ticks
		if off.tb != tb {
			shift = astiav.RescaleQ(off.ticks, off.tb, tb)
		}
		if shift != 0 {
			in.SetPTS(ptsRaw + shift)
			dts := in.GetDTS()
			if dts != astiav.NoPtsValue {
				in.SetDTS(dts + shift)
			}
		}
	}

	b.lastEmitted[csk] = lastEmitted{ticks: in.GetPTS(), tb: tb}
	b.lastChainForStream[stream] = chainID
}

// resetForChain forgets all per-stream state owned by chainID. Called
// when the chain reopens (e.g. inputwithfallback Retryable EOF→retry):
// the next packet from the freshly-opened input must re-bind its
// offset against whichever chain emitted last, so we drop the stale
// offset and lastEmitted entries keyed by chainID, and clear the
// lastChainForStream pointer when it still points at the resetting
// chain (so prevChain != chainID becomes true on the next packet,
// triggering the rebind branch in apply).
func (b *ptsBridge) resetForChain(chainID int32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.offset {
		if k.chainID == chainID {
			delete(b.offset, k)
		}
	}
	for k := range b.lastEmitted {
		if k.chainID == chainID {
			delete(b.lastEmitted, k)
		}
	}
	for stream, lc := range b.lastChainForStream {
		if lc == chainID {
			delete(b.lastChainForStream, stream)
		}
	}
}
