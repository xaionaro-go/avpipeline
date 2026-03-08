package types

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountersItem_Increment(t *testing.T) {
	c := NewCountersItem()
	c.Increment(100)
	assert.Equal(t, uint64(1), c.Count.Load())
	assert.Equal(t, uint64(100), c.Bytes.Load())

	c.Increment(200)
	assert.Equal(t, uint64(2), c.Count.Load())
	assert.Equal(t, uint64(300), c.Bytes.Load())
}

func TestCountersItem_ToStats_RoundTrip(t *testing.T) {
	c := NewCountersItem()
	c.Increment(42)
	c.Increment(58)

	stats := c.ToStats()
	assert.Equal(t, uint64(2), stats.Count)
	assert.Equal(t, uint64(100), stats.Bytes)

	// Round trip
	c2 := stats.ToCounters()
	assert.Equal(t, uint64(2), c2.Count.Load())
	assert.Equal(t, uint64(100), c2.Bytes.Load())
}

func TestCountersSubSection_Get(t *testing.T) {
	s := NewCountersSubSection()
	assert.Same(t, s.Video, s.Get(MediaTypeVideo))
	assert.Same(t, s.Audio, s.Get(MediaTypeAudio))
	// Unknown and other types all go to Other
	assert.Same(t, s.Other, s.Get(MediaTypeData))
	assert.Same(t, s.Other, s.Get(MediaTypeSubtitle))
	assert.Same(t, s.Other, s.Get(MediaTypeUnknown))
}

func TestCountersSubSection_Increment(t *testing.T) {
	s := NewCountersSubSection()
	s.Increment(MediaTypeVideo, 1000)
	s.Increment(MediaTypeVideo, 2000)
	s.Increment(MediaTypeAudio, 500)

	assert.Equal(t, uint64(2), s.Video.Count.Load())
	assert.Equal(t, uint64(3000), s.Video.Bytes.Load())
	assert.Equal(t, uint64(1), s.Audio.Count.Load())
	assert.Equal(t, uint64(500), s.Audio.Bytes.Load())
}

func TestCountersSubSection_Totals(t *testing.T) {
	s := NewCountersSubSection()
	s.Increment(MediaTypeVideo, 1000)
	s.Increment(MediaTypeAudio, 500)
	s.Increment(MediaTypeData, 200) // goes to Other
	s.Unknown.Increment(100)

	assert.Equal(t, uint64(4), s.TotalCount())
	assert.Equal(t, uint64(1800), s.TotalBytes())
}

func TestCountersSubSection_ToStats_RoundTrip(t *testing.T) {
	s := NewCountersSubSection()
	s.Increment(MediaTypeVideo, 1000)
	s.Increment(MediaTypeAudio, 500)

	stats := s.ToStats()
	assert.Equal(t, uint64(1), stats.Video.Count)
	assert.Equal(t, uint64(1000), stats.Video.Bytes)

	// Round trip
	s2 := stats.ToCounters()
	assert.Equal(t, uint64(1), s2.Video.Count.Load())
	assert.Equal(t, uint64(1000), s2.Video.Bytes.Load())
}

func TestCountersSection_IncrementAndGet(t *testing.T) {
	s := NewCountersSection()
	s.Increment(CountersSubSectionIDPackets, MediaTypeVideo, 1500)
	s.Increment(CountersSubSectionIDFrames, MediaTypeAudio, 300)

	assert.Equal(t, uint64(1), s.Packets.Video.Count.Load())
	assert.Equal(t, uint64(1500), s.Packets.Video.Bytes.Load())
	assert.Equal(t, uint64(1), s.Frames.Audio.Count.Load())
	assert.Equal(t, uint64(300), s.Frames.Audio.Bytes.Load())
}

func TestCountersSection_Get(t *testing.T) {
	s := NewCountersSection()
	assert.Equal(t, &s.Packets, s.Get(CountersSubSectionIDPackets))
	assert.Equal(t, &s.Frames, s.Get(CountersSubSectionIDFrames))
	assert.Nil(t, s.Get(UndefinedSubSectionID))
	assert.Nil(t, s.Get(EndOfCountersSubSectionID))
}

func TestCountersSection_Totals(t *testing.T) {
	s := NewCountersSection()
	s.Increment(CountersSubSectionIDPackets, MediaTypeVideo, 100)
	s.Increment(CountersSubSectionIDPackets, MediaTypeAudio, 50)
	s.Increment(CountersSubSectionIDFrames, MediaTypeVideo, 200)

	assert.Equal(t, uint64(3), s.TotalCount())
	assert.Equal(t, uint64(350), s.TotalBytes())
}

func TestCountersSection_ToStats_RoundTrip(t *testing.T) {
	s := NewCountersSection()
	s.Increment(CountersSubSectionIDPackets, MediaTypeVideo, 100)

	stats := s.ToStats()
	assert.Equal(t, uint64(100), stats.Packets.Video.Bytes)

	s2 := stats.ToCounters()
	assert.Equal(t, uint64(100), s2.Packets.Video.Bytes.Load())
}

func TestCountersSubSectionID_String(t *testing.T) {
	assert.Equal(t, "undefined", UndefinedSubSectionID.String())
	assert.Equal(t, "packets", CountersSubSectionIDPackets.String())
	assert.Equal(t, "frames", CountersSubSectionIDFrames.String())
	assert.Contains(t, CountersSubSectionID(99).String(), "unknown_99")
}

// ffstream/avd use counters for pipeline statistics exposed via gRPC.
// Test concurrent increment safety (both consumers use shared counters).
func TestCountersItem_ConcurrentIncrement(t *testing.T) {
	c := NewCountersItem()
	const n = 1000
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c.Increment(10)
		}()
	}
	wg.Wait()
	assert.Equal(t, uint64(n), c.Count.Load())
	assert.Equal(t, uint64(n*10), c.Bytes.Load())
}

func TestCountersSubSection_ConcurrentIncrement(t *testing.T) {
	s := NewCountersSubSection()
	const n = 500
	var wg sync.WaitGroup
	wg.Add(n * 2)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			s.Increment(MediaTypeVideo, 100)
		}()
		go func() {
			defer wg.Done()
			s.Increment(MediaTypeAudio, 50)
		}()
	}
	wg.Wait()
	assert.Equal(t, uint64(n), s.Video.Count.Load())
	assert.Equal(t, uint64(n), s.Audio.Count.Load())
}

func TestPipelineSideData_Contains(t *testing.T) {
	type FallbackPriority int
	data := PipelineSideData{FallbackPriority(1), "metadata", 42}

	assert.True(t, data.Contains(FallbackPriority(1)))
	assert.True(t, data.Contains("metadata"))
	assert.True(t, data.Contains(42))
	assert.False(t, data.Contains(FallbackPriority(2)))
	assert.False(t, data.Contains("other"))
}

func TestPipelineSideData_Contains_Empty(t *testing.T) {
	var data PipelineSideData
	assert.False(t, data.Contains("anything"))
}

func TestPipelineSideDataLatest(t *testing.T) {
	type FallbackPriority int
	data := PipelineSideData{FallbackPriority(1), "first", FallbackPriority(3), "second"}

	// Gets the latest FallbackPriority (last one in slice)
	v, ok := PipelineSideDataLatest[FallbackPriority](data)
	require.True(t, ok)
	assert.Equal(t, FallbackPriority(3), v)

	// Gets the latest string
	s, ok := PipelineSideDataLatest[string](data)
	require.True(t, ok)
	assert.Equal(t, "second", s)

	// Type not found
	_, ok = PipelineSideDataLatest[float64](data)
	assert.False(t, ok)
}

func TestPipelineSideDataLatest_Empty(t *testing.T) {
	var data PipelineSideData
	_, ok := PipelineSideDataLatest[int](data)
	assert.False(t, ok)
}

// ffstream uses PipelineSideData for FallbackPriority and ResourceIndex tracking.
// avd doesn't use PipelineSideData directly but it flows through the router.
func TestPipelineSideData_FallbackPriorityPattern(t *testing.T) {
	type FallbackPriority int
	type ResourceIndex int

	// Simulate ffstream's side data propagation
	data := PipelineSideData{FallbackPriority(0), ResourceIndex(2)}

	// Add new priority (simulates input switch)
	data = append(data, FallbackPriority(1))

	// Latest should be the new priority
	prio, ok := PipelineSideDataLatest[FallbackPriority](data)
	require.True(t, ok)
	assert.Equal(t, FallbackPriority(1), prio)

	// ResourceIndex should still be accessible
	idx, ok := PipelineSideDataLatest[ResourceIndex](data)
	require.True(t, ok)
	assert.Equal(t, ResourceIndex(2), idx)
}
