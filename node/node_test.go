package node

import (
	"context"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/processor"
)

func newDummyNode() *Dummy {
	return New[*processor.Dummy](processor.NewDummy())
}

func TestNew_InitialState(t *testing.T) {
	n := newDummyNode()
	require.NotNil(t, n)

	tassert.False(t, n.IsServing(), "new node should not be serving")
	tassert.True(t, n.IsDrained(context.Background()), "new node should be drained (no data in flight)")
	tassert.Empty(t, n.GetPushTos(context.Background()), "new node should have no push targets")
	tassert.NotNil(t, n.Counters, "counters should be initialized")
	tassert.NotNil(t, n.ChangeChanIsServing, "ChangeChanIsServing should be initialized")
	tassert.NotNil(t, n.ChangeChanPushTo, "ChangeChanPushTo should be initialized")
	tassert.NotNil(t, n.ChangeChanDrained, "ChangeChanDrained should be initialized")
}

func TestNew_ProcessorIsSet(t *testing.T) {
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)

	got := n.GetProcessor()
	tassert.Equal(t, proc, got)
}

func TestNilReceiver_IsServing(t *testing.T) {
	var n *Dummy
	tassert.False(t, n.IsServing(), "nil node should not be serving")
}

func TestNilReceiver_GetProcessor(t *testing.T) {
	var n *Dummy
	tassert.Nil(t, n.GetProcessor(), "nil node should return nil processor")
}

func TestNilReceiver_GetPushTos(t *testing.T) {
	var n *Dummy
	tassert.Nil(t, n.GetPushTos(context.Background()), "nil node should return nil PushTos")
}

func TestNilReceiver_WithPushTos(t *testing.T) {
	var n *Dummy
	// Should not panic
	called := false
	n.WithPushTos(context.Background(), func(ctx context.Context, pts *PushTos) {
		called = true
	})
	tassert.False(t, called, "callback should not be called on nil node")
}

func TestNilReceiver_GetInputFilter(t *testing.T) {
	var n *Dummy
	f := n.GetInputFilter(context.Background())
	// Should return a Static(false) condition
	tassert.NotNil(t, f, "nil node should return non-nil input filter")
	tassert.False(t, f.Match(context.Background(), packetorframefiltercondition.Input{}), "static false filter should not match")
}

func TestString_BasicProcessor(t *testing.T) {
	n := newDummyNode()
	s := n.String()
	tassert.Equal(t, "Dummy", s)
}

func TestString_CustomDataWithStringer(t *testing.T) {
	proc := processor.NewDummy()
	n := NewWithCustomData[stringerCustomData](proc)
	n.CustomData = stringerCustomData{name: "mydata"}
	s := n.String()
	tassert.Equal(t, "Dummy:mydata", s)
}

func TestString_CustomDataWithoutStringer(t *testing.T) {
	proc := processor.NewDummy()
	n := NewWithCustomData[int](proc)
	n.CustomData = 42
	s := n.String()
	tassert.Equal(t, "Dummy", s, "non-Stringer custom data should not appear in string")
}

func TestGetCustomData(t *testing.T) {
	proc := processor.NewDummy()
	n := NewWithCustomData[string](proc)
	n.CustomData = "hello"
	tassert.Equal(t, "hello", n.GetCustomData())
}

func TestSetCustomData(t *testing.T) {
	proc := processor.NewDummy()
	n := NewWithCustomData[int](proc)
	n.SetCustomData(42)
	tassert.Equal(t, 42, n.GetCustomData())
}

func TestSetCustomData_Overwrite(t *testing.T) {
	proc := processor.NewDummy()
	n := NewWithCustomData[string](proc)
	n.SetCustomData("first")
	tassert.Equal(t, "first", n.GetCustomData())
	n.SetCustomData("second")
	tassert.Equal(t, "second", n.GetCustomData())
}

func TestGetProcessor_ReturnsCorrectType(t *testing.T) {
	proc := processor.NewDummy()
	n := New[*processor.Dummy](proc)
	got := n.GetProcessor()
	tassert.IsType(t, (*processor.Dummy)(nil), got)
	tassert.Same(t, proc, got)
}

func TestGetCountersPtr(t *testing.T) {
	n := newDummyNode()
	c := n.GetCountersPtr()
	require.NotNil(t, c)
	// Should return the same pointer on repeated calls
	c2 := n.GetCountersPtr()
	tassert.Same(t, c, c2)
}

func TestGetObjectID(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	// Each node should have a unique object ID
	id1 := n1.GetObjectID()
	id2 := n2.GetObjectID()
	tassert.NotEqual(t, id1, id2, "different nodes should have different object IDs")
}

func TestGetChangeChanIsServing(t *testing.T) {
	n := newDummyNode()
	ch := n.GetChangeChanIsServing()
	require.NotNil(t, ch)

	// Channel should be open (not closed)
	select {
	case <-ch:
		t.Fatal("channel should not be closed initially")
	default:
		// expected
	}
}

func TestGetChangeChanPushTo(t *testing.T) {
	n := newDummyNode()
	ch := n.GetChangeChanPushTo()
	require.NotNil(t, ch)

	// Channel should be open (not closed)
	select {
	case <-ch:
		t.Fatal("channel should not be closed initially")
	default:
		// expected
	}
}

func TestGetChangeChanDrained(t *testing.T) {
	n := newDummyNode()
	ch := n.GetChangeChanDrained()
	require.NotNil(t, ch)
}

func TestSetInputFilter(t *testing.T) {
	n := newDummyNode()
	ctx := context.Background()

	// Initially nil
	f := n.GetInputFilter(ctx)
	tassert.Nil(t, f, "initial input filter should be nil")

	// Set a filter
	staticTrue := packetorframefiltercondition.Static(true)
	n.SetInputFilter(ctx, staticTrue)

	f = n.GetInputFilter(ctx)
	tassert.NotNil(t, f, "input filter should be set")
	tassert.True(t, f.Match(ctx, packetorframefiltercondition.Input{}), "static true filter should match")

	// Clear the filter
	n.SetInputFilter(ctx, nil)
	f = n.GetInputFilter(ctx)
	tassert.Nil(t, f, "input filter should be cleared")
}

func TestIsDrained_InitialState(t *testing.T) {
	n := newDummyNode()
	tassert.True(t, n.IsDrained(context.Background()), "new node should be drained")
}

func TestNodeWithCustomData_ImplementsAbstract(t *testing.T) {
	n := newDummyNode()
	var a Abstract = n
	tassert.NotNil(t, a)
}

func TestNewWithCustomData_InitialState(t *testing.T) {
	proc := processor.NewDummy()
	n := NewWithCustomData[string](proc)
	require.NotNil(t, n)

	tassert.False(t, n.IsServing())
	tassert.True(t, n.IsDrained(context.Background()))
	tassert.Empty(t, n.GetPushTos(context.Background()))
	tassert.Equal(t, "", n.GetCustomData(), "custom data should be zero value")
}

func TestNewWithCustomData_StructCustomData(t *testing.T) {
	type myData struct {
		X int
		Y string
	}
	proc := processor.NewDummy()
	n := NewWithCustomData[myData](proc)
	n.SetCustomData(myData{X: 10, Y: "hello"})
	got := n.GetCustomData()
	tassert.Equal(t, 10, got.X)
	tassert.Equal(t, "hello", got.Y)
}

func TestDotString_Node(t *testing.T) {
	n := newDummyNode()
	s := n.DotString(false)
	tassert.Contains(t, s, "digraph Pipeline")
	tassert.Contains(t, s, "Dummy")
}

// stringerCustomData implements fmt.Stringer for testing custom data display.
type stringerCustomData struct {
	name string
}

func (s stringerCustomData) String() string {
	return s.name
}
