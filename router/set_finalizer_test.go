package router

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetFinalizer_NoError(t *testing.T) {
	ctx := context.Background()
	var called atomic.Bool

	obj := new(int)
	*obj = 42

	// setFinalizer should not panic.
	setFinalizer(ctx, obj, func(o *int) {
		called.Store(true)
	})

	// We cannot easily verify the finalizer runs (requires GC),
	// but we verify no panic on calling setFinalizer.
	assert.NotNil(t, obj)
}
