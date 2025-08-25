package codec

import (
	"context"

	"github.com/asticode/go-astiav"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/logger"
	globaltypes "github.com/xaionaro-go/avpipeline/preset/transcoderwithpassthrough/types"
)

type Name codectypes.Name

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
