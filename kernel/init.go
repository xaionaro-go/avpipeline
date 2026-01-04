// init.go performs package-level initialization, such as registering FFmpeg devices.

package kernel

import (
	"github.com/asticode/go-astiav"
)

func init() {
	astiav.RegisterAllDevices()
}
