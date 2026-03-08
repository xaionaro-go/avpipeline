package ts

import (
	"context"
	"sync"
	"testing"
	"time"

	globaltypes "github.com/xaionaro-go/avpipeline/types"

	tassert "github.com/stretchr/testify/assert"
)

func TestNewClockCalculator(t *testing.T) {
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 90000})
	tassert.NotNil(t, cc)
	tassert.Equal(t, 1, cc.TimeBase.Num)
	tassert.Equal(t, 90000, cc.TimeBase.Den)
}

func TestClockCalculator_ToDuration_FirstCall(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 90000})

	dur := cc.ToDuration(ctx, 1000)
	tassert.Equal(t, time.Duration(0), dur)
	tassert.Equal(t, int64(1000), cc.StartTS)
}

func TestClockCalculator_ToDuration_SubsequentCalls(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 90000})

	cc.ToDuration(ctx, 0)
	dur := cc.ToDuration(ctx, 90000)
	tassert.InDelta(t, float64(time.Second), float64(dur), float64(time.Millisecond))
}

func TestClockCalculator_ToDuration_VideoTimebase(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 24})

	cc.ToDuration(ctx, 0)
	dur := cc.ToDuration(ctx, 24) // 1 second
	tassert.InDelta(t, float64(time.Second), float64(dur), float64(time.Millisecond))

	dur = cc.ToDuration(ctx, 1) // 1 frame = ~41.67ms
	expectedMs := float64(time.Second) / 24.0
	tassert.InDelta(t, expectedMs, float64(dur), float64(time.Millisecond))
}

func TestClockCalculator_ToDuration_NegativeDelta(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 1000})

	cc.ToDuration(ctx, 1000)
	dur := cc.ToDuration(ctx, 500) // backwards
	tassert.Less(t, dur, time.Duration(0))
}

func TestClockCalculator_ToWallClock(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 1000})

	before := time.Now()
	wc := cc.ToWallClock(ctx, 0)
	after := time.Now()

	tassert.True(t, wc.After(before) || wc.Equal(before))
	tassert.True(t, wc.Before(after) || wc.Equal(after))

	wc2 := cc.ToWallClock(ctx, 1000)
	diff := wc2.Sub(wc)
	tassert.InDelta(t, float64(time.Second), float64(diff), float64(time.Millisecond))
}

func TestClockCalculator_Until(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 1000})

	cc.ToDuration(ctx, 0)
	time.Sleep(10 * time.Millisecond)

	until := cc.Until(ctx, 2000)
	tassert.Greater(t, until, time.Duration(0))

	until = cc.Until(ctx, 0)
	tassert.Less(t, until, time.Duration(0))
}

func TestClockCalculator_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1, Den: 90000})

	var wg sync.WaitGroup
	const goroutines = 10
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(ts int64) {
			defer wg.Done()
			cc.ToDuration(ctx, ts)
			cc.ToWallClock(ctx, ts)
			cc.Until(ctx, ts+90000)
		}(int64(i * 1000))
	}
	wg.Wait()
}

func TestClockCalculator_NTSCTimebase(t *testing.T) {
	ctx := context.Background()
	cc := NewClockCalculator(globaltypes.Rational{Num: 1001, Den: 30000})

	cc.ToDuration(ctx, 0)
	dur := cc.ToDuration(ctx, 30)
	expectedNs := 30.0 * 1001.0 / 30000.0 * float64(time.Second)
	tassert.InDelta(t, expectedNs, float64(dur), float64(time.Millisecond))
}
