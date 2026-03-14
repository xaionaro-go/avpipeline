// video_device_resolve.go resolves a V4L2 device name pattern to a
// device path by scanning /sys/class/video4linux/.

package v4l2

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/xaionaro-go/avpipeline/logger"
)

// ResolveDeviceByName scans available V4L2 devices and returns the
// device path (e.g., "/dev/video12") of the device whose name matches
// the given regexp pattern.
//
// When matchIndex is nil, exactly one device must match (error on zero or multiple).
// When matchIndex is non-nil, the value selects which match to use (0-based index).
func ResolveDeviceByName(
	ctx context.Context,
	pattern string,
	matchIndex *int,
) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid device name pattern %q: %w", pattern, err)
	}

	devices, err := listVideoDevices(ctx)
	if err != nil {
		return "", err
	}

	matches := matchDevices(re, devices)

	if matchIndex != nil {
		idx := *matchIndex
		if idx < 0 || idx >= len(matches) {
			names := make([]string, len(matches))
			for i, dev := range matches {
				names[i] = fmt.Sprintf("  [%d] %s: %s", i, dev.Path, dev.Name)
			}
			return "", fmt.Errorf(
				"match_index %d out of range for pattern %q (%d matches):\n%s",
				idx, pattern, len(matches), strings.Join(names, "\n"),
			)
		}
		logger.Infof(ctx,
			"resolved V4L2 device name pattern %q (match_index=%d) to %s (%s)",
			pattern, idx, matches[idx].Path, matches[idx].Name,
		)
		return matches[idx].Path, nil
	}

	switch len(matches) {
	case 0:
		names := make([]string, len(devices))
		for i, dev := range devices {
			names[i] = fmt.Sprintf("  %s: %s", dev.Path, dev.Name)
		}
		return "", fmt.Errorf(
			"no V4L2 device matches pattern %q; available devices:\n%s",
			pattern, strings.Join(names, "\n"),
		)
	case 1:
		logger.Infof(ctx,
			"resolved V4L2 device name pattern %q to %s (%s)",
			pattern, matches[0].Path, matches[0].Name,
		)
		return matches[0].Path, nil
	default:
		names := make([]string, len(matches))
		for i, dev := range matches {
			names[i] = fmt.Sprintf("  [%d] %s: %s", i, dev.Path, dev.Name)
		}
		return "", fmt.Errorf(
			"multiple V4L2 devices match pattern %q; narrow the pattern or use match_index:\n%s",
			pattern, strings.Join(names, "\n"),
		)
	}
}

// matchDevices returns all devices whose Name matches the regexp.
func matchDevices(
	re *regexp.Regexp,
	devices []VideoDevice,
) []VideoDevice {
	var matches []VideoDevice
	for _, dev := range devices {
		if re.MatchString(dev.Name) {
			matches = append(matches, dev)
		}
	}
	return matches
}
