package types

import (
	"fmt"
)

type Resolution struct {
	Width  uint32 `yaml:"width"`
	Height uint32 `yaml:"height"`
}

func (r *Resolution) String() string {
	if r == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%dx%d", r.Width, r.Height)
}
