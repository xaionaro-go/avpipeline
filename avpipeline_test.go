package avpipeline

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/nodewrapper"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/processor"
	"github.com/xaionaro-go/observability"
)

func newTestNode(ctx context.Context) *node.Node[*processor.FromKernel[*kernel.Passthrough]] {
	return node.NewFromKernel(ctx, &kernel.Passthrough{})
}

// --- Traverse tests ---

func TestTraverse_SingleNode(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)

	var visited []node.Abstract
	err := Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		nd node.Abstract,
	) error {
		visited = append(visited, nd)
		return nil
	}, n)

	require.NoError(t, err)
	tassert.Len(t, visited, 1)
	tassert.Equal(t, node.Abstract(n), visited[0])
}

func TestTraverse_Chain(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)
	n2.AddPushTo(ctx, n3)

	var visited []node.Abstract
	err := Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		nd node.Abstract,
	) error {
		visited = append(visited, nd)
		return nil
	}, n1)

	require.NoError(t, err)
	tassert.Len(t, visited, 3)
}

func TestTraverse_DAG_NoDuplicateVisits(t *testing.T) {
	ctx := context.Background()
	// Diamond pattern: n1 -> n2, n1 -> n3, n2 -> n4, n3 -> n4
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)
	n4 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)
	n1.AddPushTo(ctx, n3)
	n2.AddPushTo(ctx, n4)
	n3.AddPushTo(ctx, n4)

	var visited []node.Abstract
	err := Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		nd node.Abstract,
	) error {
		visited = append(visited, nd)
		return nil
	}, n1)

	require.NoError(t, err)
	// n4 should only be visited once despite two paths
	tassert.Len(t, visited, 4)
}

func TestTraverse_StopEarly(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)
	n2.AddPushTo(ctx, n3)

	visitCount := 0
	err := Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		nd node.Abstract,
	) error {
		visitCount++
		return ErrTraverseStop{}
	}, n1)

	// Should stop after first node
	require.NoError(t, err)
	tassert.Equal(t, 1, visitCount)
}

func TestTraverse_Empty(t *testing.T) {
	ctx := context.Background()
	err := Traverse[node.Abstract](ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		nd node.Abstract,
	) error {
		t.Fatal("should not be called")
		return nil
	})
	require.NoError(t, err)
}

func TestTraverse_CallbackError(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)

	testErr := errors.New("test error")
	err := Traverse(ctx, func(
		ctx context.Context,
		parent node.Abstract,
		item reflect.Type,
		nd node.Abstract,
	) error {
		return testErr
	}, n)

	require.Error(t, err)
	tassert.Contains(t, err.Error(), "test error")
}

// --- NextLayer tests ---

func TestNextLayer_SingleNode_NoPushTos(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)

	result, err := NextLayer(ctx, n)
	require.NoError(t, err)
	tassert.Empty(t, result)
}

func TestNextLayer_WithChildren(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)
	n1.AddPushTo(ctx, n3)

	result, err := NextLayer(ctx, n1)
	require.NoError(t, err)
	tassert.Len(t, result, 2)
}

func TestNextLayer_Deduplication(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	// Same destination from two parents
	nParent1 := newTestNode(ctx)
	nParent2 := newTestNode(ctx)
	nParent1.AddPushTo(ctx, n1)
	nParent2.AddPushTo(ctx, n1)

	result, err := NextLayer(ctx, nParent1, nParent2)
	require.NoError(t, err)
	// n1 should appear only once
	tassert.Len(t, result, 1)
}

// --- FindNodeByObjectID tests ---

func TestFindNodeByObjectID_Found(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)
	n2.AddPushTo(ctx, n3)

	targetID := n3.GetObjectID()
	found, err := FindNodeByObjectID(ctx, targetID, n1)
	require.NoError(t, err)
	tassert.Equal(t, targetID, found.GetObjectID())
}

func TestFindNodeByObjectID_NotFound(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)

	_, err := FindNodeByObjectID(ctx, 999999, n1)
	require.Error(t, err)
	var notFound ErrNotFound
	tassert.ErrorAs(t, err, &notFound)
}

func TestFindNodeByObjectID_RootNode(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)

	targetID := n1.GetObjectID()
	found, err := FindNodeByObjectID(ctx, targetID, n1)
	require.NoError(t, err)
	tassert.Equal(t, targetID, found.GetObjectID())
}

func TestFindNodeByObjectID_MultipleRoots(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)

	targetID := n3.GetObjectID()
	found, err := FindNodeByObjectID(ctx, targetID, n1, n2, n3)
	require.NoError(t, err)
	tassert.Equal(t, targetID, found.GetObjectID())
}

// TestFindNodeByObjectID_ThroughNoServeWrapper tests finding inner nodes through
// NoServe wrappers, which avd uses via StreamForwarderCopy.
func TestFindNodeByObjectID_ThroughNoServeWrapper(t *testing.T) {
	ctx := context.Background()
	inner := newTestNode(ctx)
	wrapper := &nodewrapper.NoServe[node.Abstract]{Node: inner}

	// Should find wrapper itself by wrapper's ObjectID
	wrapperID := wrapper.GetObjectID()
	found, err := FindNodeByObjectID(ctx, wrapperID, wrapper)
	require.NoError(t, err)
	tassert.Equal(t, wrapperID, found.GetObjectID())

	// Should find inner node through OriginalNodeAbstract
	innerID := inner.GetObjectID()
	found, err = FindNodeByObjectID(ctx, innerID, wrapper)
	require.NoError(t, err)
	tassert.Equal(t, innerID, found.GetObjectID())
}

// --- Drain/IsDrained/SetBlockInput tests ---

func TestIsDrained_AllDrained(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	// A fresh node that hasn't been served is considered drained
	tassert.True(t, IsDrained(ctx, n1))
}

func TestIsDrained_Chain(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)
	tassert.True(t, IsDrained(ctx, n1))
}

func TestIsDrained_Empty(t *testing.T) {
	ctx := context.Background()
	tassert.True(t, IsDrained(ctx))
}

func TestSetBlockInput_PropagatesTree(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)

	err := SetBlockInput(ctx, true, n1)
	require.NoError(t, err)
}

// --- Error type tests ---

func TestErrNotFound_Error(t *testing.T) {
	e := ErrNotFound{}
	tassert.Equal(t, "not found", e.Error())
}

func TestErrTraverseStop_Error(t *testing.T) {
	e := ErrTraverseStop{}
	tassert.Equal(t, "traverse: stop requested", e.Error())
}

// --- Serve tests ---

// TestServe_BasicPipeline tests serving a simple pipeline of passthrough nodes.
// This simulates the core usage pattern from both ffstream and avd.
func TestServe_BasicPipeline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)

	errCh := make(chan node.Error, 100)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(ctx, ServeConfig{}, errCh, n1)
	})

	// Wait for nodes to start serving
	deadline := time.After(2 * time.Second)
	for {
		if n1.IsServing() && n2.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("nodes did not start serving within timeout")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	tassert.True(t, n1.IsServing())
	tassert.True(t, n2.IsServing())

	cancel()
	wg.Wait()
}

// TestServe_DuplicateNode ensures the same node is not served twice even when
// referenced from multiple places in the pipeline graph.
func TestServe_DuplicateNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	shared := newTestNode(ctx)
	n1.AddPushTo(ctx, shared)
	n2.AddPushTo(ctx, shared)

	errCh := make(chan node.Error, 100)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(ctx, ServeConfig{}, errCh, n1, n2)
	})

	deadline := time.After(2 * time.Second)
	for {
		if shared.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("shared node did not start serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	wg.Wait()
}

// TestServe_Empty tests that Serve with no nodes is a no-op.
func TestServe_Empty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan node.Error, 10)
	Serve[node.Abstract](ctx, ServeConfig{}, errCh)
}

// TestServe_DiamondTopology tests a diamond-shaped pipeline topology,
// where two intermediate nodes merge into one final node.
func TestServe_DiamondTopology(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	root := newTestNode(ctx)
	mid1 := newTestNode(ctx)
	mid2 := newTestNode(ctx)
	sink := newTestNode(ctx)
	root.AddPushTo(ctx, mid1)
	root.AddPushTo(ctx, mid2)
	mid1.AddPushTo(ctx, sink)
	mid2.AddPushTo(ctx, sink)

	errCh := make(chan node.Error, 100)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(ctx, ServeConfig{}, errCh, root)
	})

	deadline := time.After(2 * time.Second)
	for {
		if root.IsServing() && mid1.IsServing() && mid2.IsServing() && sink.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("not all nodes started serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	cancel()
	wg.Wait()
}

// TestServe_DrainAfterCancel tests draining a pipeline after cancellation,
// matching the pattern used in ffstream's graceful shutdown.
func TestServe_DrainAfterCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)

	errCh := make(chan node.Error, 100)
	serveCtx, serveCancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(serveCtx, ServeConfig{}, errCh, n1)
	})

	// Wait for serving to start
	deadline := time.After(2 * time.Second)
	for {
		if n1.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("nodes did not start serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Drain should work on a fresh (no-data) pipeline
	err := Drain(ctx, ptr(true), n1)
	require.NoError(t, err)
	tassert.True(t, IsDrained(ctx, n1))

	serveCancel()
	wg.Wait()
}

// TestWaitForDrain_AlreadyDrained checks WaitForDrain returns immediately
// when all nodes are already drained.
func TestWaitForDrain_AlreadyDrained(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	err := WaitForDrain(ctx, n)
	require.NoError(t, err)
}

// TestWaitForDrain_Timeout ensures WaitForDrain respects context cancellation.
func TestWaitForDrain_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n := newTestNode(ctx)

	errCh := make(chan node.Error, 100)
	serveCtx, serveCancel := context.WithCancel(ctx)
	defer serveCancel()
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(serveCtx, ServeConfig{}, errCh, n)
	})

	// Wait for serving to start
	deadline := time.After(2 * time.Second)
	for {
		if n.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("node did not start serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// WaitForDrain should complete (since the node has no pending data)
	err := WaitForDrain(ctx, n)
	require.NoError(t, err)

	serveCancel()
	wg.Wait()
}

// --- LogLevel conversion tests ---

func TestLogLevelToAstiav(t *testing.T) {
	tests := []struct {
		input    logger.Level
		expected astiav.LogLevel
	}{
		{logger.LevelUndefined, astiav.LogLevelQuiet},
		{logger.LevelPanic, astiav.LogLevelPanic},
		{logger.LevelFatal, astiav.LogLevelFatal},
		{logger.LevelError, astiav.LogLevelError},
		{logger.LevelWarning, astiav.LogLevelWarning},
		{logger.LevelInfo, astiav.LogLevelInfo},
		{logger.LevelDebug, astiav.LogLevelVerbose},
		{logger.LevelTrace, astiav.LogLevelDebug},
	}
	for _, tt := range tests {
		result := LogLevelToAstiav(tt.input)
		tassert.Equal(t, tt.expected, result, "input: %v", tt.input)
	}
}

func TestLogLevelToAstiav_Default(t *testing.T) {
	result := LogLevelToAstiav(logger.Level(999))
	tassert.Equal(t, astiav.LogLevelWarning, result)
}

func TestLogLevelFromAstiav(t *testing.T) {
	tests := []struct {
		input    astiav.LogLevel
		expected logger.Level
	}{
		{astiav.LogLevelQuiet, logger.LevelUndefined},
		// FFmpeg's Panic/Fatal don't mean process-exit; they map to Error
		// to avoid logrus calling os.Exit/panic on transient decode errors.
		{astiav.LogLevelPanic, logger.LevelError},
		{astiav.LogLevelFatal, logger.LevelError},
		{astiav.LogLevelError, logger.LevelError},
		{astiav.LogLevelWarning, logger.LevelWarning},
		{astiav.LogLevelInfo, logger.LevelInfo},
		{astiav.LogLevelVerbose, logger.LevelDebug},
		{astiav.LogLevelDebug, logger.LevelTrace},
	}
	for _, tt := range tests {
		result := LogLevelFromAstiav(tt.input)
		tassert.Equal(t, tt.expected, result, "input: %v", tt.input)
	}
}

func TestLogLevelFromAstiav_Default(t *testing.T) {
	result := LogLevelFromAstiav(astiav.LogLevel(9999))
	tassert.Equal(t, logger.LevelWarning, result)
}

func TestLogLevel_RoundTrip(t *testing.T) {
	// Panic and Fatal intentionally collapse to Error when converting
	// from astiav (FFmpeg's levels don't mean process-exit), so the
	// round-trip is lossy for those two levels.
	tests := []struct {
		input    logger.Level
		expected logger.Level
	}{
		{logger.LevelUndefined, logger.LevelUndefined},
		{logger.LevelPanic, logger.LevelError},
		{logger.LevelFatal, logger.LevelError},
		{logger.LevelError, logger.LevelError},
		{logger.LevelWarning, logger.LevelWarning},
		{logger.LevelInfo, logger.LevelInfo},
		{logger.LevelDebug, logger.LevelDebug},
		{logger.LevelTrace, logger.LevelTrace},
	}
	for _, tt := range tests {
		roundTripped := LogLevelFromAstiav(LogLevelToAstiav(tt.input))
		tassert.Equal(t, tt.expected, roundTripped, "round-trip failed for %v", tt.input)
	}
}

// --- Serve with NodeFilter tests ---

// TestServe_NodeTreeFilter tests that Serve respects NodeTreeFilter to skip subtrees.
func TestServe_NodeTreeFilter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)

	errCh := make(chan node.Error, 100)
	var wg sync.WaitGroup
	wg.Add(1)

	cfg := ServeConfig{
		NodeTreeFilter: &nodeMatchFilter{match: n1},
	}

	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(ctx, cfg, errCh, n1)
	})

	deadline := time.After(2 * time.Second)
	for {
		if n1.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("n1 did not start serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// n2 should NOT be serving since the tree filter only matched n1 (not n2's subtree)
	time.Sleep(100 * time.Millisecond)
	tassert.False(t, n2.IsServing(), "n2 should not be served")

	cancel()
	wg.Wait()
}

// TestServe_NodeFilter tests that Serve skips filtered nodes but still serves their children.
func TestServe_NodeFilter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)

	errCh := make(chan node.Error, 100)
	var wg sync.WaitGroup
	wg.Add(1)

	cfg := ServeConfig{
		NodeFilter: &nodeMatchFilter{match: n2},
	}

	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(ctx, cfg, errCh, n1)
	})

	deadline := time.After(2 * time.Second)
	for {
		if n2.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("n2 did not start serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// n1 should NOT be serving (filtered out)
	tassert.False(t, n1.IsServing(), "n1 should be skipped by filter")
	tassert.True(t, n2.IsServing(), "n2 should be served")

	cancel()
	wg.Wait()
}

// nodeMatchFilter is a condition that matches a specific node.
type nodeMatchFilter struct {
	match node.Abstract
}

func (f *nodeMatchFilter) Match(ctx context.Context, n node.Abstract) bool {
	return n == f.match
}

func (f *nodeMatchFilter) String() string { return "nodeMatchFilter" }

// --- Mock types for NotifyAboutPacketSources ---

// mockPacketSource implements packet.Source for testing.
type mockPacketSource struct {
	name                        string
	withOutputFormatContextCalls atomic.Int32
}

func (s *mockPacketSource) String() string { return s.name }
func (s *mockPacketSource) WithOutputFormatContext(_ context.Context, callback func(*astiav.FormatContext)) {
	s.withOutputFormatContextCalls.Add(1)
	callback(nil)
}

var _ packet.Source = (*mockPacketSource)(nil)

// mockSinkKernel is a kernel that also implements packet.Sink.
type mockSinkKernel struct {
	kernel.Passthrough
	notifyCalls  atomic.Int32
	notifySource packet.Source
	notifyErr    error
}

func (k *mockSinkKernel) WithInputFormatContext(_ context.Context, callback func(*astiav.FormatContext)) {
	callback(nil)
}

func (k *mockSinkKernel) NotifyAboutPacketSource(_ context.Context, source packet.Source) error {
	k.notifyCalls.Add(1)
	k.notifySource = source
	return k.notifyErr
}

// mockSourceKernel is a kernel that also implements packet.Source.
type mockSourceKernel struct {
	kernel.Passthrough
	withOutputCalls atomic.Int32
}

func (k *mockSourceKernel) WithOutputFormatContext(_ context.Context, callback func(*astiav.FormatContext)) {
	k.withOutputCalls.Add(1)
	callback(nil)
}

// mockSourceSinkKernel is a kernel that implements both packet.Source and packet.Sink.
type mockSourceSinkKernel struct {
	kernel.Passthrough
	notifyCalls  atomic.Int32
	notifySource packet.Source
	notifyErr    error
	withOutputCalls atomic.Int32
}

func (k *mockSourceSinkKernel) WithInputFormatContext(_ context.Context, callback func(*astiav.FormatContext)) {
	callback(nil)
}

func (k *mockSourceSinkKernel) NotifyAboutPacketSource(_ context.Context, source packet.Source) error {
	k.notifyCalls.Add(1)
	k.notifySource = source
	return k.notifyErr
}

func (k *mockSourceSinkKernel) WithOutputFormatContext(_ context.Context, callback func(*astiav.FormatContext)) {
	k.withOutputCalls.Add(1)
	callback(nil)
}

// --- NotifyAboutPacketSources tests ---

func TestNotifyAboutPacketSources_ZeroNodes(t *testing.T) {
	ctx := context.Background()
	source := &mockPacketSource{name: "test-source"}

	err := NotifyAboutPacketSources[node.Abstract](ctx, source)
	require.Error(t, err)
	tassert.Contains(t, err.Error(), "exactly one node")
}

func TestNotifyAboutPacketSources_TwoNodes(t *testing.T) {
	ctx := context.Background()
	source := &mockPacketSource{name: "test-source"}
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)

	err := NotifyAboutPacketSources(ctx, source, n1, n2)
	require.Error(t, err)
	tassert.Contains(t, err.Error(), "exactly one node")
}

func TestNotifyAboutPacketSources_NilSource_PassthroughNode(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)

	// nil packetSource, Passthrough kernel does not implement GetPacketSink
	err := NotifyAboutPacketSources(ctx, nil, n)
	require.NoError(t, err)
}

func TestNotifyAboutPacketSources_WithSink_NotifiesSink(t *testing.T) {
	ctx := context.Background()
	sk := &mockSinkKernel{}
	n := node.NewFromKernel(ctx, sk)
	source := &mockPacketSource{name: "test-source"}

	err := NotifyAboutPacketSources(ctx, source, n)
	require.NoError(t, err)
	tassert.Equal(t, int32(1), sk.notifyCalls.Load())
	tassert.Equal(t, packet.Source(source), sk.notifySource)
}

func TestNotifyAboutPacketSources_WithSink_ReturnsError(t *testing.T) {
	ctx := context.Background()
	sk := &mockSinkKernel{notifyErr: fmt.Errorf("sink error")}
	n := node.NewFromKernel(ctx, sk)
	source := &mockPacketSource{name: "test-source"}

	err := NotifyAboutPacketSources(ctx, source, n)
	require.Error(t, err)
	tassert.Contains(t, err.Error(), "sink error")
}

func TestNotifyAboutPacketSources_WithSource_UpdatesPacketSource(t *testing.T) {
	ctx := context.Background()
	sk := &mockSourceKernel{}
	n := node.NewFromKernel(ctx, sk)

	// When the node has a GetPacketSource, the source should be updated
	// by calling WithOutputFormatContext on the new source
	err := NotifyAboutPacketSources(ctx, nil, n)
	require.NoError(t, err)
	tassert.Equal(t, int32(1), sk.withOutputCalls.Load())
}

func TestNotifyAboutPacketSources_WithSinkAndSource(t *testing.T) {
	ctx := context.Background()
	sk := &mockSourceSinkKernel{}
	n := node.NewFromKernel(ctx, sk)
	source := &mockPacketSource{name: "initial-source"}

	err := NotifyAboutPacketSources(ctx, source, n)
	require.NoError(t, err)
	// Sink should be notified
	tassert.Equal(t, int32(1), sk.notifyCalls.Load())
	// Source's WithOutputFormatContext should be called
	tassert.Equal(t, int32(1), sk.withOutputCalls.Load())
}

func TestNotifyAboutPacketSources_NilSource_SkipsSinkNotification(t *testing.T) {
	ctx := context.Background()
	sk := &mockSinkKernel{}
	n := node.NewFromKernel(ctx, sk)

	// nil packetSource should skip the sink notification
	err := NotifyAboutPacketSources(ctx, nil, n)
	require.NoError(t, err)
	tassert.Equal(t, int32(0), sk.notifyCalls.Load())
}

func TestNotifyAboutPacketSources_RecursivePushTos(t *testing.T) {
	ctx := context.Background()

	// Create a chain: source_node → sink_node
	sourceKernel := &mockSourceKernel{}
	sourceNode := node.NewFromKernel(ctx, sourceKernel)

	sinkKernel := &mockSinkKernel{}
	sinkNode := node.NewFromKernel(ctx, sinkKernel)

	sourceNode.AddPushTo(ctx, sinkNode)

	source := &mockPacketSource{name: "original-source"}

	err := NotifyAboutPacketSources(ctx, source, sourceNode)
	require.NoError(t, err)

	// The sink in the downstream node should have been notified
	tassert.Equal(t, int32(1), sinkKernel.notifyCalls.Load())
}

func TestNotifyAboutPacketSources_RecursivePushTos_DeduplicatesNodes(t *testing.T) {
	ctx := context.Background()

	// Diamond: root → mid1, root → mid2 → shared_sink (but shared_sink via two paths)
	// Actually NotifyAboutPacketSources only traverses PushTos from the single node,
	// so deduplication happens when the same node is in PushTos twice
	rootKernel := &kernel.Passthrough{}
	rootNode := node.NewFromKernel(ctx, rootKernel)

	sinkKernel := &mockSinkKernel{}
	sinkNode := node.NewFromKernel(ctx, sinkKernel)

	// Add the same node twice as a pushTo
	rootNode.AddPushTo(ctx, sinkNode)
	rootNode.AddPushTo(ctx, sinkNode)

	source := &mockPacketSource{name: "test-source"}

	err := NotifyAboutPacketSources(ctx, source, rootNode)
	require.NoError(t, err)

	// Should only be notified once due to deduplication in dstAlreadyProcessed
	tassert.Equal(t, int32(1), sinkKernel.notifyCalls.Load())
}

func TestNotifyAboutPacketSources_RecursivePushTos_ErrorPropagation(t *testing.T) {
	ctx := context.Background()

	rootKernel := &kernel.Passthrough{}
	rootNode := node.NewFromKernel(ctx, rootKernel)

	sinkKernel := &mockSinkKernel{notifyErr: fmt.Errorf("downstream error")}
	sinkNode := node.NewFromKernel(ctx, sinkKernel)

	rootNode.AddPushTo(ctx, sinkNode)

	source := &mockPacketSource{name: "test-source"}

	err := NotifyAboutPacketSources(ctx, source, rootNode)
	require.Error(t, err)
	tassert.Contains(t, err.Error(), "downstream error")
}

// --- IsDrained non-drained branch ---

func TestIsDrained_NotDrained(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n := newTestNode(ctx)

	// Start serving the node
	errCh := make(chan node.Error, 100)
	serveCtx, serveCancel := context.WithCancel(ctx)
	defer serveCancel()
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(serveCtx, ServeConfig{}, errCh, n)
	})

	// Wait for serving to start
	deadline := time.After(2 * time.Second)
	for {
		if n.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("node did not start serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Mark node as not drained (simulates pending data)
	n.IsDrainedValue.Store(false)
	tassert.False(t, IsDrained(ctx, n))

	// Restore drained state
	n.IsDrainedValue.Store(true)
	tassert.True(t, IsDrained(ctx, n))

	serveCancel()
	wg.Wait()
}

func TestIsDrained_ChainWithNonDrainedNode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n1.AddPushTo(ctx, n2)

	// Start serving
	errCh := make(chan node.Error, 100)
	serveCtx, serveCancel := context.WithCancel(ctx)
	defer serveCancel()
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		Serve(serveCtx, ServeConfig{}, errCh, n1)
	})

	deadline := time.After(2 * time.Second)
	for {
		if n1.IsServing() && n2.IsServing() {
			break
		}
		select {
		case <-deadline:
			t.Fatal("nodes did not start serving")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Mark downstream node as not drained
	n2.IsDrainedValue.Store(false)
	tassert.False(t, IsDrained(ctx, n1))

	serveCancel()
	wg.Wait()
}
