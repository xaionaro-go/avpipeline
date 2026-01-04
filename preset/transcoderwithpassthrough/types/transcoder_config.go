// transcoder_config.go defines the configuration for the transcoder with passthrough.

// Package types provides configuration types for transcoder with passthrough functionality.
package types

import (
	streammuxtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type (
	AudioTrackConfig       = streammuxtypes.OutputAudioTrackConfig
	VideoTrackConfig       = streammuxtypes.OutputVideoTrackConfig
	TranscoderConfig       = streammuxtypes.TranscoderConfig
	TranscoderInputConfig  = streammuxtypes.TranscoderInputConfig
	TranscoderOutputConfig = streammuxtypes.TranscoderOutputConfig
	DictionaryItem         = types.DictionaryItem
	DictionaryItems        = types.DictionaryItems
	HardwareDeviceName     = types.HardwareDeviceName
	HardwareDeviceType     = types.HardwareDeviceType
)

const (
	// the constants are copied from libav's enum AVHWDeviceType:
	HardwareDeviceTypeCUDA         = types.HardwareDeviceTypeCUDA
	HardwareDeviceTypeD3D11VA      = types.HardwareDeviceTypeD3D11VA
	HardwareDeviceTypeDRM          = types.HardwareDeviceTypeDRM
	HardwareDeviceTypeDXVA2        = types.HardwareDeviceTypeDXVA2
	HardwareDeviceTypeMediaCodec   = types.HardwareDeviceTypeMediaCodec
	HardwareDeviceTypeNone         = types.HardwareDeviceTypeNone
	HardwareDeviceTypeOpenCL       = types.HardwareDeviceTypeOpenCL
	HardwareDeviceTypeQSV          = types.HardwareDeviceTypeQSV
	HardwareDeviceTypeVAAPI        = types.HardwareDeviceTypeVAAPI
	HardwareDeviceTypeVDPAU        = types.HardwareDeviceTypeVDPAU
	HardwareDeviceTypeVideoToolbox = types.HardwareDeviceTypeVideoToolbox
	HardwareDeviceTypeVulkan       = types.HardwareDeviceTypeVulkan
)

func HardwareDeviceTypeFromString(s string) HardwareDeviceType {
	return types.HardwareDeviceTypeFromString(s)
}
