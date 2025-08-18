package router

type nodeKernelConfig struct {
	PTSInitFunc string
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
	cfg := nodeKernelConfig{
		PTSInitFunc: DefaultPTSInitFunc,
	}
	opts.apply(&cfg)
	return cfg
}

type NodeKernelOptionPTSInitFunc string

func (o NodeKernelOptionPTSInitFunc) apply(cfg *nodeKernelConfig) {
	cfg.PTSInitFunc = string(o)
}
