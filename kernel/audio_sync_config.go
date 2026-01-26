package kernel

import (
	"time"

	"github.com/xaionaro-go/audio/pkg/syncerstream"
)

type AudioSyncConfig struct {
	// SyncInterval is how often to re-calculate the shift.
	SyncInterval time.Duration

	// WindowSize is the amount of audio to buffer for shift calculation.
	WindowSize time.Duration

	// OffsetThreshold is the minimum change required to update the actual offset.
	// Updates below this threshold are ignored to prevent constant minor adjustments.
	// Default is 50ms.
	OffsetThreshold time.Duration

	// ConsistencyDuration is how long the desired offset must stay in the same direction
	// before we consider changing it. Default is 1s.
	ConsistencyDuration time.Duration

	// Tracks maps StreamIndex to track-specific configuration.
	Tracks map[int]AudioSyncTrackConfig

	// Syncer is the factory used to create shift calculation streams.
	Syncer syncerstream.Factory

	// ConfidenceThreshold is the minimum confidence score (0..1) required
	// to trust the synchronization result. Default is 0.1.
	ConfidenceThreshold float64
}

type AudioSyncTrackConfig struct {
	// ReferenceStreamIndex specifies which stream to use as a reference for this stream.
	// If this value is equal to the StreamIndex itself, it's a primary reference track.
	ReferenceStreamIndex int

	// MovingAverageCount is the number of measurements to keep for the moving average.
	MovingAverageCount int
}

func DefaultAudioSyncConfig() *AudioSyncConfig {
	return &AudioSyncConfig{
		SyncInterval:        10 * time.Second,
		WindowSize:          2 * time.Second,
		OffsetThreshold:     50 * time.Millisecond,
		ConsistencyDuration: 1 * time.Second,
		// ConfidenceThreshold set to 0.1 as a safe default for GCC-PHAT.
		// A peak of 0.1 is significantly above the noise floor for typical window sizes (N >= 1024).
		ConfidenceThreshold: 0.1,
		Tracks:              make(map[int]AudioSyncTrackConfig),
	}
}
