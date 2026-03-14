package deblemish

import (
	"github.com/asticode/go-astiav"
)

// Backend selects the processing backend for the bilateral filter.
type Backend int

const (
	// BackendAuto probes available backends at construction time
	// and selects the best one.
	BackendAuto Backend = iota

	// BackendCPU uses FFmpeg's software bilateral filter.
	BackendCPU

	// BackendCUDA uses FFmpeg's CUDA-accelerated bilateral filter.
	BackendCUDA

	// BackendOpenCL uses FFmpeg's OpenCL-accelerated nlmeans filter
	// as a smoothing proxy.
	BackendOpenCL

	// BackendVulkan uses FFmpeg's libplacebo filter with Vulkan backend.
	BackendVulkan
)

func (b Backend) String() string {
	switch b {
	case BackendAuto:
		return "Auto"
	case BackendCPU:
		return "CPU"
	case BackendCUDA:
		return "CUDA"
	case BackendOpenCL:
		return "OpenCL"
	case BackendVulkan:
		return "Vulkan"
	default:
		return "Unknown"
	}
}

// detectBestBackend probes FFmpeg for available filter backends
// and returns the best one. Priority: CUDA > Vulkan > OpenCL > CPU.
func detectBestBackend() Backend {
	if astiav.FindFilterByName("bilateral_cuda") != nil {
		return BackendCUDA
	}
	if astiav.FindFilterByName("libplacebo") != nil {
		return BackendVulkan
	}
	if astiav.FindFilterByName("nlmeans_opencl") != nil {
		return BackendOpenCL
	}
	return BackendCPU
}

// resolveBackend returns the concrete backend. If b is BackendAuto,
// it probes for the best available backend.
func resolveBackend(b Backend) Backend {
	if b == BackendAuto {
		return detectBestBackend()
	}
	return b
}
