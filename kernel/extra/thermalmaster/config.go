package thermalmaster

import (
	"github.com/xaionaro-go/thermalmaster/pkg/colormap"
	"github.com/xaionaro-go/thermalmaster/pkg/thermalmaster"
)

// Config holds the kernel's dynamic configuration.
type Config struct {
	Model     thermalmaster.Model
	Sensor    thermalmaster.SensorSource
	GainMode  thermalmaster.GainMode
	EnvParams thermalmaster.EnvParams
	Upscale   *thermalmaster.UpscaleConfig // Upscale parameters for SensorBlended (nil uses defaults).
	Colormap  colormap.Colormap            // nil = keep raw pixel format; non-nil = colorize to RGB24.
	Legend    *thermalmaster.LegendConfig  // nil or !Enabled = no legend overlay.
}

// DefaultConfig returns a default configuration.
func DefaultConfig() Config {
	return Config{
		Model:     thermalmaster.ModelP3,
		Sensor:    thermalmaster.SensorThermal,
		GainMode:  thermalmaster.GainHigh,
		EnvParams: thermalmaster.DefaultEnvParams(),
	}
}
