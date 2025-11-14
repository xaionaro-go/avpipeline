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
	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"gocv.io/x/gocv"
)

type HaarCascadeProcessor interface {
	fmt.Stringer
	Process(context.Context, *gocv.Mat, []image.Rectangle) error
}

type HaarCascade struct {
	*closuresignaler.ClosureSignaler
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
		ClosureSignaler: closuresignaler.New(),
		Classifier:      classifier,
		Processor:       processor,
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

	//c.Classifier.DetectMultiScale(mat)
	outputFrame := frame.BuildOutput(input.Frame, input.StreamInfo)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case outputFramesCh <- outputFrame:
	}
	return fmt.Errorf("not implemented, yet")
}

func (c *HaarCascade) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(c)
}

func (c *HaarCascade) String() string {
	return fmt.Sprintf("HaarCascade(%s)", c.Processor)
}

func (c *HaarCascade) Close(ctx context.Context) error {
	c.ClosureSignaler.Close(ctx)
	return nil
}

func (c *HaarCascade) Generate(
	context.Context,
	chan<- packet.Output,
	chan<- frame.Output,
) error {
	return nil
}
