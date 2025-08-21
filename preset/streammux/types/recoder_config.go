package types

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/codec"
)

type AudioTrackConfig struct {
	InputTrackIDs   []int           `yaml:"input_track_ids"`
	OutputTrackIDs  []int           `yaml:"output_track_ids"`
	CodecName       codec.Name      `yaml:"codec_name"`
	AveragingPeriod time.Duration   `yaml:"averaging_period"`
	AverageBitRate  uint64          `yaml:"average_bit_rate"`
	CustomOptions   DictionaryItems `yaml:"custom_options"`
}

func (c *AudioTrackConfig) Codec(ctx context.Context) *astiav.Codec {
	return c.CodecName.Codec(ctx, true)
}

type VideoTrackConfig struct {
	InputTrackIDs      []int              `yaml:"input_track_ids"`
	OutputTrackIDs     []int              `yaml:"output_track_ids"`
	CodecName          codec.Name         `yaml:"codec_name"`
	AveragingPeriod    time.Duration      `yaml:"averaging_period"`
	AverageBitRate     uint64             `yaml:"average_bit_rate"`
	CustomOptions      DictionaryItems    `yaml:"custom_options"`
	HardwareDeviceType HardwareDeviceType `yaml:"hardware_device_type"`
	HardwareDeviceName HardwareDeviceName `yaml:"hardware_device_name"`
	Width              uint32             `yaml:"width"`
	Height             uint32             `yaml:"height"`
}

func (c *VideoTrackConfig) Codec(ctx context.Context) *astiav.Codec {
	return c.CodecName.Codec(ctx, true)
}

type RecoderConfig struct {
	AudioTrackConfigs []AudioTrackConfig `yaml:"audio_track_configs"`
	VideoTrackConfigs []VideoTrackConfig `yaml:"video_track_configs"`
}

func (c *RecoderConfig) OutputKey(
	ctx context.Context,
) OutputKey {
	var audioCodec codec.Name
	if len(c.AudioTrackConfigs) > 0 {
		audioCodec = c.AudioTrackConfigs[0].CodecName.Canonicalize(ctx, true)
	}
	var videoCodec codec.Name
	if len(c.VideoTrackConfigs) > 0 {
		videoCodec = c.VideoTrackConfigs[0].CodecName.Canonicalize(ctx, true)
	}
	resolution := codec.Resolution{
		Width:  c.VideoTrackConfigs[0].Width,
		Height: c.VideoTrackConfigs[0].Height,
	}
	return OutputKey{
		AudioCodec: audioCodec,
		VideoCodec: videoCodec,
		Resolution: resolution,
	}
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
