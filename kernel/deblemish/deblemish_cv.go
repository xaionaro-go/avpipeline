//go:build with_cv

package deblemish

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/cascadedata"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"gocv.io/x/gocv"
)

type faceDetectorState struct {
	classifier gocv.CascadeClassifier
	loaded     bool
}

func (d *Deblemish) initFaceDetector() error {
	xmlData := d.config.FaceClassifierXML
	if len(xmlData) == 0 {
		xmlData = cascadedata.FaceFrontalDefault
	}

	classifier, err := loadCascadeClassifier(xmlData)
	if err != nil {
		return err
	}

	d.faceDetector.classifier = classifier
	d.faceDetector.loaded = true
	return nil
}

func (d *Deblemish) closeFaceDetector() error {
	if d.faceDetector.loaded {
		d.faceDetector.classifier.Close()
		d.faceDetector.loaded = false
	}
	return nil
}

func loadCascadeClassifier(xmlData []byte) (gocv.CascadeClassifier, error) {
	tempFile, err := os.CreateTemp("", "avpipeline-deblemish-cascade-*")
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
		return gocv.CascadeClassifier{}, fmt.Errorf("unable to load cascade classifier XML")
	}
	return c, nil
}

func (d *Deblemish) sendInputFaceOnly(
	ctx context.Context,
	input packetorframe.InputUnion,
	frameInput *frame.Input,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	writableFrame, err := frame.CloneAsWritable(frameInput.Frame)
	if err != nil {
		return fmt.Errorf("unable to clone frame as writable: %w", err)
	}

	img, err := writableFrame.Data().GuessImageFormat()
	if err != nil {
		frame.Pool.Put(writableFrame)
		return fmt.Errorf("unable to guess image format: %w", err)
	}
	if err := writableFrame.Data().ToImage(img); err != nil {
		frame.Pool.Put(writableFrame)
		return fmt.Errorf("unable to convert frame to image: %w", err)
	}

	mat, err := gocv.ImageToMatRGB(img)
	if err != nil {
		frame.Pool.Put(writableFrame)
		return fmt.Errorf("unable to convert image to Mat: %w", err)
	}
	defer mat.Close()

	rects := d.faceDetector.classifier.DetectMultiScaleWithParams(
		mat,
		d.config.FaceScaleFactor,
		d.config.FaceMinNeighbors,
		0,
		d.config.FaceMinSize,
		d.config.FaceMaxSize,
	)

	if len(rects) > 0 {
		sigmaS := d.SigmaS.Load()
		sigmaR := d.SigmaR.Load()
		diameter := int(d.Diameter.Load())

		if err := bilateralFilterRegions(&mat, rects, diameter, sigmaR, sigmaS); err != nil {
			frame.Pool.Put(writableFrame)
			return fmt.Errorf("unable to apply bilateral filter to face regions: %w", err)
		}

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

func bilateralFilterRegions(
	mat *gocv.Mat,
	rects []image.Rectangle,
	diameter int,
	sigmaColor, sigmaSpace float64,
) error {
	matBounds := image.Rect(0, 0, mat.Cols(), mat.Rows())

	for _, rect := range rects {
		rect = rect.Intersect(matBounds)
		if rect.Empty() {
			continue
		}

		region := mat.Region(rect)
		dst := gocv.NewMat()
		gocv.BilateralFilter(region, &dst, diameter, sigmaColor, sigmaSpace)

		// Copy filtered result back into the original region.
		dst.CopyTo(&region)
		dst.Close()
		region.Close()
	}
	return nil
}

