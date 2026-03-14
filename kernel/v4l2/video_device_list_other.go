//go:build !linux

package v4l2

import (
	"context"
	"fmt"
)

func listVideoDevices(_ context.Context) ([]VideoDevice, error) {
	return nil, fmt.Errorf("V4L2 device listing is only supported on Linux")
}
