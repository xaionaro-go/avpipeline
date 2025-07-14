//go:build with_cv
// +build with_cv

package kernel

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"gocv.io/x/gocv"
)

type HaarCascadeProcessor interface {
	fmt.Stringer
	Process(context.Context, frame.Output, []image.Rectangle) error
}

type HaarCascade struct {
	*closeChan
	Classifier gocv.CascadeClassifier
	Processor  HaarCascadeProcessor
}

var _ Abstract = (*HaarCascade)(nil)

func NewHaarCascade(
	classifierXML []byte,
	processor HaarCascadeProcessor,
) (*HaarCascade, error) {
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
		closeChan:  newCloseChan(),
		Classifier: classifier,
		Processor:  processor,
	}, nil
}

func (c *HaarCascade) SendInputPacket(
	context.Context,
	packet.Input,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return fmt.Errorf("haar cascade supports only decoded frames")
}

func (c *HaarCascade) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	_ chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return fmt.Errorf("not implemented, yet")
}

func (c *HaarCascade) String() string {
	return fmt.Sprintf("HaarCascade(%s)", c.Processor)
}

func (c *HaarCascade) Close(ctx context.Context) error {
	c.closeChan.Close(ctx)
	return nil
}

func (c *HaarCascade) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}
