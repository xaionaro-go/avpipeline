package avfilter

import (
	"testing"

	testifyassert "github.com/stretchr/testify/assert"
)

func TestCropFilter(t *testing.T) {
	testifyassert.Equal(t, "crop=100:200:10:20", CropFilter(100, 200, 10, 20))
}

func TestCropFilter_ZeroOffset(t *testing.T) {
	testifyassert.Equal(t, "crop=640:480:0:0", CropFilter(640, 480, 0, 0))
}

func TestBoxBlurFilter(t *testing.T) {
	testifyassert.Equal(t, "boxblur=2:1", BoxBlurFilter(2, 1))
}

func TestBoxBlurFilter_Large(t *testing.T) {
	testifyassert.Equal(t, "boxblur=10:3", BoxBlurFilter(10, 3))
}

func TestScaleFilter(t *testing.T) {
	testifyassert.Equal(t, "scale=640:480", ScaleFilter(640, 480))
}

func TestScaleFilter_KeepAspect(t *testing.T) {
	testifyassert.Equal(t, "scale=-1:720", ScaleFilter(-1, 720))
}

func TestRotateFilter_90(t *testing.T) {
	s, err := RotateFilter(90)
	testifyassert.NoError(t, err)
	testifyassert.Equal(t, "transpose=1", s)
}

func TestRotateFilter_180(t *testing.T) {
	s, err := RotateFilter(180)
	testifyassert.NoError(t, err)
	testifyassert.Equal(t, "transpose=1,transpose=1", s)
}

func TestRotateFilter_270(t *testing.T) {
	s, err := RotateFilter(270)
	testifyassert.NoError(t, err)
	testifyassert.Equal(t, "transpose=2", s)
}

func TestRotateFilter_Invalid(t *testing.T) {
	_, err := RotateFilter(45)
	testifyassert.Error(t, err)
	testifyassert.Contains(t, err.Error(), "unsupported rotation angle")
}

func TestRotateFilter_Zero(t *testing.T) {
	_, err := RotateFilter(0)
	testifyassert.Error(t, err)
}

func TestMInterpolateFilter(t *testing.T) {
	testifyassert.Equal(t, "minterpolate=mi_mode=blend:fps=30.000000", MInterpolateFilter(30.0, "blend"))
}

func TestMInterpolateFilterAdvanced(t *testing.T) {
	s := MInterpolateFilterAdvanced(60.0, "mci", 1024)
	testifyassert.Contains(t, s, "mi_mode=mci")
	testifyassert.Contains(t, s, "fps=60.000000")
	testifyassert.Contains(t, s, "search_param=1024")
	testifyassert.Contains(t, s, "mc_mode=aobmc")
	testifyassert.Contains(t, s, "me_mode=bidir")
}

func TestRubberbandFilter_WithFormant(t *testing.T) {
	testifyassert.Equal(t, "rubberband=pitch=0.7:formant=preserved", RubberbandFilter(0.7, true))
}

func TestRubberbandFilter_WithoutFormant(t *testing.T) {
	testifyassert.Equal(t, "rubberband=pitch=1.3", RubberbandFilter(1.3, false))
}

func TestBilateralFilter(t *testing.T) {
	testifyassert.Equal(t, "bilateral=sigmaS=10:sigmaR=0.1", BilateralFilter(10, 0.1))
}

func TestBilateralFilter_Large(t *testing.T) {
	testifyassert.Equal(t, "bilateral=sigmaS=50:sigmaR=0.5", BilateralFilter(50, 0.5))
}

func TestBilateralCUDAFilter(t *testing.T) {
	testifyassert.Equal(t, "bilateral_cuda=sigmaS=10:sigmaR=0.1:window_size=5", BilateralCUDAFilter(10, 0.1, 5))
}

func TestNLMeansOpenCLFilter(t *testing.T) {
	testifyassert.Equal(t, "nlmeans_opencl=s=3.5:p=7:r=15", NLMeansOpenCLFilter(3.5, 7, 15))
}
