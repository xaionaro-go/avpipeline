package voiceanon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

// --- Mock speaker identifier ---

type mockSpeakerID struct {
	mu           sync.Mutex
	speakers     map[string]bool
	identifyFunc func(samples []float32, sampleRate int) (string, error)
	threshold    float32
}

func newMockSpeakerID() *mockSpeakerID {
	return &mockSpeakerID{
		speakers:  make(map[string]bool),
		threshold: 0.5,
	}
}

func (m *mockSpeakerID) Identify(samples []float32, sampleRate int) (string, error) {
	if m.identifyFunc != nil {
		return m.identifyFunc(samples, sampleRate)
	}
	return "", nil
}

func (m *mockSpeakerID) Register(name string, _ []float32, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.speakers[name] = true
	return nil
}

func (m *mockSpeakerID) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.speakers, name)
	return nil
}

func (m *mockSpeakerID) Speakers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for name := range m.speakers {
		names = append(names, name)
	}
	return names
}

func (m *mockSpeakerID) Close() error { return nil }

func (m *mockSpeakerID) Threshold() float32 {
	return m.threshold
}

func (m *mockSpeakerID) SetThreshold(threshold float32) {
	m.threshold = threshold
}

// --- Interface compliance ---

func TestWhitelistingVoiceAnonymizer_ImplementsAbstract(t *testing.T) {
	var _ kernel.Abstract = (*WhitelistingVoiceAnonymizer)(nil)
}

// --- Constructor ---

func TestNewWhitelisting_Defaults(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(WhitelistingConfig{}, mock)
	assert.NotNil(t, w)
	assert.Equal(t, 16000, w.config.BufferSamples)
	assert.Equal(t, 0.7, w.config.AnonymizationConfig.PitchScale)
}

func TestNewWhitelisting_Custom(t *testing.T) {
	mock := newMockSpeakerID()
	cfg := WhitelistingConfig{
		AnonymizationConfig: Config{PitchScale: 1.2},
		BufferSamples:       32000,
	}
	w := NewWhitelisting(cfg, mock)
	assert.Equal(t, 32000, w.config.BufferSamples)
	assert.Equal(t, 1.2, w.config.AnonymizationConfig.PitchScale)
}

// --- String ---

func TestWhitelistingVoiceAnonymizer_String(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	s := w.String()
	assert.Contains(t, s, "WhitelistingVoiceAnonymizer")
	assert.Contains(t, s, "0.7")
}

// --- GetObjectID ---

func TestWhitelistingVoiceAnonymizer_GetObjectID(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	id := w.GetObjectID()
	assert.NotEmpty(t, id)
}

// --- Generate (no-op) ---

func TestWhitelistingVoiceAnonymizer_Generate(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.Generate(context.Background(), outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 0)
}

// --- Close without init ---

func TestWhitelistingVoiceAnonymizer_Close_NoInit(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	err := w.Close(context.Background())
	assert.NoError(t, err)
}

// --- SendInput: packet passthrough ---

func TestWhitelistingVoiceAnonymizer_SendInput_PassthroughPacket(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	defer w.Close(context.Background())

	input, cleanup := makePacketInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.SendInput(context.Background(), input, outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 1)
}

// --- SendInput: video frame passthrough ---

func TestWhitelistingVoiceAnonymizer_SendInput_PassthroughVideoFrame(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	defer w.Close(context.Background())

	input, cleanup := makeVideoFrameInput(t)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.SendInput(context.Background(), input, outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 1)
}

// --- SendInput: disabled passthrough ---

func TestWhitelistingVoiceAnonymizer_SendInput_DisabledPassthrough(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	defer w.Close(context.Background())

	enabled := &atomic.Bool{}
	enabled.Store(false)
	w.Enabled = enabled

	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	outputCh := make(chan packetorframe.OutputUnion, 10)
	err := w.SendInput(context.Background(), input, outputCh)
	assert.NoError(t, err)
	assert.Len(t, outputCh, 1)
}

// --- SendInput: buffering until enough samples ---

func TestWhitelistingVoiceAnonymizer_SendInput_BuffersUntilReady(t *testing.T) {
	mock := newMockSpeakerID()
	// Set buffer to 2048 samples so we can control flushing.
	cfg := WhitelistingConfig{
		AnonymizationConfig: DefaultConfig(),
		BufferSamples:       2048,
	}
	w := NewWhitelisting(cfg, mock)
	defer w.Close(context.Background())

	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 100)

	// Send a frame with 1024 samples (less than buffer threshold).
	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	err := w.SendInput(ctx, input, outputCh)
	require.NoError(t, err)

	// Should be buffered, no output yet.
	assert.Len(t, outputCh, 0)
	assert.Equal(t, 1024, w.BufferedSampleCount())
}

// --- SendInput: unknown speaker → anonymize ---

func TestWhitelistingVoiceAnonymizer_SendInput_UnknownSpeakerAnonymizes(t *testing.T) {
	mock := newMockSpeakerID()
	mock.identifyFunc = func(_ []float32, _ int) (string, error) {
		return "", nil // Unknown speaker
	}

	cfg := WhitelistingConfig{
		AnonymizationConfig: DefaultConfig(),
		BufferSamples:       1024, // Flush after 1024 samples
	}
	w := NewWhitelisting(cfg, mock)
	defer w.Close(context.Background())

	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 100)

	// Send frame with exactly buffer size samples.
	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	err := w.SendInput(ctx, input, outputCh)
	require.NoError(t, err)

	// Buffer should be flushed and frames processed through anonymizer.
	assert.Equal(t, 0, w.BufferedSampleCount())
	// The rubberband filter may buffer internally, so output count varies.
}

// --- SendInput: known speaker → passthrough ---

func TestWhitelistingVoiceAnonymizer_SendInput_KnownSpeakerPassthrough(t *testing.T) {
	mock := newMockSpeakerID()
	mock.identifyFunc = func(_ []float32, _ int) (string, error) {
		return "alice", nil // Known speaker
	}

	cfg := WhitelistingConfig{
		AnonymizationConfig: DefaultConfig(),
		BufferSamples:       1024,
	}
	w := NewWhitelisting(cfg, mock)
	defer w.Close(context.Background())

	ctx := context.Background()
	outputCh := make(chan packetorframe.OutputUnion, 100)

	input, cleanup := makeAudioFrameInput(t, 1024)
	defer cleanup()

	err := w.SendInput(ctx, input, outputCh)
	require.NoError(t, err)

	// Known speaker: frames should be passed through directly.
	assert.Equal(t, 0, w.BufferedSampleCount())
	assert.Equal(t, 1, len(outputCh), "expected 1 passthrough output for known speaker")
}

// --- SpeakerID accessor ---

func TestWhitelistingVoiceAnonymizer_SpeakerID(t *testing.T) {
	mock := newMockSpeakerID()
	w := NewWhitelisting(DefaultWhitelistingConfig(), mock)
	assert.Equal(t, mock, w.SpeakerID())
}

// --- DefaultWhitelistingConfig ---

func TestDefaultWhitelistingConfig(t *testing.T) {
	cfg := DefaultWhitelistingConfig()
	assert.Equal(t, 0.7, cfg.AnonymizationConfig.PitchScale)
	assert.True(t, cfg.AnonymizationConfig.FormantPreserve)
	assert.Equal(t, 16000, cfg.BufferSamples)
}
