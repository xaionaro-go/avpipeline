package switchpair

import "context"

func (p *Pair) WithKeepUnlessDisabled(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	switchKeepUnless := p.switcher.GetKeepUnless()
	p.switcher.SetKeepUnless(nil)
	defer p.switcher.SetKeepUnless(switchKeepUnless)

	syncerKeepUnless := p.syncer.GetKeepUnless()
	p.syncer.SetKeepUnless(nil)
	defer p.syncer.SetKeepUnless(syncerKeepUnless)

	if fn == nil {
		return nil
	}
	return fn(ctx)
}
