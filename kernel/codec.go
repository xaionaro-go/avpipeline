// codec.go defines types and options for codec-related operations in the kernel.

package kernel

type CodecResetOption interface {
	apply(*codecResetConfig)
}

type CodecResetOptions []CodecResetOption

func (o CodecResetOptions) apply(cfg *codecResetConfig) {
	for _, opt := range o {
		opt.apply(cfg)
	}
}

func (o CodecResetOptions) config() codecResetConfig {
	cfg := codecResetConfig{}
	o.apply(&cfg)
	return cfg
}

type codecResetConfig struct {
}

type SideFlagFlush struct{}
