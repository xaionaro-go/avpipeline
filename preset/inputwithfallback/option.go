// option.go defines options for the InputWithFallback preset.

package inputwithfallback

import (
	"time"
)

type Option interface {
	apply(*Config)
}

type Config struct {
	RetryInterval           time.Duration
	SwitchKeepUnlessTimeout time.Duration
}

type Options []Option

func (o Options) apply(cfg *Config) {
	for _, opt := range o {
		opt.apply(cfg)
	}
}

func (o Options) Config() Config {
	cfg := Config{
		SwitchKeepUnlessTimeout: time.Second,
	}
	if o != nil {
		o.apply(&cfg)
	}
	return cfg
}

type OptionRetryInterval time.Duration

func (o OptionRetryInterval) apply(cfg *Config) {
	cfg.RetryInterval = time.Duration(o)
}

type OptionSwitchKeepUnlessTimeout time.Duration

func (o OptionSwitchKeepUnlessTimeout) apply(cfg *Config) {
	cfg.SwitchKeepUnlessTimeout = time.Duration(o)
}
