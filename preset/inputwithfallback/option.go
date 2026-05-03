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
	// QuietOnOpenFailure demotes by-design open-failure log spam to
	// Debug. Two patterns are gated:
	//   - input_chain.go: "input N error: <NewInput failure>" — covers
	//     both empty priority slots (HasResources=false) and configured-
	//     but-not-yet-publishing upstreams (e.g. rtmp connection refused).
	//   - input_with_fallback.go: "onInputChainError: unable to switch to
	//     fallback N: another switch is in progress (procN: M)" — the
	//     fallback walk races itself across consecutive empty slots.
	// Default (false) preserves the legacy ERRO/WARN so existing
	// diagnostics are not lost. Set true at the operator's discretion
	// (e.g. via the ffstream -quiet_on_open_failure CLI flag, also
	// available as the legacy alias -quiet_empty_priority) to suppress
	// the steady-state noise during normal startup before all priorities
	// have been provisioned.
	QuietOnOpenFailure bool
	// ResetDownstreamKernelsTimeout caps how long each per-processor
	// Reset call inside resetDownstreamKernels may block before being
	// abandoned. On expiry the affected Reset is skipped (with a Warn
	// log) and the loop continues with the next downstream processor.
	//
	// Defense-in-depth against a hardware-codec-level silent-consume
	// stall: if a downstream Decoder is wedged inside
	// avcodec_send_packet on a MediaCodec instance, its codec.Decoder
	// per-stream lock is held indefinitely and Decoder.ResetHard's
	// xsync.DoA2R1 never unblocks. Without a timeout, the OnKernelOpen
	// callback would wait forever — wedging the upstream Retryable's
	// reopen path and (transitively, prior to the structural lock-order
	// fix that releases KernelLocker around OnKernelOpen) the entire
	// chain. With the timeout, stale decoder state is preferred to an
	// indefinite pipeline wedge.
	//
	// Default (zero) means 10s.
	ResetDownstreamKernelsTimeout time.Duration
}

type Options []Option

func (o Options) apply(cfg *Config) {
	for _, opt := range o {
		opt.apply(cfg)
	}
}

func (o Options) Config() Config {
	cfg := Config{
		SwitchKeepUnlessTimeout:       time.Second,
		ResetDownstreamKernelsTimeout: defaultResetDownstreamKernelsTimeout,
	}
	if o != nil {
		o.apply(&cfg)
	}
	return cfg
}

// defaultResetDownstreamKernelsTimeout matches the field doc on
// Config.ResetDownstreamKernelsTimeout. Centralized here so tests and
// callers that bypass Options{}.Config() (e.g. the test helpers that
// build Config directly) can reference the same value.
const defaultResetDownstreamKernelsTimeout = 10 * time.Second

type OptionRetryInterval time.Duration

func (o OptionRetryInterval) apply(cfg *Config) {
	cfg.RetryInterval = time.Duration(o)
}

type OptionSwitchKeepUnlessTimeout time.Duration

func (o OptionSwitchKeepUnlessTimeout) apply(cfg *Config) {
	cfg.SwitchKeepUnlessTimeout = time.Duration(o)
}

type OptionQuietOnOpenFailure bool

func (o OptionQuietOnOpenFailure) apply(cfg *Config) {
	cfg.QuietOnOpenFailure = bool(o)
}

// OptionResetDownstreamKernelsTimeout overrides the default per-Reset
// timeout used by resetDownstreamKernels. See
// Config.ResetDownstreamKernelsTimeout for the semantics. A zero or
// negative value falls back to the package default (10s).
type OptionResetDownstreamKernelsTimeout time.Duration

func (o OptionResetDownstreamKernelsTimeout) apply(cfg *Config) {
	cfg.ResetDownstreamKernelsTimeout = time.Duration(o)
}
