// quality_aggregated.go defines the aggregated quality metrics for audio and video.

// Package types provides common types for quality measurements.
package types

type QualityAggregated struct {
	Audio StreamQuality
	Video StreamQuality
}
