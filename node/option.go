// option.go defines configuration options for nodes.

package node

type Config struct {
	CacheHandler CachingHandler
}

func defaultConfig() Config {
	return Config{}
}

type Option interface {
	apply(*Config)
}
type Options []Option

func (opts Options) apply(cfg *Config) {
	for _, opt := range opts {
		opt.apply(cfg)
	}
}

func (opts Options) config() Config {
	cfg := defaultConfig()
	opts.apply(&cfg)
	return cfg
}

type OptionCacheHandlerValue struct {
	CachingHandler
}

func (o OptionCacheHandlerValue) apply(cfg *Config) {
	cfg.CacheHandler = o.CachingHandler
}

func OptionCacheHandler(cachingHandler CachingHandler) OptionCacheHandlerValue {
	return OptionCacheHandlerValue{cachingHandler}
}
