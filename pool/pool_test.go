package pool

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testObj struct {
	Value int
	Reset bool
}

func TestPool_Get_AllocatesNew(t *testing.T) {
	allocCount := 0
	p := NewPool(
		func() *testObj {
			allocCount++
			return &testObj{Value: allocCount}
		},
		func(o *testObj) { o.Reset = true; o.Value = 0 },
		func(o *testObj) {},
	)

	obj := p.Get()
	assert.NotNil(t, obj)
	assert.Equal(t, 1, obj.Value)
}

func TestPool_PutThenGet_Reuses(t *testing.T) {
	// sync.Pool doesn't guarantee returning the same object,
	// but we can verify that reset was called on put and that
	// a subsequent get returns a valid object.
	var resetCount int
	p := NewPool(
		func() *testObj { return &testObj{} },
		func(o *testObj) { resetCount++; o.Value = 0 },
		func(o *testObj) {},
	)

	obj := p.Get()
	obj.Value = 42
	p.Put(obj)
	assert.Equal(t, 1, resetCount) // Reset called during Put

	obj2 := p.Get()
	assert.NotNil(t, obj2) // Got a valid object (may or may not be the same one)
}

func TestPool_Put_CallsResetFunc(t *testing.T) {
	resetCalled := false
	p := NewPool(
		func() *testObj { return &testObj{} },
		func(o *testObj) { resetCalled = true },
		func(o *testObj) {},
	)

	obj := p.Get()
	p.Put(obj)
	assert.True(t, resetCalled)
}

func TestPool_Put_MultipleItems(t *testing.T) {
	resetCount := 0
	p := NewPool(
		func() *testObj { return &testObj{} },
		func(o *testObj) { resetCount++ },
		func(o *testObj) {},
	)

	o1 := p.Get()
	o2 := p.Get()
	o3 := p.Get()
	p.Put(o1, o2, o3)
	assert.Equal(t, 3, resetCount)
}

func TestPool_ReuseMemory_False(t *testing.T) {
	old := ReuseMemory
	ReuseMemory = false
	defer func() { ReuseMemory = old }()

	resetCalled := false
	p := NewPool(
		func() *testObj { return &testObj{} },
		func(o *testObj) { resetCalled = true },
		func(o *testObj) {},
	)

	obj := p.Get()
	p.Put(obj)
	assert.False(t, resetCalled) // Put is a no-op when ReuseMemory=false
}

// ffstream uses packet.Pool for memory-efficient packet handling
// during high-throughput streaming. Test concurrent safety.
func TestPool_Concurrent(t *testing.T) {
	var allocCount atomic.Int64
	p := NewPool(
		func() *testObj {
			allocCount.Add(1)
			return &testObj{}
		},
		func(o *testObj) { o.Value = 0 },
		func(o *testObj) {},
	)

	const goroutines = 50
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				obj := p.Get()
				obj.Value = j
				p.Put(obj)
			}
		}()
	}
	wg.Wait()
	// Pool should have reused objects, so allocCount should be much less than goroutines*iterations
	assert.Less(t, allocCount.Load(), int64(goroutines*iterations))
}
