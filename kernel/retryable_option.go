// retryable_option.go defines options for configuring Retryable kernels.

package kernel

import (
	"context"
)

type (
	RetryableFuncOnInit[K Abstract]          func(context.Context, *Retryable[K])
	RetryableFuncOnPreKernelOpen[K Abstract] func(context.Context, *Retryable[K]) error
	RetryableFuncOnKernelOpen[K Abstract]    func(context.Context, K) error
	RetryableFuncOnError[K Abstract]         func(context.Context, K, error) error
)

type RetryableConfig[K Abstract] struct {
	OnInit          RetryableFuncOnInit[K]
	OnPreKernelOpen RetryableFuncOnPreKernelOpen[K]
	OnKernelOpen    RetryableFuncOnKernelOpen[K]
	StartOnInit     bool
}

type RetryableOption[K Abstract] interface {
	apply(*RetryableConfig[K])
}

type RetryableOptions[K Abstract] []RetryableOption[K]

func (r RetryableOptions[K]) apply(config *RetryableConfig[K]) {
	for _, opt := range r {
		opt.apply(config)
	}
}

func (r RetryableOptions[K]) Config() RetryableConfig[K] {
	config := RetryableConfig[K]{
		StartOnInit: true,
	}
	r.apply(&config)
	return config
}

type RetryableOptionOnInit[K Abstract] RetryableFuncOnInit[K]

func (r RetryableOptionOnInit[K]) apply(config *RetryableConfig[K]) {
	config.OnInit = (RetryableFuncOnInit[K])(r)
}

var _ = RetryableOptionOnInit[Abstract](nil).apply // anti-warning

type RetryableOptionOnPreKernelOpen[K Abstract] RetryableFuncOnPreKernelOpen[K]

func (r RetryableOptionOnPreKernelOpen[K]) apply(config *RetryableConfig[K]) {
	config.OnPreKernelOpen = (RetryableFuncOnPreKernelOpen[K])(r)
}

var _ = RetryableOptionOnPreKernelOpen[Abstract](nil).apply // anti-warning

type RetryableOptionOnKernelOpen[K Abstract] RetryableFuncOnKernelOpen[K]

func (r RetryableOptionOnKernelOpen[K]) apply(config *RetryableConfig[K]) {
	config.OnKernelOpen = (RetryableFuncOnKernelOpen[K])(r)
}

var _ = RetryableOptionOnKernelOpen[Abstract](nil).apply // anti-warning

type RetryableOptionStartOnInit[K Abstract] bool

func (r RetryableOptionStartOnInit[K]) apply(config *RetryableConfig[K]) {
	config.StartOnInit = bool(r)
}

var _ = RetryableOptionStartOnInit[Abstract](false).apply // anti-warning
