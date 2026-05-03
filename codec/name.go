// name.go provides codec name resolution and hardware-specific naming logic.

package codec

import (
	"context"

	"github.com/asticode/go-astiav"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	globaltypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
)

type Name codectypes.Name

const (
	NameCopy = Name(codectypes.NameCopy)
	NameRaw  = Name(codectypes.NameRaw)
)

func (n Name) Codec(
	ctx context.Context,
	isEncoder bool,
) (_ret *astiav.Codec) {
	logger.Tracef(ctx, "findCodecByName(ctx, %t, '%s')", isEncoder, n)
	defer func() { logger.Tracef(ctx, "/findCodecByName(ctx, %t, '%s'): %v", isEncoder, n, _ret) }()
	if isEncoder {
		return astiav.FindEncoderByName(string(n))
	}
	return astiav.FindDecoderByName(string(n))
}

func (n Name) Canonicalize(
	ctx context.Context,
	isEncoder bool,
) (_ret Name) {
	logger.Tracef(ctx, "Canonicalize(ctx, '%s')", n)
	defer func() { logger.Tracef(ctx, "/Canonicalize(ctx, '%s'): '%v'", n, _ret) }()
	switch n {
	case NameCopy:
		return NameCopy
	case NameRaw:
		return NameRaw
	}
	codec := n.Codec(ctx, isEncoder)
	if codec != nil {
		return Name(codec.ID().Name())
	}

	// TODO: use avcodec_descriptor_get_by_name to validate if the name is correct
	return n
}

func (n Name) hwName(
	ctx context.Context,
	isEncoder bool,
	hwDeviceType HardwareDeviceType,
) (_ret Name) {
	logger.Tracef(ctx, "hwName(ctx, %t, '%s', %v)", isEncoder, n, hwDeviceType)
	defer func() {
		logger.Tracef(ctx, "/hwName(ctx, %t, '%s', %v): %v", isEncoder, n, hwDeviceType, _ret)
	}()
	switch hwDeviceType {
	case globaltypes.HardwareDeviceTypeNone:
		// HardwareDeviceTypeNone is the zero-value sentinel for "no HW
		// requested". Forming a candidate like "av1_none" would silently
		// be unregistered and downstream FindDecoderByName would return
		// nil, masking the misuse as a soft fallback. Fail fast instead:
		// callers MUST resolve None upstream — preferredHWDecoderName
		// remaps None→CUDA in decoder_auto.go for backward-compat, and
		// codec.newCodec gates hwName invocations behind a None check
		// before the call site. This panic is defense-in-depth against
		// future callers that would otherwise produce silent
		// "<codec>_none" fallbacks.
		panic("hwName called with HardwareDeviceTypeNone — callers must resolve None to a concrete device type before calling")
	case globaltypes.HardwareDeviceTypeCUDA:
		if isEncoder {
			return n + "_nvenc"
		} else {
			return n + "_cuvid"
		}
	default:
		return n + "_" + Name(hwDeviceType.String())
	}
}
