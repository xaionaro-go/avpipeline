package voiceanon

// SpeakerIdentifier identifies speakers from audio samples.
// Implementations may use different backends (e.g. sherpa-onnx).
type SpeakerIdentifier interface {
	// Identify returns the speaker name if the audio samples match a
	// whitelisted speaker, or "" if the speaker is unknown.
	// samples must be mono float32 PCM at the expected sample rate.
	Identify(samples []float32, sampleRate int) (speakerName string, err error)

	// Register adds a named speaker to the whitelist using reference
	// audio samples. Multiple calls with the same name accumulate embeddings.
	Register(name string, samples []float32, sampleRate int) error

	// Remove removes a named speaker from the whitelist.
	Remove(name string) error

	// Speakers returns the names of all registered speakers.
	Speakers() []string

	// Close releases resources.
	Close() error

	// Threshold returns the cosine similarity threshold for speaker matching.
	Threshold() float32

	// SetThreshold updates the cosine similarity threshold.
	SetThreshold(threshold float32)
}
