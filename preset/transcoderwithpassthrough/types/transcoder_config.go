package types

import (
	streammuxtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type AudioTrackConfig = streammuxtypes.OutputAudioTrackConfig
type VideoTrackConfig = streammuxtypes.OutputVideoTrackConfig
type TranscoderConfig = streammuxtypes.TranscoderConfig
type TranscoderInputConfig = streammuxtypes.TranscoderInputConfig
type TranscoderOutputConfig = streammuxtypes.TranscoderOutputConfig
type DictionaryItem = types.DictionaryItem
type DictionaryItems = types.DictionaryItems
type HardwareDeviceName = types.HardwareDeviceName
type HardwareDeviceType = types.HardwareDeviceType

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
