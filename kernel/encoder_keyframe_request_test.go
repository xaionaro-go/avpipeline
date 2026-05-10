package kernel

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
)

type keyFrameRequestRecordingEncoderFactory struct {
	encoder *keyFrameRequestRecordingEncoder
}

var _ codec.EncoderFactory = (*keyFrameRequestRecordingEncoderFactory)(nil)

func (f *keyFrameRequestRecordingEncoderFactory) String() string {
	return "keyFrameRequestRecordingEncoderFactory"
}

func (f *keyFrameRequestRecordingEncoderFactory) NewEncoder(
	_ context.Context,
	_ *astiav.CodecParameters,
	_ astiav.Rational,
	_ ...codec.Option,
) (codec.Encoder, error) {
	encoderCodec := astiav.FindEncoder(astiav.CodecIDH264)
	if encoderCodec == nil {
		return nil, fmt.Errorf("h264 encoder is unavailable")
	}

	encoder := &keyFrameRequestRecordingEncoder{
		codecContext: astiav.AllocCodecContext(encoderCodec),
	}
	f.encoder = encoder
	return encoder, nil
}

func (f *keyFrameRequestRecordingEncoderFactory) Reset(context.Context) error {
	return nil
}

type keyFrameRequestRecordingEncoder struct {
	codec.EncoderRaw

	codecContext      *astiav.CodecContext
	forceNextKeyFrame atomic.Bool
}

func (e *keyFrameRequestRecordingEncoder) Close(context.Context) error {
	if e.codecContext != nil {
		e.codecContext.Free()
		e.codecContext = nil
	}
	return nil
}

func (e *keyFrameRequestRecordingEncoder) CodecContext(context.Context) *astiav.CodecContext {
	return e.codecContext
}

func (e *keyFrameRequestRecordingEncoder) MediaType(context.Context) astiav.MediaType {
	return astiav.MediaTypeVideo
}

func (e *keyFrameRequestRecordingEncoder) SetForceNextKeyFrame(
	_ context.Context,
	v bool,
) error {
	e.forceNextKeyFrame.Store(v)
	return nil
}

func TestEncoder_SetForceNextKeyFrameAppliesToFutureVideoEncoder(t *testing.T) {
	ctx := context.Background()
	factory := &keyFrameRequestRecordingEncoderFactory{}
	encoder := NewEncoder[codec.EncoderFactory](ctx, factory, nil)
	t.Cleanup(func() {
		require.NoError(t, encoder.Close(ctx))
	})

	require.NoError(t, encoder.SetForceNextKeyFrame(ctx, true))

	params := astiav.AllocCodecParameters()
	t.Cleanup(params.Free)
	params.SetMediaType(astiav.MediaTypeVideo)
	params.SetCodecID(astiav.CodecIDH264)
	params.SetWidth(1920)
	params.SetHeight(1080)

	require.NoError(t, encoder.initEncoderFor(
		ctx,
		0,
		params,
		astiav.NewRational(1, 30),
		nil,
	))
	require.NotNil(t, factory.encoder)
	require.True(t, factory.encoder.forceNextKeyFrame.Load())
}
