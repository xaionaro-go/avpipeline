// camera.go implements helpers for opening an Android camera.

package android

import (
	"context"
	"fmt"

	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

const InputFormat = "android_camera"

type CameraID int

const (
	UndefinedCameraID CameraID = iota
	CameraIDBack
	CameraIDFront
)

func (id CameraID) String() string {
	switch id {
	case UndefinedCameraID:
		return "undefined"
	case CameraIDBack:
		return "back"
	case CameraIDFront:
		return "front"
	default:
		return fmt.Sprintf("unknown_%d_", int(id))
	}
}

func (id CameraID) CameraIndex() (int, error) {
	// see https://ffmpeg.org/ffmpeg-devices.html#android_005fcamera:
	switch id {
	case UndefinedCameraID:
		return -1, fmt.Errorf("cannot get camera index for undefined camera ID")
	case CameraIDBack:
		return 0, nil
	case CameraIDFront:
		return 1, nil
	default:
		return -1, fmt.Errorf("cannot get camera index for unknown camera ID: %d", int(id))
	}
}

func NewCamera(
	ctx context.Context,
	camID CameraID,
	resolution codectypes.Resolution,
	frameRate globaltypes.Rational,
	pixelFormat codectypes.PixelFormat,
	inputCfg kernel.InputConfig,
) (*kernel.Input, error) {
	cameraIndex, err := camID.CameraIndex()
	if err != nil {
		return nil, fmt.Errorf("unable to get camera index: %w", err)
	}
	inputCfg.CustomOptions = append(inputCfg.CustomOptions,
		globaltypes.DictionaryItem{Key: "f", Value: InputFormat},
		globaltypes.DictionaryItem{Key: "video_size", Value: resolution.String()},
		globaltypes.DictionaryItem{Key: "framerate", Value: frameRate.String()},
		globaltypes.DictionaryItem{Key: "pixel_format", Value: pixelFormat.String()},
		globaltypes.DictionaryItem{Key: "camera_index", Value: fmt.Sprintf("%d", cameraIndex)},
	)
	return kernel.NewInputFromURL(ctx, "", secret.New(""), inputCfg)
}
