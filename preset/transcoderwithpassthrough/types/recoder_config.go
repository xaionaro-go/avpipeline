package types

import (
	streammuxtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
)

type AudioTrackConfig = streammuxtypes.AudioTrackConfig
type VideoTrackConfig = streammuxtypes.VideoTrackConfig
type RecoderConfig = streammuxtypes.RecoderConfig
type DictionaryItem = streammuxtypes.DictionaryItem
type DictionaryItems = streammuxtypes.DictionaryItems
type HardwareDeviceName = streammuxtypes.HardwareDeviceName
type HardwareDeviceType = streammuxtypes.HardwareDeviceType

const (
	// the constants are copied from libav's enum AVHWDeviceType:
	HardwareDeviceTypeCUDA         = streammuxtypes.HardwareDeviceTypeCUDA
	HardwareDeviceTypeD3D11VA      = streammuxtypes.HardwareDeviceTypeD3D11VA
	HardwareDeviceTypeDRM          = streammuxtypes.HardwareDeviceTypeDRM
	HardwareDeviceTypeDXVA2        = streammuxtypes.HardwareDeviceTypeDXVA2
	HardwareDeviceTypeMediaCodec   = streammuxtypes.HardwareDeviceTypeMediaCodec
	HardwareDeviceTypeNone         = streammuxtypes.HardwareDeviceTypeNone
	HardwareDeviceTypeOpenCL       = streammuxtypes.HardwareDeviceTypeOpenCL
	HardwareDeviceTypeQSV          = streammuxtypes.HardwareDeviceTypeQSV
	HardwareDeviceTypeVAAPI        = streammuxtypes.HardwareDeviceTypeVAAPI
	HardwareDeviceTypeVDPAU        = streammuxtypes.HardwareDeviceTypeVDPAU
	HardwareDeviceTypeVideoToolbox = streammuxtypes.HardwareDeviceTypeVideoToolbox
	HardwareDeviceTypeVulkan       = streammuxtypes.HardwareDeviceTypeVulkan
)

func HardwareDeviceTypeFromString(s string) HardwareDeviceType {
	return streammuxtypes.HardwareDeviceTypeFromString(s)
}
