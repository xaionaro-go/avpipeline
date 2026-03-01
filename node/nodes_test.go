package node

import (
	"context"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/processor"
)

func TestNodes_String_Empty(t *testing.T) {
	var nodes Nodes[*Dummy]
	tassert.Equal(t, "{}", nodes.String())
}

func TestNodes_String_Single(t *testing.T) {
	n := newDummyNode()
	nodes := Nodes[*Dummy]{n}
	tassert.Equal(t, "Dummy", nodes.String())
}

func TestNodes_String_Multiple(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2}
	tassert.Equal(t, "{Dummy, Dummy}", nodes.String())
}

func TestNodes_Without_Empty(t *testing.T) {
	var nodes Nodes[*Dummy]
	result := nodes.Without(nil)
	tassert.Empty(t, result)
}

func TestNodes_Without_RemoveNone(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2}
	result := nodes.Without(nil)
	require.Len(t, result, 2)
	tassert.Equal(t, n1, result[0])
	tassert.Equal(t, n2, result[1])
}

func TestNodes_Without_RemoveOne(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	n3 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2, n3}
	result := nodes.Without(Nodes[*Dummy]{n2})
	require.Len(t, result, 2)
	tassert.Equal(t, n1, result[0])
	tassert.Equal(t, n3, result[1])
}

func TestNodes_Without_RemoveMultiple(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	n3 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2, n3}
	result := nodes.Without(Nodes[*Dummy]{n1, n3})
	require.Len(t, result, 1)
	tassert.Equal(t, n2, result[0])
}

func TestNodes_Without_RemoveAll(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2}
	result := nodes.Without(Nodes[*Dummy]{n1, n2})
	tassert.Empty(t, result)
}

func TestNodes_Without_RemoveNotPresent(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	n3 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2}
	result := nodes.Without(Nodes[*Dummy]{n3})
	require.Len(t, result, 2)
	tassert.Equal(t, n1, result[0])
	tassert.Equal(t, n2, result[1])
}

func TestNodes_Without_DoesNotModifyOriginal(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2}
	_ = nodes.Without(Nodes[*Dummy]{n1})
	tassert.Len(t, nodes, 2, "original nodes should not be modified")
}

func TestNodes_StringRecursive_Empty(t *testing.T) {
	var nodes Nodes[*Dummy]
	tassert.Equal(t, "{}", nodes.StringRecursive())
}

func TestNodes_StringRecursive_SingleNoPushTos(t *testing.T) {
	n := newDummyNode()
	nodes := Nodes[*Dummy]{n}
	tassert.Equal(t, "Dummy", nodes.StringRecursive())
}

func TestNodes_StringRecursive_SingleWithOnePushTo(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()
	n.AddPushTo(ctx, dst)

	nodes := Nodes[*Dummy]{n}
	s := nodes.StringRecursive()
	tassert.Equal(t, "Dummy -> Dummy", s)
}

func TestNodes_StringRecursive_SingleWithMultiplePushTos(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst1 := newDummyNode()
	dst2 := newDummyNode()
	n.AddPushTo(ctx, dst1)
	n.AddPushTo(ctx, dst2)

	nodes := Nodes[*Dummy]{n}
	s := nodes.StringRecursive()
	tassert.Equal(t, "Dummy -> {Dummy, Dummy}", s)
}

func TestNodes_StringRecursive_MultipleNodes(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	nodes := Nodes[*Dummy]{n1, n2}
	s := nodes.StringRecursive()
	tassert.Equal(t, "{Dummy, Dummy}", s)
}

func TestNodes_DotString_Empty(t *testing.T) {
	var nodes Nodes[*Dummy]
	s := nodes.DotString(false)
	tassert.Contains(t, s, "digraph Pipeline")
	tassert.Contains(t, s, "}")
}

func TestNodes_DotString_Single(t *testing.T) {
	n := newDummyNode()
	nodes := Nodes[*Dummy]{n}
	s := nodes.DotString(false)
	tassert.Contains(t, s, "digraph Pipeline")
	tassert.Contains(t, s, "Dummy")
}

func TestNodes_DotString_WithPushTo(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()
	n.AddPushTo(ctx, dst)

	nodes := Nodes[*Dummy]{n}
	s := nodes.DotString(false)
	tassert.Contains(t, s, "digraph Pipeline")
	tassert.Contains(t, s, "->")
}

func TestNodes_DotString_WithStats_Panics(t *testing.T) {
	n := newDummyNode()
	nodes := Nodes[*Dummy]{n}
	tassert.Panics(t, func() {
		nodes.DotString(true)
	}, "DotString with stats should panic as it is not implemented")
}

func TestNodes_Abstract_String(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	nodes := Nodes[Abstract]{n1, n2}
	tassert.Equal(t, "{Dummy, Dummy}", nodes.String())
}

func TestNodes_Abstract_Without(t *testing.T) {
	n1 := newDummyNode()
	n2 := newDummyNode()
	n3 := newDummyNode()
	nodes := Nodes[Abstract]{n1, n2, n3}
	result := nodes.Without(Nodes[Abstract]{n2})
	require.Len(t, result, 2)
	tassert.Equal(t, Abstract(n1), result[0])
	tassert.Equal(t, Abstract(n3), result[1])
}

// Test with different processor types to verify generic behavior
func TestNodes_WithCustomDataNode(t *testing.T) {
	proc := processor.NewDummy()
	n := NewWithCustomData[string](proc)
	n.SetCustomData("test")

	nodes := Nodes[*NodeWithCustomData[string, *processor.Dummy]]{n}
	tassert.Equal(t, "Dummy", nodes.String())
}

// wrappedNode wraps an Abstract and implements OriginalNodeAbstracter.
type wrappedNode struct {
	Abstract
	original Abstract
}

func (w *wrappedNode) OriginalNodeAbstract() Abstract {
	return w.original
}

func TestOrigNode_WithOriginalNodeAbstracter(t *testing.T) {
	n := newDummyNode()
	wrapped := &wrappedNode{Abstract: n, original: n}

	result := origNode(wrapped)
	tassert.Equal(t, Abstract(n), result, "origNode should return the original node")
}

func TestOrigNode_WithoutOriginalNodeAbstracter(t *testing.T) {
	n := newDummyNode()
	result := origNode(n)
	tassert.Equal(t, Abstract(n), result, "origNode should return the same node when no wrapper")
}

func TestNodes_StringRecursive_WithWrappedNode(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()
	n.AddPushTo(ctx, dst)

	wrapped := &wrappedNode{Abstract: n, original: n}
	nodes := Nodes[Abstract]{wrapped}
	s := nodes.StringRecursive()
	tassert.Equal(t, "Dummy -> Dummy", s)
}

func TestNodes_StringRecursive_DeduplicatesPushTos(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	// Add same destination twice
	n.AddPushTo(ctx, dst)
	n.AddPushTo(ctx, dst)

	nodes := Nodes[*Dummy]{n}
	s := nodes.StringRecursive()
	// StringRecursive deduplicates based on node identity
	tassert.Equal(t, "Dummy -> Dummy", s)
}
