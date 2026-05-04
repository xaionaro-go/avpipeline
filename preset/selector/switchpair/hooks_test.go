package switchpair

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/packetorframe"
)

func TestHooksAdaptersNoOpWhenCallbacksUnset(t *testing.T) {
	ctx := context.Background()
	hooks := Hooks{}
	in := packetorframe.InputUnion{}

	assert.NoError(t, hooks.onSwitchRequest(ctx, in, 7))
	assert.NotPanics(t, func() {
		hooks.onBeforeSwitch(ctx, in, 1, 2)
	})
	assert.NotPanics(t, func() {
		hooks.onInterruptedSwitch(ctx, in, 1, 2)
	})
	assert.NotPanics(t, func() {
		hooks.onAfterSwitch(ctx, in, 1, 2)
	})
}
