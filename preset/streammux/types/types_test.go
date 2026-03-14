package types

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	audio "github.com/xaionaro-go/audio/pkg/audio/types"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// --- MuxMode ---

func TestMuxMode_String(t *testing.T) {
	tests := []struct {
		mode MuxMode
		want string
	}{
		{UndefinedMuxMode, "<undefined>"},
		{MuxModeForbid, "forbid"},
		{MuxModeSameOutputSameTracks, "same_output_same_tracks"},
		{MuxModeSameOutputDifferentTracks, "same_output_different_tracks"},
		{MuxModeDifferentOutputsSameTracks, "different_outputs_same_tracks"},
		{MuxModeDifferentOutputsSameTracksSplitAV, "different_outputs_same_tracks_split_av"},
		{MuxMode(99), "<unknown_mode_99>"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.mode.String())
	}
}

func TestMuxModeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  MuxMode
	}{
		{"forbid", MuxModeForbid},
		{"FORBID", MuxModeForbid},
		{" Forbid ", MuxModeForbid},
		{"same_output_same_tracks", MuxModeSameOutputSameTracks},
		{"same_output_different_tracks", MuxModeSameOutputDifferentTracks},
		{"different_outputs_same_tracks", MuxModeDifferentOutputsSameTracks},
		{"different_outputs_same_tracks_split_av", MuxModeDifferentOutputsSameTracksSplitAV},
		{"unknown_garbage", UndefinedMuxMode},
		{"", UndefinedMuxMode},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, MuxModeFromString(tt.input), "input: %q", tt.input)
	}
}

func TestMuxMode_RoundTrip(t *testing.T) {
	for i := MuxMode(0); i < EndOfPassthroughMode; i++ {
		s := i.String()
		if i == UndefinedMuxMode {
			continue // <undefined> doesn't round-trip
		}
		assert.Equal(t, i, MuxModeFromString(s), "mode: %d, string: %s", i, s)
	}
}

// --- SenderKey ---

func TestSenderKey_String_Empty(t *testing.T) {
	k := SenderKey{}
	assert.Equal(t, "<empty>", k.String())
}

func TestSenderKey_String_VideoOnly(t *testing.T) {
	k := SenderKey{
		VideoCodec:      "h264",
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
	}
	assert.Equal(t, "v:h264/1920x1080", k.String())
}

func TestSenderKey_String_AudioOnly(t *testing.T) {
	k := SenderKey{
		AudioCodec:      "aac",
		AudioSampleRate: 44100,
	}
	assert.Equal(t, "a:aac@44100Hz", k.String())
}

func TestSenderKey_String_Both(t *testing.T) {
	k := SenderKey{
		AudioCodec:      "aac",
		AudioSampleRate: 48000,
		VideoCodec:      "h264",
		VideoResolution: codectypes.Resolution{Width: 1280, Height: 720},
	}
	assert.Equal(t, "v:h264/1280x720&a:aac@48000Hz", k.String())
}

func TestSenderKey_Compare_Equal(t *testing.T) {
	a := SenderKey{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080}}
	b := SenderKey{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080}}
	assert.Equal(t, 0, a.Compare(b))
}

func TestSenderKey_Compare_CopyIsBetter(t *testing.T) {
	copy := SenderKey{VideoCodec: codectypes.NameCopy, VideoResolution: codectypes.Resolution{Width: 640, Height: 480}}
	encoded := SenderKey{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080}}
	assert.Equal(t, 1, copy.Compare(encoded))
	assert.Equal(t, -1, encoded.Compare(copy))
}

func TestSenderKey_Compare_HigherResolution(t *testing.T) {
	hd := SenderKey{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080}}
	sd := SenderKey{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 640, Height: 480}}
	assert.Equal(t, 1, hd.Compare(sd))
	assert.Equal(t, -1, sd.Compare(hd))
}

func TestSenderKey_Compare_AudioCopy(t *testing.T) {
	a := SenderKey{
		VideoCodec:      "h264",
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		AudioCodec:      codectypes.NameCopy,
		AudioSampleRate: 44100,
	}
	b := SenderKey{
		VideoCodec:      "h264",
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		AudioCodec:      "aac",
		AudioSampleRate: 44100,
	}
	assert.Equal(t, 1, a.Compare(b))
	assert.Equal(t, -1, b.Compare(a))
}

func TestSenderKey_Compare_HigherSampleRate(t *testing.T) {
	a := SenderKey{
		VideoCodec:      "h264",
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		AudioCodec:      "aac",
		AudioSampleRate: 48000,
	}
	b := SenderKey{
		VideoCodec:      "h264",
		VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080},
		AudioCodec:      "aac",
		AudioSampleRate: 44100,
	}
	assert.Equal(t, 1, a.Compare(b))
	assert.Equal(t, -1, b.Compare(a))
}

func TestSenderKeys_Sort(t *testing.T) {
	keys := SenderKeys{
		{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 1920, Height: 1080}},
		{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 640, Height: 480}},
		{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 1280, Height: 720}},
	}
	keys.Sort()
	assert.Equal(t, uint32(480), keys[0].VideoResolution.Height)
	assert.Equal(t, uint32(720), keys[1].VideoResolution.Height)
	assert.Equal(t, uint32(1080), keys[2].VideoResolution.Height)
}

func TestSenderKeys_Sort_WithCopy(t *testing.T) {
	keys := SenderKeys{
		{VideoCodec: codectypes.NameCopy, VideoResolution: codectypes.Resolution{Width: 100, Height: 100}},
		{VideoCodec: "h264", VideoResolution: codectypes.Resolution{Width: 3840, Height: 2160}},
	}
	keys.Sort()
	// Copy should be sorted last (highest quality)
	assert.Equal(t, codectypes.NameCopy, keys[1].VideoCodec)
}

// --- AutoBitRateResolutionAndBitRateConfigs ---

func sampleConfigs() AutoBitRateResolutionAndBitRateConfigs {
	return AutoBitRateResolutionAndBitRateConfigs{
		{Resolution: codectypes.Resolution{Width: 640, Height: 480}, BitrateHigh: 2_000_000, BitrateLow: 500_000},
		{Resolution: codectypes.Resolution{Width: 1280, Height: 720}, BitrateHigh: 5_000_000, BitrateLow: 1_000_000},
		{Resolution: codectypes.Resolution{Width: 1920, Height: 1080}, BitrateHigh: 10_000_000, BitrateLow: 3_000_000},
	}
}

func TestResolutionConfigs_Find(t *testing.T) {
	cfgs := sampleConfigs()
	found := cfgs.Find(codectypes.Resolution{Width: 1280, Height: 720})
	require.NotNil(t, found)
	assert.Equal(t, uint32(720), found.Height)
}

func TestResolutionConfigs_Find_NotFound(t *testing.T) {
	cfgs := sampleConfigs()
	found := cfgs.Find(codectypes.Resolution{Width: 3840, Height: 2160})
	assert.Nil(t, found)
}

func TestResolutionConfigs_BitRate_InRange(t *testing.T) {
	cfgs := sampleConfigs()
	// 1.5M is in range for both 480p (500k-2M) and 720p (1M-5M)
	result := cfgs.BitRate(1_500_000)
	assert.Len(t, result, 2)
}

func TestResolutionConfigs_BitRate_OutOfRange(t *testing.T) {
	cfgs := sampleConfigs()
	result := cfgs.BitRate(100) // too low for any
	assert.Len(t, result, 0)
}

func TestResolutionConfigs_Best(t *testing.T) {
	cfgs := sampleConfigs()
	best := cfgs.Best()
	require.NotNil(t, best)
	assert.Equal(t, uint32(1080), best.Height)
}

func TestResolutionConfigs_Best_Empty(t *testing.T) {
	cfgs := AutoBitRateResolutionAndBitRateConfigs{}
	assert.Nil(t, cfgs.Best())
}

func TestResolutionConfigs_Worst(t *testing.T) {
	cfgs := sampleConfigs()
	worst := cfgs.Worst()
	require.NotNil(t, worst)
	assert.Equal(t, uint32(480), worst.Height)
}

func TestResolutionConfigs_Worst_Empty(t *testing.T) {
	cfgs := AutoBitRateResolutionAndBitRateConfigs{}
	assert.Nil(t, cfgs.Worst())
}

func TestResolutionConfigs_MaxHeight(t *testing.T) {
	cfgs := sampleConfigs()
	result := cfgs.MaxHeight(720)
	assert.Len(t, result, 2) // 480p and 720p
}

func TestResolutionConfigs_MaxWidth(t *testing.T) {
	cfgs := sampleConfigs()
	result := cfgs.MaxWidth(1280)
	assert.Len(t, result, 2) // 640 and 1280
}

func TestResolutionConfigs_MinHeight(t *testing.T) {
	cfgs := sampleConfigs()
	result := cfgs.MinHeight(720)
	assert.Len(t, result, 2) // 720p and 1080p
}

func TestResolutionConfigs_MinWidth(t *testing.T) {
	cfgs := sampleConfigs()
	result := cfgs.MinWidth(1280)
	assert.Len(t, result, 2) // 1280 and 1920
}

func TestResolutionConfig_String(t *testing.T) {
	cfg := AutoBitRateResolutionAndBitRateConfig{
		Resolution:  codectypes.Resolution{Width: 1920, Height: 1080},
		BitrateHigh: 10_000_000,
		BitrateLow:  3_000_000,
	}
	s := cfg.String()
	assert.Contains(t, s, "1920x1080")
	assert.Contains(t, s, "bps")
}

// --- AllowedResolutionsAndBitRates ---

func TestAllowedResolutionsAndBitRates_NoLimits(t *testing.T) {
	cfg := &AutoBitRateVideoConfig{
		ResolutionsAndBitRates: sampleConfigs(),
	}
	allowed := cfg.AllowedResolutionsAndBitRates()
	assert.Len(t, allowed, 3)
}

func TestAllowedResolutionsAndBitRates_MaxHeightOnly(t *testing.T) {
	cfg := &AutoBitRateVideoConfig{
		ResolutionsAndBitRates: sampleConfigs(),
		MaxResolution:          codectypes.Resolution{Height: 720},
	}
	allowed := cfg.AllowedResolutionsAndBitRates()
	assert.Len(t, allowed, 2)
	assert.Equal(t, uint32(720), allowed.Best().Height)
}

func TestAllowedResolutionsAndBitRates_MinHeightOnly(t *testing.T) {
	cfg := &AutoBitRateVideoConfig{
		ResolutionsAndBitRates: sampleConfigs(),
		MinResolution:          codectypes.Resolution{Height: 720},
	}
	allowed := cfg.AllowedResolutionsAndBitRates()
	assert.Len(t, allowed, 2)
	assert.Equal(t, uint32(720), allowed.Worst().Height)
}

func TestAllowedResolutionsAndBitRates_BothLimits(t *testing.T) {
	cfg := &AutoBitRateVideoConfig{
		ResolutionsAndBitRates: sampleConfigs(),
		MinResolution:          codectypes.Resolution{Height: 720},
		MaxResolution:          codectypes.Resolution{Height: 720},
	}
	allowed := cfg.AllowedResolutionsAndBitRates()
	assert.Len(t, allowed, 1)
	assert.Equal(t, uint32(720), allowed[0].Height)
}

func TestAllowedResolutionsAndBitRates_WidthFilter(t *testing.T) {
	cfg := &AutoBitRateVideoConfig{
		ResolutionsAndBitRates: sampleConfigs(),
		MaxResolution:          codectypes.Resolution{Width: 1280},
	}
	allowed := cfg.AllowedResolutionsAndBitRates()
	assert.Len(t, allowed, 2)
	assert.Equal(t, uint32(1280), allowed.Best().Width)
}

func TestAllowedResolutionsAndBitRates_ConflictingLimitsFallback(t *testing.T) {
	cfg := &AutoBitRateVideoConfig{
		ResolutionsAndBitRates: sampleConfigs(),
		MinResolution:          codectypes.Resolution{Height: 2000},
		MaxResolution:          codectypes.Resolution{Height: 100},
	}
	// Conflicting constraints exclude all resolutions; should fall back to full set.
	allowed := cfg.AllowedResolutionsAndBitRates()
	assert.Len(t, allowed, 3)
}

// --- FPSReducerConfig ---

func TestDefaultFPSReducerConfig(t *testing.T) {
	cfg := DefaultFPSReducerConfig()
	require.Len(t, cfg, 1)
	assert.Equal(t, Ubps(0), cfg[0].BitrateMin)
	assert.Equal(t, Ubps(500_000), cfg[0].BitrateMax)
	assert.Equal(t, 1, cfg[0].Fraction.Num)
	assert.Equal(t, 2, cfg[0].Fraction.Den)
}

func TestFPSReducerConfig_GetFraction_InRange(t *testing.T) {
	cfg := DefaultFPSReducerConfig()
	f := cfg.GetFraction(250_000)
	assert.Equal(t, globaltypes.Rational{Num: 1, Den: 2}, f)
}

func TestFPSReducerConfig_GetFraction_OutOfRange(t *testing.T) {
	cfg := DefaultFPSReducerConfig()
	f := cfg.GetFraction(1_000_000) // above 500k
	assert.Equal(t, globaltypes.Rational{Num: 1, Den: 1}, f) // no reduction
}

func TestFPSReducerConfig_GetFraction_Empty(t *testing.T) {
	cfg := FPSReducerConfig{}
	f := cfg.GetFraction(1000)
	assert.Equal(t, globaltypes.Rational{Num: 1, Den: 1}, f)
}

func TestFPSReducerConfig_GetFraction_MultipleRanges(t *testing.T) {
	cfg := FPSReducerConfig{
		{BitrateMin: 0, BitrateMax: 200_000, Fraction: globaltypes.Rational{Num: 1, Den: 4}},
		{BitrateMin: 200_001, BitrateMax: 500_000, Fraction: globaltypes.Rational{Num: 1, Den: 2}},
	}
	assert.Equal(t, globaltypes.Rational{Num: 1, Den: 4}, cfg.GetFraction(100_000))
	assert.Equal(t, globaltypes.Rational{Num: 1, Den: 2}, cfg.GetFraction(300_000))
	assert.Equal(t, globaltypes.Rational{Num: 1, Den: 1}, cfg.GetFraction(600_000)) // no match
}

// --- AutoBitrateCalculatorStatic ---

func TestAutoBitrateCalculatorStatic(t *testing.T) {
	calc := AutoBitrateCalculatorStatic(5_000_000)
	ctx := context.Background()

	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 1_000_000,
		QueueDuration:         time.Second,
	})
	assert.Equal(t, Ubps(5_000_000), result.BitRate)
	assert.True(t, result.IsCritical)
}

func TestAutoBitrateCalculatorStatic_AlwaysSame(t *testing.T) {
	calc := AutoBitrateCalculatorStatic(3_000_000)
	ctx := context.Background()

	// Different inputs should all return the same bitrate
	for _, q := range []time.Duration{0, time.Second, 30 * time.Second} {
		result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
			CurrentBitrateSetting: 10_000_000,
			QueueDuration:         q,
		})
		assert.Equal(t, Ubps(3_000_000), result.BitRate)
	}
}

// --- AutoBitrateCalculatorThresholds ---

func TestDefaultAutoBitrateCalculatorThresholds(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorThresholds()
	require.NotNil(t, calc)
	assert.Equal(t, 30*time.Second, calc.OutputExtremelyHighQueueSizeDuration)
	assert.Equal(t, 5*time.Second, calc.OutputVeryHighQueueSizeDuration)
	assert.Equal(t, 2*time.Second, calc.OutputHighQueueSizeDuration)
	assert.Equal(t, time.Second, calc.OutputLowQueueSizeDuration)
	assert.Equal(t, 500*time.Millisecond, calc.OutputVeryLowQueueSizeDuration)
}

func TestThresholds_ExtremelyHighQueue(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorThresholds()
	ctx := context.Background()
	// QueueSize/ActualOutputBitrate*8 = queueDuration
	// To get 30s queue: size * 8 / bitrate * 1e9 = 30e9
	// size = 30 * bitrate / 8 = 30 * 1_000_000 / 8 = 3_750_000
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 1_000_000,
		ActualOutputBitrate:   1_000_000,
		QueueSize:             3_750_000, // 30s at 1Mbps
	})
	// Should apply ExtremeDecreaseK (0.1)
	assert.True(t, result.IsCritical)
	assert.Less(t, result.BitRate, Ubps(200_000)) // 0.1 * 1M = 100K
}

func TestThresholds_VeryHighQueue(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorThresholds()
	ctx := context.Background()
	// 5s queue: size = 5 * 1_000_000 / 8 = 625_000
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 1_000_000,
		ActualOutputBitrate:   1_000_000,
		QueueSize:             625_000,
	})
	assert.True(t, result.IsCritical)
	assert.Equal(t, Ubps(500_000), result.BitRate) // 0.5 * 1M
}

func TestThresholds_NormalQueue(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorThresholds()
	ctx := context.Background()
	// 1.5s queue: size = 1.5 * 1_000_000 / 8 = 187500
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 1_000_000,
		ActualOutputBitrate:   1_000_000,
		QueueSize:             187_500,
	})
	// Between Low (1s) and High (2s) - no change
	assert.Equal(t, Ubps(1_000_000), result.BitRate)
	assert.False(t, result.IsCritical)
}

func TestThresholds_VeryLowQueue(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorThresholds()
	ctx := context.Background()
	// 0.4s queue: size = 0.4 * 1_000_000 / 8 = 50_000
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 1_000_000,
		ActualOutputBitrate:   1_000_000,
		QueueSize:             50_000,
	})
	// QuickIncreaseK (1.2)
	assert.Equal(t, Ubps(1_200_000), result.BitRate)
	assert.False(t, result.IsCritical)
}

func TestThresholds_HighQueue(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorThresholds()
	ctx := context.Background()
	// 3s queue: size = 3 * 1_000_000 / 8 = 375_000
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 1_000_000,
		ActualOutputBitrate:   1_000_000,
		QueueSize:             375_000,
	})
	// DecreaseK (0.95)
	assert.Equal(t, Ubps(950_000), result.BitRate)
	assert.False(t, result.IsCritical)
}

func TestThresholds_LowQueue(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorThresholds()
	ctx := context.Background()
	// 0.8s queue: size = 0.8 * 1_000_000 / 8 = 100_000
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 1_000_000,
		ActualOutputBitrate:   1_000_000,
		QueueSize:             100_000,
	})
	// IncreaseK (1.01)
	assert.Equal(t, Ubps(1_010_000), result.BitRate)
	assert.False(t, result.IsCritical)
}

// --- AutoBitrateCalculatorLogK ---

func TestDefaultAutoBitrateCalculatorLogK(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorLogK()
	require.NotNil(t, calc)
	assert.Equal(t, time.Second, calc.QueueOptimal)
	assert.Equal(t, 0.7, calc.Inertia)
}

func TestLogK_NotEnoughData(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorLogK()
	ctx := context.Background()

	// First call - moving average not valid yet
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		QueueSize:             625_000,
	})
	assert.Equal(t, Ubps(5_000_000), result.BitRate)
	assert.False(t, result.IsCritical)
}

func TestLogK_StabilizesAroundOptimal(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorLogK()
	ctx := context.Background()

	// Feed enough data to make the moving average valid
	for i := 0; i < 15; i++ {
		calc.CalculateBitRate(ctx, CalculateBitRateRequest{
			CurrentBitrateSetting: 5_000_000,
			ActualOutputBitrate:   5_000_000,
			InputBitrate:          5_000_000,
			QueueSize:             625_000, // 1s at 5Mbps
		})
	}

	// At optimal queue size, bitrate should stay close to current
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		ActualOutputBitrate:   5_000_000,
		InputBitrate:          5_000_000,
		QueueSize:             625_000,
	})
	// Should be close to 5M (within 10%)
	assert.InDelta(t, float64(5_000_000), float64(result.BitRate), float64(500_000))
}

// --- AutoBitrateCalculatorQueueSizeGapDecay ---

func TestDefaultAutoBitrateCalculatorQueueSizeGapDecay(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorQueueSizeGapDecay()
	require.NotNil(t, calc)
	assert.Equal(t, 3*time.Second, calc.QueueDurationOptimal)
	assert.Equal(t, UB(200_000), calc.QueueSizeMin)
	assert.Equal(t, 3*time.Second, calc.GapDecay)
	assert.Equal(t, 10*time.Second, calc.InertiaIncrease)
	assert.Equal(t, 2*time.Second, calc.InertiaDecrease)
}

func TestQueueSizeGapDecay_NotEnoughData(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorQueueSizeGapDecay()
	ctx := context.Background()

	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		QueueDuration:         time.Second,
		QueueSize:             625_000,
	})
	assert.Equal(t, Ubps(5_000_000), result.BitRate)
	assert.False(t, result.IsCritical)
}

func TestQueueSizeGapDecay_QueueAboveOptimal(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorQueueSizeGapDecay()
	ctx := context.Background()
	cfg := &AutoBitRateVideoConfig{CheckInterval: time.Second}

	// Feed enough data to make derivative valid
	for i := 0; i < 25; i++ {
		calc.CalculateBitRate(ctx, CalculateBitRateRequest{
			CurrentBitrateSetting: 5_000_000,
			ActualOutputBitrate:   5_000_000,
			InputBitrate:          5_000_000,
			QueueDuration:         10 * time.Second, // well above optimal (3s)
			QueueSize:             6_250_000,
			QueueSizeDerivative:   100_000, // growing queue
			Config:                cfg,
		})
	}

	// With queue above optimal and growing, should decrease bitrate
	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		ActualOutputBitrate:   5_000_000,
		InputBitrate:          5_000_000,
		QueueDuration:         10 * time.Second,
		QueueSize:             6_250_000,
		QueueSizeDerivative:   100_000,
		Config:                cfg,
	})
	assert.Less(t, result.BitRate, Ubps(5_000_000))
}

func TestQueueSizeGapDecay_QueueBelowOptimal(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorQueueSizeGapDecay()
	ctx := context.Background()
	cfg := &AutoBitRateVideoConfig{CheckInterval: time.Second}

	// Feed enough data with queue well below optimal
	for i := 0; i < 25; i++ {
		calc.CalculateBitRate(ctx, CalculateBitRateRequest{
			CurrentBitrateSetting: 5_000_000,
			ActualOutputBitrate:   5_000_000,
			InputBitrate:          5_000_000,
			QueueDuration:         500 * time.Millisecond,
			QueueSize:             312_500,
			QueueSizeDerivative:   -50_000, // shrinking queue
			Config:                cfg,
		})
	}

	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		ActualOutputBitrate:   5_000_000,
		InputBitrate:          5_000_000,
		QueueDuration:         500 * time.Millisecond,
		QueueSize:             312_500,
		QueueSizeDerivative:   -50_000,
		Config:                cfg,
	})
	// With queue below optimal, should increase bitrate
	assert.Greater(t, result.BitRate, Ubps(5_000_000))
}

func TestQueueSizeGapDecay_NoDiffNoBitrateChange(t *testing.T) {
	calc := DefaultAutoBitrateCalculatorQueueSizeGapDecay()
	ctx := context.Background()
	cfg := &AutoBitRateVideoConfig{CheckInterval: time.Second}

	// Feed data at exact optimal conditions
	for i := 0; i < 25; i++ {
		calc.CalculateBitRate(ctx, CalculateBitRateRequest{
			CurrentBitrateSetting: 5_000_000,
			ActualOutputBitrate:   5_000_000,
			InputBitrate:          5_000_000,
			QueueDuration:         3 * time.Second,
			QueueSize:             1_875_000,
			QueueSizeDerivative:   0, // stable queue
			Config:                cfg,
		})
	}

	result := calc.CalculateBitRate(ctx, CalculateBitRateRequest{
		CurrentBitrateSetting: 5_000_000,
		ActualOutputBitrate:   5_000_000,
		InputBitrate:          5_000_000,
		QueueDuration:         3 * time.Second,
		QueueSize:             1_875_000,
		QueueSizeDerivative:   0,
		Config:                cfg,
	})
	// At optimal, should stay close to current
	assert.InDelta(t, float64(5_000_000), float64(result.BitRate), float64(500_000))
}

// --- OutputVideoTrackConfig ---

func TestOutputVideoTrackConfig_GetDecoderHardwareDeviceType(t *testing.T) {
	hwType := globaltypes.HardwareDeviceTypeFromString("cuda")
	cfg := OutputVideoTrackConfig{
		HardwareDeviceType: hwType,
	}
	assert.Equal(t, hwType, cfg.GetDecoderHardwareDeviceType())
}

func TestOutputVideoTrackConfig_GetDecoderHardwareDeviceName(t *testing.T) {
	cfg := OutputVideoTrackConfig{
		HardwareDeviceName: HardwareDeviceName("gpu0"),
	}
	assert.Equal(t, HardwareDeviceName("gpu0"), cfg.GetDecoderHardwareDeviceName())
}

// --- Latencies ---

func TestLatencies_ZeroValues(t *testing.T) {
	l := Latencies{}
	assert.Equal(t, time.Duration(0), l.Audio.PreTranscoding)
	assert.Equal(t, time.Duration(0), l.Video.Transcoding)
}

// --- BitRates ---

func TestBitRates_ZeroValues(t *testing.T) {
	b := BitRates{}
	assert.Equal(t, BitRateInfo{}, b.Input)
	assert.Equal(t, BitRateInfo{}, b.Encoded)
	assert.Equal(t, BitRateInfo{}, b.Output)
}

// --- SenderConfig ---

func TestSenderConfig(t *testing.T) {
	cfg := SenderConfig{OutputThrottlerMaxQueueSizeBytes: 1024 * 1024}
	assert.Equal(t, uint64(1024*1024), cfg.OutputThrottlerMaxQueueSizeBytes)
}

// --- SenderProps ---

func TestSenderProps_Embeds(t *testing.T) {
	props := SenderProps{
		TranscoderConfig: TranscoderConfig{
			Output: TranscoderOutputConfig{
				AudioTrackConfigs: []OutputAudioTrackConfig{
					{CodecName: "aac", SampleRate: audio.SampleRate(48000)},
				},
			},
		},
	}
	assert.Len(t, props.Output.AudioTrackConfigs, 1)
	assert.Equal(t, codectypes.Name("aac"), props.Output.AudioTrackConfigs[0].CodecName)
}
