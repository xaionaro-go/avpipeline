package types

import (
	"fmt"
	"strings"
	"time"
)

type AudioTrackConfig struct {
	InputTrackIDs   []int           `yaml:"input_track_ids"`
	OutputTrackIDs  []int           `yaml:"output_track_ids"`
	CodecName       string          `yaml:"codec_name"`
	AveragingPeriod time.Duration   `yaml:"averaging_period"`
	AverageBitRate  uint64          `yaml:"average_bit_rate"`
	CustomOptions   DictionaryItems `yaml:"custom_options"`
}

type VideoTrackConfig struct {
	InputTrackIDs      []int              `yaml:"input_track_ids"`
	OutputTrackIDs     []int              `yaml:"output_track_ids"`
	CodecName          string             `yaml:"codec_name"`
	AveragingPeriod    time.Duration      `yaml:"averaging_period"`
	AverageBitRate     uint64             `yaml:"average_bit_rate"`
	CustomOptions      DictionaryItems    `yaml:"custom_options"`
	HardwareDeviceType HardwareDeviceType `yaml:"hardware_device_type"`
	HardwareDeviceName HardwareDeviceName `yaml:"hardware_device_name"`
	Width              uint32             `yaml:"width"`
	Height             uint32             `yaml:"height"`
}

type RecoderConfig struct {
	AudioTrackConfigs []AudioTrackConfig `yaml:"audio_track_configs"`
	VideoTrackConfigs []VideoTrackConfig `yaml:"video_track_configs"`
}

type DictionaryItem struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
}
type DictionaryItems []DictionaryItem

type HardwareDeviceName string
type HardwareDeviceType int

const (
	// the constants are copied from libav's enum AVHWDeviceType:
	HardwareDeviceTypeCUDA         = HardwareDeviceType(0x2)
	HardwareDeviceTypeD3D11VA      = HardwareDeviceType(0x7)
	HardwareDeviceTypeDRM          = HardwareDeviceType(0x8)
	HardwareDeviceTypeDXVA2        = HardwareDeviceType(0x4)
	HardwareDeviceTypeMediaCodec   = HardwareDeviceType(0xa)
	HardwareDeviceTypeNone         = HardwareDeviceType(0x0)
	HardwareDeviceTypeOpenCL       = HardwareDeviceType(0x9)
	HardwareDeviceTypeQSV          = HardwareDeviceType(0x5)
	HardwareDeviceTypeVAAPI        = HardwareDeviceType(0x3)
	HardwareDeviceTypeVDPAU        = HardwareDeviceType(0x1)
	HardwareDeviceTypeVideoToolbox = HardwareDeviceType(0x6)
	HardwareDeviceTypeVulkan       = HardwareDeviceType(0xb)
)

func (hwt HardwareDeviceType) String() string {
	switch hwt {
	case HardwareDeviceTypeCUDA:
		return "cuda"
	case HardwareDeviceTypeDRM:
		return "drm"
	case HardwareDeviceTypeDXVA2:
		return "dxva2"
	case HardwareDeviceTypeD3D11VA:
		return "d3d11va"
	//case HardwareDeviceTypeD3D12VA:
	//	return "d3d12va"
	case HardwareDeviceTypeOpenCL:
		return "opencl"
	case HardwareDeviceTypeQSV:
		return "qsv"
	case HardwareDeviceTypeVAAPI:
		return "vaapi"
	case HardwareDeviceTypeVDPAU:
		return "vdpau"
	case HardwareDeviceTypeVideoToolbox:
		return "videotoolbox"
	case HardwareDeviceTypeMediaCodec:
		return "mediacodec"
	case HardwareDeviceTypeVulkan:
		return "vulkan"
	}
	return fmt.Sprintf("unknown_%X", int64(hwt))
}

func HardwareDeviceTypeFromString(s string) HardwareDeviceType {
	sanitizeString := func(s string) string {
		return strings.Trim(strings.ToLower(s), " \n\r\t")
	}
	s = sanitizeString(s)
	for i := 0; i <= 0xff; i++ {
		hwt := HardwareDeviceType(i)
		c := sanitizeString(hwt.String())
		if s == c {
			return hwt
		}
	}
	return -1
}
