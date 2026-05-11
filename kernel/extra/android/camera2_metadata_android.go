//go:build android && cgo
// +build android,cgo

package android

import (
	"context"
	"fmt"

	"github.com/AndroidGoLab/ndk/camera"
	cameracapi "github.com/AndroidGoLab/ndk/capi/camera"
)

func camera2NDKMetadataFromCharacteristics(
	cameraID string,
	metadata *camera.Metadata,
) (Camera2NDKMetadata, error) {
	result := Camera2NDKMetadata{
		CameraID:                 cameraID,
		LensFacing:               camera2NDKMetadataLensFacing(metadata),
		PhysicalCameraIDs:        camera2NDKMetadataPhysicalIDs(metadata),
		AvailableFocalLengths:    camera2NDKMetadataFloat32s(metadata, uint32(cameracapi.ACAMERA_LENS_INFO_AVAILABLE_FOCAL_LENGTHS)),
		AvailableTargetFPSRanges: camera2NDKMetadataFrameRateRanges(metadata),
	}
	streams, hasStreams, err := camera2NDKMetadataStreamConfigurations(metadata)
	if err != nil {
		return Camera2NDKMetadata{}, err
	}
	result.StreamConfigurations = streams
	result.HasStreamConfiguration = hasStreams
	return result, nil
}

func ListCamera2NDKMetadata(ctx context.Context) ([]Camera2NDKMetadata, error) {
	_ = ctx
	mgr := camera.NewManager()
	if mgr == nil {
		return nil, fmt.Errorf("unable to create camera manager")
	}
	defer func() {
		_ = mgr.Close()
	}()
	return camera2NDKReadMetadata(mgr)
}

func camera2NDKMetadataLensFacing(metadata *camera.Metadata) Camera2NDKLensFacing {
	values := camera2NDKMetadataU8s(metadata, uint32(cameracapi.ACAMERA_LENS_FACING))
	if len(values) == 0 {
		return Camera2NDKLensFacingUnknown
	}
	switch Camera2NDKLensFacing(values[0]) {
	case Camera2NDKLensFacingFront,
		Camera2NDKLensFacingBack,
		Camera2NDKLensFacingExternal:
		return Camera2NDKLensFacing(values[0])
	default:
		return Camera2NDKLensFacingUnknown
	}
}

func camera2NDKMetadataPhysicalIDs(metadata *camera.Metadata) []string {
	values := camera2NDKMetadataU8s(metadata, uint32(cameracapi.ACAMERA_LOGICAL_MULTI_CAMERA_PHYSICAL_IDS))
	if len(values) == 0 {
		return nil
	}
	var result []string
	start := 0
	for idx, value := range values {
		if value != 0 {
			continue
		}
		if start < idx {
			result = append(result, string(values[start:idx]))
		}
		start = idx + 1
	}
	if start < len(values) {
		result = append(result, string(values[start:]))
	}
	return result
}

func camera2NDKMetadataStreamConfigurations(
	metadata *camera.Metadata,
) ([]Camera2NDKStreamConfiguration, bool, error) {
	tag := uint32(cameracapi.ACAMERA_SCALER_AVAILABLE_STREAM_CONFIGURATIONS)
	count := metadata.I32Count(tag)
	if count <= 0 {
		return nil, false, nil
	}
	if count%4 != 0 {
		return nil, true, fmt.Errorf("invalid camera stream configuration count: %d", count)
	}
	result := make([]Camera2NDKStreamConfiguration, 0, count/4)
	for idx := int32(0); idx+3 < count; idx += 4 {
		result = append(result, Camera2NDKStreamConfiguration{
			ImageFormat: Camera2NDKImageFormat(metadata.I32At(tag, idx)),
			Width:       metadata.I32At(tag, idx+1),
			Height:      metadata.I32At(tag, idx+2),
			Output:      metadata.I32At(tag, idx+3) == 0,
		})
	}
	return result, true, nil
}

func camera2NDKMetadataFrameRateRanges(metadata *camera.Metadata) []Camera2NDKFrameRateRange {
	tag := uint32(cameracapi.ACAMERA_CONTROL_AE_AVAILABLE_TARGET_FPS_RANGES)
	count := metadata.I32Count(tag)
	if count <= 0 {
		return nil
	}
	result := make([]Camera2NDKFrameRateRange, 0, count/2)
	for idx := int32(0); idx+1 < count; idx += 2 {
		result = append(result, Camera2NDKFrameRateRange{
			Min: metadata.I32At(tag, idx),
			Max: metadata.I32At(tag, idx+1),
		})
	}
	return result
}

func camera2NDKMetadataU8s(metadata *camera.Metadata, tag uint32) []byte {
	count := int(metadata.U8Count(tag))
	if count <= 0 {
		return nil
	}
	result := make([]byte, count)
	for idx := range result {
		result[idx] = metadata.U8At(tag, int32(idx))
	}
	return result
}

func camera2NDKMetadataFloat32s(metadata *camera.Metadata, tag uint32) []float32 {
	count := int(metadata.FloatCount(tag))
	if count <= 0 {
		return nil
	}
	result := make([]float32, count)
	for idx := range result {
		result[idx] = metadata.FloatAt(tag, int32(idx))
	}
	return result
}
