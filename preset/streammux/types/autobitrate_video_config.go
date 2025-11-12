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

type CalculateBitRateRequest struct {
	CurrentBitrateSetting Ubps
	InputBitrate          Ubps
	ActualOutputBitrate   Ubps
	QueueSize             UB
	QueueSizeDerivative   UBps
	Config                *AutoBitRateVideoConfig
}

type BitRateChangeRequest struct {
	BitRate    Ubps
	IsCritical bool
}

type AutoBitRateCalculator interface {
	CalculateBitRate(
		ctx context.Context,
		req CalculateBitRateRequest,
	) BitRateChangeRequest
}

type AutoBitRateResolutionAndBitRateConfig struct {
	codectypes.Resolution
	BitrateHigh Ubps
	BitrateLow  Ubps
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
	bitrate Ubps,
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

type AutoBitRateVideoConfig struct {
	ResolutionsAndBitRates AutoBitRateResolutionAndBitRateConfigs
	Calculator             AutoBitRateCalculator
	FPSReducer             FPSReducerConfig
	CheckInterval          time.Duration
	AutoByPass             bool
	MaxBitRate             Ubps
	MinBitRate             Ubps

	BitRateIncreaseSlowdown             time.Duration
	ResolutionSlowdownDurationUpgrade   time.Duration
	ResolutionSlowdownDurationDowngrade time.Duration
}

type FPSReducerConfig []FPSReductionRange

type FPSReductionRange struct {
	BitrateMin  Ubps
	BitrateMax  Ubps
	FractionNum uint32
	FractionDen uint32
}

func DefaultFPSReducerConfig() FPSReducerConfig {
	return FPSReducerConfig{
		{
			BitrateMax:  500_000,
			BitrateMin:  0,
			FractionNum: 1,
			FractionDen: 2,
		},
	}
}

func (r FPSReducerConfig) GetFraction(bitrate Ubps) (num, den uint32) {
	for i := range r {
		if r[i].BitrateMin <= bitrate && bitrate <= r[i].BitrateMax {
			return r[i].FractionNum, r[i].FractionDen
		}
	}
	return 1, 1
}
