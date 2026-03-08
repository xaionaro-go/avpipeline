package avpipelinenolibav

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/indicator"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/typesnolibav"
	nodetypes "github.com/xaionaro-go/avpipeline/node/types"
	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	processortypes "github.com/xaionaro-go/avpipeline/processor/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// --- PipelineSideData ---

func TestPipelineSideDataFromProtobuf(t *testing.T) {
	input := []byte(`{"type":"test"}`)
	psd := PipelineSideDataFromProtobuf(input)
	assert.Equal(t, PipelineSideData(input), psd)
}

func TestPipelineSideData_Protobuf(t *testing.T) {
	data := PipelineSideData(`{"foo":"bar"}`)
	pb := data.Protobuf()
	assert.Equal(t, []byte(`{"foo":"bar"}`), pb)
}

func TestPipelineSideDataFromGo_Nil(t *testing.T) {
	psd, err := PipelineSideDataFromGo(nil)
	assert.NoError(t, err)
	assert.Nil(t, psd)
}

func TestPipelineSideDataFromGo_SimpleStruct(t *testing.T) {
	input := map[string]string{"key": "value"}
	psd, err := PipelineSideDataFromGo(input)
	assert.NoError(t, err)
	assert.NotNil(t, psd)
	assert.Contains(t, string(psd), "key")
}

// --- InputConfig ---

func TestInputConfigFromProto_Nil(t *testing.T) {
	cfg := InputConfigFromProto(nil)
	assert.Equal(t, kerneltypes.InputConfig{}, cfg)
}

func TestInputConfigFromProto_WithValues(t *testing.T) {
	forceRT := true
	startPTS := int64(1000)
	startDTS := int64(2000)
	proto := &avpipelinegrpc.InputConfig{
		CustomOptions: []*avpipelinegrpc.CustomOption{
			{Key: "k1", Value: "v1"},
		},
		RecvBufferSize:     65536,
		AsyncOpen:          true,
		AutoClose:          true,
		IgnoreIncorrectDts: true,
		IgnoreZeroDuration: true,
		ForceRealTime:      &forceRT,
		ForceStartPts:      &startPTS,
		ForceStartDts:      &startDTS,
	}
	cfg := InputConfigFromProto(proto)
	assert.Equal(t, uint(65536), cfg.RecvBufferSize)
	assert.True(t, cfg.AsyncOpen)
	assert.True(t, cfg.AutoClose)
	assert.True(t, cfg.IgnoreIncorrectDTS)
	assert.True(t, cfg.IgnoreZeroDuration)
	require.NotNil(t, cfg.ForceRealTime)
	assert.True(t, *cfg.ForceRealTime)
	require.NotNil(t, cfg.ForceStartPTS)
	assert.Equal(t, int64(1000), *cfg.ForceStartPTS)
	require.Len(t, cfg.CustomOptions, 1)
	assert.Equal(t, "k1", cfg.CustomOptions[0].Key)
}

func TestInputConfigToProto(t *testing.T) {
	forceRT := true
	cfg := kerneltypes.InputConfig{
		RecvBufferSize: 32768,
		AsyncOpen:      true,
		ForceRealTime:  &forceRT,
		CustomOptions: globaltypes.DictionaryItems{
			{Key: "opt1", Value: "val1"},
		},
	}
	proto := InputConfigToProto(cfg)
	require.NotNil(t, proto)
	assert.Equal(t, uint32(32768), proto.RecvBufferSize)
	assert.True(t, proto.AsyncOpen)
	require.NotNil(t, proto.ForceRealTime)
	assert.True(t, *proto.ForceRealTime)
	require.Len(t, proto.CustomOptions, 1)
	assert.Equal(t, "opt1", proto.CustomOptions[0].Key)
}

func TestInputConfig_RoundTrip(t *testing.T) {
	forceRT := false
	startPTS := int64(5000)
	original := kerneltypes.InputConfig{
		RecvBufferSize:     8192,
		AsyncOpen:          true,
		AutoClose:          false,
		IgnoreIncorrectDTS: true,
		IgnoreZeroDuration: false,
		ForceRealTime:      &forceRT,
		ForceStartPTS:      &startPTS,
		CustomOptions: globaltypes.DictionaryItems{
			{Key: "a", Value: "b"},
			{Key: "c", Value: "d"},
		},
	}
	proto := InputConfigToProto(original)
	roundTripped := InputConfigFromProto(proto)
	assert.Equal(t, original.RecvBufferSize, roundTripped.RecvBufferSize)
	assert.Equal(t, original.AsyncOpen, roundTripped.AsyncOpen)
	assert.Equal(t, original.IgnoreIncorrectDTS, roundTripped.IgnoreIncorrectDTS)
	assert.Equal(t, *original.ForceRealTime, *roundTripped.ForceRealTime)
	assert.Equal(t, *original.ForceStartPTS, *roundTripped.ForceStartPTS)
	assert.Len(t, roundTripped.CustomOptions, 2)
}

func TestCustomOptions_Nil(t *testing.T) {
	assert.Nil(t, customOptionsFromProto(nil))
	assert.Nil(t, customOptionsToProto(nil))
}

// --- NodeCounters ---

func TestNodeCountersToGRPC_Nil(t *testing.T) {
	assert.Nil(t, NodeCountersToGRPC(nil, nil))
	assert.Nil(t, NodeCountersToGRPC(nodetypes.NewCounters(), nil))
	assert.Nil(t, NodeCountersToGRPC(nil, processortypes.NewCounters()))
}

func TestNodeCountersToGRPC_WithData(t *testing.T) {
	nc := nodetypes.NewCounters()
	pc := processortypes.NewCounters()

	// Add some data
	nc.Received.Packets.Video.Increment(1024)
	pc.Processed.Frames.Audio.Increment(512)

	grpc := NodeCountersToGRPC(nc, pc)
	require.NotNil(t, grpc)
	require.NotNil(t, grpc.Received)
	require.NotNil(t, grpc.Received.Packets)
	require.NotNil(t, grpc.Received.Packets.Video)
	assert.Equal(t, uint64(1), grpc.Received.Packets.Video.Count)
	assert.Equal(t, uint64(1024), grpc.Received.Packets.Video.Bytes)

	require.NotNil(t, grpc.Processed)
	require.NotNil(t, grpc.Processed.Frames)
	require.NotNil(t, grpc.Processed.Frames.Audio)
	assert.Equal(t, uint64(1), grpc.Processed.Frames.Audio.Count)
	assert.Equal(t, uint64(512), grpc.Processed.Frames.Audio.Bytes)
}

func TestCountersSectionToGRPC_Nil(t *testing.T) {
	assert.Nil(t, CountersSectionToGRPC(nil))
}

func TestCountersSubSectionToGRPC_Nil(t *testing.T) {
	assert.Nil(t, CountersSubSectionToGRPC(nil))
}

func TestCountersItemToGRPC_Nil(t *testing.T) {
	assert.Nil(t, CountersItemToGRPC(nil))
}

// --- Resolution ---

func TestResolutionFromProto_Nil(t *testing.T) {
	assert.Nil(t, ResolutionFromProto(nil))
}

func TestResolutionFromProto(t *testing.T) {
	proto := &avpipelinegrpc.Resolution{Width: 1920, Height: 1080}
	r := ResolutionFromProto(proto)
	require.NotNil(t, r)
	assert.Equal(t, uint32(1920), r.Width)
	assert.Equal(t, uint32(1080), r.Height)
}

func TestResolutionToProto(t *testing.T) {
	r := codectypes.Resolution{Width: 1280, Height: 720}
	proto := ResolutionToProto(r)
	assert.Equal(t, uint32(1280), proto.Width)
	assert.Equal(t, uint32(720), proto.Height)
}

func TestResolution_RoundTrip(t *testing.T) {
	original := codectypes.Resolution{Width: 3840, Height: 2160}
	proto := ResolutionToProto(original)
	result := ResolutionFromProto(proto)
	assert.Equal(t, original, *result)
}

// --- FPSReducer ---

func TestFPSReductionRangeFromProto_Nil(t *testing.T) {
	assert.Nil(t, FPSReductionRangeFromProto(nil))
}

func TestFPSReductionRangeFromProto(t *testing.T) {
	proto := &avpipelinegrpc.FPSReductionRange{
		BitrateMinBps: 100_000,
		BitrateMaxBps: 500_000,
		FractionNum:   1,
		FractionDen:   2,
	}
	r := FPSReductionRangeFromProto(proto)
	require.NotNil(t, r)
	assert.Equal(t, smtypes.Ubps(100_000), r.BitrateMin)
	assert.Equal(t, smtypes.Ubps(500_000), r.BitrateMax)
	assert.Equal(t, 1, r.Fraction.Num)
	assert.Equal(t, 2, r.Fraction.Den)
}

func TestFPSReductionRangeToProto_Nil(t *testing.T) {
	assert.Nil(t, FPSReductionRangeToProto(nil))
}

func TestFPSReductionRange_RoundTrip(t *testing.T) {
	original := &smtypes.FPSReductionRange{
		BitrateMin: 200_000,
		BitrateMax: 800_000,
		Fraction:   globaltypes.Rational{Num: 1, Den: 4},
	}
	proto := FPSReductionRangeToProto(original)
	result := FPSReductionRangeFromProto(proto)
	assert.Equal(t, original, result)
}

func TestFPSReducerConfigFromProto_Nil(t *testing.T) {
	assert.Nil(t, FPSReducerConfigFromProto(nil))
}

func TestFPSReducerConfigFromProto_EmptyRanges(t *testing.T) {
	proto := &avpipelinegrpc.FPSReducerConfig{Ranges: nil}
	assert.Nil(t, FPSReducerConfigFromProto(proto))
}

func TestFPSReducerConfigToProto_Empty(t *testing.T) {
	assert.Nil(t, FPSReducerConfigToProto(nil))
}

func TestFPSReducerConfig_RoundTrip(t *testing.T) {
	original := smtypes.FPSReducerConfig{
		{BitrateMin: 0, BitrateMax: 200_000, Fraction: globaltypes.Rational{Num: 1, Den: 4}},
		{BitrateMin: 200_001, BitrateMax: 500_000, Fraction: globaltypes.Rational{Num: 1, Den: 2}},
	}
	proto := FPSReducerConfigToProto(original)
	result := FPSReducerConfigFromProto(proto)
	assert.Equal(t, original, result)
}

// --- MovingAverage ---

func TestMovingAverageToGRPC_Nil(t *testing.T) {
	assert.Nil(t, MovingAverageToGRPC[float64](nil))
}

func TestMovingAverageToGRPC_MAMA(t *testing.T) {
	ma := indicator.NewMAMA[float64](10, 0.5, 0.05)
	proto := MovingAverageToGRPC(ma)
	require.NotNil(t, proto)
	mamaCfg, ok := proto.GetMovingAverageConfig().(*avpipelinegrpc.MovingAverageConfig_Mama)
	require.True(t, ok)
	assert.Equal(t, 0.5, mamaCfg.Mama.FastLimit)
	assert.Equal(t, 0.05, mamaCfg.Mama.SlowLimit)
}

func TestMovingAverageFromGRPC_Nil(t *testing.T) {
	assert.Nil(t, MovingAverageFromGRPC[float64](nil))
}

func TestMovingAverageFromGRPC_MAMA(t *testing.T) {
	proto := &avpipelinegrpc.MovingAverageConfig{
		MovingAverageConfig: &avpipelinegrpc.MovingAverageConfig_Mama{
			Mama: &avpipelinegrpc.MovingAverageConfigMAMA{
				FastLimit: 0.3,
				SlowLimit: 0.05,
			},
		},
	}
	ma := MovingAverageFromGRPC[float64](proto)
	require.NotNil(t, ma)
}

// --- AutoBitRateCalculator ---

func TestAutoBitRateCalculatorFromProto_Nil(t *testing.T) {
	calc, err := AutoBitRateCalculatorFromProto(nil)
	assert.NoError(t, err)
	assert.Nil(t, calc)
}

func TestAutoBitRateCalculatorFromProto_Static(t *testing.T) {
	proto := &avpipelinegrpc.AutoBitrateCalculator{
		AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_Static{
			Static: 5_000_000,
		},
	}
	calc, err := AutoBitRateCalculatorFromProto(proto)
	assert.NoError(t, err)
	require.NotNil(t, calc)
	static, ok := calc.(smtypes.AutoBitrateCalculatorStatic)
	assert.True(t, ok)
	assert.Equal(t, smtypes.AutoBitrateCalculatorStatic(5_000_000), static)
}

func TestAutoBitRateCalculatorFromProto_Thresholds(t *testing.T) {
	proto := &avpipelinegrpc.AutoBitrateCalculator{
		AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_Thresholds{
			Thresholds: &avpipelinegrpc.AutoBitRateCalculatorThresholds{
				OutputExtremelyHighQueueSizeDurationMs: 30000,
				OutputVeryHighQueueSizeDurationMs:      5000,
				OutputHighQueueSizeDurationMs:          2000,
				OutputLowQueueSizeDurationMs:           1000,
				OutputVeryLowQueueSizeDurationMs:       500,
				IncreaseK:                              1.01,
				DecreaseK:                              0.95,
			},
		},
	}
	calc, err := AutoBitRateCalculatorFromProto(proto)
	assert.NoError(t, err)
	th, ok := calc.(*smtypes.AutoBitrateCalculatorThresholds)
	require.True(t, ok)
	assert.Equal(t, 30*time.Second, th.OutputExtremelyHighQueueSizeDuration)
	assert.Equal(t, 1.01, th.IncreaseK)
}

func TestAutoBitRateCalculatorFromProto_LogK(t *testing.T) {
	proto := &avpipelinegrpc.AutoBitrateCalculator{
		AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_LogK{
			LogK: &avpipelinegrpc.AutoBitrateCalculatorLogK{
				QueueOptimalMs: 1000,
				Inertia:        0.7,
			},
		},
	}
	calc, err := AutoBitRateCalculatorFromProto(proto)
	assert.NoError(t, err)
	logK, ok := calc.(*smtypes.AutoBitrateCalculatorLogK)
	require.True(t, ok)
	assert.Equal(t, time.Second, logK.QueueOptimal)
	assert.Equal(t, 0.7, logK.Inertia)
}

func TestAutoBitRateCalculatorFromProto_QueueSizeGapDecay(t *testing.T) {
	proto := &avpipelinegrpc.AutoBitrateCalculator{
		AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_QueueSizeGapDecay{
			QueueSizeGapDecay: &avpipelinegrpc.AutoBitrateCalculatorQueueSizeGapDecay{
				QueueOptimalMs:    3000,
				QueueSizeMin:      200_000,
				GapDecayMs:        3000,
				IncreaseInertiaMs: 10000,
			},
		},
	}
	calc, err := AutoBitRateCalculatorFromProto(proto)
	assert.NoError(t, err)
	decay, ok := calc.(*smtypes.AutoBitrateCalculatorQueueSizeGapDecay)
	require.True(t, ok)
	assert.Equal(t, 3*time.Second, decay.QueueDurationOptimal)
	assert.Equal(t, smtypes.UB(200_000), decay.QueueSizeMin)
}

func TestAutoBitRateCalculatorToProto_Nil(t *testing.T) {
	proto, err := AutoBitRateCalculatorToProto(nil)
	assert.NoError(t, err)
	assert.Nil(t, proto)
}

func TestAutoBitRateCalculatorToProto_Static(t *testing.T) {
	calc := smtypes.AutoBitrateCalculatorStatic(3_000_000)
	proto, err := AutoBitRateCalculatorToProto(calc)
	assert.NoError(t, err)
	require.NotNil(t, proto)
	static, ok := proto.GetAutoBitrateCalculator().(*avpipelinegrpc.AutoBitrateCalculator_Static)
	require.True(t, ok)
	assert.Equal(t, uint64(3_000_000), static.Static)
}

func TestAutoBitRateCalculatorToProto_Thresholds(t *testing.T) {
	calc := smtypes.DefaultAutoBitrateCalculatorThresholds()
	proto, err := AutoBitRateCalculatorToProto(calc)
	assert.NoError(t, err)
	require.NotNil(t, proto)
	th, ok := proto.GetAutoBitrateCalculator().(*avpipelinegrpc.AutoBitrateCalculator_Thresholds)
	require.True(t, ok)
	assert.Equal(t, uint64(30000), th.Thresholds.OutputExtremelyHighQueueSizeDurationMs)
}

func TestAutoBitRateCalculator_RoundTrip_Static(t *testing.T) {
	original := smtypes.AutoBitrateCalculatorStatic(7_000_000)
	proto, err := AutoBitRateCalculatorToProto(original)
	require.NoError(t, err)
	result, err := AutoBitRateCalculatorFromProto(proto)
	require.NoError(t, err)
	assert.Equal(t, original, result)
}

// --- AutoBitRateConfig ---

func TestAutoBitRateResolutionAndBitRateConfigFromProto_Nil(t *testing.T) {
	assert.Nil(t, AutoBitRateResolutionAndBitRateConfigFromProto(nil))
}

func TestAutoBitRateResolutionAndBitRateConfigFromProto_NoResolution(t *testing.T) {
	proto := &avpipelinegrpc.AutoBitRateResolutionAndBitRateConfig{
		BitrateHighBps: 5_000_000,
	}
	assert.Nil(t, AutoBitRateResolutionAndBitRateConfigFromProto(proto))
}

func TestAutoBitRateResolutionAndBitRateConfig_RoundTrip(t *testing.T) {
	original := &smtypes.AutoBitRateResolutionAndBitRateConfig{
		Resolution:  codectypes.Resolution{Width: 1920, Height: 1080},
		BitrateHigh: 10_000_000,
		BitrateLow:  3_000_000,
	}
	proto := AutoBitRateResolutionAndBitRateConfigToProto(original)
	result := AutoBitRateResolutionAndBitRateConfigFromProto(proto)
	require.NotNil(t, result)
	assert.Equal(t, original, result)
}

func TestAutoBitRateResolutionAndBitRateConfigsFromProto_Nil(t *testing.T) {
	assert.Nil(t, AutoBitRateResolutionAndBitRateConfigsFromProto(nil))
}

func TestAutoBitRateResolutionAndBitRateConfigsToProto_Nil(t *testing.T) {
	assert.Nil(t, AutoBitRateResolutionAndBitRateConfigsToProto(nil))
}

func TestAutoBitRateResolutionAndBitRateConfigs_RoundTrip(t *testing.T) {
	original := smtypes.AutoBitRateResolutionAndBitRateConfigs{
		{Resolution: codectypes.Resolution{Width: 640, Height: 480}, BitrateHigh: 2_000_000, BitrateLow: 500_000},
		{Resolution: codectypes.Resolution{Width: 1920, Height: 1080}, BitrateHigh: 10_000_000, BitrateLow: 3_000_000},
	}
	proto := AutoBitRateResolutionAndBitRateConfigsToProto(original)
	result := AutoBitRateResolutionAndBitRateConfigsFromProto(proto)
	assert.Equal(t, original, result)
}

func TestAutoBitRateVideoConfigFromProto_Nil(t *testing.T) {
	cfg, err := AutoBitRateVideoConfigFromProto(nil)
	assert.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestAutoBitRateVideoConfigToProto_Nil(t *testing.T) {
	cfg, err := AutoBitRateVideoConfigToProto(nil)
	assert.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestAutoBitRateVideoConfig_RoundTrip(t *testing.T) {
	original := &smtypes.AutoBitRateVideoConfig{
		ResolutionsAndBitRates: smtypes.AutoBitRateResolutionAndBitRateConfigs{
			{Resolution: codectypes.Resolution{Width: 1920, Height: 1080}, BitrateHigh: 10_000_000, BitrateLow: 3_000_000},
		},
		Calculator:    smtypes.AutoBitrateCalculatorStatic(5_000_000),
		CheckInterval: time.Second,
		AutoByPass:    true,
		MaxBitRate:    20_000_000,
		MinBitRate:    500_000,
		FPSReducer: smtypes.FPSReducerConfig{
			{BitrateMin: 0, BitrateMax: 500_000, Fraction: globaltypes.Rational{Num: 1, Den: 2}},
		},
		BitRateIncreaseSlowdown:              5 * time.Second,
		ResolutionUpgradeSlowdownMinDuration: 10 * time.Second,
		ResolutionDowngradeSlowdownDuration:  2 * time.Second,
	}
	proto, err := AutoBitRateVideoConfigToProto(original)
	require.NoError(t, err)
	result, err := AutoBitRateVideoConfigFromProto(proto)
	require.NoError(t, err)

	assert.Equal(t, original.CheckInterval, result.CheckInterval)
	assert.Equal(t, original.AutoByPass, result.AutoByPass)
	assert.Equal(t, original.MaxBitRate, result.MaxBitRate)
	assert.Equal(t, original.MinBitRate, result.MinBitRate)
	assert.Equal(t, original.BitRateIncreaseSlowdown, result.BitRateIncreaseSlowdown)
	assert.Equal(t, original.ResolutionUpgradeSlowdownMinDuration, result.ResolutionUpgradeSlowdownMinDuration)
	assert.Equal(t, original.ResolutionDowngradeSlowdownDuration, result.ResolutionDowngradeSlowdownDuration)
	assert.Len(t, result.ResolutionsAndBitRates, 1)
	assert.Len(t, result.FPSReducer, 1)
}
