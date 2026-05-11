package android

import (
	"errors"
	"fmt"
	"math"
)

const (
	camera2NDKTimeBaseDen            = 1_000_000_000
	camera2NDKDefaultWidth     int32 = 640
	camera2NDKDefaultHeight    int32 = 480
	camera2NDKDefaultFrameRate int32 = 30
	camera2NDKDefaultMaxImages int32 = 2
)

var (
	ErrCamera2NDKUnsupportedImageFormat = errors.New("camera2 ndk unsupported image format")
	ErrCamera2NDKCameraNotFound         = errors.New("camera2 ndk camera not found")
	ErrCamera2NDKPhysicalCameraNotFound = errors.New("camera2 ndk physical camera not found")
	ErrCamera2NDKNoBackCamera           = errors.New("camera2 ndk no back camera")
	ErrCamera2NDKNoCompatibleBackCamera = errors.New("camera2 ndk no compatible back camera")
	ErrCamera2NDKSessionNotReady        = errors.New("camera2 ndk session not ready")
	ErrCamera2NDKSessionClosed          = errors.New("camera2 ndk session closed")
	ErrCamera2NDKSessionCloseTimeout    = errors.New("camera2 ndk session close timeout")
)

type Camera2NDKImageFormat int32

const (
	Camera2NDKImageFormatRGBA8888   Camera2NDKImageFormat = 1
	Camera2NDKImageFormatYUV420888  Camera2NDKImageFormat = 35
	Camera2NDKImageFormatJPEG       Camera2NDKImageFormat = 256
	Camera2NDKImageFormatPrivate    Camera2NDKImageFormat = 34
	Camera2NDKImageFormatRaw16      Camera2NDKImageFormat = 32
	Camera2NDKImageFormatRawPrivate Camera2NDKImageFormat = 36
	Camera2NDKImageFormatRaw10      Camera2NDKImageFormat = 37
	Camera2NDKImageFormatRaw12      Camera2NDKImageFormat = 38
	Camera2NDKImageFormatY8         Camera2NDKImageFormat = 538982489
)

type Camera2NDKLensFacing int32

const (
	Camera2NDKLensFacingUnknown  Camera2NDKLensFacing = -1
	Camera2NDKLensFacingFront    Camera2NDKLensFacing = 0
	Camera2NDKLensFacingBack     Camera2NDKLensFacing = 1
	Camera2NDKLensFacingExternal Camera2NDKLensFacing = 2
)

type Camera2NDKConfig struct {
	LogicalCameraID  string
	PhysicalCameraID string
	ImageFormat      Camera2NDKImageFormat
	Width            int32
	Height           int32
	// FrameRate requests the Camera2 AE target FPS range when the selected camera
	// advertises a range containing it; otherwise it is used for fallback PTS duration.
	FrameRate int32
	MaxImages int32
}

type Camera2NDKFrameRateRange struct {
	Min int32
	Max int32
}

type Camera2NDKMetadata struct {
	CameraID                 string
	LensFacing               Camera2NDKLensFacing
	PhysicalCameraIDs        []string
	AvailableFocalLengths    []float32
	AvailableTargetFPSRanges []Camera2NDKFrameRateRange
	StreamConfigurations     []Camera2NDKStreamConfiguration
	HasStreamConfiguration   bool
}

type Camera2NDKStreamConfiguration struct {
	ImageFormat Camera2NDKImageFormat
	Width       int32
	Height      int32
	Output      bool
}

func normalizeCamera2NDKConfig(cfg Camera2NDKConfig) (Camera2NDKConfig, error) {
	if cfg.ImageFormat == 0 {
		cfg.ImageFormat = Camera2NDKImageFormatYUV420888
	}
	if !camera2NDKImageFormatSupportedByCapture(cfg.ImageFormat) {
		return Camera2NDKConfig{}, fmt.Errorf("%w: %s", ErrCamera2NDKUnsupportedImageFormat, cfg.ImageFormat)
	}
	if cfg.Width <= 0 {
		cfg.Width = camera2NDKDefaultWidth
	}
	if cfg.Height <= 0 {
		cfg.Height = camera2NDKDefaultHeight
	}
	if cfg.FrameRate <= 0 {
		cfg.FrameRate = camera2NDKDefaultFrameRate
	}
	if cfg.MaxImages < camera2NDKDefaultMaxImages {
		cfg.MaxImages = camera2NDKDefaultMaxImages
	}
	return cfg, nil
}

func camera2NDKImageFormatSupportedByCapture(format Camera2NDKImageFormat) bool {
	return IsCamera2NDKImageFormatSupported(format)
}

func camera2NDKSelectTargetFPSRange(
	frameRate int32,
	ranges []Camera2NDKFrameRateRange,
) (Camera2NDKFrameRateRange, bool) {
	if frameRate <= 0 {
		return Camera2NDKFrameRateRange{}, false
	}
	var selected Camera2NDKFrameRateRange
	var selectedWidth int32
	for _, candidate := range ranges {
		if candidate.Min <= 0 || candidate.Max < candidate.Min {
			continue
		}
		if frameRate < candidate.Min || frameRate > candidate.Max {
			continue
		}
		width := candidate.Max - candidate.Min
		if selected == (Camera2NDKFrameRateRange{}) || width < selectedWidth {
			selected = candidate
			selectedWidth = width
		}
	}
	return selected, selected != (Camera2NDKFrameRateRange{})
}

func validateCamera2NDKStreamFormat(
	cfg Camera2NDKConfig,
	metadata Camera2NDKMetadata,
) error {
	if !metadata.HasStreamConfiguration && len(metadata.StreamConfigurations) == 0 {
		return nil
	}
	for _, streamCfg := range metadata.StreamConfigurations {
		if !streamCfg.Output {
			continue
		}
		if streamCfg.ImageFormat != cfg.ImageFormat {
			continue
		}
		if streamCfg.Width != cfg.Width || streamCfg.Height != cfg.Height {
			continue
		}
		return nil
	}
	return fmt.Errorf(
		"%w: camera_id=%s format=%s size=%dx%d",
		ErrCamera2NDKUnsupportedImageFormat,
		metadata.CameraID,
		cfg.ImageFormat,
		cfg.Width,
		cfg.Height,
	)
}

func validateCamera2NDKPhysicalCamera(
	cfg Camera2NDKConfig,
	metadata Camera2NDKMetadata,
) error {
	if cfg.PhysicalCameraID == "" {
		return nil
	}
	for _, id := range metadata.PhysicalCameraIDs {
		if id == cfg.PhysicalCameraID {
			return nil
		}
	}
	return fmt.Errorf(
		"%w: logical_camera_id=%s physical_camera_id=%s",
		ErrCamera2NDKPhysicalCameraNotFound,
		metadata.CameraID,
		cfg.PhysicalCameraID,
	)
}

func resolveCamera2NDKSelection(
	cfg Camera2NDKConfig,
	metadatas []Camera2NDKMetadata,
) (Camera2NDKMetadata, error) {
	if cfg.LogicalCameraID != "" {
		return resolveCamera2NDKExplicitCamera(cfg, metadatas)
	}
	if cfg.PhysicalCameraID != "" {
		return resolveCamera2NDKPhysicalCamera(cfg, metadatas)
	}
	return resolveCamera2NDKWidestCompatibleBackCamera(cfg, metadatas)
}

func resolveCamera2NDKExplicitCamera(
	cfg Camera2NDKConfig,
	metadatas []Camera2NDKMetadata,
) (Camera2NDKMetadata, error) {
	if len(metadatas) == 0 {
		return Camera2NDKMetadata{CameraID: cfg.LogicalCameraID}, nil
	}
	for _, metadata := range metadatas {
		if metadata.CameraID == cfg.LogicalCameraID {
			return metadata, nil
		}
	}
	return Camera2NDKMetadata{}, fmt.Errorf("%w: camera_id=%s", ErrCamera2NDKCameraNotFound, cfg.LogicalCameraID)
}

func resolveCamera2NDKPhysicalCamera(
	cfg Camera2NDKConfig,
	metadatas []Camera2NDKMetadata,
) (Camera2NDKMetadata, error) {
	var selected Camera2NDKMetadata
	selectedScore := math.Inf(1)
	foundPhysicalCamera := false
	for _, metadata := range metadatas {
		if !camera2NDKMetadataHasPhysicalCamera(metadata, cfg.PhysicalCameraID) {
			continue
		}
		foundPhysicalCamera = true
		if err := validateCamera2NDKStreamFormat(cfg, metadata); err != nil {
			continue
		}
		score := camera2NDKWideScore(metadata)
		if score >= selectedScore {
			continue
		}
		selected = metadata
		selectedScore = score
	}
	if selected.CameraID == "" {
		if foundPhysicalCamera {
			return Camera2NDKMetadata{}, fmt.Errorf(
				"%w: physical_camera_id=%s format=%s size=%dx%d",
				ErrCamera2NDKUnsupportedImageFormat,
				cfg.PhysicalCameraID,
				cfg.ImageFormat,
				cfg.Width,
				cfg.Height,
			)
		}
		return Camera2NDKMetadata{}, fmt.Errorf(
			"%w: physical_camera_id=%s format=%s size=%dx%d",
			ErrCamera2NDKPhysicalCameraNotFound,
			cfg.PhysicalCameraID,
			cfg.ImageFormat,
			cfg.Width,
			cfg.Height,
		)
	}
	return selected, nil
}

func camera2NDKMetadataHasPhysicalCamera(
	metadata Camera2NDKMetadata,
	physicalCameraID string,
) bool {
	for _, id := range metadata.PhysicalCameraIDs {
		if id == physicalCameraID {
			return true
		}
	}
	return false
}

func resolveCamera2NDKWidestCompatibleBackCamera(
	cfg Camera2NDKConfig,
	metadatas []Camera2NDKMetadata,
) (Camera2NDKMetadata, error) {
	var selected Camera2NDKMetadata
	selectedScore := math.Inf(1)
	hasBackCamera := false
	for _, metadata := range metadatas {
		if metadata.LensFacing != Camera2NDKLensFacingBack {
			continue
		}
		hasBackCamera = true
		if err := validateCamera2NDKStreamFormat(cfg, metadata); err != nil {
			continue
		}
		score := camera2NDKWideScore(metadata)
		if score >= selectedScore {
			continue
		}
		selected = metadata
		selectedScore = score
	}
	if selected.CameraID == "" {
		if hasBackCamera {
			return Camera2NDKMetadata{}, fmt.Errorf(
				"%w: format=%s size=%dx%d",
				ErrCamera2NDKNoCompatibleBackCamera,
				cfg.ImageFormat,
				cfg.Width,
				cfg.Height,
			)
		}
		return Camera2NDKMetadata{}, ErrCamera2NDKNoBackCamera
	}
	return selected, nil
}

func camera2NDKWideScore(metadata Camera2NDKMetadata) float64 {
	score := math.Inf(1)
	for _, focalLength := range metadata.AvailableFocalLengths {
		if focalLength <= 0 {
			continue
		}
		score = min(score, float64(focalLength))
	}
	if !math.IsInf(score, 1) {
		return score
	}
	return float64(1 << 30)
}

func (format Camera2NDKImageFormat) String() string {
	if name := camera2NDKImageFormatName(format); name != "" {
		return name
	}
	return fmt.Sprintf("UNKNOWN_%d", int32(format))
}
