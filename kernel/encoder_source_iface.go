// encoder_source_iface.go declares the encoder-side source interfaces that
// streamEncoder hooks against. Declaring the dependency in the kernel
// package replaces the previous duck-typed cross-package assertion against
// codec.GetDecoderer at the encoder.go HW-frame transfer / tight-packing
// sites — the dependency is now stated, not inferred.

package kernel

import (
	"github.com/xaionaro-go/avpipeline/codec"
)

// HardwareSourceFramesContextProvider is implemented by frame.Source
// values that can supply an upstream decoder. The streamEncoder uses
// this to:
//
//   - Borrow the upstream decoder's hw_frames_ctx when the input frame
//     has a HW pixel format but no attached context (the mediacodec-
//     decoder case).
//
//   - Detect a mediacodec-decoded source so the tight-packing rescaler
//     gate can engage (see encoder.go's encoderRescaleEnableTightPacking
//     branch).
//
// Structurally identical to codec.GetDecoderer — the duplication is
// intentional: assertion against a kernel-local name keeps the
// dependency declared inside the package that owns the consumer code,
// instead of being a duck-typed reach into a sibling package. Existing
// implementations of GetDecoder() *codec.Decoder
// (kernel/decoder.go's decoderAsSource, kernel/map_stream_indices.go)
// satisfy this interface automatically.
type HardwareSourceFramesContextProvider interface {
	GetDecoder() *codec.Decoder
}
