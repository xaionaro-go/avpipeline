package quality

import (
	"github.com/asticode/go-astiav"
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
		switch sq.MediaType {
		case astiav.MediaTypeAudio:
			audioContinuitySum += sq.Continuity
			audioFrameRateSum += sq.FrameRate
			audioCount++
		case astiav.MediaTypeVideo:
			videoContinuitySum += sq.Continuity
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

type QualityAggregated struct {
	Audio StreamQuality
	Video StreamQuality
}

type StreamQuality struct {
	Continuity float64
	Overlap    float64
	FrameRate  float64
	InvalidDTS uint
}
