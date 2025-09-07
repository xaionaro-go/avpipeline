package types

import (
	"time"

	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/types"
)

type InputAudioTrackConfig struct {
	TrackID    *int            `yaml:"track_id,omitempty"`
	CodecName  codectypes.Name `yaml:"codec_name"`
	SampleRate uint32          `yaml:"sample_rate"`
}

type InputVideoTrackConfig struct {
	TrackID    *int                  `yaml:"track_id,omitempty"`
	CodecName  codectypes.Name       `yaml:"codec_name"`
	Resolution codectypes.Resolution `yaml:"resolution"`
}

type OutputAudioTrackConfig struct {
	InputTrackIDs   []int           `yaml:"input_track_ids"`
	OutputTrackIDs  []int           `yaml:"output_track_ids"`
	CodecName       codectypes.Name `yaml:"codec_name"`
	AveragingPeriod time.Duration   `yaml:"averaging_period"`
	AverageBitRate  uint64          `yaml:"average_bit_rate"`
	CustomOptions   DictionaryItems `yaml:"custom_options"`
}

// TODO: allow for separate HardwareDeviceType/HardwareDeviceName for decoding and encoding (and for each track)
type OutputVideoTrackConfig struct {
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
func (cfg *OutputVideoTrackConfig) GetDecoderHardwareDeviceType() HardwareDeviceType {
	return cfg.HardwareDeviceType
}

// TODO: allow for separate HardwareDeviceType/HardwareDeviceName for decoding and encoding (and for each track)
func (cfg *OutputVideoTrackConfig) GetDecoderHardwareDeviceName() HardwareDeviceName {
	return cfg.HardwareDeviceName
}

type RecoderConfig struct {
	Input  *RecoderInputConfig `yaml:"input"`
	Output RecoderOutputConfig `yaml:"output"`
}

type RecoderInputConfig struct {
	AudioTrackConfigs []InputAudioTrackConfig `yaml:"audio_track_configs"`
	VideoTrackConfigs []InputVideoTrackConfig `yaml:"video_track_configs"`
}

type RecoderOutputConfig struct {
	AudioTrackConfigs []OutputAudioTrackConfig `yaml:"audio_track_configs"`
	VideoTrackConfigs []OutputVideoTrackConfig `yaml:"video_track_configs"`
}

type DictionaryItem = types.DictionaryItem
type DictionaryItems = types.DictionaryItems
type HardwareDeviceName = types.HardwareDeviceName
type HardwareDeviceType = types.HardwareDeviceType
