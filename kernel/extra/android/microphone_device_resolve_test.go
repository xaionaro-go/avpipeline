//go:build android && cgo
// +build android,cgo

package android

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleDumpsys = `
 Available input devices (6):
  1. Port ID: 21; "microphones"; {AUDIO_DEVICE_IN_BUILTIN_MIC, @:bottom}
     Encapsulation modes: 0, metadata types: 0
     Port Hal ID: 101001
   - Profiles (2):
      1. "profile-1"; AUDIO_FORMAT_PCM_16_BIT (0x1)
         sampling rates: 48000
         channel masks: 0x0010
         AUDIO_ENCAPSULATION_TYPE_NONE
  2. Port ID: 27; "telephony-rx"; {AUDIO_DEVICE_IN_TELEPHONY_RX, @:}
     Encapsulation modes: 0, metadata types: 0
  3. Port ID: 22; "back-microphones"; {AUDIO_DEVICE_IN_BACK_MIC, @:back}
     Encapsulation modes: 0, metadata types: 0
  4. Port ID: 33; "Remote Submix In"; {AUDIO_DEVICE_IN_REMOTE_SUBMIX, @:0}
     Encapsulation modes: 0, metadata types: 0
  5. Port ID: 305; "usb-device-microphones"; {AUDIO_DEVICE_IN_USB_DEVICE, @:card=1;device=0}
     Encapsulation modes: 0, metadata types: 0
     "USB-Audio - AI Wireless Lavalier Microphone"
     Port Hal ID: 0
   - Profiles (1):
      1. ""; [dynamic format][dynamic channels][dynamic rates]; AUDIO_FORMAT_PCM_16_BIT (0x1)
  6. Port ID: 31; "echo-reference"; {AUDIO_DEVICE_IN_ECHO_REFERENCE, @:}
     Encapsulation modes: 0, metadata types: 0

 Preferred devices for capture preset:
`

func TestParseInputDevices(t *testing.T) {
	devices := parseInputDevices(sampleDumpsys)
	require.Len(t, devices, 6)

	assert.Equal(t, int32(21), devices[0].PortID)
	assert.Equal(t, "microphones", devices[0].Name)
	assert.Contains(t, devices[0].Type, "BUILTIN_MIC")

	assert.Equal(t, int32(27), devices[1].PortID)
	assert.Equal(t, "telephony-rx", devices[1].Name)

	assert.Equal(t, int32(22), devices[2].PortID)
	assert.Equal(t, "back-microphones", devices[2].Name)
	assert.Contains(t, devices[2].Type, "BACK_MIC")

	assert.Equal(t, int32(33), devices[3].PortID)
	assert.Equal(t, "Remote Submix In", devices[3].Name)

	// USB device: extra name line overrides the quoted name.
	assert.Equal(t, int32(305), devices[4].PortID)
	assert.Equal(t, "USB-Audio - AI Wireless Lavalier Microphone", devices[4].Name)
	assert.Contains(t, devices[4].Type, "USB_DEVICE")

	assert.Equal(t, int32(31), devices[5].PortID)
	assert.Equal(t, "echo-reference", devices[5].Name)
}

func TestParseInputDevices_Empty(t *testing.T) {
	devices := parseInputDevices("")
	assert.Empty(t, devices)
}

func TestParseInputDevices_NoSection(t *testing.T) {
	devices := parseInputDevices("some other dumpsys output\nwithout input devices section\n")
	assert.Empty(t, devices)
}
