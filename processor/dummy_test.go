package processor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDummy(t *testing.T) {
	d := NewDummy()
	require.NotNil(t, d)
	assert.NotNil(t, d.CountersStorage, "CountersStorage should be initialized")
}

func TestDummy_InputChan_ReturnsDiscardInputChan(t *testing.T) {
	d := NewDummy()
	ch := d.InputChan()
	assert.Equal(t, DiscardInputChan, ch, "InputChan should return DiscardInputChan")
}

func TestDummy_OutputChan_ReturnsNil(t *testing.T) {
	d := NewDummy()
	ch := d.OutputChan()
	assert.Nil(t, ch, "OutputChan should return nil")
}

func TestDummy_ErrorChan_ReturnsNil(t *testing.T) {
	d := NewDummy()
	ch := d.ErrorChan()
	assert.Nil(t, ch, "ErrorChan should return nil")
}

func TestDummy_Close_ReturnsNil(t *testing.T) {
	d := NewDummy()
	err := d.Close(context.Background())
	assert.NoError(t, err, "Close should return nil")
}

func TestDummy_Close_MultipleCallsSafe(t *testing.T) {
	d := NewDummy()
	for i := 0; i < 5; i++ {
		err := d.Close(context.Background())
		assert.NoError(t, err, "Close should always return nil")
	}
}

func TestDummy_CountersPtr_ReturnsValidPointer(t *testing.T) {
	d := NewDummy()
	ptr := d.CountersPtr()
	require.NotNil(t, ptr, "CountersPtr should return a non-nil pointer")
}

func TestDummy_CountersPtr_ConsistentPointer(t *testing.T) {
	d := NewDummy()
	ptr1 := d.CountersPtr()
	ptr2 := d.CountersPtr()
	assert.Same(t, ptr1, ptr2, "CountersPtr should return the same pointer on repeated calls")
}

func TestDummy_String(t *testing.T) {
	d := NewDummy()
	assert.Equal(t, "Dummy", d.String())
}

func TestDummy_ImplementsAbstract(t *testing.T) {
	// Compile-time check is already in dummy.go, but verify at runtime too.
	var _ Abstract = (*Dummy)(nil)
	d := NewDummy()
	var a Abstract = d
	assert.NotNil(t, a)
}
