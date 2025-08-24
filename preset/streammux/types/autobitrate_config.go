package types

import (
	"context"
	"time"

	"github.com/dustin/go-humanize"
	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
)

type AutoBitRateCalculator interface {
	CalculateBitRate(
		ctx context.Context,
		currentBitrate uint64,
		queueSize uint64,
		config *AutoBitRateConfig,
	) uint64
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

type AutoBitRateConfig struct {
	ResolutionsAndBitRates AutoBitRateResolutionAndBitRateConfigs
	Calculator             AutoBitRateCalculator
	CheckInterval          time.Duration
	AutoByPass             bool
	MaxBitRate             uint64
	MinBitRate             uint64
}
