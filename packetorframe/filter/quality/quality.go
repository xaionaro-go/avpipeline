// quality.go defines the quality metrics for media streams.

// Package quality provides tools for measuring the quality of media streams.
package quality

import (
	"math"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/packetorframe/filter/quality/types"
)

type Quality []*StreamQualityWithMediaType

func (q Quality) Aggregate() *QualityAggregated {
	var audioContinuitySum float64
	var audioOverlapSum float64
	var audioFrameRateSum float64
	var audioCount int
	var videoContinuitySum float64
	var videoOverlapSum float64
	var videoFrameRateSum float64
	var videoCount int
	for _, sq := range q {
		// Skip NaN values
		if math.IsNaN(sq.Continuity) || math.IsInf(sq.Continuity, 0) {
			sq.Continuity = 0
		}
		if math.IsNaN(sq.Overlap) || math.IsInf(sq.Overlap, 0) {
			sq.Overlap = 0
		}
		if math.IsNaN(sq.FrameRate) || math.IsInf(sq.FrameRate, 0) {
			sq.FrameRate = 0
		}
		switch sq.MediaType {
		case astiav.MediaTypeAudio:
			audioContinuitySum += sq.Continuity
			audioOverlapSum += sq.Overlap
			audioFrameRateSum += sq.FrameRate
			audioCount++
		case astiav.MediaTypeVideo:
			videoContinuitySum += sq.Continuity
			videoOverlapSum += sq.Overlap
			videoFrameRateSum += sq.FrameRate
			videoCount++
		}
	}
	var audioContinuity, audioOverlap, audioFrameRate float64
	if audioCount > 0 {
		audioContinuity = audioContinuitySum / float64(audioCount)
		audioOverlap = audioOverlapSum / float64(audioCount)
		audioFrameRate = audioFrameRateSum / float64(audioCount)
	}
	var videoContinuity, videoOverlap, videoFrameRate float64
	if videoCount > 0 {
		videoContinuity = videoContinuitySum / float64(videoCount)
		videoOverlap = videoOverlapSum / float64(videoCount)
		videoFrameRate = videoFrameRateSum / float64(videoCount)
	}
	// Final NaN check
	if math.IsNaN(audioContinuity) || math.IsInf(audioContinuity, 0) {
		audioContinuity = 0
	}
	if math.IsNaN(audioOverlap) || math.IsInf(audioOverlap, 0) {
		audioOverlap = 0
	}
	if math.IsNaN(audioFrameRate) || math.IsInf(audioFrameRate, 0) {
		audioFrameRate = 0
	}
	if math.IsNaN(videoContinuity) || math.IsInf(videoContinuity, 0) {
		videoContinuity = 0
	}
	if math.IsNaN(videoOverlap) || math.IsInf(videoOverlap, 0) {
		videoOverlap = 0
	}
	if math.IsNaN(videoFrameRate) || math.IsInf(videoFrameRate, 0) {
		videoFrameRate = 0
	}
	return &QualityAggregated{
		Audio: StreamQuality{
			Continuity: audioContinuity,
			Overlap:    audioOverlap,
			FrameRate:  audioFrameRate,
		},
		Video: StreamQuality{
			Continuity: videoContinuity,
			Overlap:    videoOverlap,
			FrameRate:  videoFrameRate,
		},
	}
}

type (
	StreamQuality     = types.StreamQuality
	QualityAggregated = types.QualityAggregated
)
