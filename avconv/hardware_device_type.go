package avconv

import (
	"context"
	"strings"

	"github.com/asticode/go-astiav"
)

func HardwareDeviceTypeFromString(
	ctx context.Context,
	s string,
) astiav.HardwareDeviceType {
	normalizeString := func(s string) string {
		return strings.ToLower(strings.Trim(s, " "))
	}
	s = normalizeString(s)
	for _, candidate := range []astiav.HardwareDeviceType{
		astiav.HardwareDeviceTypeCUDA,
		astiav.HardwareDeviceTypeD3D11VA,
		astiav.HardwareDeviceTypeDRM,
		astiav.HardwareDeviceTypeDXVA2,
		astiav.HardwareDeviceTypeMediaCodec,
		astiav.HardwareDeviceTypeOpenCL,
		astiav.HardwareDeviceTypeQSV,
		astiav.HardwareDeviceTypeVAAPI,
		astiav.HardwareDeviceTypeVDPAU,
		astiav.HardwareDeviceTypeVideoToolbox,
		astiav.HardwareDeviceTypeVulkan,
	} {
		if normalizeString(candidate.String()) == s {
			return candidate
		}
	}

	return astiav.HardwareDeviceTypeNone
}
