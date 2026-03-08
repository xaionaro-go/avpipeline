# Enable CUDA Video Decoding (h264_cuvid)

## Problem

CUDA hardware decoding is explicitly disabled in `codec/codec.go:361-364` with the message
"hardware decoding using CUDA is not supported, yet". Additionally, even if the block is removed,
`initHardwarePixelFormat` has a logic bug that prevents `h264_cuvid` from initializing correctly.

## Root Causes

### 1. Explicit block

```go
if !isEncoder && hardwareDeviceType == globaltypes.HardwareDeviceTypeCUDA {
    logger.Warnf(ctx, "hardware decoding using CUDA is not supported, yet")
    hardwareDeviceType = globaltypes.HardwareDeviceTypeNone
}
```

### 2. initHardwarePixelFormat logic bug

`h264_cuvid` reports a single hardware config with method flags = 7 (HwDeviceCtx | HwFramesCtx | Internal).
The switch in `initHardwarePixelFormat` checks `HwFramesCtx` first. Since it's present, it sets
`hardwareContextType = hardwareContextTypeFrames` and `continue`s. Since there's only one config,
the loop ends without ever trying `HwDeviceCtx`. Since `HwFramesCtx` is unimplemented, this fails.

## Fix

1. Remove the CUDA decoding block (lines 361-364).
2. In `initHardwarePixelFormat`, when a config has both `HwFramesCtx` and `HwDeviceCtx`, prefer
   `HwDeviceCtx` (which is implemented). Only fall back to `HwFramesCtx` if `HwDeviceCtx` is absent.

## Testing

- Integration test: encode a synthetic frame with `h264_nvenc`, decode with `h264_cuvid`,
  verify the decoded frame has correct dimensions and pixel format.
- Tests skip gracefully when no NVIDIA GPU is available.
