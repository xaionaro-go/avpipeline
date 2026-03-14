package deblemish

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/kernel/avfilter"
)

// filterStringForBackend returns the complete avfilter chain string
// for the given backend, including hwupload/hwdownload for GPU backends.
func filterStringForBackend(
	backend Backend,
	sigmaS, sigmaR float64,
	diameter int,
) string {
	windowSize := diameter
	if windowSize < 0 {
		windowSize = int(sigmaS)*2 + 1
	}

	switch backend {
	case BackendCUDA:
		return fmt.Sprintf(
			"hwupload_cuda,%s,hwdownload,format=yuv420p",
			avfilter.BilateralCUDAFilter(sigmaS, sigmaR, windowSize),
		)
	case BackendOpenCL:
		patchSize := windowSize
		if patchSize < 1 {
			patchSize = 7
		}
		researchSize := patchSize * 2
		return fmt.Sprintf(
			"hwupload,format_opencl,%s,hwdownload,format=yuv420p",
			avfilter.NLMeansOpenCLFilter(sigmaR*100, patchSize, researchSize),
		)
	case BackendVulkan:
		return fmt.Sprintf(
			"hwupload,format_vulkan,libplacebo=deband=true:deband_grain=0:deband_threshold=%g,hwdownload,format=yuv420p",
			sigmaR*100,
		)
	default:
		return avfilter.BilateralFilter(sigmaS, sigmaR)
	}
}
