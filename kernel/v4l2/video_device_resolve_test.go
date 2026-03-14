package v4l2

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchDevices(t *testing.T) {
	devices := []VideoDevice{
		{Path: "/dev/video6", Name: "s5p-mfc-dec"},
		{Path: "/dev/video7", Name: "s5p-mfc-enc"},
		{Path: "/dev/video8", Name: "s5p-mfc-dec-secure"},
		{Path: "/dev/video9", Name: "s5p-mfc-enc-secure"},
		{Path: "/dev/video10", Name: "s5p-mfc-enc-otf"},
		{Path: "/dev/video11", Name: "s5p-mfc-enc-otf-secure"},
		{Path: "/dev/video12", Name: "USB Video: DJI Osmo Pocket 3"},
	}

	t.Run("exact_match", func(t *testing.T) {
		re := regexp.MustCompile("DJI")
		matches := matchDevices(re, devices)
		require.Len(t, matches, 1)
		assert.Equal(t, "/dev/video12", matches[0].Path)
		assert.Equal(t, "USB Video: DJI Osmo Pocket 3", matches[0].Name)
	})

	t.Run("partial_match", func(t *testing.T) {
		re := regexp.MustCompile("USB")
		matches := matchDevices(re, devices)
		require.Len(t, matches, 1)
		assert.Equal(t, "/dev/video12", matches[0].Path)
	})

	t.Run("multiple_matches", func(t *testing.T) {
		re := regexp.MustCompile("mfc-enc")
		matches := matchDevices(re, devices)
		assert.Len(t, matches, 4) // video7, video9, video10, video11
	})

	t.Run("no_match", func(t *testing.T) {
		re := regexp.MustCompile("NoSuchDevice")
		matches := matchDevices(re, devices)
		assert.Empty(t, matches)
	})

	t.Run("empty_devices", func(t *testing.T) {
		re := regexp.MustCompile("DJI")
		matches := matchDevices(re, nil)
		assert.Empty(t, matches)
	})

	t.Run("case_sensitive", func(t *testing.T) {
		re := regexp.MustCompile("dji")
		matches := matchDevices(re, devices)
		assert.Empty(t, matches)
	})

	t.Run("case_insensitive_regex", func(t *testing.T) {
		re := regexp.MustCompile("(?i)dji")
		matches := matchDevices(re, devices)
		require.Len(t, matches, 1)
		assert.Equal(t, "/dev/video12", matches[0].Path)
	})
}

func TestIsV4L2Format(t *testing.T) {
	assert.True(t, IsV4L2Format("v4l2"))
	assert.True(t, IsV4L2Format("video4linux2"))
	assert.False(t, IsV4L2Format(""))
	assert.False(t, IsV4L2Format("android_microphone"))
	assert.False(t, IsV4L2Format("rtsp"))
}

func TestListVideoDevices(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("V4L2 sysfs only available on Linux")
	}

	if _, err := os.Stat(sysfsV4L2Path); os.IsNotExist(err) {
		t.Skip("no /sys/class/video4linux on this system")
	}

	ctx := context.Background()
	devices, err := listVideoDevices(ctx)
	require.NoError(t, err)

	for _, dev := range devices {
		assert.NotEmpty(t, dev.Path, "device path should not be empty")
		assert.NotEmpty(t, dev.Name, "device name should not be empty")
		t.Logf("found V4L2 device: %s → %s", dev.Path, dev.Name)
	}
}

func TestResolveDeviceByName_InvalidPattern(t *testing.T) {
	ctx := context.Background()
	_, err := ResolveDeviceByName(ctx, "[invalid", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid device name pattern")
}
