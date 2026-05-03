package whisper

import (
	"testing"
	"time"

	testifyassert "github.com/stretchr/testify/assert"
)

func TestDefaultWhisperConfig(t *testing.T) {
	cfg := DefaultWhisperConfig()
	testifyassert.Equal(t, "auto", cfg.Language)
	testifyassert.Equal(t, 3*time.Second, cfg.Queue)
	testifyassert.Empty(t, cfg.Model)
	testifyassert.Nil(t, cfg.UseGPU)
	testifyassert.Nil(t, cfg.GPUDevice)
	testifyassert.Empty(t, cfg.Destination)
	testifyassert.Empty(t, cfg.Format)
	testifyassert.Empty(t, cfg.VADModel)
	testifyassert.Nil(t, cfg.VADThreshold)
	testifyassert.Nil(t, cfg.VADMinSpeechDuration)
	testifyassert.Nil(t, cfg.VADMinSilenceDuration)
}

func TestWhisperConfig_FilterString_Minimal(t *testing.T) {
	cfg := &WhisperConfig{
		Model: "/path/to/model.bin",
	}
	got := cfg.FilterString()
	testifyassert.Equal(t, "whisper=model=/path/to/model.bin", got)
}

func TestWhisperConfig_FilterString_WithDefaults(t *testing.T) {
	cfg := DefaultWhisperConfig()
	cfg.Model = "/path/to/model.bin"
	got := cfg.FilterString()
	testifyassert.Equal(t, "whisper=model=/path/to/model.bin:language=auto:queue=3000000", got)
}

func TestWhisperConfig_FilterString_AllOptions(t *testing.T) {
	useGPU := true
	gpuDevice := 2
	vadThreshold := 0.7
	vadMinSpeech := 200 * time.Millisecond
	vadMinSilence := 1 * time.Second

	cfg := &WhisperConfig{
		Model:                 "/models/ggml-large.bin",
		Language:              "en",
		Queue:                 5 * time.Second,
		UseGPU:                &useGPU,
		GPUDevice:             &gpuDevice,
		Destination:           "/tmp/output.srt",
		Format:                "srt",
		VADModel:              "/models/silero-vad.bin",
		VADThreshold:          &vadThreshold,
		VADMinSpeechDuration:  &vadMinSpeech,
		VADMinSilenceDuration: &vadMinSilence,
	}
	got := cfg.FilterString()
	testifyassert.Contains(t, got, "whisper=")
	testifyassert.Contains(t, got, "model=/models/ggml-large.bin")
	testifyassert.Contains(t, got, "language=en")
	testifyassert.Contains(t, got, "queue=5000000")
	testifyassert.Contains(t, got, "use_gpu=1")
	testifyassert.Contains(t, got, "gpu_device=2")
	testifyassert.Contains(t, got, "destination=/tmp/output.srt")
	testifyassert.Contains(t, got, "format=srt")
	testifyassert.Contains(t, got, "vad_model=/models/silero-vad.bin")
	testifyassert.Contains(t, got, "vad_threshold=0.7")
	testifyassert.Contains(t, got, "vad_min_speech_duration=200000")
	testifyassert.Contains(t, got, "vad_min_silence_duration=1000000")
}

func TestWhisperConfig_FilterString_GPUDisabled(t *testing.T) {
	useGPU := false
	cfg := &WhisperConfig{
		Model:  "/path/to/model.bin",
		UseGPU: &useGPU,
	}
	got := cfg.FilterString()
	testifyassert.Contains(t, got, "use_gpu=0")
}

func TestWhisperConfig_FilterString_JSONFormat(t *testing.T) {
	cfg := &WhisperConfig{
		Model:       "/path/to/model.bin",
		Destination: "-",
		Format:      "json",
	}
	got := cfg.FilterString()
	testifyassert.Contains(t, got, "destination=-")
	testifyassert.Contains(t, got, "format=json")
}

func TestWhisperConfig_FilterString_ZeroQueue(t *testing.T) {
	cfg := &WhisperConfig{
		Model: "/path/to/model.bin",
	}
	got := cfg.FilterString()
	testifyassert.NotContains(t, got, "queue=")
}

func TestWhisperConfig_FilterString_EmptyModel(t *testing.T) {
	cfg := &WhisperConfig{}
	got := cfg.FilterString()
	testifyassert.Equal(t, "whisper=", got)
}

func TestWhisperConfig_FilterString_EscapesSpecialChars(t *testing.T) {
	cfg := &WhisperConfig{
		Model:       `/path/with:colon/and\backslash/model.bin`,
		Destination: `file:output's.srt`,
	}
	got := cfg.FilterString()
	testifyassert.Contains(t, got, `model=/path/with\:colon/and\\backslash/model.bin`)
	testifyassert.Contains(t, got, `destination=file\:output\'s.srt`)
}

func TestEscapeFilterValue(t *testing.T) {
	testifyassert.Equal(t, `simple`, escapeFilterValue(`simple`))
	testifyassert.Equal(t, `with\:colon`, escapeFilterValue(`with:colon`))
	testifyassert.Equal(t, `with\\backslash`, escapeFilterValue(`with\backslash`))
	testifyassert.Equal(t, `with\'quote`, escapeFilterValue(`with'quote`))
	testifyassert.Equal(t, `all\:\\\\\'three`, escapeFilterValue(`all:\\'three`))
}
