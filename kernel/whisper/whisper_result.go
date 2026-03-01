package whisper

import "time"

// WhisperResult represents a speech-to-text transcription result
// extracted from the FFmpeg whisper filter's frame metadata.
// It is attached as PipelineSideData to output frames.
//
// Downstream consumers retrieve it via:
//
//	types.PipelineSideDataLatest[*whisper.WhisperResult](sideData)
type WhisperResult struct {
	// Text is the transcribed text from the audio segment.
	Text string

	// Duration is the duration of the transcribed speech segment,
	// as reported by the whisper filter via lavfi.whisper.duration.
	Duration time.Duration
}
