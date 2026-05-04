# Encoder Factory Guide

`codec.EncoderFactory` is the extension point for choosing encoder settings at
stream-open time. `kernel.Encoder` and `kernel.Transcoder` call
`EncoderFactory.NewEncoder` once AVPipeline knows the input stream metadata.

## What NewEncoder Receives

`NewEncoder(ctx, params, timeBase, opts...)` receives per-stream metadata:

- `params.MediaType()` selects video, audio, or passthrough handling.
- `params.CodecID()` identifies the input codec.
- Video metadata includes `params.Width()`, `params.Height()`,
  `params.FrameRate()`, `params.PixelFormat()`, `params.BitRate()`,
  color fields, sample aspect ratio, profile, level, extradata, and side data.
- Audio metadata includes `params.SampleRate()`, `params.ChannelLayout()`,
  `params.SampleFormat()`, frame size, bitrate, profile, extradata, and side
  data.
- `timeBase` is the stream time base used for output packet timestamps.
- `opts` may include `codec.EncoderFactoryOptionGetDecoderer` when decoded
  frames expose their decoder. Use
  `codec.EncoderFactoryOptionLatest[codec.EncoderFactoryOptionGetDecoderer](opts)`
  to retrieve it.

For most metadata-driven configuration, inspect `params` and `timeBase`. Use
the decoder option only when a decision needs decoder state that is not present
in `CodecParameters`.

## Custom Factory Shape

A custom factory implements three methods:

```go
type MyEncoderFactory struct{}

func (f *MyEncoderFactory) String() string { return "MyEncoderFactory" }

func (f *MyEncoderFactory) Reset(ctx context.Context) error {
	return nil
}

func (f *MyEncoderFactory) NewEncoder(
	ctx context.Context,
	params *astiav.CodecParameters,
	timeBase astiav.Rational,
	opts ...codec.Option,
) (codec.Encoder, error) {
	if params == nil {
		return nil, errors.New("nil codec parameters")
	}
	if timeBase.Num() == 0 || timeBase.Den() == 0 {
		return nil, fmt.Errorf("zero time base %s", timeBase)
	}

	switch params.MediaType() {
	case astiav.MediaTypeVideo:
		return codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			VideoCodec:     codec.Name("libx264"),
			VideoQuality:   quality.ConstantBitrate(selectVideoBitrate(params)),
			VideoResolution: capResolution(params, 1920),
		}).NewEncoder(ctx, params, timeBase, opts...)
	case astiav.MediaTypeAudio:
		return codec.NewNaiveEncoderFactory(ctx, &codec.NaiveEncoderFactoryParams{
			AudioCodec:   codec.Name("aac"),
			AudioQuality: quality.ConstantBitrate(selectAudioBitrate(params)),
		}).NewEncoder(ctx, params, timeBase, opts...)
	case astiav.MediaTypeSubtitle, astiav.MediaTypeData, astiav.MediaTypeAttachment:
		return codec.EncoderCopy{}, nil
	default:
		return nil, fmt.Errorf("unsupported media type %s", params.MediaType())
	}
}
```

For a complete compilable example, see
`codec/encoder_factory_example_test.go`.

## Dynamic Metadata-Based Settings

Make stream-specific decisions inside `NewEncoder`:

- Choose a codec from `params.MediaType()` and `params.CodecID()`.
- Cap large input resolutions with `params.Width()` and `params.Height()`.
- Scale bitrate from resolution, frame rate, channel count, or source bitrate.
- Set `NaiveEncoderFactoryParams.VideoOptions` or `AudioOptions` before
  delegating to `NaiveEncoderFactory`.
- Return `codec.EncoderCopy{}` for streams that should pass through instead of
  being re-encoded.

`VideoOptions` and `AudioOptions` are FFmpeg open-time options. They are cloned
when an encoder is created. Updating them after an encoder is open does not
change that live encoder. To apply a new open-time option to an existing stream,
update the factory state and hard-reset the bound `kernel.Encoder` or
`kernel.Transcoder` so AVPipeline opens the encoder again.

Use runtime APIs for live changes that the encoder supports without reopening:
`codec.Encoder.SetQuality`, `codec.Encoder.SetResolution`, or the matching
kernel-level operations that call them.

## Error Handling

Return errors instead of silently falling back when metadata is missing or a
media type is unsupported. At minimum, validate nil `CodecParameters`, zero
time base, unsupported media types, option construction errors, and errors
returned by delegated factories. If a stream should intentionally bypass
encoding, return `codec.EncoderCopy{}` rather than hiding the decision behind a
nil encoder.
