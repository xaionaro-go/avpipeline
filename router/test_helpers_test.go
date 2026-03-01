package router

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xaionaro-go/avpipeline/node"
)

// mockPublisher implements Publisher[any] for testing purposes.
type mockPublisher struct {
	name        string
	mode        PublishMode
	inputNode   node.Abstract
	outputRoute *Route[any]
	closeFn     func(context.Context) error
	mu          sync.Mutex
	closeCount  int
	closed      bool
}

func newMockPublisher(name string, mode PublishMode) *mockPublisher {
	return &mockPublisher{
		name: name,
		mode: mode,
	}
}

func (p *mockPublisher) String() string {
	return p.name
}

func (p *mockPublisher) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCount++
	p.closed = true
	if p.closeFn != nil {
		return p.closeFn(ctx)
	}
	return nil
}

func (p *mockPublisher) GetInputNode(ctx context.Context) node.Abstract {
	return p.inputNode
}

func (p *mockPublisher) GetOutputRoute(ctx context.Context) *Route[any] {
	return p.outputRoute
}

func (p *mockPublisher) GetPublishMode(ctx context.Context) PublishMode {
	return p.mode
}

func (p *mockPublisher) getCloseCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeCount
}

func (p *mockPublisher) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// Verify the mock implements Publisher[any].
var _ Publisher[any] = (*mockPublisher)(nil)

// waitForServing waits for the route's Serve goroutine to start.
// This must be called before cancelling the context to avoid a race
// where the Serve goroutine hasn't read Processor yet.
func waitForServing(t *testing.T, route *Route[any]) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !route.Node.IsServing() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Serve to start")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// waitForNotServing waits for the route's Serve goroutine to stop.
func waitForNotServing(t *testing.T, route *Route[any]) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for route.Node != nil && route.Node.IsServing() {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for Serve to stop")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// newTestRouter creates a Router[any] for testing with proper cleanup.
// The cleanup cancels all route contexts, waits for Serve goroutines
// to stop, removes routes, and then closes the router.
func newTestRouter(t *testing.T) *Router[any] {
	t.Helper()
	ctx := context.Background()
	r := New[any](ctx)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Step 1: Cancel all route contexts to trigger Serve goroutine exits.
		// This is done first to ensure all Serve goroutines begin their
		// shutdown sequence before we try to remove routes.
		var routes []*Route[any]
		r.Locker.Do(cleanupCtx, func() {
			for _, route := range r.RoutesByPath {
				routes = append(routes, route)
				route.CancelFunc()
			}
		})

		// Step 2: Wait for all Serve goroutines to stop. Once IsServing()
		// returns false, the Serve method has returned and no more sends
		// to ErrorChan will occur.
		for _, route := range routes {
			deadline := time.After(5 * time.Second)
			for route.Node != nil && route.Node.IsServing() {
				select {
				case <-deadline:
					t.Logf("warning: route %s Serve goroutine did not stop in time", route.Path)
					goto nextRoute
				case <-time.After(10 * time.Millisecond):
				}
			}
		nextRoute:
		}

		// Step 3: Allow deferred closures in Serve goroutines to complete.
		// After Serve() returns, the newRoute goroutine runs defer r.Close(ctx)
		// which triggers onRouteClosed → RemoveRoute → WaitGroup.Done().
		time.Sleep(200 * time.Millisecond)

		// Step 4: Remove any routes still in the map (some may have already
		// been removed by their own lifecycle via onRouteClosed).
		r.Locker.Do(cleanupCtx, func() {
			for _, route := range r.RoutesByPath {
				routes = append(routes, route)
			}
		})
		for _, route := range routes {
			r.RemoveRoute(cleanupCtx, route)
		}

		// Step 5: Close the router. By this point, all Serve goroutines
		// should have finished and WaitGroup should be at 0.
		r.Close(cleanupCtx)
	})
	return r
}

// newTestRouteViaRouter creates a named route through the router's GetRoute
// method, returning both the router and the route.
func newTestRouteViaRouter(t *testing.T, path string) (*Router[any], *Route[any]) {
	t.Helper()
	r := newTestRouter(t)
	ctx := context.Background()
	route, err := r.GetRoute(ctx, RoutePath(path), GetRouteModeCreateTemporary)
	if err != nil {
		t.Fatalf("failed to create route %q: %v", path, err)
	}
	if route == nil {
		t.Fatalf("route %q is nil", path)
	}
	return r, route
}
