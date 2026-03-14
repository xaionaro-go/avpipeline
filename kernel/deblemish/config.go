package deblemish

import "image"

// Config configures the Deblemish kernel.
type Config struct {
	// Backend selects the processing backend.
	// BackendAuto probes available backends at construction time.
	Backend Backend

	// SigmaS is the spatial sigma for the bilateral filter.
	// Higher values = more spatial smoothing. Default: 10.
	SigmaS float64

	// SigmaR is the range/color sigma for the bilateral filter.
	// Higher values = more color smoothing (less edge preservation).
	// Default: 0.1.
	SigmaR float64

	// Diameter is the filter kernel diameter.
	// Used as window_size for CUDA and as the d parameter for gocv bilateral.
	// -1 means auto-compute from SigmaS. Default: -1.
	Diameter int

	// FaceOnly restricts smoothing to detected face regions.
	// Requires the with_cv build tag; returns an error otherwise.
	FaceOnly bool

	// FaceClassifierXML is the Haar cascade XML data for face detection.
	// Only used when FaceOnly is true.
	// If nil, uses cascadedata.FaceFrontalDefault.
	FaceClassifierXML []byte

	// FaceScaleFactor is the detection scale factor (default 1.1).
	FaceScaleFactor float64

	// FaceMinNeighbors is the minimum neighbors for detection (default 3).
	FaceMinNeighbors int

	// FaceMinSize is the minimum detection region size (default 30x30).
	FaceMinSize image.Point

	// FaceMaxSize is the maximum detection region size (default 0x0 = unlimited).
	FaceMaxSize image.Point
}

func (cfg *Config) setDefaults() {
	if cfg.SigmaS == 0 {
		cfg.SigmaS = 10
	}
	if cfg.SigmaR == 0 {
		cfg.SigmaR = 0.1
	}
	if cfg.Diameter == 0 {
		cfg.Diameter = -1
	}
	if cfg.FaceScaleFactor == 0 {
		cfg.FaceScaleFactor = 1.1
	}
	if cfg.FaceMinNeighbors == 0 {
		cfg.FaceMinNeighbors = 3
	}
	if cfg.FaceMinSize.X == 0 && cfg.FaceMinSize.Y == 0 {
		cfg.FaceMinSize = image.Pt(30, 30)
	}
}
