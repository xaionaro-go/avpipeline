package types

import (
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDisplayRotation(t *testing.T) {
	dr := NewDisplayRotation(90)
	assert.Equal(t, 90.0, dr.Load())
	assert.True(t, dr.IsSet())
}

func TestNewDisplayRotation_Zero(t *testing.T) {
	dr := NewDisplayRotation(0)
	assert.Equal(t, 0.0, dr.Load())
	assert.True(t, dr.IsSet())
}

func TestNewDisplayRotation_NaN(t *testing.T) {
	dr := NewDisplayRotation(math.NaN())
	assert.True(t, math.IsNaN(dr.Load()))
	assert.False(t, dr.IsSet())
}

func TestDisplayRotation_Store(t *testing.T) {
	dr := NewDisplayRotation(0)
	dr.Store(270)
	assert.Equal(t, 270.0, dr.Load())
}

func TestDisplayRotation_StoreNaN(t *testing.T) {
	dr := NewDisplayRotation(90)
	require.True(t, dr.IsSet())

	dr.Store(math.NaN())
	assert.False(t, dr.IsSet())
	assert.True(t, math.IsNaN(dr.Load()))
}

func TestDisplayRotation_NegativeAngle(t *testing.T) {
	dr := NewDisplayRotation(-90)
	assert.Equal(t, -90.0, dr.Load())
	assert.True(t, dr.IsSet())
}

func TestDisplayRotation_ConcurrentAccess(t *testing.T) {
	dr := NewDisplayRotation(0)
	const goroutines = 100
	const iterations = 1000

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)

	// Writers: rotate through 0, 90, 180, 270
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			angles := []float64{0, 90, 180, 270}
			for i := 0; i < iterations; i++ {
				dr.Store(angles[i%len(angles)])
			}
		}()
	}

	// Readers: verify we always get a valid angle
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			validAngles := map[float64]bool{0: true, 90: true, 180: true, 270: true}
			for i := 0; i < iterations; i++ {
				v := dr.Load()
				assert.True(t, validAngles[v], "unexpected angle: %v", v)
			}
		}()
	}

	wg.Wait()
}
