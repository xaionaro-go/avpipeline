package avfilter

import (
	"context"
	"fmt"
	"math"

	"github.com/asticode/go-astiav"
	kernelavfilter "github.com/xaionaro-go/avpipeline/kernel/avfilter"
)

// FrameRotator applies rotation to video frames using a SimpleGraph.
type FrameRotator struct {
	graph *kernelavfilter.SimpleGraph
}

// NewFrameRotator creates a FrameRotator for the given rotation angle.
// Supported angles: 90, 180, 270 (and their negative equivalents).
// Returns an error if rotation is 0 or unsupported.
func NewFrameRotator(
	ctx context.Context,
	f *astiav.Frame,
	timeBase astiav.Rational,
	rotation float64,
) (*FrameRotator, error) {
	normRotation := math.Mod(rotation, 360)
	if normRotation < 0 {
		normRotation += 360
	}

	var filterStr string
	switch normRotation {
	case 90:
		filterStr = "transpose=1"
	case 180:
		filterStr = "transpose=1,transpose=1"
	case 270:
		filterStr = "transpose=2"
	case 0:
		return nil, fmt.Errorf("no rotation needed")
	default:
		return nil, fmt.Errorf("unsupported rotation angle: %f", rotation)
	}

	graph, err := kernelavfilter.NewSimpleVideoGraphFromFrame(ctx, filterStr, f, timeBase)
	if err != nil {
		return nil, fmt.Errorf("unable to create rotation filter graph: %w", err)
	}

	return &FrameRotator{
		graph: graph,
	}, nil
}

// Rotate applies rotation to the input frame and returns the rotated frame.
func (fr *FrameRotator) Rotate(ctx context.Context, in *astiav.Frame) (*astiav.Frame, error) {
	frames, err := fr.graph.ProcessFrame(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("unable to rotate frame: %w", err)
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("rotation filter produced no output")
	}
	return frames[0], nil
}

// Close frees the filter graph.
func (fr *FrameRotator) Close() {
	if fr == nil {
		return
	}
	if fr.graph != nil {
		fr.graph.Close()
		fr.graph = nil
	}
}
