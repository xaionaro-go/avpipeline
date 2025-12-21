package avpipelinenolibav

import (
	"fmt"
	"time"

	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func AutoBitRateResolutionAndBitRateConfigFromProto(
	rc *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfig,
) *smtypes.AutoBitRateResolutionAndBitRateConfig {
	if rc == nil {
		return nil
	}
	res := ResolutionFromProto(rc.GetResolution())
	if res == nil {
		return nil
	}
	return &smtypes.AutoBitRateResolutionAndBitRateConfig{
		Resolution:  *res,
		BitrateHigh: smtypes.Ubps(rc.GetBitrateHighBps()),
		BitrateLow:  smtypes.Ubps(rc.GetBitrateLowBps()),
	}
}

func AutoBitRateResolutionAndBitRateConfigToProto(
	rc *smtypes.AutoBitRateResolutionAndBitRateConfig,
) *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfig {
	if rc == nil {
		return nil
	}
	return &avpipelinegrpc.AutoBitRateResolutionAndBitRateConfig{
		Resolution:     ResolutionToProto(rc.Resolution),
		BitrateHighBps: uint64(rc.BitrateHigh),
		BitrateLowBps:  uint64(rc.BitrateLow),
	}
}

func AutoBitRateResolutionAndBitRateConfigsFromProto(
	in *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfigs,
) smtypes.AutoBitRateResolutionAndBitRateConfigs {
	if in == nil {
		return nil
	}
	out := make(smtypes.AutoBitRateResolutionAndBitRateConfigs, 0, len(in.GetConfigs()))
	for _, rc := range in.GetConfigs() {
		if cfg := AutoBitRateResolutionAndBitRateConfigFromProto(rc); cfg != nil {
			out = append(out, *cfg)
		}
	}
	return out
}

func AutoBitRateResolutionAndBitRateConfigsToProto(
	in smtypes.AutoBitRateResolutionAndBitRateConfigs,
) *avpipelinegrpc.AutoBitRateResolutionAndBitRateConfigs {
	if in == nil {
		return nil
	}
	protoCfgs := make([]*avpipelinegrpc.AutoBitRateResolutionAndBitRateConfig, 0, len(in))
	for _, rc := range in {
		protoCfgs = append(protoCfgs, AutoBitRateResolutionAndBitRateConfigToProto(&rc))
	}
	return &avpipelinegrpc.AutoBitRateResolutionAndBitRateConfigs{
		Configs: protoCfgs,
	}
}

func AutoBitRateVideoConfigFromProto(
	cfg *avpipelinegrpc.AutoBitRateVideoConfig,
) (*smtypes.AutoBitRateVideoConfig, error) {
	if cfg == nil {
		return nil, nil
	}

	calc, err := AutoBitRateCalculatorFromProto(cfg.GetCalculator())
	if err != nil {
		return nil, fmt.Errorf("failed to convert AutoBitRateCalculator: %w", err)
	}

	return &smtypes.AutoBitRateVideoConfig{
		ResolutionsAndBitRates:              AutoBitRateResolutionAndBitRateConfigsFromProto(cfg.ResolutionsAndBitRates),
		Calculator:                          calc,
		FPSReducer:                          FPSReducerConfigFromProto(cfg.GetFpsReducer()),
		CheckInterval:                       time.Duration(cfg.GetCheckIntervalMs()) * time.Millisecond,
		AutoByPass:                          cfg.GetAutoByPass(),
		MaxBitRate:                          smtypes.Ubps(cfg.GetMaxBitRateBps()),
		MinBitRate:                          smtypes.Ubps(cfg.GetMinBitRateBps()),
		BitRateIncreaseSlowdown:             time.Duration(cfg.GetBitRateIncreaseSlowdownMs()) * time.Millisecond,
		ResolutionSlowdownDurationUpgrade:   time.Duration(cfg.GetResolutionSlowdownDurationUpgradeMs()) * time.Millisecond,
		ResolutionSlowdownDurationDowngrade: time.Duration(cfg.GetResolutionSlowdownDurationDowngradeMs()) * time.Millisecond,
	}, nil
}

func AutoBitRateVideoConfigToProto(
	cfg *smtypes.AutoBitRateVideoConfig,
) (*avpipelinegrpc.AutoBitRateVideoConfig, error) {
	if cfg == nil {
		return nil, nil
	}

	calc, err := AutoBitRateCalculatorToProto(cfg.Calculator)
	if err != nil {
		return nil, fmt.Errorf("failed to convert AutoBitRateCalculator: %w", err)
	}

	return &avpipelinegrpc.AutoBitRateVideoConfig{
		ResolutionsAndBitRates:                AutoBitRateResolutionAndBitRateConfigsToProto(cfg.ResolutionsAndBitRates),
		Calculator:                            calc,
		FpsReducer:                            FPSReducerConfigToProto(cfg.FPSReducer),
		CheckIntervalMs:                       uint64(cfg.CheckInterval / time.Millisecond),
		AutoByPass:                            cfg.AutoByPass,
		MaxBitRateBps:                         uint64(cfg.MaxBitRate),
		MinBitRateBps:                         uint64(cfg.MinBitRate),
		BitRateIncreaseSlowdownMs:             uint64(cfg.BitRateIncreaseSlowdown / time.Millisecond),
		ResolutionSlowdownDurationUpgradeMs:   uint64(cfg.ResolutionSlowdownDurationUpgrade / time.Millisecond),
		ResolutionSlowdownDurationDowngradeMs: uint64(cfg.ResolutionSlowdownDurationDowngrade / time.Millisecond),
	}, nil
}
