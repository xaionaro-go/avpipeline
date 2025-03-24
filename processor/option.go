package processor

type config struct {
	InputPacketQueue  uint
	OutputPacketQueue uint
	InputFrameQueue   uint
	OutputFrameQueue  uint
	ErrorQueue        uint
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

type OptionQueueSizeInputPacket uint

func (opt OptionQueueSizeInputPacket) apply(cfg *config) {
	cfg.InputPacketQueue = uint(opt)
}

type OptionQueueSizeOutputPacket uint

func (opt OptionQueueSizeOutputPacket) apply(cfg *config) {
	cfg.OutputPacketQueue = uint(opt)
}

type OptionQueueSizeInputFrame uint

func (opt OptionQueueSizeInputFrame) apply(cfg *config) {
	cfg.InputFrameQueue = uint(opt)
}

type OptionQueueSizeOutputFrame uint

func (opt OptionQueueSizeOutputFrame) apply(cfg *config) {
	cfg.OutputFrameQueue = uint(opt)
}

type OptionQueueSizeError uint

func (opt OptionQueueSizeError) apply(cfg *config) {
	cfg.ErrorQueue = uint(opt)
}
