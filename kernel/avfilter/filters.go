package avfilter

import "fmt"

// CropFilter returns an FFmpeg crop filter string.
// Parameters: output width, height, and top-left corner (x, y).
func CropFilter(w, h, x, y int) string {
	return fmt.Sprintf("crop=%d:%d:%d:%d", w, h, x, y)
}

// BoxBlurFilter returns an FFmpeg boxblur filter string.
func BoxBlurFilter(lumaRadius, lumaPower int) string {
	return fmt.Sprintf("boxblur=%d:%d", lumaRadius, lumaPower)
}

// ScaleFilter returns an FFmpeg scale filter string.
// Use -1 for either dimension to maintain aspect ratio.
func ScaleFilter(w, h int) string {
	return fmt.Sprintf("scale=%d:%d", w, h)
}

// RotateFilter returns an FFmpeg filter string for rotation via transpose.
// Supported angles: 90, 180, 270 degrees clockwise.
func RotateFilter(degrees int) (string, error) {
	switch degrees {
	case 90:
		return "transpose=1", nil
	case 180:
		return "transpose=1,transpose=1", nil
	case 270:
		return "transpose=2", nil
	default:
		return "", fmt.Errorf("unsupported rotation angle: %d (only 90, 180, 270 supported)", degrees)
	}
}

// MInterpolateFilter returns an FFmpeg minterpolate filter string for
// motion-compensated frame interpolation.
func MInterpolateFilter(fps float64, miMode string) string {
	return fmt.Sprintf("minterpolate=mi_mode=%s:fps=%f", miMode, fps)
}

// MInterpolateFilterAdvanced returns an FFmpeg minterpolate filter string
// with advanced motion compensation options.
func MInterpolateFilterAdvanced(fps float64, miMode string, searchParam int) string {
	return fmt.Sprintf(
		"minterpolate=mi_mode=%s:fps=%f:mc_mode=aobmc:me_mode=bidir:me=esa:search_param=%d:vsbmc=1:scd=fdiff",
		miMode, fps, searchParam,
	)
}

// RubberbandFilter returns an FFmpeg rubberband audio filter string for pitch shifting.
// pitchScale < 1.0 lowers pitch; > 1.0 raises pitch. formantPreserve keeps
// formants stable so the shifted voice sounds more natural.
func RubberbandFilter(pitchScale float64, formantPreserve bool) string {
	s := fmt.Sprintf("rubberband=pitch=%g", pitchScale)
	if formantPreserve {
		s += ":formant=preserved"
	}
	return s
}
