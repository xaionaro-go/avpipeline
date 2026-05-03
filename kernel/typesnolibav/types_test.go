package typesnolibav

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHookFunc_FireHook(t *testing.T) {
	called := false
	var receivedInput Abstract
	fn := HookFunc(func(ctx context.Context, input Abstract) error {
		called = true
		receivedInput = input
		return nil
	})

	err := fn.FireHook(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, called)
	assert.Nil(t, receivedInput)
}

func TestHookFunc_FireHook_ReturnsError(t *testing.T) {
	fn := HookFunc(func(ctx context.Context, input Abstract) error {
		return ErrNotImplemented{}
	})

	err := fn.FireHook(context.Background(), nil)
	assert.Error(t, err)
	assert.Equal(t, "not implemented", err.Error())
}

func TestInputConfig_Defaults(t *testing.T) {
	cfg := InputConfig{}
	assert.Nil(t, cfg.CustomOptions)
	assert.Equal(t, uint(0), cfg.RecvBufferSize)
	assert.False(t, cfg.AsyncOpen)
	assert.False(t, cfg.AutoClose)
	assert.False(t, cfg.QuietOnOpenFailure)
	assert.Nil(t, cfg.ForceRealTime)
	assert.Nil(t, cfg.ForceStartPTS)
	assert.Nil(t, cfg.ForceStartDTS)
	assert.Nil(t, cfg.DisplayRotation)
	assert.Nil(t, cfg.AutoRotate)
	assert.False(t, cfg.IgnoreIncorrectDTS)
	assert.False(t, cfg.IgnoreZeroDuration)
	assert.Nil(t, cfg.OnPostOpen)
	assert.Nil(t, cfg.OnPreClose)
}

func TestInputConfig_WithHooks(t *testing.T) {
	openCalled := false
	closeCalled := false

	cfg := InputConfig{
		OnPostOpen: HookFunc(func(ctx context.Context, input Abstract) error {
			openCalled = true
			return nil
		}),
		OnPreClose: HookFunc(func(ctx context.Context, input Abstract) error {
			closeCalled = true
			return nil
		}),
	}

	err := cfg.OnPostOpen.FireHook(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, openCalled)

	err = cfg.OnPreClose.FireHook(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, closeCalled)
}

func TestInputConfig_OptionalFields(t *testing.T) {
	realTime := true
	startPTS := int64(12345)
	startDTS := int64(67890)
	rotation := 90.0
	autoRotate := false

	cfg := InputConfig{
		ForceRealTime:  &realTime,
		ForceStartPTS:  &startPTS,
		ForceStartDTS:  &startDTS,
		DisplayRotation: &rotation,
		AutoRotate:      &autoRotate,
		RecvBufferSize:  65536,
		AsyncOpen:       true,
		AutoClose:       true,
		IgnoreIncorrectDTS: true,
		IgnoreZeroDuration: true,
	}

	assert.Equal(t, true, *cfg.ForceRealTime)
	assert.Equal(t, int64(12345), *cfg.ForceStartPTS)
	assert.Equal(t, int64(67890), *cfg.ForceStartDTS)
	assert.Equal(t, 90.0, *cfg.DisplayRotation)
	assert.Equal(t, false, *cfg.AutoRotate)
	assert.Equal(t, uint(65536), cfg.RecvBufferSize)
	assert.True(t, cfg.AsyncOpen)
	assert.True(t, cfg.AutoClose)
	assert.True(t, cfg.IgnoreIncorrectDTS)
	assert.True(t, cfg.IgnoreZeroDuration)
}

