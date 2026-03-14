//go:build !with_cv

package deblemish

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

type faceDetectorState struct{}

func (d *Deblemish) initFaceDetector() error {
	if d.config.FaceOnly {
		return fmt.Errorf("face-only mode requires the with_cv build tag")
	}
	return nil
}

func (d *Deblemish) closeFaceDetector() error {
	_ = d.faceDetector
	return nil
}

func (d *Deblemish) sendInputFaceOnly(
	_ context.Context,
	_ packetorframe.InputUnion,
	_ *frame.Input,
	_ chan<- packetorframe.OutputUnion,
) error {
	return fmt.Errorf("face-only mode requires the with_cv build tag")
}
