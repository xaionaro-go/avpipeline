package router

import (
	"context"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/assert"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/processor"
)

// mockProcessorAbstract implements processor.Abstract without packet source/sink.
type mockProcessorAbstract struct{}

func (m *mockProcessorAbstract) String() string                         { return "mock" }
func (m *mockProcessorAbstract) Close(context.Context) error            { return nil }
func (m *mockProcessorAbstract) InputChan() chan<- packetorframe.InputUnion { return nil }
func (m *mockProcessorAbstract) OutputChan() <-chan packetorframe.OutputUnion { return nil }
func (m *mockProcessorAbstract) ErrorChan() <-chan error                { return nil }
func (m *mockProcessorAbstract) CountersPtr() *processor.Counters      { return nil }

var _ processor.Abstract = (*mockProcessorAbstract)(nil)

// mockProcessorWithPacketSource implements processor.Abstract + GetPacketSourcer.
type mockProcessorWithPacketSource struct {
	mockProcessorAbstract
	source packet.Source
}

func (m *mockProcessorWithPacketSource) GetPacketSource() packet.Source {
	return m.source
}

var _ processor.GetPacketSourcer = (*mockProcessorWithPacketSource)(nil)

// mockProcessorWithPacketSink implements processor.Abstract + GetPacketSinker.
type mockProcessorWithPacketSink struct {
	mockProcessorAbstract
	sink packet.Sink
}

func (m *mockProcessorWithPacketSink) GetPacketSink() packet.Sink {
	return m.sink
}

var _ processor.GetPacketSinker = (*mockProcessorWithPacketSink)(nil)

// mockPacketSink implements packet.Sink.
type mockPacketSink struct{}

func (s *mockPacketSink) WithInputFormatContext(ctx context.Context, callback func(*astiav.FormatContext)) {
	fc := astiav.AllocFormatContext()
	callback(fc)
}

func (s *mockPacketSink) NotifyAboutPacketSource(ctx context.Context, source packet.Source) error {
	return nil
}

var _ packet.Sink = (*mockPacketSink)(nil)

func TestAsPacketSource_NilIfNotSupported(t *testing.T) {
	proc := &mockProcessorAbstract{}
	result := asPacketSource(proc)
	assert.Nil(t, result)
}

func TestAsPacketSource_NilIfSourceIsNil(t *testing.T) {
	proc := &mockProcessorWithPacketSource{source: nil}
	result := asPacketSource(proc)
	assert.Nil(t, result)
}

func TestAsPacketSource_ReturnsSourceWhenPresent(t *testing.T) {
	source := newMockPacketSource("test-source")
	proc := &mockProcessorWithPacketSource{source: source}
	result := asPacketSource(proc)
	assert.Same(t, source, result)
}

func TestAsPacketSink_NilIfNotSupported(t *testing.T) {
	proc := &mockProcessorAbstract{}
	result := asPacketSink(proc)
	assert.Nil(t, result)
}

func TestAsPacketSink_NilIfSinkIsNil(t *testing.T) {
	proc := &mockProcessorWithPacketSink{sink: nil}
	result := asPacketSink(proc)
	assert.Nil(t, result)
}

func TestAsPacketSink_ReturnsSinkWhenPresent(t *testing.T) {
	sink := &mockPacketSink{}
	proc := &mockProcessorWithPacketSink{sink: sink}
	result := asPacketSink(proc)
	assert.Same(t, sink, result)
}
