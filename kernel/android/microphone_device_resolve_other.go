//go:build !android || !cgo
// +build !android !cgo

package android

import (
	"context"
	"fmt"
)

// resolveDeviceByName is a stub for non-Android builds. The real implementation
// parses `dumpsys media.audio_policy` to find the device ID by name.
var _ = resolveDeviceByName

func resolveDeviceByName(
	_ context.Context,
	pattern string,
) (int32, error) {
	return 0, fmt.Errorf("device name resolution is only supported on android (pattern: %q)", pattern)
}
