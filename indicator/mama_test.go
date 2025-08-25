package indicator

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMAMA(t *testing.T) {
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
}
