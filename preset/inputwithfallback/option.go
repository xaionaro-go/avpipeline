package inputwithfallback

import (
	"time"
)

type Option interface {
	apply(*Config)
}

type Config struct {
	RetryInterval              time.Duration
	FailuresBeforeNextFallback uint
}

type Options []Option

func (o Options) apply(cfg *Config) {
	for _, opt := range o {
		opt.apply(cfg)
	}
}

func (o Options) Config() Config {
	var cfg Config
	if o != nil {
		o.apply(&cfg)
	}
	return cfg
}

type OptionRetryInterval time.Duration

func (o OptionRetryInterval) apply(cfg *Config) {
	cfg.RetryInterval = time.Duration(o)
}

type OptionFailuresBeforeNextFallback uint

func (o OptionFailuresBeforeNextFallback) apply(cfg *Config) {
	cfg.FailuresBeforeNextFallback = uint(o)
}
