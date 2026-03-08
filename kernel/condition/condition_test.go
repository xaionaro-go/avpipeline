package condition

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// mockKernel satisfies kerneltypes.Abstract for condition testing.
type mockKernel struct{}

func (m *mockKernel) String() string { return "mockKernel" }
func (m *mockKernel) Close(context.Context) error {
	return nil
}
func (m *mockKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(m)
}
func (m *mockKernel) CloseChan() <-chan struct{} { return nil }
func (m *mockKernel) SendInput(ctx context.Context, input packetorframe.InputUnion, outputCh chan<- packetorframe.OutputUnion) error {
	return nil
}
func (m *mockKernel) Generate(ctx context.Context, outputCh chan<- packetorframe.OutputUnion) error {
	return nil
}

var _ kerneltypes.Abstract = (*mockKernel)(nil)

func TestStatic_True(t *testing.T) {
	cond := Static[kerneltypes.Abstract](true)
	ctx := context.Background()
	assert.True(t, cond.Match(ctx, &mockKernel{}))
}

func TestStatic_False(t *testing.T) {
	cond := Static[kerneltypes.Abstract](false)
	ctx := context.Background()
	assert.False(t, cond.Match(ctx, &mockKernel{}))
}

func TestStatic_String(t *testing.T) {
	assert.Equal(t, "true", Static[kerneltypes.Abstract](true).String())
	assert.Equal(t, "false", Static[kerneltypes.Abstract](false).String())
}

func TestNot(t *testing.T) {
	ctx := context.Background()
	trueCond := Static[kerneltypes.Abstract](true)
	falseCond := Static[kerneltypes.Abstract](false)

	notTrue := Not[kerneltypes.Abstract]{Condition: trueCond}
	assert.False(t, notTrue.Match(ctx, &mockKernel{}))

	notFalse := Not[kerneltypes.Abstract]{Condition: falseCond}
	assert.True(t, notFalse.Match(ctx, &mockKernel{}))
}

func TestNot_String(t *testing.T) {
	cond := Not[kerneltypes.Abstract]{Condition: Static[kerneltypes.Abstract](true)}
	assert.Equal(t, "Not(true)", cond.String())
}

func TestAnd_AllTrue(t *testing.T) {
	ctx := context.Background()
	cond := And[kerneltypes.Abstract]{
		Static[kerneltypes.Abstract](true),
		Static[kerneltypes.Abstract](true),
	}
	assert.True(t, cond.Match(ctx, &mockKernel{}))
}

func TestAnd_OneFalse(t *testing.T) {
	ctx := context.Background()
	cond := And[kerneltypes.Abstract]{
		Static[kerneltypes.Abstract](true),
		Static[kerneltypes.Abstract](false),
	}
	assert.False(t, cond.Match(ctx, &mockKernel{}))
}

func TestAnd_Empty(t *testing.T) {
	ctx := context.Background()
	cond := And[kerneltypes.Abstract]{}
	assert.True(t, cond.Match(ctx, &mockKernel{}))
}

func TestAnd_Add(t *testing.T) {
	cond := &And[kerneltypes.Abstract]{}
	cond.Add(Static[kerneltypes.Abstract](true))
	cond.Add(Static[kerneltypes.Abstract](true))
	assert.Len(t, *cond, 2)
}

func TestAnd_String(t *testing.T) {
	cond := And[kerneltypes.Abstract]{
		Static[kerneltypes.Abstract](true),
		Static[kerneltypes.Abstract](false),
	}
	assert.Equal(t, "(true&false)", cond.String())
}

func TestOr_AllFalse(t *testing.T) {
	ctx := context.Background()
	cond := Or[kerneltypes.Abstract]{
		Static[kerneltypes.Abstract](false),
		Static[kerneltypes.Abstract](false),
	}
	assert.False(t, cond.Match(ctx, &mockKernel{}))
}

func TestOr_OneTrue(t *testing.T) {
	ctx := context.Background()
	cond := Or[kerneltypes.Abstract]{
		Static[kerneltypes.Abstract](false),
		Static[kerneltypes.Abstract](true),
	}
	assert.True(t, cond.Match(ctx, &mockKernel{}))
}

func TestOr_Empty(t *testing.T) {
	ctx := context.Background()
	cond := Or[kerneltypes.Abstract]{}
	assert.False(t, cond.Match(ctx, &mockKernel{}))
}

func TestOr_Add(t *testing.T) {
	cond := &Or[kerneltypes.Abstract]{}
	cond.Add(Static[kerneltypes.Abstract](false))
	cond.Add(Static[kerneltypes.Abstract](true))
	assert.Len(t, *cond, 2)
}

func TestOr_String(t *testing.T) {
	cond := Or[kerneltypes.Abstract]{
		Static[kerneltypes.Abstract](true),
		Static[kerneltypes.Abstract](false),
	}
	assert.Equal(t, "(true|false)", cond.String())
}

func TestFunction_ReturnsTrue(t *testing.T) {
	ctx := context.Background()
	called := false
	fn := Function[kerneltypes.Abstract](func(ctx context.Context, k kerneltypes.Abstract) bool {
		called = true
		return true
	})
	assert.True(t, fn.Match(ctx, &mockKernel{}))
	assert.True(t, called)
}

func TestFunction_ReturnsFalse(t *testing.T) {
	ctx := context.Background()
	fn := Function[kerneltypes.Abstract](func(ctx context.Context, k kerneltypes.Abstract) bool {
		return false
	})
	assert.False(t, fn.Match(ctx, &mockKernel{}))
}

func TestFunction_String(t *testing.T) {
	fn := Function[kerneltypes.Abstract](func(ctx context.Context, k kerneltypes.Abstract) bool {
		return true
	})
	s := fn.String()
	assert.Contains(t, s, "custom_function")
}

// Test composition: And(Or(true, false), Not(false)) == true
func TestComposition(t *testing.T) {
	ctx := context.Background()
	cond := And[kerneltypes.Abstract]{
		Or[kerneltypes.Abstract]{
			Static[kerneltypes.Abstract](true),
			Static[kerneltypes.Abstract](false),
		},
		&Not[kerneltypes.Abstract]{Condition: Static[kerneltypes.Abstract](false)},
	}
	assert.True(t, cond.Match(ctx, &mockKernel{}))
}
