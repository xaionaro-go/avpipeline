package android

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCamera2NDKNormalizeConfigDefaultsImageFormatAndMaxImages(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{})

	require.NoError(t, err)
	require.Equal(t, Camera2NDKImageFormatYUV420888, cfg.ImageFormat)
	require.Equal(t, int32(2), cfg.MaxImages)
	require.Equal(t, int32(640), cfg.Width)
	require.Equal(t, int32(480), cfg.Height)
	require.Equal(t, int32(30), cfg.FrameRate)
}

func TestCamera2NDKNormalizeConfigRejectsUnsupportedConfiguredFormat(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{
		ImageFormat: Camera2NDKImageFormatJPEG,
	})

	require.ErrorIs(t, err, ErrCamera2NDKUnsupportedImageFormat)
	require.Zero(t, cfg)
}

func TestCamera2NDKSelectConfiguredFormatRequiresCameraOutputSupport(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{
		ImageFormat: Camera2NDKImageFormatRGBA8888,
	})
	require.NoError(t, err)

	err = validateCamera2NDKStreamFormat(cfg, Camera2NDKMetadata{
		CameraID: "0",
		StreamConfigurations: []Camera2NDKStreamConfiguration{
			{ImageFormat: Camera2NDKImageFormatRGBA8888, Width: 640, Height: 480, Output: true},
		},
	})
	require.NoError(t, err)

	err = validateCamera2NDKStreamFormat(cfg, Camera2NDKMetadata{
		CameraID: "0",
		StreamConfigurations: []Camera2NDKStreamConfiguration{
			{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 1280, Height: 720, Output: true},
			{ImageFormat: Camera2NDKImageFormatRGBA8888, Width: 640, Height: 480, Output: false},
		},
	})

	require.ErrorIs(t, err, ErrCamera2NDKUnsupportedImageFormat)
}

func TestCamera2NDKSupportedImageFormatsIncludesOnlyCaptureFormats(t *testing.T) {
	formats := Camera2NDKSupportedImageFormats()

	require.ElementsMatch(t, []Camera2NDKImageFormat{
		Camera2NDKImageFormatYUV420888,
		Camera2NDKImageFormatRGBA8888,
	}, formats)
	require.True(t, IsCamera2NDKImageFormatSupported(Camera2NDKImageFormatYUV420888))
	require.True(t, IsCamera2NDKImageFormatSupported(Camera2NDKImageFormatRGBA8888))
	require.False(t, IsCamera2NDKImageFormatSupported(Camera2NDKImageFormatJPEG))
}

func TestCamera2NDKListMetadataReturnsPlatformErrorOnHost(t *testing.T) {
	if runtime.GOOS == "android" {
		t.Skip("host-only platform stub behavior")
	}
	metadata, err := ListCamera2NDKMetadata(context.Background())

	require.Error(t, err)
	require.Nil(t, metadata)
}

func TestCamera2NDKSelectTargetFPSRangeChoosesNarrowestContainingRange(t *testing.T) {
	selected, ok := camera2NDKSelectTargetFPSRange(30, []Camera2NDKFrameRateRange{
		{Min: 15, Max: 30},
		{Min: 30, Max: 30},
		{Min: 24, Max: 60},
	})

	require.True(t, ok)
	require.Equal(t, Camera2NDKFrameRateRange{Min: 30, Max: 30}, selected)
}

func TestCamera2NDKSelectTargetFPSRangeReportsUnsupportedRate(t *testing.T) {
	selected, ok := camera2NDKSelectTargetFPSRange(120, []Camera2NDKFrameRateRange{
		{Min: 15, Max: 30},
		{Min: 30, Max: 60},
	})

	require.False(t, ok)
	require.Zero(t, selected)
}

func TestCamera2NDKValidatePhysicalCameraID(t *testing.T) {
	err := validateCamera2NDKPhysicalCamera(Camera2NDKConfig{
		PhysicalCameraID: "2",
	}, Camera2NDKMetadata{
		CameraID:          "0",
		PhysicalCameraIDs: []string{"2", "3"},
	})
	require.NoError(t, err)

	err = validateCamera2NDKPhysicalCamera(Camera2NDKConfig{
		PhysicalCameraID: "4",
	}, Camera2NDKMetadata{
		CameraID:          "0",
		PhysicalCameraIDs: []string{"2", "3"},
	})
	require.ErrorIs(t, err, ErrCamera2NDKPhysicalCameraNotFound)
}

func TestCamera2NDKResolvePhysicalCameraWithoutLogicalSelectsContainingCompatibleLogicalCamera(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{
		PhysicalCameraID: "wide",
	})
	require.NoError(t, err)

	selected, err := resolveCamera2NDKSelection(cfg, []Camera2NDKMetadata{
		{
			CameraID:              "default-wide-logical",
			LensFacing:            Camera2NDKLensFacingBack,
			AvailableFocalLengths: []float32{1.8},
			PhysicalCameraIDs:     []string{"main"},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 640, Height: 480, Output: true},
			},
		},
		{
			CameraID:              "contains-requested-physical",
			LensFacing:            Camera2NDKLensFacingBack,
			AvailableFocalLengths: []float32{4.2},
			PhysicalCameraIDs:     []string{"wide"},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 640, Height: 480, Output: true},
			},
		},
	})

	require.NoError(t, err)
	require.Equal(t, "contains-requested-physical", selected.CameraID)
}

func TestCamera2NDKResolvePhysicalCameraWithoutLogicalRequiresContainingCompatibleLogicalCamera(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{
		PhysicalCameraID: "missing",
	})
	require.NoError(t, err)

	selected, err := resolveCamera2NDKSelection(cfg, []Camera2NDKMetadata{
		{
			CameraID:          "0",
			LensFacing:        Camera2NDKLensFacingBack,
			PhysicalCameraIDs: []string{"main"},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 640, Height: 480, Output: true},
			},
		},
	})

	require.ErrorIs(t, err, ErrCamera2NDKPhysicalCameraNotFound)
	require.Zero(t, selected)
}

func TestCamera2NDKResolvePhysicalCameraWithoutLogicalReportsIncompatibleContainingLogicalCamera(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{
		PhysicalCameraID: "wide",
	})
	require.NoError(t, err)

	selected, err := resolveCamera2NDKSelection(cfg, []Camera2NDKMetadata{
		{
			CameraID:          "incompatible-containing-logical",
			LensFacing:        Camera2NDKLensFacingBack,
			PhysicalCameraIDs: []string{"wide"},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatRGBA8888, Width: 640, Height: 480, Output: true},
			},
		},
		{
			CameraID:          "compatible-without-physical",
			LensFacing:        Camera2NDKLensFacingBack,
			PhysicalCameraIDs: []string{"main"},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 640, Height: 480, Output: true},
			},
		},
	})

	require.ErrorIs(t, err, ErrCamera2NDKUnsupportedImageFormat)
	require.Zero(t, selected)
}

func TestCamera2NDKResolveExplicitLogicalCameraStillValidatesPhysicalSeparately(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{
		LogicalCameraID:  "explicit",
		PhysicalCameraID: "missing-from-explicit",
	})
	require.NoError(t, err)

	selected, err := resolveCamera2NDKSelection(cfg, []Camera2NDKMetadata{
		{
			CameraID:          "explicit",
			LensFacing:        Camera2NDKLensFacingBack,
			PhysicalCameraIDs: []string{"different"},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 640, Height: 480, Output: true},
			},
		},
		{
			CameraID:          "contains-requested-physical",
			LensFacing:        Camera2NDKLensFacingBack,
			PhysicalCameraIDs: []string{"missing-from-explicit"},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 640, Height: 480, Output: true},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "explicit", selected.CameraID)

	err = validateCamera2NDKPhysicalCamera(cfg, selected)
	require.ErrorIs(t, err, ErrCamera2NDKPhysicalCameraNotFound)
}

func TestCamera2NDKResolveWidestCompatibleBackCamera(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{})
	require.NoError(t, err)

	selected, err := resolveCamera2NDKSelection(cfg, []Camera2NDKMetadata{
		{
			CameraID:              "0",
			LensFacing:            Camera2NDKLensFacingBack,
			AvailableFocalLengths: []float32{4.2},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 640, Height: 480, Output: true},
			},
		},
		{
			CameraID:              "1",
			LensFacing:            Camera2NDKLensFacingFront,
			AvailableFocalLengths: []float32{2.0},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 1280, Height: 720, Output: true},
			},
		},
		{
			CameraID:              "2",
			LensFacing:            Camera2NDKLensFacingBack,
			AvailableFocalLengths: []float32{1.8},
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 1920, Height: 1080, Output: true},
			},
		},
	})

	require.NoError(t, err)
	require.Equal(t, "0", selected.CameraID)
}

func TestCamera2NDKResolveWidestCompatibleBackCameraReturnsSpecificErrorWhenBackCamerasAreIncompatible(t *testing.T) {
	cfg, err := normalizeCamera2NDKConfig(Camera2NDKConfig{})
	require.NoError(t, err)

	selected, err := resolveCamera2NDKSelection(cfg, []Camera2NDKMetadata{
		{
			CameraID:   "0",
			LensFacing: Camera2NDKLensFacingBack,
			StreamConfigurations: []Camera2NDKStreamConfiguration{
				{ImageFormat: Camera2NDKImageFormatYUV420888, Width: 1920, Height: 1080, Output: true},
			},
		},
	})

	require.ErrorIs(t, err, ErrCamera2NDKNoCompatibleBackCamera)
	require.Zero(t, selected)
}

func TestCamera2NDKResolveExplicitLogicalCamera(t *testing.T) {
	selected, err := resolveCamera2NDKSelection(Camera2NDKConfig{
		LogicalCameraID: "1",
	}, []Camera2NDKMetadata{
		{
			CameraID:              "0",
			LensFacing:            Camera2NDKLensFacingBack,
			AvailableFocalLengths: []float32{1.0},
		},
		{
			CameraID:              "1",
			LensFacing:            Camera2NDKLensFacingFront,
			AvailableFocalLengths: []float32{3.0},
		},
	})

	require.NoError(t, err)
	require.Equal(t, "1", selected.CameraID)
}

func TestCamera2NDKReadinessErrorsAreSentinels(t *testing.T) {
	require.ErrorIs(t, ErrCamera2NDKSessionNotReady, ErrCamera2NDKSessionNotReady)
	require.ErrorIs(t, ErrCamera2NDKSessionClosed, ErrCamera2NDKSessionClosed)
	require.ErrorIs(t, ErrCamera2NDKSessionCloseTimeout, ErrCamera2NDKSessionCloseTimeout)
}
