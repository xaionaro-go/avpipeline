// auto_bitrate_calculator.go provides conversion functions for auto bitrate calculator between Protobuf and Go.

// Package avpipelinenolibav provides conversion functions between Protobuf and Go for avpipeline types without libav dependency.
package avpipelinenolibav

import (
	"fmt"
	"time"

	smtypes "github.com/xaionaro-go/avpipeline/preset/streammux/types"
	avpipelinegrpc "github.com/xaionaro-go/avpipeline/protobuf/avpipeline"
)

func AutoBitRateCalculatorFromProto(
	in *avpipelinegrpc.AutoBitrateCalculator,
) (smtypes.AutoBitRateCalculator, error) {
	switch calculator := in.GetAutoBitrateCalculator().(type) {
	case nil:
		return nil, nil
	case *avpipelinegrpc.AutoBitrateCalculator_Thresholds:
		return &smtypes.AutoBitrateCalculatorThresholds{
			OutputExtremelyHighQueueSizeDuration: time.Duration(calculator.Thresholds.GetOutputExtremelyHighQueueSizeDurationMs()) * time.Millisecond,
			OutputVeryHighQueueSizeDuration:      time.Duration(calculator.Thresholds.GetOutputVeryHighQueueSizeDurationMs()) * time.Millisecond,
			OutputHighQueueSizeDuration:          time.Duration(calculator.Thresholds.GetOutputHighQueueSizeDurationMs()) * time.Millisecond,
			OutputLowQueueSizeDuration:           time.Duration(calculator.Thresholds.GetOutputLowQueueSizeDurationMs()) * time.Millisecond,
			OutputVeryLowQueueSizeDuration:       time.Duration(calculator.Thresholds.GetOutputVeryLowQueueSizeDurationMs()) * time.Millisecond,
			IncreaseK:                            calculator.Thresholds.GetIncreaseK(),
			DecreaseK:                            calculator.Thresholds.GetDecreaseK(),
			QuickIncreaseK:                       calculator.Thresholds.GetQuickIncreaseK(),
			QuickDecreaseK:                       calculator.Thresholds.GetQuickDecreaseK(),
			ExtremeDecreaseK:                     calculator.Thresholds.GetExtremeDecreaseK(),
		}, nil
	case *avpipelinegrpc.AutoBitrateCalculator_LogK:
		return &smtypes.AutoBitrateCalculatorLogK{
			QueueOptimal:  time.Duration(calculator.LogK.GetQueueOptimalMs()) * time.Millisecond,
			Inertia:       calculator.LogK.GetInertia(),
			MovingAverage: MovingAverageFromGRPC[float64](calculator.LogK.GetMovingAverage()),
		}, nil
	case *avpipelinegrpc.AutoBitrateCalculator_Static:
		return smtypes.AutoBitrateCalculatorStatic(calculator.Static), nil
	case *avpipelinegrpc.AutoBitrateCalculator_QueueSizeGapDecay:
		return &smtypes.AutoBitrateCalculatorQueueSizeGapDecay{
			QueueDurationOptimal: time.Duration(calculator.QueueSizeGapDecay.GetQueueOptimalMs()) * time.Millisecond,
			QueueSizeMin:         smtypes.UB(calculator.QueueSizeGapDecay.GetQueueSizeMin()),
			GapDecay:             time.Duration(calculator.QueueSizeGapDecay.GetGapDecayMs()) * time.Millisecond,
			InertiaIncrease:      time.Duration(calculator.QueueSizeGapDecay.GetIncreaseInertiaMs()) * time.Millisecond,
			DerivativeSmoothed:   MovingAverageFromGRPC[smtypes.UBps](calculator.QueueSizeGapDecay.GetDerivativeSmoothed()),
		}, nil
	default:
		return nil, fmt.Errorf("unknown AutoBitRateCalculator type: %T", calculator)
	}
}

func AutoBitRateCalculatorToProto(
	in smtypes.AutoBitRateCalculator,
) (*avpipelinegrpc.AutoBitrateCalculator, error) {
	if in == nil {
		return nil, nil
	}

	switch c := in.(type) {
	case *smtypes.AutoBitrateCalculatorThresholds:
		return &avpipelinegrpc.AutoBitrateCalculator{
			AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_Thresholds{
				Thresholds: &avpipelinegrpc.AutoBitRateCalculatorThresholds{
					OutputExtremelyHighQueueSizeDurationMs: uint64(c.OutputExtremelyHighQueueSizeDuration / time.Millisecond),
					OutputVeryHighQueueSizeDurationMs:      uint64(c.OutputVeryHighQueueSizeDuration / time.Millisecond),
					OutputHighQueueSizeDurationMs:          uint64(c.OutputHighQueueSizeDuration / time.Millisecond),
					OutputLowQueueSizeDurationMs:           uint64(c.OutputLowQueueSizeDuration / time.Millisecond),
					OutputVeryLowQueueSizeDurationMs:       uint64(c.OutputVeryLowQueueSizeDuration / time.Millisecond),
					IncreaseK:                              c.IncreaseK,
					DecreaseK:                              c.DecreaseK,
					QuickIncreaseK:                         c.QuickIncreaseK,
					QuickDecreaseK:                         c.QuickDecreaseK,
					ExtremeDecreaseK:                       c.ExtremeDecreaseK,
				},
			},
		}, nil
	case *smtypes.AutoBitrateCalculatorLogK:
		return &avpipelinegrpc.AutoBitrateCalculator{
			AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_LogK{
				LogK: &avpipelinegrpc.AutoBitrateCalculatorLogK{
					QueueOptimalMs: uint64(c.QueueOptimal / time.Millisecond),
					Inertia:        c.Inertia,
					MovingAverage:  MovingAverageToGRPC(c.MovingAverage),
				},
			},
		}, nil
	case smtypes.AutoBitrateCalculatorStatic:
		return &avpipelinegrpc.AutoBitrateCalculator{
			AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_Static{
				Static: uint64(c),
			},
		}, nil
	case *smtypes.AutoBitrateCalculatorQueueSizeGapDecay:
		return &avpipelinegrpc.AutoBitrateCalculator{
			AutoBitrateCalculator: &avpipelinegrpc.AutoBitrateCalculator_QueueSizeGapDecay{
				QueueSizeGapDecay: &avpipelinegrpc.AutoBitrateCalculatorQueueSizeGapDecay{
					QueueOptimalMs:     uint64(c.QueueDurationOptimal / time.Millisecond),
					QueueSizeMin:       uint64(c.QueueSizeMin),
					GapDecayMs:         uint64(c.GapDecay / time.Millisecond),
					IncreaseInertiaMs:  uint64(c.InertiaIncrease / time.Millisecond),
					DerivativeSmoothed: MovingAverageToGRPC(c.DerivativeSmoothed),
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown AutoBitRateCalculator type: %T", in)
	}
}
