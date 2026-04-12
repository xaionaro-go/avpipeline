//go:build with_cv
// +build with_cv

// haar_cascade_cv.go implements a kernel for object detection using Haar cascades (requires OpenCV).

package kernel

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"gocv.io/x/gocv"
)

// HaarCascadeProcessor processes detected regions in a frame.
type HaarCascadeProcessor interface {
	fmt.Stringer
	Process(context.Context, *gocv.Mat, []image.Rectangle) error
}

// HaarCascadeConfig configures a HaarCascade kernel.
type HaarCascadeConfig struct {
	ScaleFactor  float64     // Detection scale factor (default 1.1)
	MinNeighbors int         // Min neighbors for detection (default 3)
	MinSize      image.Point // Min detection region size (default 30x30)
	MaxSize      image.Point // Max detection region size (default 0x0 = unlimited)
}

func (cfg *HaarCascadeConfig) setDefaults() {
	if cfg.ScaleFactor == 0 {
		cfg.ScaleFactor = 1.1
	}
	if cfg.MinNeighbors == 0 {
		cfg.MinNeighbors = 3
	}
	if cfg.MinSize.X == 0 && cfg.MinSize.Y == 0 {
		cfg.MinSize = image.Pt(30, 30)
	}
}

// HaarCascade detects objects in video frames using a Haar cascade classifier
// and delegates processing of detected regions to a HaarCascadeProcessor.
type HaarCascade struct {
	*closuresignaler.ClosureSignaler
	Classifier   gocv.CascadeClassifier
	Processor    HaarCascadeProcessor
	Enabled      *atomic.Bool // nil = always enabled; set to false to passthrough
	ScaleFactor  float64
	MinNeighbors int
	MinSize      image.Point
	MaxSize      image.Point
}

var _ Abstract = (*HaarCascade)(nil)

// NewHaarCascade creates a new HaarCascade kernel from classifier XML data.
func NewHaarCascade(
	classifierXML []byte,
	processor HaarCascadeProcessor,
	cfg HaarCascadeConfig,
) (*HaarCascade, error) {
	cfg.setDefaults()

	tempFile, err := os.CreateTemp("", "avpipeline-haar-cascade-classifier-*")
	if err != nil {
		return nil, fmt.Errorf("unable to create a temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	_, err = io.Copy(tempFile, bytes.NewReader(classifierXML))
	tempFile.Close()
	if err != nil {
		return nil, fmt.Errorf("unable to write the classifier XML into file '%s': %w", tempFile.Name(), err)
	}

	classifier := gocv.NewCascadeClassifier()
	if !classifier.Load(tempFile.Name()) {
		return nil, fmt.Errorf("unable to load the classifier XML")
	}

	return &HaarCascade{
		ClosureSignaler: closuresignaler.New(),
		Classifier:      classifier,
		Processor:       processor,
		ScaleFactor:     cfg.ScaleFactor,
		MinNeighbors:    cfg.MinNeighbors,
		MinSize:         cfg.MinSize,
		MaxSize:         cfg.MaxSize,
	}, nil
}

func (c *HaarCascade) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_, frameInput := input.Unwrap()
	if frameInput == nil {
		return fmt.Errorf("haar cascade supports only decoded frames")
	}

	// Passthrough when disabled.
	if c.Enabled != nil && !c.Enabled.Load() {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Passthrough non-video frames.
	if frameInput.GetMediaType() != astiav.MediaTypeVideo {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Clone the frame as writable so we can modify pixel data.
	writableFrame, err := frame.CloneAsWritable(frameInput.Frame)
	if err != nil {
		return fmt.Errorf("unable to clone frame as writable: %w", err)
	}

	// Convert frame to Go image.
	img, err := writableFrame.Data().GuessImageFormat()
	if err != nil {
		frame.Pool.Put(writableFrame)
		return fmt.Errorf("unable to guess image format: %w", err)
	}
	if err := writableFrame.Data().ToImage(img); err != nil {
		frame.Pool.Put(writableFrame)
		return fmt.Errorf("unable to convert frame to image: %w", err)
	}

	// Convert Go image to OpenCV Mat.
	mat, err := gocv.ImageToMatRGB(img)
	if err != nil {
		frame.Pool.Put(writableFrame)
		return fmt.Errorf("unable to convert image to Mat: %w", err)
	}
	defer mat.Close()

	// Detect objects.
	rects := c.Classifier.DetectMultiScaleWithParams(
		mat,
		c.ScaleFactor,
		c.MinNeighbors,
		0,
		c.MinSize,
		c.MaxSize,
	)

	// Apply processor to detected regions.
	if len(rects) > 0 {
		if err := c.Processor.Process(ctx, &mat, rects); err != nil {
			frame.Pool.Put(writableFrame)
			return fmt.Errorf("unable to process detected regions: %w", err)
		}

		// Convert Mat back to Go image, then write to frame.
		resultImg, err := mat.ToImage()
		if err != nil {
			frame.Pool.Put(writableFrame)
			return fmt.Errorf("unable to convert Mat to image: %w", err)
		}
		if err := writableFrame.Data().FromImage(resultImg); err != nil {
			frame.Pool.Put(writableFrame)
			return fmt.Errorf("unable to write image to frame: %w", err)
		}
	}

	outputFrame := frame.BuildOutput(writableFrame, frameInput.StreamInfo)
	select {
	case <-ctx.Done():
		frame.Pool.Put(writableFrame)
		return ctx.Err()
	case outputCh <- packetorframe.OutputUnion{Frame: &outputFrame}:
	}
	return nil
}

func (c *HaarCascade) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(c)
}

func (c *HaarCascade) String() string {
	return fmt.Sprintf("HaarCascade(%s)", c.Processor)
}

func (c *HaarCascade) Close(ctx context.Context) error {
	c.ClosureSignaler.Close(ctx)
	c.Classifier.Close()
	return nil
}

func (c *HaarCascade) Generate(
	_ context.Context,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}
