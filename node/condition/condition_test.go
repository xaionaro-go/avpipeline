package condition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
)

func newTestNode(ctx context.Context) node.Abstract {
	return node.NewFromKernel(ctx, &kernel.Passthrough{})
}

func TestIn_Match(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)

	cond := In{n1, n2}
	assert.True(t, cond.Match(ctx, n1))
	assert.True(t, cond.Match(ctx, n2))
	assert.False(t, cond.Match(ctx, n3))
}

func TestIn_Empty(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := In{}
	assert.False(t, cond.Match(ctx, n))
}

func TestIn_String(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	cond := In{n1}
	s := cond.String()
	assert.Contains(t, s, "Passthrough")
}

func TestAnd_AllTrue(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := And{
		In{n},
		In{n},
	}
	assert.True(t, cond.Match(ctx, n))
}

func TestAnd_OneFalse(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	cond := And{
		In{n1},
		In{n2},
	}
	assert.False(t, cond.Match(ctx, n1))
}

func TestAnd_Empty(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := And{}
	assert.True(t, cond.Match(ctx, n))
}

func TestAnd_Add(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := &And{}
	cond.Add(In{n})
	assert.Len(t, *cond, 1)
}

func TestAnd_String(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	cond := And{In{n1}, In{n2}}
	s := cond.String()
	assert.Contains(t, s, "&")
}

func TestOr_OneTrue(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	cond := Or{
		In{n1},
		In{n2},
	}
	assert.True(t, cond.Match(ctx, n1))
}

func TestOr_AllFalse(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	n3 := newTestNode(ctx)
	cond := Or{
		In{n1},
		In{n2},
	}
	assert.False(t, cond.Match(ctx, n3))
}

func TestOr_Empty(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := Or{}
	assert.False(t, cond.Match(ctx, n))
}

func TestOr_Add(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := &Or{}
	cond.Add(In{n})
	assert.Len(t, *cond, 1)
}

func TestOr_String(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	cond := Or{In{n1}, In{n2}}
	s := cond.String()
	assert.Contains(t, s, "|")
}

func TestNot_Single(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := Not{In{n}}
	assert.False(t, cond.Match(ctx, n))
}

func TestNot_Invert(t *testing.T) {
	ctx := context.Background()
	n1 := newTestNode(ctx)
	n2 := newTestNode(ctx)
	cond := Not{In{n1}}
	assert.True(t, cond.Match(ctx, n2))
}

func TestNot_Multiple(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	// Not with multiple conditions acts like Not(And(...))
	cond := Not{In{n}, In{n}}
	assert.False(t, cond.Match(ctx, n))
}

func TestNot_String_Single(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := Not{In{n}}
	s := cond.String()
	assert.Contains(t, s, "Not(")
}

func TestNot_String_Multiple(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	cond := Not{In{n}, In{n}}
	s := cond.String()
	assert.Contains(t, s, "Not(")
	assert.Contains(t, s, "&")
}

func TestFunction_ReturnsTrue(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	called := false
	fn := Function(func(ctx context.Context, nd node.Abstract) bool {
		called = true
		return true
	})
	assert.True(t, fn.Match(ctx, n))
	assert.True(t, called)
}

func TestFunction_ReturnsFalse(t *testing.T) {
	ctx := context.Background()
	n := newTestNode(ctx)
	fn := Function(func(ctx context.Context, nd node.Abstract) bool {
		return false
	})
	assert.False(t, fn.Match(ctx, n))
}

func TestFunction_String(t *testing.T) {
	fn := Function(func(ctx context.Context, nd node.Abstract) bool {
		return true
	})
	s := fn.String()
	assert.Contains(t, s, "custom_function")
}
