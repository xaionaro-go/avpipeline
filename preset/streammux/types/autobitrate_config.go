package types

import (
	"context"
	"time"

	"github.com/dustin/go-humanize"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
)

const (
	queueDurationError = 20 * time.Millisecond
)

type BitRateChangeRequest struct {
	BitRate    uint64
	IsCritical bool
}

type AutoBitRateCalculator interface {
	CalculateBitRate(
		ctx context.Context,
		currentBitrateSetting uint64,
		inputBitrate uint64,
		actualOutputBitrate uint64,
		queueSize uint64,
		config *AutoBitRateConfig,
	) BitRateChangeRequest
}

type AutoBitRateResolutionAndBitRateConfig struct {
	codectypes.Resolution
	BitrateHigh uint64
	BitrateLow  uint64
}

func (res AutoBitRateResolutionAndBitRateConfig) String() string {
	return res.Resolution.String() + " (" + humanize.SI(float64(res.BitrateLow), "bps") + " .. " + humanize.SI(float64(res.BitrateHigh), "bps") + ")"
}

type AutoBitRateResolutionAndBitRateConfigs []AutoBitRateResolutionAndBitRateConfig

func (r AutoBitRateResolutionAndBitRateConfigs) Find(
	res codectypes.Resolution,
) *AutoBitRateResolutionAndBitRateConfig {
	for i := range r {
		if r[i].Width == res.Width && r[i].Height == res.Height {
			return &r[i]
		}
	}
	return nil
}

func (r AutoBitRateResolutionAndBitRateConfigs) BitRate(
	bitrate uint64,
) AutoBitRateResolutionAndBitRateConfigs {
	out := make(AutoBitRateResolutionAndBitRateConfigs, 0, len(r))
	for i := range r {
		if r[i].BitrateLow <= bitrate && bitrate <= r[i].BitrateHigh {
			out = append(out, r[i])
		}
	}
	return out
}

func (r AutoBitRateResolutionAndBitRateConfigs) Best() *AutoBitRateResolutionAndBitRateConfig {
	if len(r) == 0 {
		return nil
	}
	best := r[0]
	for i := range r {
		if r[i].Width*r[i].Height > best.Width*best.Height {
			best = r[i]
		}
	}
	return &best
}

func (r AutoBitRateResolutionAndBitRateConfigs) Worst() *AutoBitRateResolutionAndBitRateConfig {
	if len(r) == 0 {
		return nil
	}
	worst := r[0]
	for i := range r {
		if r[i].Width*r[i].Height < worst.Width*worst.Height {
			worst = r[i]
		}
	}
	return &worst
}

func (r AutoBitRateResolutionAndBitRateConfigs) MaxHeight(h uint32) AutoBitRateResolutionAndBitRateConfigs {
	out := make(AutoBitRateResolutionAndBitRateConfigs, 0, len(r))
	for i := range r {
		if r[i].Height <= h {
			out = append(out, r[i])
		}
	}
	return out
}

func (r AutoBitRateResolutionAndBitRateConfigs) MaxWidth(w uint32) AutoBitRateResolutionAndBitRateConfigs {
	out := make(AutoBitRateResolutionAndBitRateConfigs, 0, len(r))
	for i := range r {
		if r[i].Width <= w {
			out = append(out, r[i])
		}
	}
	return out
}

func (r AutoBitRateResolutionAndBitRateConfigs) MinHeight(h uint32) AutoBitRateResolutionAndBitRateConfigs {
	out := make(AutoBitRateResolutionAndBitRateConfigs, 0, len(r))
	for i := range r {
		if r[i].Height >= h {
			out = append(out, r[i])
		}
	}
	return out
}

func (r AutoBitRateResolutionAndBitRateConfigs) MinWidth(w uint32) AutoBitRateResolutionAndBitRateConfigs {
	out := make(AutoBitRateResolutionAndBitRateConfigs, 0, len(r))
	for i := range r {
		if r[i].Width >= w {
			out = append(out, r[i])
		}
	}
	return out
}

type AutoBitRateConfig struct {
	ResolutionsAndBitRates AutoBitRateResolutionAndBitRateConfigs
	Calculator             AutoBitRateCalculator
	FPSReducer             FPSReducerConfig
	CheckInterval          time.Duration
	AutoByPass             bool
	MaxBitRate             uint64
	MinBitRate             uint64

	ResolutionSlowdownDurationUpgrade   time.Duration
	ResolutionSlowdownDurationDowngrade time.Duration
}

type FPSReducerConfig []FPSReductionRange

type FPSReductionRange struct {
	BitrateMin  uint64
	BitrateMax  uint64
	FractionNum uint32
	FractionDen uint32
}

func DefaultFPSReducerConfig() FPSReducerConfig {
	return FPSReducerConfig{
		{
			BitrateMax:  500_000,
			BitrateMin:  150_000,
			FractionNum: 1,
			FractionDen: 2,
		},
		{
			BitrateMax:  150_000,
			BitrateMin:  50_000,
			FractionNum: 1,
			FractionDen: 4,
		},
		{
			BitrateMax:  50_000,
			BitrateMin:  10_000,
			FractionNum: 1,
			FractionDen: 10,
		},
	}
}

func (r FPSReducerConfig) GetFraction(bitrate uint64) (num, den uint32) {
	for i := range r {
		if r[i].BitrateMin <= bitrate && bitrate <= r[i].BitrateMax {
			return r[i].FractionNum, r[i].FractionDen
		}
	}
	return 1, 1
}
