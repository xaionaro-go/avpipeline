// intra_only.go centralises the "every packet is a keyframe" predicate
// used across the pipeline (decoder, syncer keep-unless, OutputSwitch
// keep-unless, fallback InputSwitch keep-unless).
//
// Why a SSOT helper: prior to this consolidation the same case-list
// (rawvideo, wrapped_avframe) was hand-mirrored across at least three
// presets, and inputwithfallback's two switches accidentally diverged
// from streammux — only rawvideo, no wrapped_avframe — silently
// suppressing wrapped_avframe sources from ever committing a switch
// while looking like the matching test "passed". Centralising the
// list here eliminates that drift class entirely.
//
// Definition of "intra-only" for our purposes: codecs where every
// packet/frame is independently decodable (no inter-frame prediction),
// so libav demuxers may legitimately omit AV_PKT_FLAG_KEY on every
// packet. Pipeline stages that gate on "is this a switch anchor /
// keyframe" must accept these packets even when the flag is missing.
//
//   astiav.CodecIDRawvideo        — uncompressed, every packet self-contained.
//   astiav.CodecIDWrappedAvframe  — lavfi/test source carrier; every
//                                    packet contains one fully-decoded
//                                    AVFrame.
//
// Adding a new intra-only codec? Add it here once and every call site
// picks it up. Do NOT case-match these CodecIDs inline at call sites.

package codec

import "github.com/asticode/go-astiav"

// IsIntraOnlyCodec reports whether codecID names a codec whose packets
// are individually decodable without reference to previous packets. See
// the package-level commentary above for the definition and rationale.
func IsIntraOnlyCodec(codecID astiav.CodecID) bool {
	switch codecID {
	case astiav.CodecIDRawvideo, astiav.CodecIDWrappedAvframe:
		return true
	default:
		return false
	}
}
