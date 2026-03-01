package node

import (
	"context"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
)

// --- PushTos tests ---

func TestPushTos_Add_Empty(t *testing.T) {
	var pts PushTos
	n := newDummyNode()
	pts.Add(n)
	require.Len(t, pts, 1)
	tassert.Equal(t, Abstract(n), pts[0].Node)
	tassert.Nil(t, pts[0].Condition, "no conditions means nil condition")
}

func TestPushTos_Add_WithOneCondition(t *testing.T) {
	var pts PushTos
	n := newDummyNode()
	cond := packetorframefiltercondition.Static(true)
	pts.Add(n, cond)
	require.Len(t, pts, 1)
	tassert.NotNil(t, pts[0].Condition)
}

func TestPushTos_Add_WithMultipleConditions(t *testing.T) {
	var pts PushTos
	n := newDummyNode()
	cond1 := packetorframefiltercondition.Static(true)
	cond2 := packetorframefiltercondition.Static(false)
	pts.Add(n, cond1, cond2)
	require.Len(t, pts, 1)
	// Multiple conditions are combined into an And
	tassert.NotNil(t, pts[0].Condition)
}

func TestPushTos_Add_Multiple(t *testing.T) {
	var pts PushTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	n3 := newDummyNode()
	pts.Add(n1)
	pts.Add(n2)
	pts.Add(n3)
	require.Len(t, pts, 3)
	tassert.Equal(t, Abstract(n1), pts[0].Node)
	tassert.Equal(t, Abstract(n2), pts[1].Node)
	tassert.Equal(t, Abstract(n3), pts[2].Node)
}

func TestPushTos_Add_Chaining(t *testing.T) {
	var pts PushTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	result := pts.Add(n1)
	tassert.Same(t, &pts, result, "Add should return the receiver for chaining")
	result.Add(n2)
	tassert.Len(t, pts, 2)
}

func TestPushTos_Nodes(t *testing.T) {
	var pts PushTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	pts.Add(n1)
	pts.Add(n2)

	nodes := pts.Nodes()
	require.Len(t, nodes, 2)
	tassert.Equal(t, Abstract(n1), nodes[0])
	tassert.Equal(t, Abstract(n2), nodes[1])
}

func TestPushTos_Nodes_Empty(t *testing.T) {
	var pts PushTos
	nodes := pts.Nodes()
	tassert.Empty(t, nodes)
}

func TestPushTos_Contains(t *testing.T) {
	var pts PushTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	pt1 := PushTo{Node: n1}
	pt2 := PushTo{Node: n2}

	pts.Add(n1)

	tassert.True(t, pts.Contains(pt1))
	tassert.False(t, pts.Contains(pt2))
}

func TestPushTos_Contains_WithCondition(t *testing.T) {
	var pts PushTos
	n := newDummyNode()
	cond := packetorframefiltercondition.Static(true)
	pts.Add(n, cond)

	// Must match exactly (same condition reference)
	ptWithCond := PushTo{Node: n, Condition: cond}
	ptWithoutCond := PushTo{Node: n}

	tassert.True(t, pts.Contains(ptWithCond))
	tassert.False(t, pts.Contains(ptWithoutCond), "different condition should not match")
}

func TestPushTos_Contains_Empty(t *testing.T) {
	var pts PushTos
	n := newDummyNode()
	tassert.False(t, pts.Contains(PushTo{Node: n}))
}

// --- PushFramesTos tests ---

func TestPushFramesTos_Add(t *testing.T) {
	var pts PushFramesTos
	n := newDummyNode()
	pts.Add(n)
	require.Len(t, pts, 1)
	tassert.Equal(t, Abstract(n), pts[0].Node)
}

func TestPushFramesTos_Add_Chaining(t *testing.T) {
	var pts PushFramesTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	result := pts.Add(n1)
	tassert.Same(t, &pts, result)
	result.Add(n2)
	tassert.Len(t, pts, 2)
}

func TestPushFramesTos_Nodes(t *testing.T) {
	var pts PushFramesTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	pts.Add(n1)
	pts.Add(n2)

	nodes := pts.Nodes()
	require.Len(t, nodes, 2)
	tassert.Equal(t, Abstract(n1), nodes[0])
	tassert.Equal(t, Abstract(n2), nodes[1])
}

func TestPushFramesTos_Nodes_Empty(t *testing.T) {
	var pts PushFramesTos
	nodes := pts.Nodes()
	tassert.Empty(t, nodes)
}

func TestPushFramesTos_Contains(t *testing.T) {
	var pts PushFramesTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	pt1 := PushFramesTo{Node: n1}
	pt2 := PushFramesTo{Node: n2}

	pts.Add(n1)

	tassert.True(t, pts.Contains(pt1))
	tassert.False(t, pts.Contains(pt2))
}

// --- PushPacketsTos tests ---

func TestPushPacketsTos_Add(t *testing.T) {
	var pts PushPacketsTos
	n := newDummyNode()
	pts.Add(n)
	require.Len(t, pts, 1)
	tassert.Equal(t, Abstract(n), pts[0].Node)
}

func TestPushPacketsTos_Add_Chaining(t *testing.T) {
	var pts PushPacketsTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	result := pts.Add(n1)
	tassert.Same(t, &pts, result)
	result.Add(n2)
	tassert.Len(t, pts, 2)
}

func TestPushPacketsTos_Nodes(t *testing.T) {
	var pts PushPacketsTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	pts.Add(n1)
	pts.Add(n2)

	nodes := pts.Nodes()
	require.Len(t, nodes, 2)
	tassert.Equal(t, Abstract(n1), nodes[0])
	tassert.Equal(t, Abstract(n2), nodes[1])
}

func TestPushPacketsTos_Nodes_Empty(t *testing.T) {
	var pts PushPacketsTos
	nodes := pts.Nodes()
	tassert.Empty(t, nodes)
}

func TestPushPacketsTos_Contains(t *testing.T) {
	var pts PushPacketsTos
	n1 := newDummyNode()
	n2 := newDummyNode()
	pt1 := PushPacketsTo{Node: n1}
	pt2 := PushPacketsTo{Node: n2}

	pts.Add(n1)

	tassert.True(t, pts.Contains(pt1))
	tassert.False(t, pts.Contains(pt2))
}

// --- Node-level PushTo management tests ---

func TestNode_AddPushTo(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	n.AddPushTo(ctx, dst)

	pts := n.GetPushTos(ctx)
	require.Len(t, pts, 1)
	tassert.Equal(t, Abstract(dst), pts[0].Node)
}

func TestNode_AddPushTo_Multiple(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst1 := newDummyNode()
	dst2 := newDummyNode()

	n.AddPushTo(ctx, dst1)
	n.AddPushTo(ctx, dst2)

	pts := n.GetPushTos(ctx)
	require.Len(t, pts, 2)
}

func TestNode_AddPushTo_WithCondition(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()
	cond := packetorframefiltercondition.Static(true)

	n.AddPushTo(ctx, dst, cond)

	pts := n.GetPushTos(ctx)
	require.Len(t, pts, 1)
	tassert.NotNil(t, pts[0].Condition)
}

func TestNode_RemovePushTo(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	n.AddPushTo(ctx, dst)
	require.Len(t, n.GetPushTos(ctx), 1)

	err := n.RemovePushTo(ctx, dst)
	tassert.NoError(t, err)
	tassert.Empty(t, n.GetPushTos(ctx))
}

func TestNode_RemovePushTo_NotFound(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	err := n.RemovePushTo(ctx, dst)
	tassert.Error(t, err)
	tassert.Contains(t, err.Error(), "does not push to")
}

func TestNode_RemovePushTo_FromMultiple(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst1 := newDummyNode()
	dst2 := newDummyNode()
	dst3 := newDummyNode()

	n.AddPushTo(ctx, dst1)
	n.AddPushTo(ctx, dst2)
	n.AddPushTo(ctx, dst3)
	require.Len(t, n.GetPushTos(ctx), 3)

	err := n.RemovePushTo(ctx, dst2)
	tassert.NoError(t, err)

	pts := n.GetPushTos(ctx)
	require.Len(t, pts, 2)
	tassert.Equal(t, Abstract(dst1), pts[0].Node)
	tassert.Equal(t, Abstract(dst3), pts[1].Node)
}

func TestNode_SetPushTos(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst1 := newDummyNode()
	dst2 := newDummyNode()

	n.SetPushTos(ctx, PushTos{
		{Node: dst1},
		{Node: dst2},
	})

	pts := n.GetPushTos(ctx)
	require.Len(t, pts, 2)
	tassert.Equal(t, Abstract(dst1), pts[0].Node)
	tassert.Equal(t, Abstract(dst2), pts[1].Node)
}

func TestNode_SetPushTos_Replace(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst1 := newDummyNode()
	dst2 := newDummyNode()

	n.AddPushTo(ctx, dst1)
	require.Len(t, n.GetPushTos(ctx), 1)

	n.SetPushTos(ctx, PushTos{{Node: dst2}})
	pts := n.GetPushTos(ctx)
	require.Len(t, pts, 1)
	tassert.Equal(t, Abstract(dst2), pts[0].Node)
}

func TestNode_SetPushTos_Empty(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	n.AddPushTo(ctx, dst)
	require.Len(t, n.GetPushTos(ctx), 1)

	n.SetPushTos(ctx, PushTos{})
	pts := n.GetPushTos(ctx)
	tassert.Empty(t, pts)
}

func TestGetChangeChanPushTo_FiresOnAdd(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	ch := n.GetChangeChanPushTo()
	// Channel should not be closed yet
	select {
	case <-ch:
		t.Fatal("change chan should not be closed before change")
	default:
	}

	n.AddPushTo(ctx, dst)

	// Now the old channel should be closed
	select {
	case <-ch:
		// expected
	default:
		t.Fatal("change chan should be closed after AddPushTo")
	}
}

func TestGetChangeChanPushTo_FiresOnRemove(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()
	n.AddPushTo(ctx, dst)

	ch := n.GetChangeChanPushTo()
	err := n.RemovePushTo(ctx, dst)
	require.NoError(t, err)

	select {
	case <-ch:
		// expected
	default:
		t.Fatal("change chan should be closed after RemovePushTo")
	}
}

func TestGetChangeChanPushTo_FiresOnSet(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	ch := n.GetChangeChanPushTo()
	n.SetPushTos(ctx, PushTos{{Node: dst}})

	select {
	case <-ch:
		// expected
	default:
		t.Fatal("change chan should be closed after SetPushTos")
	}
}

func TestGetChangeChanPushTo_NewChannelAfterChange(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst1 := newDummyNode()
	dst2 := newDummyNode()

	ch1 := n.GetChangeChanPushTo()
	n.AddPushTo(ctx, dst1)

	// ch1 should be closed
	select {
	case <-ch1:
	default:
		t.Fatal("ch1 should be closed")
	}

	// Get new channel
	ch2 := n.GetChangeChanPushTo()
	// ch2 should be open
	select {
	case <-ch2:
		t.Fatal("ch2 should not be closed yet")
	default:
	}

	// Make another change
	n.AddPushTo(ctx, dst2)

	// ch2 should now be closed
	select {
	case <-ch2:
	default:
		t.Fatal("ch2 should be closed after second AddPushTo")
	}
}

func TestNode_WithPushTos(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	n.WithPushTos(ctx, func(ctx context.Context, pts *PushTos) {
		pts.Add(dst)
	})

	pts := n.GetPushTos(ctx)
	require.Len(t, pts, 1)
	tassert.Equal(t, Abstract(dst), pts[0].Node)
}

func TestNode_WithPushTos_NoChange(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	ch := n.GetChangeChanPushTo()

	// Callback that doesn't modify PushTos
	n.WithPushTos(ctx, func(ctx context.Context, pts *PushTos) {
		// do nothing
	})

	// Channel should NOT be closed since nothing changed
	select {
	case <-ch:
		t.Fatal("change chan should not be closed when nothing changed")
	default:
	}
}

func TestNode_GetPushTos_ReturnsClone(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()
	n.AddPushTo(ctx, dst)

	pts1 := n.GetPushTos(ctx)
	pts2 := n.GetPushTos(ctx)

	// They should be equal but not the same slice
	tassert.Equal(t, pts1, pts2)
	// Modifying one should not affect the other
	pts1 = append(pts1, PushTo{Node: newDummyNode()})
	tassert.NotEqual(t, len(pts1), len(pts2))
}

func TestPushToGeneric_GetNode(t *testing.T) {
	n := newDummyNode()
	pt := PushTo{Node: n}

	// Test the nodeGetter interface
	var ng nodeGetter = pt
	tassert.Equal(t, Abstract(n), ng.getNode())
}

func TestNode_AddPushTo_DuplicateDestination(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	n.AddPushTo(ctx, dst)
	n.AddPushTo(ctx, dst)

	pts := n.GetPushTos(ctx)
	// Adding the same destination twice should create two entries
	tassert.Len(t, pts, 2)
}

func TestNode_RemovePushTo_FirstDuplicate(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()
	dst := newDummyNode()

	n.AddPushTo(ctx, dst)
	n.AddPushTo(ctx, dst)
	require.Len(t, n.GetPushTos(ctx), 2)

	err := n.RemovePushTo(ctx, dst)
	tassert.NoError(t, err)
	// Should remove only the first match
	pts := n.GetPushTos(ctx)
	tassert.Len(t, pts, 1)
}
