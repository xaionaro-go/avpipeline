package types

type StreamQuality struct {
	Continuity float64
	Overlap    float64
	FrameRate  float64
	InvalidDTS uint
}
