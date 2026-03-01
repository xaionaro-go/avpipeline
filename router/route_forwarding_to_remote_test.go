package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouteForwardingToRemote_Close_EmptyStruct(t *testing.T) {
	ctx := context.Background()

	fwd := &RouteForwardingToRemote[any]{
		CancelFunc: func() {},
	}

	// Close with nil StreamForwarder and nil Output should not error.
	err := fwd.Close(ctx)
	assert.NoError(t, err)

	// Calling Close a second time should be a no-op due to CloseOnce.
	err = fwd.Close(ctx)
	assert.NoError(t, err)
}
