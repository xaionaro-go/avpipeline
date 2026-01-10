package ts

import (
	"context"
	"math"
	"time"

	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type ClockCalculator struct {
	xsync.Mutex
	TimeBase  globaltypes.Rational
	StartTS   int64
	StartTime time.Time
}

func NewClockCalculator(
	timeBase globaltypes.Rational,
) *ClockCalculator {
	return &ClockCalculator{
		TimeBase: timeBase,
		StartTS:  math.MinInt64,
	}
}

func (c *ClockCalculator) ToDuration(
	ctx context.Context,
	ts int64,
) time.Duration {
	return xsync.DoR1(ctx, &c.Mutex, func() time.Duration {
		return c.asLocked().ToDuration(ts)
	})
}

func (c *ClockCalculator) ToWallClock(
	ctx context.Context,
	ts int64,
) time.Time {
	return xsync.DoR1(ctx, &c.Mutex, func() time.Time {
		return c.asLocked().ToWallClock(ts)
	})
}

type clockCalculatorLocked struct {
	*ClockCalculator
}

func (c *ClockCalculator) asLocked() *clockCalculatorLocked {
	return &clockCalculatorLocked{c}
}

func (c *clockCalculatorLocked) ToDuration(
	ts int64,
) time.Duration {
	if c.StartTS == math.MinInt64 {
		c.StartTS = ts
		c.StartTime = time.Now()
		return 0
	}

	tsDelta := ts - c.StartTS
	dur := time.Duration(float64(tsDelta) * c.TimeBase.Float64() * float64(time.Second))
	return dur
}

func (c *clockCalculatorLocked) ToWallClock(
	ts int64,
) time.Time {
	d := c.ToDuration(ts)
	return c.StartTime.Add(d)
}

func (c *ClockCalculator) Until(
	ctx context.Context,
	ts int64,
) time.Duration {
	return xsync.DoR1(ctx, &c.Mutex, func() time.Duration {
		return c.asLocked().Until(ctx, ts)
	})
}

func (c *clockCalculatorLocked) Until(
	ctx context.Context,
	ts int64,
) time.Duration {
	wantDur := c.ToDuration(ts)
	elapsed := time.Since(c.StartTime)
	return wantDur - elapsed
}
