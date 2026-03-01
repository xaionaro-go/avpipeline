//go:build with_cv
// +build with_cv

// Package privacyblur provides preset constructors for face and license plate
// blurring kernels with sensible default configurations.
package privacyblur

import (
	"context"
	"image"
	"sync/atomic"

	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/kernel/cascadedata"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/processor"
)

// NewBlurFaces creates a PrivacyBlur kernel configured for frontal face detection.
func NewBlurFaces(enabled *atomic.Bool) (*kernel.PrivacyBlur, error) {
	pb, err := kernel.NewPrivacyBlur(kernel.PrivacyBlurConfig{
		Classifiers: []kernel.ClassifierConfig{
			{
				Name:         "face",
				XML:          cascadedata.FaceFrontalDefault,
				ScaleFactor:  1.1,
				MinNeighbors: 3,
				MinSize:      image.Pt(30, 30),
			},
		},
		BlurRadius: 15,
	})
	if err != nil {
		return nil, err
	}
	pb.Enabled = enabled
	return pb, nil
}

// NewBlurPlates creates a PrivacyBlur kernel configured for license plate detection.
func NewBlurPlates(enabled *atomic.Bool) (*kernel.PrivacyBlur, error) {
	pb, err := kernel.NewPrivacyBlur(kernel.PrivacyBlurConfig{
		Classifiers: []kernel.ClassifierConfig{
			{
				Name:         "plate",
				XML:          cascadedata.PlateRussian,
				ScaleFactor:  1.1,
				MinNeighbors: 3,
				MinSize:      image.Pt(60, 20),
			},
		},
		BlurRadius: 15,
	})
	if err != nil {
		return nil, err
	}
	pb.Enabled = enabled
	return pb, nil
}

// NewBlurAll creates a PrivacyBlur kernel configured for both face and plate detection.
func NewBlurAll(enabled *atomic.Bool) (*kernel.PrivacyBlur, error) {
	pb, err := kernel.NewPrivacyBlur(kernel.PrivacyBlurConfig{
		Classifiers: []kernel.ClassifierConfig{
			{
				Name:         "face",
				XML:          cascadedata.FaceFrontalDefault,
				ScaleFactor:  1.1,
				MinNeighbors: 3,
				MinSize:      image.Pt(30, 30),
			},
			{
				Name:         "plate",
				XML:          cascadedata.PlateRussian,
				ScaleFactor:  1.1,
				MinNeighbors: 3,
				MinSize:      image.Pt(60, 20),
			},
		},
		BlurRadius: 15,
	})
	if err != nil {
		return nil, err
	}
	pb.Enabled = enabled
	return pb, nil
}

// NewNode wraps a PrivacyBlur kernel in a pipeline node.
func NewNode(
	ctx context.Context,
	blur *kernel.PrivacyBlur,
	procOpts ...processor.Option,
) *node.Node[*processor.FromKernel[*kernel.PrivacyBlur]] {
	opts := append(processor.DefaultOptionsTranscoder(), procOpts...)
	return node.New(processor.NewFromKernel(ctx, blur, opts...))
}
