package camera

import (
	"context"
	"fmt"

	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
	"github.com/xaionaro-go/avpipeline/kernel"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/secret"
)

const InputFormat = "android_camera"

type ID int

const (
	UndefinedID ID = iota
	IDBack
	IDFront
)

func (id ID) String() string {
	switch id {
	case UndefinedID:
		return "undefined"
	case IDBack:
		return "back"
	case IDFront:
		return "front"
	default:
		return fmt.Sprintf("unknown_%d_", int(id))
	}
}

func (id ID) CameraIndex() int {
	// see https://ffmpeg.org/ffmpeg-devices.html#android_005fcamera:
	switch id {
	case UndefinedID:
		panic("cannot get URL string for undefined camera ID")
	case IDBack:
		return 0
	case IDFront:
		return 1
	default:
		panic("cannot get URL string for unknown camera ID")
	}
}

func Open(
	ctx context.Context,
	camID ID,
	resolution codectypes.Resolution,
	frameRate globaltypes.Rational,
	pixelFormat codectypes.PixelFormat,
	inputCfg kernel.InputConfig,
) (*kernel.Input, error) {
	inputCfg.CustomOptions = append(inputCfg.CustomOptions,
		globaltypes.DictionaryItem{Key: "f", Value: InputFormat},
		globaltypes.DictionaryItem{Key: "video_size", Value: resolution.String()},
		globaltypes.DictionaryItem{Key: "framerate", Value: frameRate.String()},
		globaltypes.DictionaryItem{Key: "pixel_format", Value: pixelFormat.String()},
		globaltypes.DictionaryItem{Key: "camera_index", Value: fmt.Sprintf("%d", camID.CameraIndex())},
	)
	return kernel.NewInputFromURL(ctx, "", secret.New(""), inputCfg)
}
