// mama_test.go provides tests for the MAMA indicator.

package indicator

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMAMA(t *testing.T) {
	for _, size := range []int{10, 100} {
		t.Run(fmt.Sprintf("size=%d", size), func(t *testing.T) {
			t.Run("flat", func(t *testing.T) {
				m := NewMAMADefault[int64](50)
				for i := range 100 {
					v := m.Update(100)
					fmt.Printf("%d: %d\n", i, v)
					require.Equal(t, int64(100), v)
				}
			})

			t.Run("0-100", func(t *testing.T) {
				m := NewMAMA[int64](50, 0.3, 0.05)
				for i := int64(0); i <= 100; i++ {
					v := m.Update(i)
					fmt.Printf("%d: %d\n", i, v)
					require.True(t, i/2 <= v && v <= i, "%d: %d", i, v)
				}
			})

			t.Run("0.1-10", func(t *testing.T) {
				m := NewMAMA[float64](50, 0.3, 0.05)
				for i := int64(0); i <= 100; i++ {
					in := float64(i) / 10
					v := m.Update(in)
					fmt.Printf("%d: %f: %f\n", i, in, v)
					require.True(t, in/2 <= v && v <= in*1.1, "%d: %f: %f", i, in, v)
				}
			})

			t.Run("0,100,0,100...", func(t *testing.T) {
				m := NewMAMA[int64](50, 0.3, 0.05)
				for i := range 100 {
					v := m.Update(0)
					fmt.Printf("%d: %d\n", i, v)
					if i > 50 {
						require.True(t, 40 <= v && v <= 60, fmt.Sprintf("%d: %d", i, v))
					}

					v = m.Update(100)
					fmt.Printf("%d: %d\n", i, v)
					if i > 50 {
						require.True(t, 40 <= v && v <= 60, fmt.Sprintf("%d: %d", i, v))
					}
				}
			})

			t.Run("0,0,100,100,0,0,100,100...", func(t *testing.T) {
				m := NewMAMA[int64](50, 0.3, 0.05)
				for i := range 100 {
					v := m.Update(0)
					fmt.Printf("%d: %d\n", i, v)
					v = m.Update(0)
					fmt.Printf("%d: %d\n", i, v)
					if i > 50 {
						require.True(t, 20 <= v && v <= 80, fmt.Sprintf("%d: %d", i, v))
					}

					v = m.Update(100)
					fmt.Printf("%d: %d\n", i, v)
					v = m.Update(100)
					fmt.Printf("%d: %d\n", i, v)
					if i > 50 {
						require.True(t, 20 <= v && v <= 80, fmt.Sprintf("%d: %d", i, v))
					}
				}
			})
		})
	}
}

func TestMAMA_InitPeriod(t *testing.T) {
	m := NewMAMA[int64](50, 0.5, 0.05)
	assert.Equal(t, int64(50), m.InitPeriod())

	m2 := NewMAMA[float64](10, 0.3, 0.01)
	assert.Equal(t, int64(10), m2.InitPeriod())
}

func TestMAMA_Valid(t *testing.T) {
	m := NewMAMA[int64](5, 0.5, 0.05)

	// Not valid until InitPeriod measurements
	for i := 0; i < 4; i++ {
		m.Update(int64(i * 10))
		assert.False(t, m.Valid(), "should not be valid after %d updates", i+1)
	}

	// Valid after InitPeriod measurements
	m.Update(40)
	assert.True(t, m.Valid(), "should be valid after %d updates", 5)

	// Stays valid
	m.Update(50)
	assert.True(t, m.Valid())
}

func TestMAMA_Concurrent(t *testing.T) {
	m := NewMAMA[int64](20, 0.5, 0.05)
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(base int64) {
			defer wg.Done()
			for j := int64(0); j < 100; j++ {
				m.Update(base + j)
			}
		}(int64(i * 100))
	}
	wg.Wait()
	assert.True(t, m.Valid())
}
