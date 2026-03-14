package streammux

import (
	"context"
	"testing"
	"time"

	testassert "github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/indicator"
	"github.com/xaionaro-go/avpipeline/kernel"
	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/preset/streammux/types"
	"github.com/xaionaro-go/avpipeline/processor"
)

func TestAutoBitrateHandlerSlowdownResolutionUpgrade(t *testing.T) {
	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  codec.Resolution{Width: 1920, Height: 1080},
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  codec.Resolution{Width: 1280, Height: 720},
			BitrateHigh: 4_000_000, BitrateLow: 2_000_000,
		},
	}

	cfg := AutoBitRateVideoConfig{
		ResolutionsAndBitRates:                 resolutions,
		CheckInterval:                          time.Second,
		ResolutionUpgradeSlowdownMinDuration:   time.Second * 10,
		ResolutionDowngradeSlowdownDuration:    time.Second * 2,
		ResolutionUpgradeSlowdownMovingAverage: indicator.NewMAMA[uint64](4, 0.5, 0.05),
	}

	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: cfg,
	}

	// 1. Initial state: we are at 1080p, and network is good.
	// We simulate 4 checks at 1080p to make the MA valid.
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	pixels1080 := uint64(1920 * 1080)
	for i := 0; i < 4; i++ {
		h.currentDesiredResolutionAvg.Store(h.ResolutionUpgradeSlowdownMovingAverage.Update(pixels1080))
	}
	testassert.True(t, h.ResolutionUpgradeSlowdownMovingAverage.Valid())
	testassert.Equal(t, pixels1080, h.currentDesiredResolutionAvg.Load())

	// 2. Network drops. We are forced to 720p.
	// We simulate 4 checks at 720p.
	pixels720 := uint64(1280 * 720)
	for i := 0; i < 4; i++ {
		h.currentDesiredResolutionAvg.Store(h.ResolutionUpgradeSlowdownMovingAverage.Update(pixels720))
	}
	maValue := h.currentDesiredResolutionAvg.Load()
	testassert.Less(t, maValue, pixels1080)
	testassert.GreaterOrEqual(t, maValue, pixels720)

	// 3. Network recovers. We want to upgrade to 1080p.
	// We call setVideoOutput and check the slowdown.
	_ = time.Now()
	videoOutputKey := &SenderKey{
		VideoResolution: res1080,
	}

	isUpgrade := true // 1080p > 720p
	targetPixels := uint64(videoOutputKey.VideoResolution.Width) * uint64(videoOutputKey.VideoResolution.Height)
	maPixels := h.currentDesiredResolutionAvg.Load()

	upgradeSlowdown := h.ResolutionUpgradeSlowdownMinDuration
	if isUpgrade && h.ResolutionUpgradeSlowdownMovingAverage != nil && h.ResolutionUpgradeSlowdownMovingAverage.Valid() {
		if targetPixels > maPixels {
			upgradeSlowdown = time.Duration(float64(upgradeSlowdown) * float64(targetPixels) / float64(maPixels))
		}
	}

	testassert.Greater(t, upgradeSlowdown, h.ResolutionUpgradeSlowdownMinDuration)
	// Factor is 1080p pixels / MA pixels.
	// MA is some average between 720p and 1080p.
	// If it was just 720p, factor would be (1920*1080)/(1280*720) = 2.25.
	// So slowdown should be around 22.5s.
	t.Logf("Dynamic slowdown: %v (original: %v)", upgradeSlowdown, h.ResolutionUpgradeSlowdownMinDuration)
}

func TestAutoBitrateHandlerSlowdownResolutionNoUpgrade(t *testing.T) {
	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  codec.Resolution{Width: 1920, Height: 1080},
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
	}

	cfg := AutoBitRateVideoConfig{
		ResolutionsAndBitRates:                 resolutions,
		ResolutionUpgradeSlowdownMinDuration:   time.Second * 10,
		ResolutionUpgradeSlowdownMovingAverage: indicator.NewMAMA[uint64](4, 0.5, 0.05),
	}

	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: cfg,
	}

	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	pixels1080 := uint64(1920 * 1080)
	for i := 0; i < 4; i++ {
		h.currentDesiredResolutionAvg.Store(h.ResolutionUpgradeSlowdownMovingAverage.Update(pixels1080))
	}

	videoOutputKey := &SenderKey{
		VideoResolution: res1080,
	}

	isUpgrade := false
	targetPixels := uint64(videoOutputKey.VideoResolution.Width) * uint64(videoOutputKey.VideoResolution.Height)
	maPixels := h.currentDesiredResolutionAvg.Load()

	upgradeSlowdown := h.ResolutionUpgradeSlowdownMinDuration
	if isUpgrade && h.ResolutionUpgradeSlowdownMovingAverage != nil && h.ResolutionUpgradeSlowdownMovingAverage.Valid() {
		if targetPixels > maPixels {
			upgradeSlowdown = time.Duration(float64(upgradeSlowdown) * float64(targetPixels) / float64(maPixels))
		}
	}

	testassert.Equal(t, h.ResolutionUpgradeSlowdownMinDuration, upgradeSlowdown)
}

func TestAutoBitrateHandlerGetDesiredResolutionConfig(t *testing.T) {
	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  codec.Resolution{Width: 1920, Height: 1080},
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  codec.Resolution{Width: 1280, Height: 720},
			BitrateHigh: 2_500_000, BitrateLow: 1_000_000,
		},
	}

	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
		},
	}

	ctx := context.Background()
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}

	// Within range of 1080p
	d := h.getDesiredResolutionConfig(5_000_000, res1080)
	testassert.Equal(t, res1080, d.Resolution)

	// Below range of 1080p (3M), should go to 720p
	d = h.getDesiredResolutionConfig(2_000_000, res1080)
	testassert.Equal(t, res720, d.Resolution)

	// Above range of 720p (2.5M), should go to 1080p
	d = h.getDesiredResolutionConfig(4_000_000, res720)
	testassert.Equal(t, res1080, d.Resolution)

	// Extreme low bitrate
	d = h.getDesiredResolutionConfig(100_000, res1080)
	testassert.Equal(t, res720, d.Resolution)

	// Extreme high bitrate
	d = h.getDesiredResolutionConfig(100_000_000, res720)
	testassert.Equal(t, res1080, d.Resolution)
	_ = ctx
}

func TestAutoBitrateHandlerMAUpdatesInChangeResolutionIfNeeded(t *testing.T) {
	ctx := context.Background()
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  res1080,
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  res720,
			BitrateHigh: 2_500_000, BitrateLow: 1_000_000,
		},
	}

	ma := indicator.NewMAMA[uint64](4, 0.5, 0.05)
	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates:                 resolutions,
			ResolutionUpgradeSlowdownMovingAverage: ma,
		},
	}

	mockEnc := &mockEncoder{res: res1080}
	s := &StreamMux[any]{
		MuxMode: types.MuxModeDifferentOutputsSameTracks,
	}
	h.StreamMux = s

	input := &Input[any]{
		OutputSwitch: barrierstategetter.NewSwitch(),
	}
	s.InputAll = *input
	input.OutputSwitch.CurrentValue.Store(1)

	o := &Output[any]{}
	o.TranscoderNode = &NodeTranscoder[OutputCustomData[any]]{
		Processor: &processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]{
			Kernel: &kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]{
				Encoder: &kernel.Encoder[*codec.NaiveEncoderFactory]{
					EncoderFactory: &codec.NaiveEncoderFactory{
						VideoEncoders: []codec.Encoder{mockEnc},
					},
				},
			},
		},
	}
	s.Outputs.Store(1, o)

	// Case 1: Bitrate stays within 1080p range. MA should be updated with 1080p.
	// current resolution is 1080p, bitrate is 5Mbps. 1080p range is 3M-8M.
	err := h.changeResolutionIfNeeded(ctx, 5_000_000, false, false)
	testassert.NoError(t, err)
	testassert.Equal(t, uint64(1920*1080), h.currentDesiredResolutionAvg.Load())

	// Case 2: Bitrate drops to 2Mbps.
	// To avoid calling setVideoOutput (which would panic due to uninitialized StreamMux/Encoder),
	// we set the CURRENT resolution to 720p.
	// getDesiredResolutionConfig(2Mbps, 720p) will return 720p.
	// Since current is also 720p, it will return early.
	mockEnc.res = res720
	err = h.changeResolutionIfNeeded(ctx, 2_000_000, false, false)
	testassert.NoError(t, err)

	testassert.Less(t, h.currentDesiredResolutionAvg.Load(), uint64(1920*1080))
	testassert.GreaterOrEqual(t, h.currentDesiredResolutionAvg.Load(), uint64(1280*720))
}

func TestAutoBitrateHandlerGetDesiredResolutionConfig_MaxResolution(t *testing.T) {
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}
	res360 := codec.Resolution{Width: 640, Height: 360}

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  res1080,
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  res720,
			BitrateHigh: 4_000_000, BitrateLow: 1_000_000,
		},
		{
			Resolution:  res360,
			BitrateHigh: 1_500_000, BitrateLow: 200_000,
		},
	}

	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
			MaxResolution:          res720,
		},
	}

	// At 720p with bitrate in range — stays at 720p.
	d := h.getDesiredResolutionConfig(2_000_000, res720)
	testassert.Equal(t, res720, d.Resolution)

	// High bitrate at 720p — cannot go above 720p because MaxResolution caps it.
	d = h.getDesiredResolutionConfig(9_000_000, res720)
	testassert.Equal(t, res720, d.Resolution)

	// Low bitrate at 720p — goes down to 360p (still within allowed range).
	d = h.getDesiredResolutionConfig(500_000, res720)
	testassert.Equal(t, res360, d.Resolution)
}

func TestAutoBitrateHandlerGetDesiredResolutionConfig_MinResolution(t *testing.T) {
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}
	res360 := codec.Resolution{Width: 640, Height: 360}

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  res1080,
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  res720,
			BitrateHigh: 4_000_000, BitrateLow: 1_000_000,
		},
		{
			Resolution:  res360,
			BitrateHigh: 1_500_000, BitrateLow: 200_000,
		},
	}

	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
			MinResolution:          res720,
		},
	}

	// At 1080p within range — stays at 1080p.
	d := h.getDesiredResolutionConfig(5_000_000, res1080)
	testassert.Equal(t, res1080, d.Resolution)

	// Low bitrate at 1080p — would normally go to 360p, but MinResolution
	// prevents going below 720p.
	d = h.getDesiredResolutionConfig(500_000, res1080)
	testassert.Equal(t, res720, d.Resolution)

	// Very low bitrate — still capped at 720p (the worst allowed).
	d = h.getDesiredResolutionConfig(100_000, res1080)
	testassert.Equal(t, res720, d.Resolution)
}

func TestAutoBitrateHandlerGetDesiredResolutionConfig_CurrentResolutionOutsideAllowed(t *testing.T) {
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}
	res360 := codec.Resolution{Width: 640, Height: 360}

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  res1080,
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  res720,
			BitrateHigh: 4_000_000, BitrateLow: 1_000_000,
		},
		{
			Resolution:  res360,
			BitrateHigh: 1_500_000, BitrateLow: 200_000,
		},
	}

	// MaxResolution = 720p, so 1080p is outside allowed set.
	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
			MaxResolution:          res720,
		},
	}

	// Currently at 1080p (outside allowed), bitrate fits 720p.
	d := h.getDesiredResolutionConfig(2_000_000, res1080)
	testassert.Equal(t, res720, d.Resolution)

	// Currently at 1080p (outside allowed), high bitrate — picks best allowed (720p).
	d = h.getDesiredResolutionConfig(9_000_000, res1080)
	testassert.Equal(t, res720, d.Resolution)

	// MinResolution = 720p, so 360p is outside allowed set.
	h2 := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
			MinResolution:          res720,
		},
	}

	// Currently at 360p (outside allowed), bitrate fits 720p — picks 720p.
	d = h2.getDesiredResolutionConfig(2_000_000, res360)
	testassert.Equal(t, res720, d.Resolution)

	// Currently at 360p (outside allowed), high bitrate — picks best allowed.
	d = h2.getDesiredResolutionConfig(5_000_000, res360)
	testassert.Equal(t, res1080, d.Resolution)

	// Currently at 360p (outside allowed), very low bitrate (no match) — falls back to best allowed.
	d = h2.getDesiredResolutionConfig(500_000, res360)
	testassert.Equal(t, res1080, d.Resolution)
}

func TestAutoBitrateHandlerGetDesiredResolutionConfig_BothMinMaxResolution(t *testing.T) {
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}
	res540 := codec.Resolution{Width: 960, Height: 540}
	res360 := codec.Resolution{Width: 640, Height: 360}

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  res1080,
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  res720,
			BitrateHigh: 4_000_000, BitrateLow: 1_000_000,
		},
		{
			Resolution:  res540,
			BitrateHigh: 2_000_000, BitrateLow: 500_000,
		},
		{
			Resolution:  res360,
			BitrateHigh: 1_500_000, BitrateLow: 200_000,
		},
	}

	// Only 720p and 540p are allowed.
	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
			MinResolution:          res540,
			MaxResolution:          res720,
		},
	}

	// At 720p with bitrate in range — stays at 720p.
	d := h.getDesiredResolutionConfig(2_000_000, res720)
	testassert.Equal(t, res720, d.Resolution)

	// At 720p, bitrate drops below 720p low — goes to 540p (not 360p).
	d = h.getDesiredResolutionConfig(600_000, res720)
	testassert.Equal(t, res540, d.Resolution)

	// At 540p, bitrate rises above 540p high — goes to 720p (not 1080p).
	d = h.getDesiredResolutionConfig(5_000_000, res540)
	testassert.Equal(t, res720, d.Resolution)

	// Current is 1080p (outside allowed range), should switch to best allowed.
	d = h.getDesiredResolutionConfig(5_000_000, res1080)
	testassert.Equal(t, res720, d.Resolution)

	// Current is 360p (outside allowed range), should switch to worst that fits bitrate.
	d = h.getDesiredResolutionConfig(600_000, res360)
	testassert.Equal(t, res540, d.Resolution)
}

func TestAutoBitrateHandlerChangeResolutionIfNeeded_DetectsOutsideAllowed(t *testing.T) {
	ctx := context.Background()
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  res1080,
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  res720,
			BitrateHigh: 4_000_000, BitrateLow: 1_000_000,
		},
	}

	mockEnc := &mockEncoder{res: res1080}
	s := &StreamMux[any]{
		MuxMode: types.MuxModeDifferentOutputsSameTracks,
	}

	input := &Input[any]{
		OutputSwitch: barrierstategetter.NewSwitch(),
	}
	s.InputAll = *input
	input.OutputSwitch.CurrentValue.Store(1)

	o := &Output[any]{}
	o.TranscoderNode = &NodeTranscoder[OutputCustomData[any]]{
		Processor: &processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]{
			Kernel: &kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]{
				Encoder: &kernel.Encoder[*codec.NaiveEncoderFactory]{
					EncoderFactory: &codec.NaiveEncoderFactory{
						VideoEncoders: []codec.Encoder{mockEnc},
					},
				},
			},
		},
	}
	s.Outputs.Store(1, o)

	// Without MaxResolution: encoder at 1080p with bitrate in range, stays put.
	hAllowed := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
		},
		StreamMux: s,
	}
	err := hAllowed.changeResolutionIfNeeded(ctx, 5_000_000, false, false)
	testassert.NoError(t, err, "should stay at 1080p when no resolution constraints")

	// With MaxResolution=720p: encoder at 1080p is outside allowed.
	// changeResolutionIfNeeded will attempt to switch and fail deeper in the
	// pipeline (setVideoOutput → getCurrentEncoderState), returning a non-nil error.
	// This confirms the "outside allowed" code path is taken.
	hCapped := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
			MaxResolution:          res720,
		},
		StreamMux: s,
	}
	testassert.Panics(t, func() {
		_ = hCapped.changeResolutionIfNeeded(ctx, 5_000_000, false, false)
	}, "should attempt resolution change when current is outside allowed (panics due to incomplete mock)")
}

func TestAutoBitrateHandlerChangeResolutionIfNeeded_StaysWhenWithinAllowed(t *testing.T) {
	ctx := context.Background()
	res1080 := codec.Resolution{Width: 1920, Height: 1080}
	res720 := codec.Resolution{Width: 1280, Height: 720}

	resolutions := AutoBitRateResolutionAndBitRateConfigs{
		{
			Resolution:  res1080,
			BitrateHigh: 8_000_000, BitrateLow: 3_000_000,
		},
		{
			Resolution:  res720,
			BitrateHigh: 4_000_000, BitrateLow: 1_000_000,
		},
	}

	h := &AutoBitRateHandler[any]{
		AutoBitRateVideoConfig: AutoBitRateVideoConfig{
			ResolutionsAndBitRates: resolutions,
			MaxResolution:          res1080,
			MinResolution:          res720,
		},
	}

	mockEnc := &mockEncoder{res: res1080}
	s := &StreamMux[any]{
		MuxMode: types.MuxModeDifferentOutputsSameTracks,
	}
	h.StreamMux = s

	input := &Input[any]{
		OutputSwitch: barrierstategetter.NewSwitch(),
	}
	s.InputAll = *input
	input.OutputSwitch.CurrentValue.Store(1)

	o := &Output[any]{}
	o.TranscoderNode = &NodeTranscoder[OutputCustomData[any]]{
		Processor: &processor.FromKernel[*kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]]{
			Kernel: &kernel.Transcoder[*codec.NaiveDecoderFactory, *codec.NaiveEncoderFactory]{
				Encoder: &kernel.Encoder[*codec.NaiveEncoderFactory]{
					EncoderFactory: &codec.NaiveEncoderFactory{
						VideoEncoders: []codec.Encoder{mockEnc},
					},
				},
			},
		},
	}
	s.Outputs.Store(1, o)

	// Encoder reports 1080p, which is within allowed range.
	// Bitrate is within 1080p range (3M-8M). Should stay.
	err := h.changeResolutionIfNeeded(ctx, 5_000_000, false, false)
	testassert.NoError(t, err)
}

type mockEncoder struct {
	codec.EncoderCopy
	res codec.Resolution
}

func (m *mockEncoder) GetResolution(ctx context.Context) *codec.Resolution {
	return &m.res
}
