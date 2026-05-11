package inputwithfallback

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/node"
)

func TestInputWithFallback_AddFactoryRequestContextCancelDoesNotCloseChain(t *testing.T) {
	serveCtx, serveCancel := context.WithCancel(context.Background())
	defer serveCancel()

	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](
		context.Background(),
		nil,
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = iwf.Close(context.Background()) })

	errCh := make(chan node.Error, 16)
	go iwf.Serve(serveCtx, node.ServeConfig{}, errCh)

	addCtx, cancelAdd := context.WithCancel(context.Background())
	require.NoError(t, iwf.AddFactory(addCtx, &mockInputFactory{name: "hot-front-camera"}))
	cancelAdd()

	require.Eventually(t, func() bool {
		return len(iwf.InputChains) == 1 && iwf.InputChains[0].IsKernelOpen(context.Background())
	}, 2*time.Second, 5*time.Millisecond, "newly-added chain should stay live and open after AddFactory context cancellation")

	deadline := time.After(time.Second)
	for {
		select {
		case nodeErr := <-errCh:
			switch {
			case errors.Is(nodeErr.Err, io.EOF), errors.Is(nodeErr.Err, context.Canceled):
				t.Fatalf("AddFactory request-context cancellation closed the newly-added chain: %v", nodeErr.Err)
			case nodeErr.Err != nil:
				t.Fatalf("AddFactory request-context cancellation produced a node error: %v", nodeErr.Err)
			}
		case <-deadline:
			return
		}
	}
}
