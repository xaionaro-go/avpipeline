package subpixelshift

// planeData holds pixel data for a single color plane with its dimensions.
// Chroma planes in YUV420P are half the luma resolution, so each plane
// tracks its own width and height.
type planeData struct {
	data          [][]float64
	width, height int
}
