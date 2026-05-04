package codec

import (
	"context"
	"errors"
	"fmt"

	"github.com/asticode/go-astiav"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type codecCandidate struct {
	CodecName          Name
	HardwareDeviceType HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	CustomOptions      *astiav.Dictionary
	ExactCodecName     bool
}

func newCodecCandidate(
	ctx context.Context,
	codecName Name,
	hardwareDeviceType HardwareDeviceType,
	hardwareDeviceName HardwareDeviceName,
	customOptions *astiav.Dictionary,
	exactCodecName bool,
) (codecCandidate, error) {
	opts, err := cloneDictionary(ctx, customOptions)
	if err != nil {
		return codecCandidate{}, fmt.Errorf("unable to clone custom options for codec candidate %q: %w", codecName, err)
	}
	return codecCandidate{
		CodecName:          codecName,
		HardwareDeviceType: hardwareDeviceType,
		HardwareDeviceName: hardwareDeviceName,
		CustomOptions:      opts,
		ExactCodecName:     exactCodecName,
	}, nil
}

func cloneDictionary(
	ctx context.Context,
	src *astiav.Dictionary,
) (*astiav.Dictionary, error) {
	if src == nil {
		return nil, nil
	}
	dst := astiav.NewDictionary()
	setFinalizerFree(ctx, dst)
	if err := src.Copy(dst, 0); err != nil {
		return nil, err
	}
	return dst, nil
}

func isRetryableCodecCandidateError(err error) bool {
	var codecNotFound ErrCodecNotFound
	if errors.As(err, &codecNotFound) {
		return true
	}
	var hardwareUnavailable ErrHardwareUnavailable
	if errors.As(err, &hardwareUnavailable) {
		return true
	}
	var codecOpen ErrCodecOpen
	return errors.As(err, &codecOpen)
}

func validateCodecCandidate(
	ctx context.Context,
	isEncoder bool,
	candidate codecCandidate,
	codecID astiav.CodecID,
) error {
	if !candidate.ExactCodecName {
		return nil
	}
	switch candidate.CodecName {
	case "", NameCopy, NameRaw:
		return nil
	}
	if candidate.CodecName.Codec(ctx, isEncoder) != nil {
		return nil
	}
	return ErrCodecNotFound{
		IsEncoder: isEncoder,
		CodecName: candidate.CodecName,
		CodecID:   codecID,
	}
}

func validateEncoderCandidateCodecIDs(
	ctx context.Context,
	mediaType astiav.MediaType,
	candidates []codecCandidate,
) error {
	var expectedCodecName Name
	var expectedCodecID astiav.CodecID
	for _, candidate := range candidates {
		if !candidate.ExactCodecName {
			continue
		}
		switch candidate.CodecName {
		case "", NameCopy, NameRaw:
			continue
		}
		codec := candidate.CodecName.Codec(ctx, true)
		if codec == nil {
			continue
		}
		if codec.ID().MediaType() != mediaType {
			return ErrCodecCandidateMismatch{
				MediaType:         mediaType,
				ExpectedCodecName: candidate.CodecName,
				ExpectedCodecID:   astiav.CodecIDNone,
				ActualCodecName:   candidate.CodecName,
				ActualCodecID:     codec.ID(),
			}
		}
		if expectedCodecID == astiav.CodecIDNone {
			expectedCodecName = candidate.CodecName
			expectedCodecID = codec.ID()
			continue
		}
		if codec.ID() != expectedCodecID {
			return ErrCodecCandidateMismatch{
				MediaType:         mediaType,
				ExpectedCodecName: expectedCodecName,
				ExpectedCodecID:   expectedCodecID,
				ActualCodecName:   candidate.CodecName,
				ActualCodecID:     codec.ID(),
			}
		}
	}
	return nil
}

func explicitCodecCandidateHardwareDeviceType(
	codecName Name,
) HardwareDeviceType {
	if hardwareDeviceType := detectHardwareDeviceType(string(codecName)); hardwareDeviceType != globaltypes.HardwareDeviceTypeNone {
		return hardwareDeviceType
	}
	return globaltypes.HardwareDeviceTypeNone
}
