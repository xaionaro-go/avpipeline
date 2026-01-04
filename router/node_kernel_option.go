// node_kernel_option.go defines functional options for configuring NodeKernel.

package router

type nodeKernelConfig struct {
	ShouldFixPTS bool
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

type NodeKernelOptionShouldFixPTS bool

func (o NodeKernelOptionShouldFixPTS) apply(cfg *nodeKernelConfig) {
	cfg.ShouldFixPTS = bool(o)
}
