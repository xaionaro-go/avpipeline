package android

import (
	"sort"

	"github.com/asticode/go-astiav"
)

type camera2NDKImagePlaneLayout uint8

const (
	camera2NDKImagePlaneLayoutYUV420 camera2NDKImagePlaneLayout = iota + 1
	camera2NDKImagePlaneLayoutRGBA
)

type camera2NDKImageFormatDescriptor struct {
	Format      Camera2NDKImageFormat
	Name        string
	PixelFormat astiav.PixelFormat
	PlaneLayout camera2NDKImagePlaneLayout
}

// Capture support is intentionally limited to descriptors with PixelFormat and
// PlaneLayout set. Add both fields plus image plane conversion before accepting
// another ImageReader format for frame output.
var camera2NDKImageFormatDescriptors = map[Camera2NDKImageFormat]camera2NDKImageFormatDescriptor{
	Camera2NDKImageFormatYUV420888: {
		Format:      Camera2NDKImageFormatYUV420888,
		Name:        "YUV_420_888",
		PixelFormat: astiav.PixelFormatYuv420P,
		PlaneLayout: camera2NDKImagePlaneLayoutYUV420,
	},
	Camera2NDKImageFormatRGBA8888: {
		Format:      Camera2NDKImageFormatRGBA8888,
		Name:        "RGBA_8888",
		PixelFormat: astiav.PixelFormatRgba,
		PlaneLayout: camera2NDKImagePlaneLayoutRGBA,
	},
	Camera2NDKImageFormatJPEG: {
		Format: Camera2NDKImageFormatJPEG,
		Name:   "JPEG",
	},
	Camera2NDKImageFormatPrivate: {
		Format: Camera2NDKImageFormatPrivate,
		Name:   "PRIVATE",
	},
	Camera2NDKImageFormatRaw16: {
		Format: Camera2NDKImageFormatRaw16,
		Name:   "RAW16",
	},
	Camera2NDKImageFormatRawPrivate: {
		Format: Camera2NDKImageFormatRawPrivate,
		Name:   "RAW_PRIVATE",
	},
	Camera2NDKImageFormatRaw10: {
		Format: Camera2NDKImageFormatRaw10,
		Name:   "RAW10",
	},
	Camera2NDKImageFormatRaw12: {
		Format: Camera2NDKImageFormatRaw12,
		Name:   "RAW12",
	},
	Camera2NDKImageFormatY8: {
		Format: Camera2NDKImageFormatY8,
		Name:   "Y8",
	},
}

func camera2NDKImageFormatDescriptorByFormat(
	format Camera2NDKImageFormat,
) (camera2NDKImageFormatDescriptor, bool) {
	descriptor, ok := camera2NDKImageFormatDescriptors[format]
	if !ok || descriptor.PlaneLayout == 0 {
		return camera2NDKImageFormatDescriptor{}, false
	}
	return descriptor, true
}

func Camera2NDKSupportedImageFormats() []Camera2NDKImageFormat {
	result := make([]Camera2NDKImageFormat, 0, len(camera2NDKImageFormatDescriptors))
	for format := range camera2NDKImageFormatDescriptors {
		if !IsCamera2NDKImageFormatSupported(format) {
			continue
		}
		result = append(result, format)
	}
	sort.Slice(result, func(idxA, idxB int) bool {
		return result[idxA] < result[idxB]
	})
	return result
}

func IsCamera2NDKImageFormatSupported(format Camera2NDKImageFormat) bool {
	_, ok := camera2NDKImageFormatDescriptorByFormat(format)
	return ok
}

func camera2NDKImageFormatName(
	format Camera2NDKImageFormat,
) string {
	descriptor, ok := camera2NDKImageFormatDescriptors[format]
	if !ok {
		return ""
	}
	return descriptor.Name
}

func camera2NDKPixelFormat(
	format Camera2NDKImageFormat,
) astiav.PixelFormat {
	descriptor, ok := camera2NDKImageFormatDescriptorByFormat(format)
	if !ok {
		return astiav.PixelFormatNone
	}
	return descriptor.PixelFormat
}
