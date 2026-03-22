//go:build android && cgo
// +build android,cgo

// microphone_device_resolve.go resolves a microphone device name
// pattern to an AAudio device ID by parsing the output of
// `dumpsys media.audio_policy`.

package android

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/xaionaro-go/avpipeline/logger"
)

// audioInputDevice represents a single input device entry from
// the Android audio policy dump.
type audioInputDevice struct {
	PortID int32
	Name   string
	Type   string
}

// resolveDeviceByName runs `dumpsys media.audio_policy`, parses the
// "Available input devices" section, and returns the Port ID of the
// device whose name or type description matches the given regexp
// pattern. Returns an error if zero or more than one device matches.
func resolveDeviceByName(
	ctx context.Context,
	pattern string,
) (int32, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, fmt.Errorf("invalid device name pattern %q: %w", pattern, err)
	}

	devices, err := listInputDevices(ctx)
	if err != nil {
		return 0, err
	}

	var matches []audioInputDevice
	for _, dev := range devices {
		if re.MatchString(dev.Name) || re.MatchString(dev.Type) {
			matches = append(matches, dev)
		}
	}

	switch len(matches) {
	case 0:
		names := make([]string, len(devices))
		for i, dev := range devices {
			names[i] = fmt.Sprintf("  Port ID %d: %s (%s)", dev.PortID, dev.Name, dev.Type)
		}
		return 0, fmt.Errorf(
			"no input device matches pattern %q; available devices:\n%s",
			pattern, strings.Join(names, "\n"),
		)
	case 1:
		logger.Infof(ctx,
			"resolved device name pattern %q to Port ID %d (%s)",
			pattern, matches[0].PortID, matches[0].Name,
		)
		return matches[0].PortID, nil
	default:
		names := make([]string, len(matches))
		for i, dev := range matches {
			names[i] = fmt.Sprintf("  Port ID %d: %s (%s)", dev.PortID, dev.Name, dev.Type)
		}
		return 0, fmt.Errorf(
			"multiple input devices match pattern %q; narrow the pattern:\n%s",
			pattern, strings.Join(names, "\n"),
		)
	}
}

// listInputDevices runs `su -c "dumpsys media.audio_policy"` and parses
// the "Available input devices" section into structured entries.
// Requires Magisk su because dumpsys needs shell/root privileges.
func listInputDevices(ctx context.Context) ([]audioInputDevice, error) {
	out, err := exec.CommandContext(
		ctx, "su", "-c", "/system/bin/dumpsys media.audio_policy",
	).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("unable to run 'su -c dumpsys media.audio_policy': %w: %s", err, out)
	}

	return parseInputDevices(string(out)), nil
}

// parseInputDevices extracts input device entries from the dumpsys
// output. The format is:
//
//	Available input devices (N):
//	  1. Port ID: 21; "microphones"; {AUDIO_DEVICE_IN_BUILTIN_MIC, @:bottom}
//	     ...
//	  5. Port ID: 305; "usb-device-microphones"; {AUDIO_DEVICE_IN_USB_DEVICE, @:card=1;device=0}
//	     "USB-Audio - AI Wireless Lavalier Microphone"
func parseInputDevices(dump string) []audioInputDevice {
	lines := strings.Split(dump, "\n")

	// Find "Available input devices" section.
	var inSection bool
	var devices []audioInputDevice
	var lastDevice *audioInputDevice

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Available input devices") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		// Empty line ends the section.
		if trimmed == "" {
			break
		}

		// Lines like: `1. Port ID: 21; "microphones"; {AUDIO_DEVICE_IN_BUILTIN_MIC, @:bottom}`
		if strings.Contains(trimmed, "Port ID:") {
			dev := parsePortIDLine(trimmed)
			if dev != nil {
				devices = append(devices, *dev)
				lastDevice = &devices[len(devices)-1]
			}
			continue
		}

		// Lines like: `"USB-Audio - AI Wireless Lavalier Microphone"` (extra name)
		if lastDevice != nil && strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"") {
			extraName := strings.Trim(trimmed, "\"")
			if extraName != "" {
				lastDevice.Name = extraName
			}
			continue
		}

		// Other lines (profiles, etc.) — just skip, but don't end the section.
	}

	return devices
}

// parsePortIDLine parses a line like:
// `1. Port ID: 305; "usb-device-microphones"; {AUDIO_DEVICE_IN_USB_DEVICE, @:card=1;device=0}`
func parsePortIDLine(line string) *audioInputDevice {
	// Extract Port ID.
	idx := strings.Index(line, "Port ID:")
	if idx < 0 {
		return nil
	}
	rest := line[idx+len("Port ID:"):]
	rest = strings.TrimSpace(rest)

	// Port ID ends at ';'.
	semicolonIdx := strings.Index(rest, ";")
	if semicolonIdx < 0 {
		return nil
	}
	portIDStr := strings.TrimSpace(rest[:semicolonIdx])
	portID, err := strconv.ParseInt(portIDStr, 10, 32)
	if err != nil {
		return nil
	}
	rest = rest[semicolonIdx+1:]

	// Extract name (quoted string).
	name := extractQuoted(rest)

	// Extract type (inside braces).
	deviceType := extractBraced(rest)

	return &audioInputDevice{
		PortID: int32(portID),
		Name:   name,
		Type:   deviceType,
	}
}

func extractQuoted(s string) string {
	start := strings.Index(s, "\"")
	if start < 0 {
		return ""
	}
	end := strings.Index(s[start+1:], "\"")
	if end < 0 {
		return ""
	}
	return s[start+1 : start+1+end]
}

func extractBraced(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	end := strings.Index(s[start:], "}")
	if end < 0 {
		return ""
	}
	return s[start+1 : start+end]
}
