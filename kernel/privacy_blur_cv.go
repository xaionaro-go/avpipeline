//go:build with_cv
// +build with_cv

// privacy_blur_cv.go implements a kernel that detects faces and license plates
// in video frames and applies blur to hide them.

package kernel

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"sync/atomic"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	uberatomic "go.uber.org/atomic"
	"gocv.io/x/gocv"
)

// ClassifierConfig configures a single Haar cascade classifier.
type ClassifierConfig struct {
	Name         string      // Human-readable name (e.g. "face", "plate")
	XML          []byte      // Haar cascade XML data
	ScaleFactor  float64     // Detection scale factor (default 1.1)
	MinNeighbors int         // Min neighbors for detection (default 3)
	MinSize      image.Point // Min detection region size (default 30x30)
	MaxSize      image.Point // Max detection region size (default 0x0 = unlimited)
}

func (cfg *ClassifierConfig) setDefaults() {
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

// PrivacyBlurConfig configures the PrivacyBlur kernel.
type PrivacyBlurConfig struct {
	Classifiers []ClassifierConfig
	BlurRadius  float64 // Initial GaussianBlur radius (default 15)
	BlockSize   int     // Initial Pixelate block size (default 0 = use GaussianBlur)
}

// PrivacyBlur detects objects (faces, license plates) in video frames using
// multiple Haar cascade classifiers and applies blur to all detected regions.
// It supports dynamic enable/disable via Enabled and runtime-adjustable blur
// via BlurRadius/PixelateBlockSize.
type PrivacyBlur struct {
	*closuresignaler.ClosureSignaler
	Enabled           *atomic.Bool            // nil = always on; false = passthrough
	BlurRadius        uberatomic.Float64      // GaussianBlur radius (0 = use pixelation)
	PixelateBlockSize uberatomic.Int64        // Pixelate block size (0 = use gaussian)
	classifiers       []classifierState
}

type classifierState struct {
	name         string
	classifier   gocv.CascadeClassifier
	scaleFactor  float64
	minNeighbors int
	minSize      image.Point
	maxSize      image.Point
}

var _ Abstract = (*PrivacyBlur)(nil)

// NewPrivacyBlur creates a PrivacyBlur kernel with the given classifiers.
func NewPrivacyBlur(
	cfg PrivacyBlurConfig,
) (*PrivacyBlur, error) {
	if len(cfg.Classifiers) == 0 {
		return nil, fmt.Errorf("at least one classifier is required")
	}
	if cfg.BlurRadius == 0 && cfg.BlockSize == 0 {
		cfg.BlurRadius = 15
	}

	classifiers := make([]classifierState, 0, len(cfg.Classifiers))
	for i, cc := range cfg.Classifiers {
		cc.setDefaults()
		c, err := loadClassifier(cc.XML)
		if err != nil {
			// Close already-loaded classifiers on error.
			for _, prev := range classifiers {
				prev.classifier.Close()
			}
			return nil, fmt.Errorf("unable to load classifier %d (%s): %w", i, cc.Name, err)
		}
		classifiers = append(classifiers, classifierState{
			name:         cc.Name,
			classifier:   c,
			scaleFactor:  cc.ScaleFactor,
			minNeighbors: cc.MinNeighbors,
			minSize:      cc.MinSize,
			maxSize:      cc.MaxSize,
		})
	}

	pb := &PrivacyBlur{
		ClosureSignaler: closuresignaler.New(),
		classifiers:     classifiers,
	}
	pb.BlurRadius.Store(cfg.BlurRadius)
	pb.PixelateBlockSize.Store(int64(cfg.BlockSize))
	return pb, nil
}

func loadClassifier(xmlData []byte) (gocv.CascadeClassifier, error) {
	tempFile, err := os.CreateTemp("", "avpipeline-cascade-*")
	if err != nil {
		return gocv.CascadeClassifier{}, fmt.Errorf("unable to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	_, err = io.Copy(tempFile, bytes.NewReader(xmlData))
	tempFile.Close()
	if err != nil {
		return gocv.CascadeClassifier{}, fmt.Errorf("unable to write classifier XML: %w", err)
	}

	c := gocv.NewCascadeClassifier()
	if !c.Load(tempFile.Name()) {
		return gocv.CascadeClassifier{}, fmt.Errorf("unable to load classifier XML")
	}
	return c, nil
}

func (pb *PrivacyBlur) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_, frameInput := input.Unwrap()
	if frameInput == nil {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Passthrough when disabled.
	if pb.Enabled != nil && !pb.Enabled.Load() {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Passthrough non-video frames.
	if frameInput.GetMediaType() != astiav.MediaTypeVideo {
		outputCh <- input.CloneAsReferencedOutput()
		return nil
	}

	// Clone the frame as writable.
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

	// Run all classifiers and collect detected regions.
	var allRects []image.Rectangle
	for i := range pb.classifiers {
		cs := &pb.classifiers[i]
		rects := cs.classifier.DetectMultiScaleWithParams(
			mat,
			cs.scaleFactor,
			cs.minNeighbors,
			0,
			cs.minSize,
			cs.maxSize,
		)
		allRects = append(allRects, rects...)
	}

	// Apply blur to all detected regions.
	if len(allRects) > 0 {
		if err := pb.blurRegions(ctx, &mat, allRects); err != nil {
			frame.Pool.Put(writableFrame)
			return fmt.Errorf("unable to blur detected regions: %w", err)
		}

		// Convert Mat back to frame.
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

func (pb *PrivacyBlur) blurRegions(_ context.Context, mat *gocv.Mat, rects []image.Rectangle) error {
	matBounds := image.Rect(0, 0, mat.Cols(), mat.Rows())

	blockSize := int(pb.PixelateBlockSize.Load())
	if blockSize > 1 {
		for _, rect := range rects {
			rect = rect.Intersect(matBounds)
			if rect.Empty() {
				continue
			}
			region := mat.Region(rect)
			w, h := rect.Dx(), rect.Dy()
			smallW := max(1, w/blockSize)
			smallH := max(1, h/blockSize)
			small := gocv.NewMat()
			gocv.Resize(region, &small, image.Pt(smallW, smallH), 0, 0, gocv.InterpolationNearestNeighbor)
			gocv.Resize(small, &region, image.Pt(w, h), 0, 0, gocv.InterpolationNearestNeighbor)
			small.Close()
			region.Close()
		}
		return nil
	}

	radius := pb.BlurRadius.Load()
	if radius == 0 {
		radius = 15
	}
	ksize := int(radius)*2 + 1
	size := image.Pt(ksize, ksize)
	for _, rect := range rects {
		rect = rect.Intersect(matBounds)
		if rect.Empty() {
			continue
		}
		region := mat.Region(rect)
		gocv.GaussianBlur(region, &region, size, 0, 0, gocv.BorderDefault)
		region.Close()
	}
	return nil
}

func (pb *PrivacyBlur) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(pb)
}

func (pb *PrivacyBlur) String() string {
	names := make([]string, len(pb.classifiers))
	for i, cs := range pb.classifiers {
		names[i] = cs.name
	}
	return fmt.Sprintf("PrivacyBlur(%s)", strings.Join(names, ","))
}

func (pb *PrivacyBlur) Close(ctx context.Context) error {
	pb.ClosureSignaler.Close(ctx)
	for i := range pb.classifiers {
		pb.classifiers[i].classifier.Close()
	}
	return nil
}

func (pb *PrivacyBlur) Generate(
	_ context.Context,
	_ chan<- packetorframe.OutputUnion,
) error {
	return nil
}
