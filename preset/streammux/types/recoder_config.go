package types

import (
	"time"

	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type AudioTrackConfig struct {
	InputTrackIDs   []int           `yaml:"input_track_ids"`
	OutputTrackIDs  []int           `yaml:"output_track_ids"`
	CodecName       codectypes.Name `yaml:"codec_name"`
	AveragingPeriod time.Duration   `yaml:"averaging_period"`
	AverageBitRate  uint64          `yaml:"average_bit_rate"`
	CustomOptions   DictionaryItems `yaml:"custom_options"`
}

// TODO: allow for separate HardwareDeviceType/HardwareDeviceName for decoding and encoding (and for each track)
type VideoTrackConfig struct {
	InputTrackIDs      []int                 `yaml:"input_track_ids"`
	OutputTrackIDs     []int                 `yaml:"output_track_ids"`
	CodecName          codectypes.Name       `yaml:"codec_name"`
	AveragingPeriod    time.Duration         `yaml:"averaging_period"`
	AverageFrameRate   float64               `yaml:"average_frame_rate"`
	AverageBitRate     uint64                `yaml:"average_bit_rate"`
	CustomOptions      DictionaryItems       `yaml:"custom_options"`
	HardwareDeviceType HardwareDeviceType    `yaml:"hardware_device_type"`
	HardwareDeviceName HardwareDeviceName    `yaml:"hardware_device_name"`
	Resolution         codectypes.Resolution `yaml:"resolution"`
}

// TODO: allow for separate HardwareDeviceType/HardwareDeviceName for decoding and encoding (and for each track)
func (cfg *VideoTrackConfig) GetDecoderHardwareDeviceType() HardwareDeviceType {
	return cfg.HardwareDeviceType
}

// TODO: allow for separate HardwareDeviceType/HardwareDeviceName for decoding and encoding (and for each track)
func (cfg *VideoTrackConfig) GetDecoderHardwareDeviceName() HardwareDeviceName {
	return cfg.HardwareDeviceName
}

type RecoderConfig struct {
	AudioTrackConfigs []AudioTrackConfig `yaml:"audio_track_configs"`
	VideoTrackConfigs []VideoTrackConfig `yaml:"video_track_configs"`
}

type DictionaryItem = types.DictionaryItem
type DictionaryItems = types.DictionaryItems
type HardwareDeviceName = types.HardwareDeviceName
type HardwareDeviceType = types.HardwareDeviceType
