package types

import (
	"fmt"
)

type Resolution struct {
	Width  uint32 `yaml:"width"`
	Height uint32 `yaml:"height"`
}

func (r Resolution) String() string {
	return fmt.Sprintf("%dx%d", r.Width, r.Height)
}

func (r *Resolution) Parse(s string) error {
	_, err := fmt.Sscanf(s, "%dx%d", &r.Width, &r.Height)
	if err != nil {
		return fmt.Errorf("unable to parse resolution '%s': %w", s, err)
	}
	return nil
}
