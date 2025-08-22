package router

type nodeKernelConfig struct {
}

type NodeKernelOption interface {
	apply(*nodeKernelConfig)
}

type NodeKernelOptions []NodeKernelOption

func (opts NodeKernelOptions) apply(cfg *nodeKernelConfig) {
	for _, opt := range opts {
		opt.apply(cfg)
	}
}

func (opts NodeKernelOptions) config() nodeKernelConfig {
	cfg := nodeKernelConfig{}
	opts.apply(&cfg)
	return cfg
}
