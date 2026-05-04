package codec

import (
	"fmt"

	"github.com/asticode/go-astiav"
)

type ErrCodecNotFound struct {
	IsEncoder bool
	CodecName Name
	CodecID   astiav.CodecID
}

func (e ErrCodecNotFound) Error() string {
	role := "decoder"
	if e.IsEncoder {
		role = "encoder"
	}
	if e.CodecID == astiav.CodecIDNone {
		return fmt.Sprintf("%s codec %q was not found", role, e.CodecName)
	}
	return fmt.Sprintf("%s codec %q was not found for codec ID %s", role, e.CodecName, e.CodecID)
}

type ErrHardwareUnavailable struct {
	IsEncoder          bool
	CodecName          Name
	HardwareDeviceType HardwareDeviceType
	HardwareDeviceName HardwareDeviceName
	Err                error
}

func (e ErrHardwareUnavailable) Error() string {
	role := "decoder"
	if e.IsEncoder {
		role = "encoder"
	}
	return fmt.Sprintf(
		"%s codec %q hardware %s:%s is unavailable: %v",
		role,
		e.CodecName,
		e.HardwareDeviceType,
		e.HardwareDeviceName,
		e.Err,
	)
}

func (e ErrHardwareUnavailable) Unwrap() error {
	return e.Err
}

type ErrCodecOpen struct {
	IsEncoder bool
	CodecName Name
	Case      string
	Err       error
}

func (e ErrCodecOpen) Error() string {
	role := "decoder"
	if e.IsEncoder {
		role = "encoder"
	}
	return fmt.Sprintf("unable to open %s codec %q (%s): %v", role, e.CodecName, e.Case, e.Err)
}

func (e ErrCodecOpen) Unwrap() error {
	return e.Err
}

type ErrCodecCandidateMismatch struct {
	MediaType         astiav.MediaType
	ExpectedCodecName Name
	ExpectedCodecID   astiav.CodecID
	ActualCodecName   Name
	ActualCodecID     astiav.CodecID
}

func (e ErrCodecCandidateMismatch) Error() string {
	return fmt.Sprintf(
		"%s encoder codec candidate %q resolves to %s, but %q resolves to %s",
		e.MediaType,
		e.ActualCodecName,
		e.ActualCodecID,
		e.ExpectedCodecName,
		e.ExpectedCodecID,
	)
}
