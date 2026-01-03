// option.go defines functional options for configuring processors.

package processor

type config struct {
	InputQueue  uint
	OutputQueue uint
	ErrorQueue  uint
}

type Option interface {
	apply(*config)
}

type Options []Option

func (s Options) apply(cfg *config) {
	for _, opt := range s {
		opt.apply(cfg)
	}
}

func (s Options) config() config {
	cfg := config{}
	s.apply(&cfg)
	return cfg
}

type OptionQueueSizeInput uint

func (opt OptionQueueSizeInput) apply(cfg *config) {
	cfg.InputQueue = uint(opt)
}

type OptionQueueSizeOutput uint

func (opt OptionQueueSizeOutput) apply(cfg *config) {
	cfg.OutputQueue = uint(opt)
}

type OptionQueueSizeError uint

func (opt OptionQueueSizeError) apply(cfg *config) {
	cfg.ErrorQueue = uint(opt)
}
