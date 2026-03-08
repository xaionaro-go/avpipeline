package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

func TestNewCounters(t *testing.T) {
	c := NewCounters()
	require.NotNil(t, c)

	// All sections should be initialized with zero-valued counters
	assert.Equal(t, uint64(0), c.Processed.TotalCount())
	assert.Equal(t, uint64(0), c.Generated.TotalCount())
	assert.Equal(t, uint64(0), c.Omitted.TotalCount())
}

func TestCounters_Increment_Processed(t *testing.T) {
	c := NewCounters()
	ctx := context.Background()

	item := c.Increment(ctx, CountersSectionIDProcessed, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 1024)
	require.NotNil(t, item)
	assert.Equal(t, uint64(1), item.Count.Load())
	assert.Equal(t, uint64(1024), item.Bytes.Load())

	// Increment again
	item2 := c.Increment(ctx, CountersSectionIDProcessed, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 2048)
	assert.Equal(t, item, item2) // same counter item
	assert.Equal(t, uint64(2), item.Count.Load())
	assert.Equal(t, uint64(3072), item.Bytes.Load())
}

func TestCounters_Increment_Generated(t *testing.T) {
	c := NewCounters()
	ctx := context.Background()

	item := c.Increment(ctx, CountersSectionIDGenerated, globaltypes.CountersSubSectionIDFrames, globaltypes.MediaTypeAudio, 512)
	require.NotNil(t, item)
	assert.Equal(t, uint64(1), item.Count.Load())
	assert.Equal(t, uint64(512), item.Bytes.Load())
}

func TestCounters_Increment_Omitted(t *testing.T) {
	c := NewCounters()
	ctx := context.Background()

	item := c.Increment(ctx, CountersSectionIDOmitted, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 256)
	require.NotNil(t, item)
	assert.Equal(t, uint64(1), item.Count.Load())
	assert.Equal(t, uint64(256), item.Bytes.Load())
}

func TestCounters_Increment_InvalidSection(t *testing.T) {
	c := NewCounters()
	ctx := context.Background()

	item := c.Increment(ctx, CountersSectionID(99), globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 100)
	assert.Nil(t, item)
}

func TestCounters_Increment_SectionsAreIndependent(t *testing.T) {
	c := NewCounters()
	ctx := context.Background()

	c.Increment(ctx, CountersSectionIDProcessed, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 100)
	c.Increment(ctx, CountersSectionIDGenerated, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 200)
	c.Increment(ctx, CountersSectionIDOmitted, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 300)

	assert.Equal(t, uint64(100), c.Processed.TotalBytes())
	assert.Equal(t, uint64(200), c.Generated.TotalBytes())
	assert.Equal(t, uint64(300), c.Omitted.TotalBytes())
}

func TestCounters_Increment_MediaTypes(t *testing.T) {
	c := NewCounters()
	ctx := context.Background()

	c.Increment(ctx, CountersSectionIDProcessed, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeVideo, 100)
	c.Increment(ctx, CountersSectionIDProcessed, globaltypes.CountersSubSectionIDPackets, globaltypes.MediaTypeAudio, 50)

	assert.Equal(t, uint64(100), c.Processed.Packets.Video.Bytes.Load())
	assert.Equal(t, uint64(50), c.Processed.Packets.Audio.Bytes.Load())
}
