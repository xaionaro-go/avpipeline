//go:build with_sherpa
// +build with_sherpa

package voiceanon

import (
	"fmt"
	"sync"

	sherpa "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"
)

// SherpaOnnxSpeakerIDConfig configures the sherpa-onnx speaker identification backend.
type SherpaOnnxSpeakerIDConfig struct {
	// ModelPath is the path to the speaker embedding ONNX model file.
	// For example: "3dspeaker_speech_campplus_sv_zh-cn_16k-common.onnx"
	ModelPath string

	// NumThreads is the number of threads for inference. Default: 1.
	NumThreads int

	// Threshold is the cosine similarity threshold for speaker matching.
	// Higher = stricter matching. Default: 0.5.
	Threshold float32
}

// SherpaOnnxSpeakerID implements SpeakerIdentifier using sherpa-onnx
// speaker embedding extraction and management.
type SherpaOnnxSpeakerID struct {
	mu        sync.RWMutex
	extractor *sherpa.SpeakerEmbeddingExtractor
	manager   *sherpa.SpeakerEmbeddingManager
	threshold float32
}

var _ SpeakerIdentifier = (*SherpaOnnxSpeakerID)(nil)

// NewSherpaOnnxSpeakerID creates a new sherpa-onnx-based speaker identifier.
func NewSherpaOnnxSpeakerID(cfg SherpaOnnxSpeakerIDConfig) (*SherpaOnnxSpeakerID, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("speaker embedding model path is required")
	}
	if cfg.NumThreads <= 0 {
		cfg.NumThreads = 1
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.5
	}

	extractorConfig := &sherpa.SpeakerEmbeddingExtractorConfig{
		Model:      cfg.ModelPath,
		NumThreads: cfg.NumThreads,
		Provider:   "cpu",
	}

	extractor := sherpa.NewSpeakerEmbeddingExtractor(extractorConfig)
	if extractor == nil {
		return nil, fmt.Errorf("unable to create speaker embedding extractor with model %q", cfg.ModelPath)
	}

	dim := extractor.Dim()
	manager := sherpa.NewSpeakerEmbeddingManager(dim)
	if manager == nil {
		sherpa.DeleteSpeakerEmbeddingExtractor(extractor)
		return nil, fmt.Errorf("unable to create speaker embedding manager (dim=%d)", dim)
	}

	return &SherpaOnnxSpeakerID{
		extractor: extractor,
		manager:   manager,
		threshold: cfg.Threshold,
	}, nil
}

func (s *SherpaOnnxSpeakerID) computeEmbedding(samples []float32, sampleRate int) ([]float32, error) {
	stream := s.extractor.CreateStream()
	if stream == nil {
		return nil, fmt.Errorf("unable to create embedding stream")
	}
	defer sherpa.DeleteOnlineStream(stream)

	stream.AcceptWaveform(sampleRate, samples)
	stream.InputFinished()

	if !s.extractor.IsReady(stream) {
		return nil, fmt.Errorf("insufficient audio for embedding extraction")
	}

	embedding := s.extractor.Compute(stream)
	return embedding, nil
}

func (s *SherpaOnnxSpeakerID) Identify(samples []float32, sampleRate int) (string, error) {
	embedding, err := s.computeEmbedding(samples, sampleRate)
	if err != nil {
		return "", err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	name := s.manager.Search(embedding, s.threshold)
	return name, nil
}

func (s *SherpaOnnxSpeakerID) Register(name string, samples []float32, sampleRate int) error {
	embedding, err := s.computeEmbedding(samples, sampleRate)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ok := s.manager.Register(name, embedding); !ok {
		return fmt.Errorf("unable to register speaker %q", name)
	}
	return nil
}

func (s *SherpaOnnxSpeakerID) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ok := s.manager.Remove(name); !ok {
		return fmt.Errorf("speaker %q not found", name)
	}
	return nil
}

func (s *SherpaOnnxSpeakerID) Speakers() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manager.AllSpeakers()
}

func (s *SherpaOnnxSpeakerID) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.manager != nil {
		sherpa.DeleteSpeakerEmbeddingManager(s.manager)
		s.manager = nil
	}
	if s.extractor != nil {
		sherpa.DeleteSpeakerEmbeddingExtractor(s.extractor)
		s.extractor = nil
	}
	return nil
}

func (s *SherpaOnnxSpeakerID) Threshold() float32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.threshold
}

func (s *SherpaOnnxSpeakerID) SetThreshold(threshold float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threshold = threshold
}
