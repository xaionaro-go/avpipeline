package node

import (
	"context"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
)

func TestAppendInputFilter_NilInitialFilter(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// Initially nil
	f := n.GetInputFilter(ctx)
	tassert.Nil(t, f)

	cond := packetorframefiltercondition.Static(true)
	AppendInputFilter(ctx, n, cond)

	f = n.GetInputFilter(ctx)
	require.NotNil(t, f)
	// Should be the same condition we set
	tassert.Equal(t, cond, f)
}

func TestAppendInputFilter_ExistingNonAndFilter(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// Set a non-And filter first
	existingCond := packetorframefiltercondition.Static(true)
	n.SetInputFilter(ctx, existingCond)

	// Append another condition
	newCond := packetorframefiltercondition.Static(false)
	AppendInputFilter(ctx, n, newCond)

	f := n.GetInputFilter(ctx)
	require.NotNil(t, f)

	// Should now be an And with both conditions
	andCond, ok := f.(*packetorframefiltercondition.And)
	require.True(t, ok, "filter should be an And condition")
	tassert.Len(t, *andCond, 2)
}

func TestAppendInputFilter_ExistingAndFilter(t *testing.T) {
	ctx := context.Background()
	n := newDummyNode()

	// Set an And filter first
	cond1 := packetorframefiltercondition.Static(true)
	cond2 := packetorframefiltercondition.Static(false)
	andCond := packetorframefiltercondition.And{cond1, cond2}
	n.SetInputFilter(ctx, andCond)

	// Append another condition
	cond3 := packetorframefiltercondition.Static(true)
	AppendInputFilter(ctx, n, cond3)

	f := n.GetInputFilter(ctx)
	require.NotNil(t, f)

	// The And should now have 3 conditions
	resultAndCond, ok := f.(packetorframefiltercondition.And)
	require.True(t, ok, "filter should remain an And condition")
	tassert.Len(t, resultAndCond, 3)
}
