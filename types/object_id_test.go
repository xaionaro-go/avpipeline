package types

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetObjectID_Pointer(t *testing.T) {
	x := new(int)
	*x = 42

	// Pin the object so GC cannot move it (Go 1.21+)
	var pinner runtime.Pinner
	pinner.Pin(x)
	defer pinner.Unpin()

	id := GetObjectID(x)
	assert.NotEqual(t, ObjectID(0), id)

	// Same pointer gives same ID
	id2 := GetObjectID(x)
	assert.Equal(t, id, id2)
}

func TestGetObjectID_DifferentPointers(t *testing.T) {
	x := new(int)
	*x = 42
	y := new(int)
	*y = 42

	var pinner runtime.Pinner
	pinner.Pin(x)
	pinner.Pin(y)
	defer pinner.Unpin()

	idX := GetObjectID(x)
	idY := GetObjectID(y)
	assert.NotEqual(t, idX, idY)
}

func TestGetObjectID_NilPointer(t *testing.T) {
	var p *int
	id := GetObjectID(p)
	assert.Equal(t, ObjectID(0), id)
}

func TestGetObjectID_Struct(t *testing.T) {
	type MyStruct struct {
		Value int
	}
	s := &MyStruct{Value: 1}

	var pinner runtime.Pinner
	pinner.Pin(s)
	defer pinner.Unpin()

	id := GetObjectID(s)
	assert.NotEqual(t, ObjectID(0), id)
}

// Used by avd's monitor.go: avpipeline.FindNodeByObjectID(ctx, obj, pipeline...)
// ObjectID must be stable across calls for the same heap-allocated object.
func TestGetObjectID_Consistency(t *testing.T) {
	x := new(string)
	*x = "test"

	var pinner runtime.Pinner
	pinner.Pin(x)
	defer pinner.Unpin()

	id1 := GetObjectID(x)
	id2 := GetObjectID(x)
	id3 := GetObjectID(x)
	assert.Equal(t, id1, id2)
	assert.Equal(t, id2, id3)
}
