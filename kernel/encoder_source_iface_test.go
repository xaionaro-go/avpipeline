// encoder_source_iface_test.go pins that the kernel-local
// HardwareSourceFramesContextProvider interface is satisfied by the
// expected source implementations. The compile-time _ assignments here
// are the load-bearing assertions; if a new source type or a refactor
// stops satisfying the interface, this file fails to compile and the
// regression is caught at build time, before runtime.

package kernel

import (
	"github.com/xaionaro-go/avpipeline/codec"
)

// Compile-time assertions: known kernel-side frame.Source impls satisfy
// HardwareSourceFramesContextProvider so the encoder.go assertion sites
// (HW->SW transfer, tight-pack rescaler gate) succeed without depending
// on the codec package's GetDecoderer name.
var (
	_ HardwareSourceFramesContextProvider = (*decoderAsSource[codec.DecoderFactory])(nil)
	_ HardwareSourceFramesContextProvider = (*MapStreamIndices)(nil)
)
