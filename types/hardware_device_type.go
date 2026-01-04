// hardware_device_type.go defines the HardwareDeviceType enum and its methods.

// Package types provides common types and interfaces used throughout the avpipeline project.
package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

type HardwareDeviceType int

const (
	// the constants are copied from libav's enum AVHWDeviceType:
	HardwareDeviceTypeCUDA         = HardwareDeviceType(0x2)
	HardwareDeviceTypeD3D11VA      = HardwareDeviceType(0x7)
	HardwareDeviceTypeDRM          = HardwareDeviceType(0x8)
	HardwareDeviceTypeDXVA2        = HardwareDeviceType(0x4)
	HardwareDeviceTypeMediaCodec   = HardwareDeviceType(0xa)
	HardwareDeviceTypeNone         = HardwareDeviceType(0x0)
	HardwareDeviceTypeOpenCL       = HardwareDeviceType(0x9)
	HardwareDeviceTypeQSV          = HardwareDeviceType(0x5)
	HardwareDeviceTypeVAAPI        = HardwareDeviceType(0x3)
	HardwareDeviceTypeVDPAU        = HardwareDeviceType(0x1)
	HardwareDeviceTypeVideoToolbox = HardwareDeviceType(0x6)
	HardwareDeviceTypeVulkan       = HardwareDeviceType(0xb)
)

func (r HardwareDeviceType) String() string {
	switch r {
	case 0:
		return "none"
	case HardwareDeviceTypeCUDA:
		return "cuda"
	case HardwareDeviceTypeDRM:
		return "drm"
	case HardwareDeviceTypeDXVA2:
		return "dxva2"
	case HardwareDeviceTypeD3D11VA:
		return "d3d11va"
	// case HardwareDeviceTypeD3D12VA:
	//	return "d3d12va"
	case HardwareDeviceTypeOpenCL:
		return "opencl"
	case HardwareDeviceTypeQSV:
		return "qsv"
	case HardwareDeviceTypeVAAPI:
		return "vaapi"
	case HardwareDeviceTypeVDPAU:
		return "vdpau"
	case HardwareDeviceTypeVideoToolbox:
		return "videotoolbox"
	case HardwareDeviceTypeMediaCodec:
		return "mediacodec"
	case HardwareDeviceTypeVulkan:
		return "vulkan"
	}
	return fmt.Sprintf("unknown_%X", int64(r))
}

func HardwareDeviceTypeFromString(s string) HardwareDeviceType {
	sanitizeString := func(s string) string {
		return strings.Trim(strings.ToLower(s), " \n\r\t")
	}
	s = sanitizeString(s)
	for i := 0; i <= 0xff; i++ {
		hwt := HardwareDeviceType(i)
		c := sanitizeString(hwt.String())
		if s == c {
			return hwt
		}
	}
	return -1
}

func (r *HardwareDeviceType) UnmarshalYAML(b []byte) error {
	devType := string(b)
	devType = strings.Trim(strings.ToLower(devType), " \"\n\t\r")
	for candidate := range HardwareDeviceType(0xf) {
		if strings.ToLower(candidate.String()) == devType {
			*r = candidate
			return nil
		}
	}

	return fmt.Errorf("unknown hardware device type: '%s'", devType)
}

func (r HardwareDeviceType) MarshalYAML() ([]byte, error) {
	return json.Marshal(r.String())
}
