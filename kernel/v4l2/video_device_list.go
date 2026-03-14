//go:build linux

// video_device_list.go lists V4L2 devices by reading sysfs.

package v4l2

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xaionaro-go/avpipeline/logger"
)

const sysfsV4L2Path = "/sys/class/video4linux"

// listVideoDevices reads /sys/class/video4linux/ and returns
// all video device entries with their names.
func listVideoDevices(ctx context.Context) ([]VideoDevice, error) {
	entries, err := os.ReadDir(sysfsV4L2Path)
	if err != nil {
		return nil, fmt.Errorf("unable to read %s: %w", sysfsV4L2Path, err)
	}

	var devices []VideoDevice
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "video") {
			continue
		}

		nameFile := filepath.Join(sysfsV4L2Path, name, "name")
		data, err := os.ReadFile(nameFile)
		if err != nil {
			logger.Debugf(ctx, "unable to read %s: %v", nameFile, err)
			continue
		}

		deviceName := strings.TrimSpace(string(data))
		devicePath := filepath.Join("/dev", name)

		devices = append(devices, VideoDevice{
			Path: devicePath,
			Name: deviceName,
		})
	}

	return devices, nil
}
