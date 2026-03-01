package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHardwareDeviceType_String(t *testing.T) {
	tests := []struct {
		hwt      HardwareDeviceType
		expected string
	}{
		{HardwareDeviceTypeNone, "none"},
		{HardwareDeviceTypeCUDA, "cuda"},
		{HardwareDeviceTypeVDPAU, "vdpau"},
		{HardwareDeviceTypeVAAPI, "vaapi"},
		{HardwareDeviceTypeDXVA2, "dxva2"},
		{HardwareDeviceTypeQSV, "qsv"},
		{HardwareDeviceTypeVideoToolbox, "videotoolbox"},
		{HardwareDeviceTypeD3D11VA, "d3d11va"},
		{HardwareDeviceTypeDRM, "drm"},
		{HardwareDeviceTypeOpenCL, "opencl"},
		{HardwareDeviceTypeMediaCodec, "mediacodec"},
		{HardwareDeviceTypeVulkan, "vulkan"},
	}
	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.hwt.String())
		})
	}
}

func TestHardwareDeviceType_String_Unknown(t *testing.T) {
	s := HardwareDeviceType(0xFF).String()
	assert.Contains(t, s, "unknown_")
}

func TestHardwareDeviceTypeFromString(t *testing.T) {
	tests := []struct {
		input    string
		expected HardwareDeviceType
	}{
		{"cuda", HardwareDeviceTypeCUDA},
		{"CUDA", HardwareDeviceTypeCUDA},
		{" cuda ", HardwareDeviceTypeCUDA},
		{"vaapi", HardwareDeviceTypeVAAPI},
		{"none", HardwareDeviceTypeNone},
		{"qsv", HardwareDeviceTypeQSV},
		{"videotoolbox", HardwareDeviceTypeVideoToolbox},
		{"mediacodec", HardwareDeviceTypeMediaCodec},
		{"vulkan", HardwareDeviceTypeVulkan},
		{"drm", HardwareDeviceTypeDRM},
		{"d3d11va", HardwareDeviceTypeD3D11VA},
		{"dxva2", HardwareDeviceTypeDXVA2},
		{"opencl", HardwareDeviceTypeOpenCL},
		{"vdpau", HardwareDeviceTypeVDPAU},
		{"", HardwareDeviceTypeNone},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, HardwareDeviceTypeFromString(tc.input))
		})
	}
}

func TestHardwareDeviceTypeFromString_Unknown(t *testing.T) {
	assert.Equal(t, HardwareDeviceType(-1), HardwareDeviceTypeFromString("nonexistent"))
}

func TestHardwareDeviceType_YAML_RoundTrip(t *testing.T) {
	for _, hwt := range []HardwareDeviceType{
		HardwareDeviceTypeCUDA,
		HardwareDeviceTypeVAAPI,
		HardwareDeviceTypeNone,
	} {
		t.Run(hwt.String(), func(t *testing.T) {
			data, err := hwt.MarshalYAML()
			require.NoError(t, err)

			var decoded HardwareDeviceType
			err = decoded.UnmarshalYAML(data)
			require.NoError(t, err)
			assert.Equal(t, hwt, decoded)
		})
	}
}

func TestHardwareDeviceType_UnmarshalYAML_Unknown(t *testing.T) {
	var hwt HardwareDeviceType
	err := hwt.UnmarshalYAML([]byte("nonexistent_device"))
	assert.Error(t, err)
}
