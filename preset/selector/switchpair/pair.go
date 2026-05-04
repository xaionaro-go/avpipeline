package switchpair

import (
	"context"

	barrierstategetter "github.com/xaionaro-go/avpipeline/kernel/barrier/stategetter"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Pair struct {
	switcher *barrierstategetter.Switch
	syncer   *barrierstategetter.Switch
}

func New(
	_ context.Context,
	cfg Config,
) (*Pair, error) {
	switcher := barrierstategetter.NewSwitch()
	switcher.CurrentValue.Store(int32(cfg.InitialValue))
	switcher.SetKeepUnless(cfg.SwitchKeepUnless)
	switcher.Flags = cfg.SwitchFlags
	if cfg.Hooks.OnSwitchRequest != nil {
		switcher.SetOnSwitchRequest(cfg.Hooks.onSwitchRequest)
	}
	if cfg.Hooks.OnBeforeSwitch != nil {
		switcher.SetOnBeforeSwitch(cfg.Hooks.onBeforeSwitch)
	}
	if cfg.Hooks.OnInterruptedSwitch != nil {
		switcher.SetOnInterruptedSwitch(cfg.Hooks.onInterruptedSwitch)
	}
	if cfg.Hooks.OnAfterSwitch != nil {
		switcher.SetOnAfterSwitch(cfg.Hooks.onAfterSwitch)
	}

	syncer := barrierstategetter.NewSwitch()
	syncer.CurrentValue.Store(int32(cfg.InitialValue))
	syncer.SetKeepUnless(cfg.SyncerKeepUnless)
	syncer.Flags = cfg.SyncerFlags

	return &Pair{
		switcher: switcher,
		syncer:   syncer,
	}, nil
}

func (p *Pair) Switch() *barrierstategetter.Switch {
	return p.switcher
}

func (p *Pair) Syncer() *barrierstategetter.Switch {
	return p.syncer
}

func (p *Pair) SetValue(
	ctx context.Context,
	to id.MemberID,
) error {
	return p.switcher.SetValue(ctx, int32(to))
}

func (p *Pair) Current(
	_ context.Context,
) id.MemberID {
	return id.MemberID(p.switcher.CurrentValue.Load())
}

func (p *Pair) SyncerCurrent(
	_ context.Context,
) id.MemberID {
	return id.MemberID(p.syncer.CurrentValue.Load())
}

func (p *Pair) Next(
	_ context.Context,
) id.MemberID {
	return id.MemberID(p.switcher.NextValue.Load())
}
