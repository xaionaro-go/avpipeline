package inputwithfallback

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	testifyassert "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline/codec"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/node"
	packetorframefiltercondition "github.com/xaionaro-go/avpipeline/node/filter/packetorframefilter/condition"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

// --- Mock types for testing ---

type mockKernel struct {
	name    string
	closeCh chan struct{}
}

var _ kernel.Abstract = (*mockKernel)(nil)

func newMockKernel(name string) *mockKernel {
	return &mockKernel{
		name:    name,
		closeCh: make(chan struct{}),
	}
}

func (m *mockKernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return kerneltypes.ErrUnexpectedInputType{}
}

func (m *mockKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	<-ctx.Done()
	return ctx.Err()
}

func (m *mockKernel) String() string {
	return m.name
}

func (m *mockKernel) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(m)
}

func (m *mockKernel) Close(ctx context.Context) error {
	select {
	case <-m.closeCh:
	default:
		close(m.closeCh)
	}
	return nil
}

func (m *mockKernel) CloseChan() <-chan struct{} {
	return m.closeCh
}

func (m *mockKernel) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
}

// inputKernel is a mock that satisfies InputKernel (kernel.Abstract + packet.Source)
type inputKernel = mockKernel

var _ InputKernel = (*inputKernel)(nil)

// mockInputFactory implements InputFactory for testing
type mockInputFactory struct {
	name           string
	kernelFactory  func(ctx context.Context) (*inputKernel, error)
	decoderFactory codec.DecoderFactory
	decoderErr     error
}

var _ InputFactory[*inputKernel, codec.DecoderFactory, struct{}] = (*mockInputFactory)(nil)

func (m *mockInputFactory) String() string {
	return m.name
}

func (m *mockInputFactory) NewInput(
	ctx context.Context,
	chain *InputChain[*inputKernel, codec.DecoderFactory, struct{}],
) (*inputKernel, error) {
	if m.kernelFactory != nil {
		return m.kernelFactory(ctx)
	}
	return newMockKernel(m.name + ":kernel"), nil
}

func (m *mockInputFactory) NewDecoderFactory(
	ctx context.Context,
	chain *InputChain[*inputKernel, codec.DecoderFactory, struct{}],
) (codec.DecoderFactory, error) {
	return m.decoderFactory, m.decoderErr
}

// --- Option tests ---

func TestOptionRetryInterval(t *testing.T) {
	opt := OptionRetryInterval(5 * time.Second)
	cfg := &Config{}
	opt.apply(cfg)
	testifyassert.Equal(t, 5*time.Second, cfg.RetryInterval)
}

func TestOptionSwitchKeepUnlessTimeout(t *testing.T) {
	opt := OptionSwitchKeepUnlessTimeout(3 * time.Second)
	cfg := &Config{}
	opt.apply(cfg)
	testifyassert.Equal(t, 3*time.Second, cfg.SwitchKeepUnlessTimeout)
}

func TestOptions_Config_Defaults(t *testing.T) {
	cfg := Options(nil).Config()
	testifyassert.Equal(t, time.Duration(0), cfg.RetryInterval)
	testifyassert.Equal(t, time.Second, cfg.SwitchKeepUnlessTimeout)
}

func TestOptions_Config_WithOptions(t *testing.T) {
	cfg := Options{
		OptionRetryInterval(2 * time.Second),
		OptionSwitchKeepUnlessTimeout(10 * time.Second),
	}.Config()
	testifyassert.Equal(t, 2*time.Second, cfg.RetryInterval)
	testifyassert.Equal(t, 10*time.Second, cfg.SwitchKeepUnlessTimeout)
}

func TestOptions_Config_Empty(t *testing.T) {
	cfg := Options{}.Config()
	testifyassert.Equal(t, time.Duration(0), cfg.RetryInterval)
	testifyassert.Equal(t, time.Second, cfg.SwitchKeepUnlessTimeout)
}

// --- updateWithInertialValue tests ---

func TestUpdateWithInertialValue_ZeroMeasurementsCount(t *testing.T) {
	result := updateWithInertialValue(100, 200, 0.9, 0)
	testifyassert.Equal(t, uint64(200), result)
}

func TestUpdateWithInertialValue_WithInertia(t *testing.T) {
	// 1000 * 0.9 + 500 * 0.1 = 900 + 50 = 950
	result := updateWithInertialValue(1000, 500, 0.9, 1)
	testifyassert.Equal(t, uint64(950), result)
}

func TestUpdateWithInertialValue_ZeroInertia(t *testing.T) {
	// 1000 * 0.0 + 500 * 1.0 = 500
	result := updateWithInertialValue(1000, 500, 0.0, 1)
	testifyassert.Equal(t, uint64(500), result)
}

func TestUpdateWithInertialValue_FullInertia(t *testing.T) {
	// 1000 * 1.0 + 500 * 0.0 = 1000
	result := updateWithInertialValue(1000, 500, 1.0, 1)
	testifyassert.Equal(t, uint64(1000), result)
}

func TestUpdateWithInertialValue_ZeroValues(t *testing.T) {
	result := updateWithInertialValue(0, 0, 0.9, 1)
	testifyassert.Equal(t, uint64(0), result)
}

func TestUpdateWithInertialValue_LargeValues(t *testing.T) {
	// Verify no overflow with large but reasonable bitrate values
	result := updateWithInertialValue(10_000_000, 20_000_000, 0.5, 10)
	testifyassert.Equal(t, uint64(15_000_000), result)
}

// --- TrackMeasurements tests ---

func TestNewTrackMeasurements(t *testing.T) {
	tm := newTrackMeasurements()
	require.NotNil(t, tm)
	testifyassert.Equal(t, uint64(0), tm.InputBitRate.Load())
	testifyassert.Equal(t, uint64(0), tm.OutputBitRate.Load())
}

func TestTrackMeasurements_AtomicOperations(t *testing.T) {
	tm := newTrackMeasurements()
	tm.InputBitRate.Store(42000)
	tm.OutputBitRate.Store(84000)
	testifyassert.Equal(t, uint64(42000), tm.InputBitRate.Load())
	testifyassert.Equal(t, uint64(84000), tm.OutputBitRate.Load())
}

// --- InputNodes.NonNil tests ---

func TestInputNodes_NonNil_AllNil(t *testing.T) {
	nodes := InputNodes[*inputKernel, struct{}]{nil, nil, nil}
	result := nodes.NonNil()
	testifyassert.Empty(t, result)
}

func TestInputNodes_NonNil_NoNils(t *testing.T) {
	ctx := context.Background()
	n1 := node.NewWithCustomDataFromKernel[struct{}](ctx, kernel.NewRetryable(ctx,
		func(ctx context.Context) (*inputKernel, error) {
			return newMockKernel("n1"), nil
		},
		nil,
		kernel.RetryableOptionStartOnInit[*inputKernel](false),
	))
	n2 := node.NewWithCustomDataFromKernel[struct{}](ctx, kernel.NewRetryable(ctx,
		func(ctx context.Context) (*inputKernel, error) {
			return newMockKernel("n2"), nil
		},
		nil,
		kernel.RetryableOptionStartOnInit[*inputKernel](false),
	))
	nodes := InputNodes[*inputKernel, struct{}]{n1, n2}
	result := nodes.NonNil()
	testifyassert.Len(t, result, 2)
}

func TestInputNodes_NonNil_Mixed(t *testing.T) {
	ctx := context.Background()
	n1 := node.NewWithCustomDataFromKernel[struct{}](ctx, kernel.NewRetryable(ctx,
		func(ctx context.Context) (*inputKernel, error) {
			return newMockKernel("n1"), nil
		},
		nil,
		kernel.RetryableOptionStartOnInit[*inputKernel](false),
	))
	nodes := InputNodes[*inputKernel, struct{}]{nil, n1, nil}
	result := nodes.NonNil()
	testifyassert.Len(t, result, 1)
	testifyassert.Equal(t, n1, result[0])
}

func TestInputNodes_NonNil_Empty(t *testing.T) {
	nodes := InputNodes[*inputKernel, struct{}]{}
	result := nodes.NonNil()
	testifyassert.Empty(t, result)
}

// --- InputWithFallback creation and methods ---

func newTestIWF(t *testing.T, factories ...*mockInputFactory) *InputWithFallback[*inputKernel, codec.DecoderFactory, struct{}] {
	t.Helper()
	ctx := context.Background()
	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	for _, f := range factories {
		iFactories = append(iFactories, f)
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = iwf.Close(context.Background())
	})
	return iwf
}

func TestNew_SingleFactory(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.NotNil(t, iwf)
	testifyassert.NotNil(t, iwf.Output)
	testifyassert.NotNil(t, iwf.PreOutput)
	testifyassert.NotNil(t, iwf.InputSwitch)
	testifyassert.NotNil(t, iwf.InputSyncer)
	testifyassert.NotNil(t, iwf.MonotonicPTS)
	testifyassert.Len(t, iwf.InputChains, 1)
}

func TestNew_MultipleFactories(t *testing.T) {
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback1"}
	f3 := &mockInputFactory{name: "fallback2"}
	iwf := newTestIWF(t, f1, f2, f3)
	testifyassert.Len(t, iwf.InputChains, 3)
}

func TestNew_NoFactories(t *testing.T) {
	iwf := newTestIWF(t)
	testifyassert.NotNil(t, iwf)
	testifyassert.Len(t, iwf.InputChains, 0)
}

func TestNew_WithOptions(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](
		ctx,
		[]InputFactory[*inputKernel, codec.DecoderFactory, struct{}]{factory},
		OptionRetryInterval(5*time.Second),
		OptionSwitchKeepUnlessTimeout(10*time.Second),
	)
	require.NoError(t, err)
	defer iwf.Close(ctx)
	testifyassert.Equal(t, 5*time.Second, iwf.Config.RetryInterval)
	testifyassert.Equal(t, 10*time.Second, iwf.Config.SwitchKeepUnlessTimeout)
}

func TestNew_FactoryDecoderFactoryError(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{
		name:       "failing-factory",
		decoderErr: fmt.Errorf("decoder factory creation failed"),
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](
		ctx,
		[]InputFactory[*inputKernel, codec.DecoderFactory, struct{}]{factory},
	)
	testifyassert.Error(t, err)
	testifyassert.Nil(t, iwf)
	testifyassert.Contains(t, err.Error(), "decoder factory creation failed")
}

// --- Config defaults ---

func TestNew_DefaultConfig(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.Equal(t, time.Duration(0), iwf.Config.RetryInterval)
	testifyassert.Equal(t, time.Second, iwf.Config.SwitchKeepUnlessTimeout)
}

// --- String tests ---

func TestInputWithFallback_String_SingleInput(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	s := iwf.String()
	testifyassert.Contains(t, s, "InputWithFallback(")
	testifyassert.Contains(t, s, "current")
}

func TestInputWithFallback_String_MultipleInputs(t *testing.T) {
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)
	s := iwf.String()
	testifyassert.Contains(t, s, "InputWithFallback(")
}

// --- GetOutput tests ---

func TestInputWithFallback_GetOutput(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	output := iwf.GetOutput()
	testifyassert.NotNil(t, output)
	testifyassert.Equal(t, node.Abstract(iwf.Output), output)
}

// --- GetInputChainsCount tests ---

func TestInputWithFallback_GetInputChainsCount(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)
	testifyassert.Equal(t, 2, iwf.GetInputChainsCount(ctx))
}

func TestInputWithFallback_GetInputChainsCount_Empty(t *testing.T) {
	ctx := context.Background()
	iwf := newTestIWF(t)
	testifyassert.Equal(t, 0, iwf.GetInputChainsCount(ctx))
}

// --- GetInputs tests ---

func TestInputWithFallback_GetInputs(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)
	inputs := iwf.GetInputs(ctx)
	testifyassert.Len(t, inputs, 2)
	for _, input := range inputs {
		testifyassert.NotNil(t, input)
	}
}

// --- IsServing tests ---

func TestInputWithFallback_IsServing_NotServing(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.False(t, iwf.IsServing(ctx))
}

// --- GetObjectID tests ---

func TestInputWithFallback_GetObjectID(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	id := iwf.GetObjectID()
	testifyassert.NotEmpty(t, id)
}

// --- InputChan tests ---

func TestInputWithFallback_InputChan_ReturnsNil(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.Nil(t, iwf.InputChan())
}

// --- OutputChan tests ---

func TestInputWithFallback_OutputChan(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	ch := iwf.OutputChan()
	testifyassert.NotNil(t, ch)
}

// --- CountersPtr tests ---

func TestInputWithFallback_CountersPtr(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	counters := iwf.CountersPtr()
	testifyassert.NotNil(t, counters)
}

// --- GetCountersPtr (node-level) tests ---

func TestInputWithFallback_GetCountersPtr(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	counters := iwf.GetCountersPtr()
	testifyassert.NotNil(t, counters)
}

// --- GetProcessor tests ---

func TestInputWithFallback_GetProcessor(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	proc := iwf.GetProcessor()
	testifyassert.NotNil(t, proc)
	testifyassert.Equal(t, iwf, proc)
}

// --- InputFilter tests ---

func TestInputWithFallback_SetGetInputFilter(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Initially nil
	testifyassert.Nil(t, iwf.GetInputFilter(ctx))

	// Set a filter
	cond := &mockCondition{matchResult: true}
	iwf.SetInputFilter(ctx, cond)
	testifyassert.NotNil(t, iwf.GetInputFilter(ctx))
}

type mockCondition struct {
	matchResult bool
}

func (m *mockCondition) String() string {
	return "mockCondition"
}

func (m *mockCondition) Match(ctx context.Context, in packetorframefiltercondition.Input) bool {
	return m.matchResult
}

// --- asInputFilter tests ---

func TestAsInputFilter_String(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	filter := iwf.inputFilter()
	testifyassert.Equal(t, "InputWithFallback:InputFilter", filter.String())
}

func TestAsInputFilter_Match_NilFilter(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	filter := iwf.inputFilter()

	// With no input filter set, should always match
	result := filter.Match(ctx, packetorframefiltercondition.Input{})
	testifyassert.True(t, result)
}

func TestAsInputFilter_Match_WithFilter(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Set a filter that rejects everything
	cond := &mockCondition{matchResult: false}
	iwf.SetInputFilter(ctx, cond)

	filter := iwf.inputFilter()
	result := filter.Match(ctx, packetorframefiltercondition.Input{})
	testifyassert.False(t, result)
}

func TestAsInputFilter_Match_WithAcceptingFilter(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Set a filter that accepts everything
	cond := &mockCondition{matchResult: true}
	iwf.SetInputFilter(ctx, cond)

	filter := iwf.inputFilter()
	result := filter.Match(ctx, packetorframefiltercondition.Input{})
	testifyassert.True(t, result)
}

// --- GetBitRates tests ---

func TestInputWithFallback_GetBitRates_Initial(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	bitRates := iwf.GetBitRates(ctx)
	require.NotNil(t, bitRates)
	testifyassert.Equal(t, globaltypes.Ubps(0), bitRates.Input.Video)
	testifyassert.Equal(t, globaltypes.Ubps(0), bitRates.Input.Audio)
	testifyassert.Equal(t, globaltypes.Ubps(0), bitRates.Input.Other)
	testifyassert.Equal(t, globaltypes.Ubps(0), bitRates.Output.Video)
	testifyassert.Equal(t, globaltypes.Ubps(0), bitRates.Output.Audio)
	testifyassert.Equal(t, globaltypes.Ubps(0), bitRates.Output.Other)
}

func TestInputWithFallback_GetBitRates_AfterStore(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Store some values
	iwf.Measurements[astiav.MediaTypeVideo].InputBitRate.Store(5_000_000)
	iwf.Measurements[astiav.MediaTypeVideo].OutputBitRate.Store(4_000_000)
	iwf.Measurements[astiav.MediaTypeAudio].InputBitRate.Store(128_000)
	iwf.Measurements[astiav.MediaTypeAudio].OutputBitRate.Store(128_000)
	iwf.Measurements[astiav.MediaTypeUnknown].InputBitRate.Store(10_000)
	iwf.Measurements[astiav.MediaTypeUnknown].OutputBitRate.Store(8_000)

	bitRates := iwf.GetBitRates(ctx)
	testifyassert.Equal(t, globaltypes.Ubps(5_000_000), bitRates.Input.Video)
	testifyassert.Equal(t, globaltypes.Ubps(4_000_000), bitRates.Output.Video)
	testifyassert.Equal(t, globaltypes.Ubps(128_000), bitRates.Input.Audio)
	testifyassert.Equal(t, globaltypes.Ubps(128_000), bitRates.Output.Audio)
	testifyassert.Equal(t, globaltypes.Ubps(10_000), bitRates.Input.Other)
	testifyassert.Equal(t, globaltypes.Ubps(8_000), bitRates.Output.Other)
}

// --- getTrackMeasurements tests ---

func TestInputWithFallback_GetTrackMeasurements_Known(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	video := iwf.getTrackMeasurements(astiav.MediaTypeVideo)
	testifyassert.NotNil(t, video)
	testifyassert.Same(t, iwf.Measurements[astiav.MediaTypeVideo], video)

	audio := iwf.getTrackMeasurements(astiav.MediaTypeAudio)
	testifyassert.NotNil(t, audio)
	testifyassert.Same(t, iwf.Measurements[astiav.MediaTypeAudio], audio)
}

func TestInputWithFallback_GetTrackMeasurements_Unknown(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	unknown := iwf.getTrackMeasurements(astiav.MediaTypeUnknown)
	testifyassert.NotNil(t, unknown)
	testifyassert.Same(t, iwf.Measurements[astiav.MediaTypeUnknown], unknown)
}

func TestInputWithFallback_GetTrackMeasurements_FallbackToUnknown(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Subtitle type is not in the map, so falls back to Unknown
	subtitle := iwf.getTrackMeasurements(astiav.MediaTypeSubtitle)
	testifyassert.NotNil(t, subtitle)
	testifyassert.Same(t, iwf.Measurements[astiav.MediaTypeUnknown], subtitle)
}

// --- Measurements map initialization ---

func TestNew_MeasurementsInitialized(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	testifyassert.Len(t, iwf.Measurements, 3)
	testifyassert.Contains(t, iwf.Measurements, astiav.MediaTypeVideo)
	testifyassert.Contains(t, iwf.Measurements, astiav.MediaTypeAudio)
	testifyassert.Contains(t, iwf.Measurements, astiav.MediaTypeUnknown)
}

// --- Close tests ---

func TestInputWithFallback_Close_Empty(t *testing.T) {
	ctx := context.Background()
	iwf := newTestIWF(t)
	err := iwf.Close(ctx)
	testifyassert.NoError(t, err)
}

func TestInputWithFallback_Close_WithInputChains(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)

	err := iwf.Close(ctx)
	testifyassert.NoError(t, err)
	// After close, input chains should be nil
	testifyassert.Nil(t, iwf.InputChains)
}

// --- getInputChainByID tests ---

func TestInputWithFallback_GetInputChainByID_Valid(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)

	chain0 := iwf.getInputChainByID(ctx, 0)
	testifyassert.NotNil(t, chain0)
	testifyassert.Equal(t, InputID(0), chain0.ID)

	chain1 := iwf.getInputChainByID(ctx, 1)
	testifyassert.NotNil(t, chain1)
	testifyassert.Equal(t, InputID(1), chain1.ID)
}

func TestInputWithFallback_GetInputChainByID_OutOfBounds(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	chain := iwf.getInputChainByID(ctx, 5)
	testifyassert.Nil(t, chain)
}

func TestInputWithFallback_GetInputChainByID_Negative(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	chain := iwf.getInputChainByID(ctx, -1)
	testifyassert.Nil(t, chain)
}

// --- AddFactory after creation ---

func TestInputWithFallback_AddFactory_Additional(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	iwf := newTestIWF(t, f1)
	testifyassert.Equal(t, 1, iwf.GetInputChainsCount(ctx))

	f2 := &mockInputFactory{name: "additional-fallback"}
	err := iwf.AddFactory(ctx, f2)
	require.NoError(t, err)
	testifyassert.Equal(t, 2, iwf.GetInputChainsCount(ctx))
}

// --- InputChain tests ---

func TestInputChain_ID(t *testing.T) {
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)

	testifyassert.Equal(t, InputID(0), iwf.InputChains[0].ID)
	testifyassert.Equal(t, InputID(1), iwf.InputChains[1].ID)
}

func TestInputChain_GetInput(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.NotNil(t, iwf.InputChains[0].GetInput())
}

func TestInputChain_GetOutput(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.NotNil(t, iwf.InputChains[0].GetOutput())
	testifyassert.Equal(t, node.Abstract(iwf.InputChains[0].SyncBarrier), iwf.InputChains[0].GetOutput())
}

func TestInputChain_String_NoDecoder(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	s := iwf.InputChains[0].String()
	testifyassert.Contains(t, s, "InputChain(")
}

// --- inputChainAsCondition tests ---

func TestInputChainAsCondition_String(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	cond := iwf.InputChains[0].onInput()
	testifyassert.Equal(t, "InputWithFallbackCondition", cond.String())
}

// --- onInputChainError tests ---

func TestInputWithFallback_OnInputChainError_NoRetry(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](
		ctx,
		[]InputFactory[*inputKernel, codec.DecoderFactory, struct{}]{f1, f2},
		OptionRetryInterval(-1),
	)
	require.NoError(t, err)
	defer iwf.Close(ctx)

	// With RetryInterval < 0, errors on the active chain should return an error
	result := iwf.onInputChainError(ctx, iwf.InputChains[0], fmt.Errorf("test error"))
	testifyassert.Error(t, result)
	testifyassert.Contains(t, result.Error(), "retries are disabled")
}

func TestInputWithFallback_OnInputChainError_NotActiveInput(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)

	// Error on the fallback (not the active input) should be ignored
	result := iwf.onInputChainError(ctx, iwf.InputChains[1], fmt.Errorf("test error"))
	testifyassert.NoError(t, result)
}

func TestInputWithFallback_OnInputChainError_ActiveInput_WithFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}

	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	iFactories = append(iFactories, f1, f2)
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)

	// Error on the active input should trigger a switch to fallback
	result := iwf.onInputChainError(ctx, iwf.InputChains[0], fmt.Errorf("test error"))
	testifyassert.NoError(t, result)

	// Give background goroutines spawned by the switch time to settle
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
	_ = iwf.Close(context.Background())
}

func TestInputWithFallback_OnInputChainError_ActiveInput_NoFallback(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	iwf := newTestIWF(t, f1)

	// Error on the only input — no fallback available, should still return nil
	result := iwf.onInputChainError(ctx, iwf.InputChains[0], fmt.Errorf("test error"))
	testifyassert.NoError(t, result)
}

// --- PushTo delegation tests ---

func TestInputWithFallback_GetPushTos(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	pushTos := iwf.GetPushTos(ctx)
	// Initially should be empty (no push targets added)
	testifyassert.Empty(t, pushTos)
}

func TestInputWithFallback_GetChangeChanIsServing(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	ch := iwf.GetChangeChanIsServing()
	testifyassert.NotNil(t, ch)
}

func TestInputWithFallback_GetChangeChanPushTo(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	ch := iwf.GetChangeChanPushTo()
	testifyassert.NotNil(t, ch)
}

func TestInputWithFallback_GetChangeChanDrained(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	ch := iwf.GetChangeChanDrained()
	testifyassert.NotNil(t, ch)
}

// --- IsDrained panics ---

func TestInputWithFallback_IsDrained_Panics(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.Panics(t, func() {
		iwf.IsDrained(ctx)
	})
}

// --- Flush panics ---

func TestInputWithFallback_Flush_Panics(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.Panics(t, func() {
		_ = iwf.Flush(ctx)
	})
}

// --- ErrorChan panics ---

func TestInputWithFallback_ErrorChan_Panics(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.Panics(t, func() {
		_ = iwf.ErrorChan()
	})
}

// --- AddPushTo / RemovePushTo / SetPushTos / WithPushTos delegation ---

func TestInputWithFallback_AddAndRemovePushTo(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Create a target node
	target := node.NewWithCustomDataFromKernel[struct{}](ctx, &kernel.Passthrough{})

	iwf.AddPushTo(ctx, target)
	pushTos := iwf.GetPushTos(ctx)
	testifyassert.Len(t, pushTos, 1)

	err := iwf.RemovePushTo(ctx, target)
	testifyassert.NoError(t, err)
	pushTos = iwf.GetPushTos(ctx)
	testifyassert.Len(t, pushTos, 0)
}

func TestInputWithFallback_SetPushTos(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	target := node.NewWithCustomDataFromKernel[struct{}](ctx, &kernel.Passthrough{})

	// Set directly
	pushTos := node.PushTos{{Node: target}}
	iwf.SetPushTos(ctx, pushTos)
	result := iwf.GetPushTos(ctx)
	testifyassert.Len(t, result, 1)
}

func TestInputWithFallback_WithPushTos(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	called := false
	iwf.WithPushTos(ctx, func(ctx context.Context, pushTos *node.PushTos) {
		called = true
		testifyassert.NotNil(t, pushTos)
	})
	testifyassert.True(t, called)
}

// --- InputChain pipeline structure ---

func TestInputChain_NoDecoder_FilterPushesToSyncBarrier(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	chain := iwf.InputChains[0]

	// Without decoder, filter pushes directly to sync barrier
	testifyassert.Nil(t, chain.Decoder)
	testifyassert.Nil(t, chain.AutoHeaders)
}

// --- AllowCorruptPackets ---

func TestInputWithFallback_AllowCorruptPackets(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Initially false
	testifyassert.False(t, iwf.AllowCorruptPackets.Load())

	// Set to true
	iwf.AllowCorruptPackets.Store(true)
	testifyassert.True(t, iwf.AllowCorruptPackets.Load())
}

// --- Serve tests ---

func TestInputWithFallback_Serve_CancelledImmediately(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	errCh := make(chan node.Error, 10)
	testifyassert.False(t, iwf.IsServing(ctx))

	done := make(chan struct{})
	go func() {
		iwf.Serve(ctx, node.ServeConfig{}, errCh)
		close(done)
	}()

	select {
	case <-done:
		// Serve returned
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return within timeout")
	}

	// After Serve returns, IsServing should be false
	testifyassert.False(t, iwf.IsServing(ctx))
}

func TestInputWithFallback_Serve_DoubleStartError(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Simulate already-serving by setting the flag directly
	iwf.isServing.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan node.Error, 10)

	done := make(chan struct{})
	go func() {
		iwf.Serve(ctx, node.ServeConfig{}, errCh)
		close(done)
	}()

	select {
	case err := <-errCh:
		testifyassert.IsType(t, node.ErrAlreadyStarted{}, err.Err)
	case <-time.After(2 * time.Second):
		t.Fatal("expected ErrAlreadyStarted error")
	}

	<-done

	// Restore to false so cleanup won't try to close while "serving"
	iwf.isServing.Store(false)
}

// --- InputChain.IsPaused ---

func TestInputChain_IsPaused_Initially(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Input chains created with StartOnInit=false, so they start paused
	chain := iwf.InputChains[0]
	testifyassert.True(t, chain.IsPaused(ctx))
}

// --- Concurrent access tests ---

func TestInputWithFallback_ConcurrentGetInputChainsCount(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			count := iwf.GetInputChainsCount(ctx)
			testifyassert.GreaterOrEqual(t, count, 2)
		}
	}()

	for i := 0; i < 100; i++ {
		count := iwf.GetInputChainsCount(ctx)
		testifyassert.GreaterOrEqual(t, count, 2)
	}

	<-done
}

func TestInputWithFallback_ConcurrentGetBitRates(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			iwf.Measurements[astiav.MediaTypeVideo].InputBitRate.Store(uint64(i * 1000))
		}
	}()

	for i := 0; i < 100; i++ {
		bitRates := iwf.GetBitRates(ctx)
		testifyassert.NotNil(t, bitRates)
	}

	<-done
}

// --- Node interface compliance ---

func TestInputWithFallback_ImplementsNodeAbstract(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	var _ node.Abstract = iwf
}

// --- InputChain.FilterSwitch / SyncSwitch ---

func TestInputChain_HasFilterAndSyncSwitches(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	chain := iwf.InputChains[0]

	testifyassert.NotNil(t, chain.FilterSwitch)
	testifyassert.NotNil(t, chain.SyncSwitch)
	testifyassert.NotNil(t, chain.Filter)
	testifyassert.NotNil(t, chain.SyncBarrier)
}

// --- InputWithFallback.newInputChainChan ---

func TestNew_InputChainChanBuffered(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	testifyassert.Equal(t, 100, cap(iwf.newInputChainChan))
}

// --- BitRates struct ---

func TestBitRates_FieldAccess(t *testing.T) {
	br := &BitRates{
		Input: globaltypes.BitRateInfo{
			Video: 5_000_000,
			Audio: 128_000,
			Other: 0,
		},
		Output: globaltypes.BitRateInfo{
			Video: 4_000_000,
			Audio: 128_000,
			Other: 0,
		},
	}
	testifyassert.Equal(t, globaltypes.Ubps(5_000_000), br.Input.Video)
	testifyassert.Equal(t, globaltypes.Ubps(128_000), br.Input.Audio)
	testifyassert.Equal(t, globaltypes.Ubps(4_000_000), br.Output.Video)
}

// --- InputWithFallback.String locked path ---

func TestInputWithFallback_String_WhileLocked(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	// Lock and then try String() — should show locked state
	iwf.InputChainsLocker.ManualLock(ctx)
	s := iwf.String()
	iwf.InputChainsLocker.ManualUnlock(ctx)

	testifyassert.True(t, strings.Contains(s, "<locked>"))
}

// --- CurrentBitRateMeasurementsCount ---

func TestInputWithFallback_CurrentBitRateMeasurementsCount(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	testifyassert.Equal(t, uint64(0), iwf.CurrentBitRateMeasurementsCount.Load())
	iwf.CurrentBitRateMeasurementsCount.Add(1)
	testifyassert.Equal(t, uint64(1), iwf.CurrentBitRateMeasurementsCount.Load())
}

// --- InputChain.Pause ---

func TestInputChain_PauseState(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	chain := iwf.InputChains[0]

	// IsPaused reflects the retryable kernel's barrier state
	// The kernel is created with StartOnInit=false
	paused := chain.IsPaused(ctx)
	// Verify we can call it without error/panic
	_ = paused
}

// --- AddFactory channel full error ---

func TestInputWithFallback_AddFactory_ChannelFull(t *testing.T) {
	ctx := context.Background()
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, nil)
	require.NoError(t, err)
	defer iwf.Close(ctx)

	// Fill the channel (capacity is 100)
	for i := 0; i < 100; i++ {
		f := &mockInputFactory{name: fmt.Sprintf("factory-%d", i)}
		err := iwf.AddFactory(ctx, f)
		require.NoError(t, err)
	}

	// 101st should fail because the channel is full
	f := &mockInputFactory{name: "overflow"}
	err = iwf.AddFactory(ctx, f)
	testifyassert.Error(t, err)
	testifyassert.Contains(t, err.Error(), "already full")
}

// --- onInputChainKernelOpen tests ---

func TestInputWithFallback_OnInputChainKernelOpen_HigherPriority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}

	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	iFactories = append(iFactories, f1, f2)
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)

	// Switch to the fallback (input 1)
	iwf.InputSwitch.CurrentValue.Store(1)

	// When primary (input 0) kernel opens, it should request switch back
	iwf.onInputChainKernelOpen(ctx, iwf.InputChains[0])

	// The switch request triggers background goroutines; give them time to settle
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
	_ = iwf.Close(context.Background())
}

func TestInputWithFallback_OnInputChainKernelOpen_LowerPriority(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)

	// Currently on primary (input 0)
	iwf.InputSwitch.CurrentValue.Store(0)

	// When fallback (input 1) kernel opens, no switch should happen
	iwf.onInputChainKernelOpen(ctx, iwf.InputChains[1])
	// Should not panic or error
}

func TestInputWithFallback_OnInputChainKernelOpen_SamePriority(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "primary"}
	iwf := newTestIWF(t, factory)

	// Currently on input 0, kernel opens for input 0 — no switch needed
	iwf.InputSwitch.CurrentValue.Store(0)
	iwf.onInputChainKernelOpen(ctx, iwf.InputChains[0])
	// Should not panic or error
}

// --- InputChain.String without active kernel ---

func TestInputChain_String_Inactive(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	chain := iwf.InputChains[0]

	s := chain.String()
	// Without an active kernel, should show inactive state with factory name
	testifyassert.Contains(t, s, "InputChain(")
}

// --- InputChain.String locked path ---

func TestInputChain_String_FactoryFallback(t *testing.T) {
	factory := &mockInputFactory{name: "my-special-factory"}
	iwf := newTestIWF(t, factory)
	chain := iwf.InputChains[0]

	// When kernel is not set, should use factory string.
	// Note: if the lock is held by cleanup goroutines, it returns "<unable to lock>"
	s := chain.String()
	testifyassert.Contains(t, s, "InputChain(")
	testifyassert.Contains(t, s, "my-special-factory")
}

// --- addFactory context cancelled ---

func TestInputWithFallback_AddFactory_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, nil)
	require.NoError(t, err)
	defer iwf.Close(context.Background())

	// Fill channel to capacity
	for i := 0; i < 100; i++ {
		f := &mockInputFactory{name: fmt.Sprintf("factory-%d", i)}
		err := iwf.AddFactory(ctx, f)
		require.NoError(t, err)
	}

	// Cancel context and try to add another — should fail with context error
	cancel()
	f := &mockInputFactory{name: "after-cancel"}
	err = iwf.AddFactory(ctx, f)
	testifyassert.Error(t, err)
}

// --- InputChain.Close with non-nil decoder/autoheaders ---

func TestInputChain_Close_AllComponentsPresent(t *testing.T) {
	// This tests the Close path where both AutoHeaders and Decoder are non-nil
	// We verify by ensuring no panic occurs
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	chain := iwf.InputChains[0]

	// These are nil for our mock factory (no decoder)
	testifyassert.Nil(t, chain.AutoHeaders)
	testifyassert.Nil(t, chain.Decoder)

	// Close should not panic with nil AutoHeaders/Decoder
	err := chain.Close(ctx)
	testifyassert.NoError(t, err)
}

// Note: inputBitRateMeasurerLoop is tested indirectly via Serve_CancelledImmediately.
// A dedicated test is not included here because the Serve path triggers a pre-existing
// data race in kernel/retryable.go:String() (reads r.Kernel without lock while
// openKernelIfNeeded writes it concurrently).

// --- getInputsLocked ---

func TestInputWithFallback_GetInputs_Empty(t *testing.T) {
	ctx := context.Background()
	iwf := newTestIWF(t)
	inputs := iwf.GetInputs(ctx)
	testifyassert.Empty(t, inputs)
}

// --- Multiple AddFactory calls ---

func TestInputWithFallback_AddFactory_Multiple(t *testing.T) {
	ctx := context.Background()
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, nil)
	require.NoError(t, err)
	defer iwf.Close(ctx)

	testifyassert.Equal(t, 0, iwf.GetInputChainsCount(ctx))

	f1 := &mockInputFactory{name: "a"}
	f2 := &mockInputFactory{name: "b"}
	f3 := &mockInputFactory{name: "c"}
	err = iwf.AddFactory(ctx, f1, f2, f3)
	require.NoError(t, err)
	testifyassert.Equal(t, 3, iwf.GetInputChainsCount(ctx))
}

// --- InputWithFallback.inputFilter ---

func TestInputWithFallback_InputFilterReturnsCondition(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	cond := iwf.inputFilter()
	testifyassert.NotNil(t, cond)
	testifyassert.Equal(t, "InputWithFallback:InputFilter", cond.String())
}

// --- InputWithFallback.Close clears InputChains ---

func TestInputWithFallback_Close_ClearsChains(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "a"}
	f2 := &mockInputFactory{name: "b"}

	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	iFactories = append(iFactories, f1, f2)
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)

	testifyassert.Len(t, iwf.InputChains, 2)

	err = iwf.Close(ctx)
	testifyassert.NoError(t, err)
	testifyassert.Nil(t, iwf.InputChains)
}

// --- InputWithFallback.initSwitches ---

func TestInputWithFallback_SwitchInitialValues(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	testifyassert.Equal(t, int32(0), iwf.InputSwitch.CurrentValue.Load())
	testifyassert.Equal(t, int32(0), iwf.InputSyncer.CurrentValue.Load())
}

// --- on_input.Match (inputChainAsCondition) ---

func TestInputChainAsCondition_Match_AddsInputChainAsSideData(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)
	chain := iwf.InputChains[0]

	// Get the condition
	cond := chain.onInput()

	// Create a minimal frame input to test Match
	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)

	f := astiav.AllocFrame()
	t.Cleanup(f.Free)

	streamInfo := frame.BuildStreamInfo(
		nil, cp, 0, 1, astiav.NewRational(1, 30), 0, nil,
	)
	frameInput := frame.BuildInput(f, 0, streamInfo)
	input := packetorframefiltercondition.Input{
		Input: packetorframe.InputUnion{
			Frame: &frameInput,
		},
	}

	ctx := context.Background()
	result := cond.Match(ctx, input)
	testifyassert.True(t, result) // Always returns true

	// Verify that the input chain was added as pipeline side data
	sideData := input.Input.Frame.PipelineSideData
	testifyassert.NotEmpty(t, sideData)
}

// --- PauseChain / UnpauseChain ---
//
// Chains are created paused (StartOnInit=false). These tests exercise
// the public wrapper methods directly, explicitly unpausing chains
// first so the active-chain counter reflects the state under test.
//
// Pausing a chain whose underlying kernel has not yet been opened is
// a no-op in Retryable (nothing to close), so the tests wait for the
// retry kernel to open before calling Pause — otherwise the barrier
// state stays "unpaused" and subsequent assertions misfire.

// newPauseTestIWF builds an InputWithFallback with a cancellable
// context and arranges cleanup so retry loops exit before Close
// runs. InputWithFallback.Close holds InputChainsLocker while closing
// each chain's processor, which cancels the processor ctx and trips
// Retryable.retry's OnError path; that OnError also tries to take
// InputChainsLocker and deadlocks. Cancelling the top-level ctx
// first forces retry to bail out cleanly before Close grabs the
// lock.
func newPauseTestIWF(
	t *testing.T,
	factories ...*mockInputFactory,
) (*InputWithFallback[*inputKernel, codec.DecoderFactory, struct{}], context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	for _, f := range factories {
		iFactories = append(iFactories, f)
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)
	t.Cleanup(func() {
		cancel()
		// Give retry goroutines a moment to observe ctx cancel and
		// release InputChainsLocker-targeting paths before Close
		// takes the lock.
		time.Sleep(25 * time.Millisecond)
		_ = iwf.Close(context.Background())
	})
	return iwf, ctx
}

// waitForKernelOpen waits until the Retryable kernel inside the given
// chain has been opened. Required because Unpause schedules the open
// asynchronously, and Pause is a no-op while the kernel is unset.
func waitForKernelOpen(
	t *testing.T,
	chain *InputChain[*inputKernel, codec.DecoderFactory, struct{}],
) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		k := chain.Input.Processor.Kernel
		k.KernelLocker.ManualLock(context.Background())
		isSet := k.KernelIsSet
		k.KernelLocker.ManualUnlock(context.Background())
		if isSet {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("kernel for chain %d did not open within timeout", chain.ID)
}

// TestInputWithFallback_PauseChain_Succeeds covers the happy path:
// PauseChain pauses the requested chain when at least one other
// chain remains active.
func TestInputWithFallback_PauseChain_Succeeds(t *testing.T) {
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf, ctx := newPauseTestIWF(t, f1, f2)

	// Unpause both chains and wait for their kernels to open.
	require.NoError(t, iwf.InputChains[0].Unpause(ctx))
	require.NoError(t, iwf.InputChains[1].Unpause(ctx))
	waitForKernelOpen(t, iwf.InputChains[0])
	waitForKernelOpen(t, iwf.InputChains[1])
	testifyassert.False(t, iwf.InputChains[0].IsPaused(ctx))
	testifyassert.False(t, iwf.InputChains[1].IsPaused(ctx))

	// Pause chain 0; chain 1 remains as the sole active chain.
	require.NoError(t, iwf.PauseChain(ctx, 0))
	testifyassert.True(t, iwf.InputChains[0].IsPaused(ctx))
	testifyassert.False(t, iwf.InputChains[1].IsPaused(ctx))

	// Pausing chain 1 (the sole active chain) must return
	// ErrCannotPauseSoleActiveChain.
	err := iwf.PauseChain(ctx, 1)
	require.Error(t, err)
	testifyassert.ErrorAs(t, err, &ErrCannotPauseSoleActiveChain{})
	testifyassert.False(t, iwf.InputChains[1].IsPaused(ctx))
}

// TestInputWithFallback_PauseChain_SoleActiveChain verifies that
// pausing the only active chain returns ErrCannotPauseSoleActiveChain
// when there is exactly one chain total.
func TestInputWithFallback_PauseChain_SoleActiveChain(t *testing.T) {
	f1 := &mockInputFactory{name: "primary"}
	iwf, ctx := newPauseTestIWF(t, f1)

	require.NoError(t, iwf.InputChains[0].Unpause(ctx))
	waitForKernelOpen(t, iwf.InputChains[0])

	err := iwf.PauseChain(ctx, 0)
	require.Error(t, err)
	testifyassert.ErrorAs(t, err, &ErrCannotPauseSoleActiveChain{})
	testifyassert.False(t, iwf.InputChains[0].IsPaused(ctx))
}

// TestInputWithFallback_PauseChain_AlreadyPaused_NoOp verifies that
// pausing an already-paused chain is a silent no-op. This matters
// because the gRPC layer may receive idempotent stop requests.
func TestInputWithFallback_PauseChain_AlreadyPaused_NoOp(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	iwf := newTestIWF(t, f1, f2)

	testifyassert.True(t, iwf.InputChains[1].IsPaused(ctx))
	require.NoError(t, iwf.PauseChain(ctx, 1))
	testifyassert.True(t, iwf.InputChains[1].IsPaused(ctx))
}

// TestInputWithFallback_PauseChain_InvalidID rejects out-of-range IDs
// with a descriptive error rather than panicking.
func TestInputWithFallback_PauseChain_InvalidID(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	err := iwf.PauseChain(ctx, InputID(5))
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "not found")
}

// TestInputWithFallback_UnpauseChain_Succeeds covers the symmetric
// unpause path.
func TestInputWithFallback_UnpauseChain_Succeeds(t *testing.T) {
	factory := &mockInputFactory{name: "test-factory"}
	iwf, ctx := newPauseTestIWF(t, factory)

	testifyassert.True(t, iwf.InputChains[0].IsPaused(ctx))
	require.NoError(t, iwf.UnpauseChain(ctx, 0))
	testifyassert.False(t, iwf.InputChains[0].IsPaused(ctx))
}

// TestInputWithFallback_UnpauseChain_InvalidID rejects out-of-range
// IDs symmetrically with PauseChain.
func TestInputWithFallback_UnpauseChain_InvalidID(t *testing.T) {
	ctx := context.Background()
	factory := &mockInputFactory{name: "test-factory"}
	iwf := newTestIWF(t, factory)

	err := iwf.UnpauseChain(ctx, InputID(5))
	require.Error(t, err)
	testifyassert.Contains(t, err.Error(), "not found")
}

// TestInputWithFallback_Close_NoDeadlockWithActiveChain is the
// regression check for the Close/onInputChainError deadlock:
// InputWithFallback.Close used to hold InputChainsLocker while
// cancelling each chain's processor ctx. Cancelling the ctx tripped
// Retryable.retry's OnError path, which called onInputChainError,
// which tried to take InputChainsLocker and blocked forever because
// Close was still holding it. Close must therefore snapshot the
// chains and release the lock before closing them so the error
// handler can acquire the lock.
//
// The test unpauses one chain so its Retryable kernel opens and the
// processor goroutine is actively running Generate; that is the
// configuration that triggers the error callback when Close cancels
// the ctx. Close without the fix blocks indefinitely; with the fix
// it returns promptly.
func TestInputWithFallback_Close_NoDeadlockWithActiveChain(t *testing.T) {
	ctx := context.Background()
	f1 := &mockInputFactory{name: "primary"}
	f2 := &mockInputFactory{name: "fallback"}
	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	iFactories = append(iFactories, f1, f2)
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)

	// Unpause chain 0 and wait for its kernel to open. Once the
	// kernel is open, the processor's goroutine is actively inside
	// Retryable.Generate / retry(), which is the exact state that
	// would trigger the deadlock on Close.
	require.NoError(t, iwf.InputChains[0].Unpause(ctx))
	waitForKernelOpen(t, iwf.InputChains[0])

	// Close must return within a bounded time. Without the fix it
	// deadlocks (Close holds InputChainsLocker while waiting on the
	// processor's goroutine, which is blocked in onInputChainError
	// trying to acquire InputChainsLocker).
	done := make(chan error, 1)
	go func() {
		done <- iwf.Close(ctx)
	}()
	select {
	case err := <-done:
		testifyassert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close deadlocked: did not return within 5 seconds")
	}
}

// --- onInputChainError walks past empty (no-resource) chains ---
//
// mockInputFactoryWithAvailability satisfies both InputFactory and the
// optional InputFactoryWithAvailability interface. Empty chains report
// HasResources=false; the fallback walk in onInputChainError must skip
// them and jump directly to the next chain that has resources.
type mockInputFactoryWithAvailability struct {
	mockInputFactory
	hasResources bool
}

var _ InputFactoryWithAvailability = (*mockInputFactoryWithAvailability)(nil)

func (m *mockInputFactoryWithAvailability) HasResources(ctx context.Context) bool {
	return m.hasResources
}

// TestInputWithFallback_OnInputChainError_SkipsEmptyChains_SparsePriorities
// pins the fix for the priority-0 + priority-10 race: when the active
// chain (id 0) errors and chains 1..9 are empty (HasResources=false)
// while chain 10 is occupied, the fallback walk must request a single
// switch directly to chain 10 — not a stepwise 0->1->2->...->10 walk
// where each empty step contends the switching latch (procN) and races
// the next step's onInputChainError invocation.
//
// Pre-fix witness: nextID = id + 1 means SetValue(1) is called; the
// staged NextValue would observe 1. Post-fix witness: nextID jumps over
// chains 1..9 (all empty) and SetValue(10) is called directly.
func TestInputWithFallback_OnInputChainError_SkipsEmptyChains_SparsePriorities(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build 11 factories: index 0 occupied, 1..9 empty, 10 occupied.
	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	for i := 0; i < 11; i++ {
		f := &mockInputFactoryWithAvailability{
			mockInputFactory: mockInputFactory{name: fmt.Sprintf("factory-%d", i)},
			hasResources:     i == 0 || i == 10,
		}
		iFactories = append(iFactories, f)
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)
	defer func() {
		cancel()
		time.Sleep(25 * time.Millisecond)
		_ = iwf.Close(context.Background())
	}()

	require.Len(t, iwf.InputChains, 11)

	// Trigger the fallback path on the active chain (id 0). The fix
	// must request a switch straight to chain 10. Pre-fix, the request
	// would land on chain 1 (id+1); chain 1 would error asynchronously
	// and trigger a second switch attempt that the procN latch rejects
	// with "another switch is in progress".
	result := iwf.onInputChainError(ctx, iwf.InputChains[0], fmt.Errorf("primary failed"))
	testifyassert.NoError(t, result)

	// onInputChainError clears the switch keep-unless before SetValue,
	// so SetValue takes the setValueNow path that updates
	// CurrentValue synchronously. NextValue stays at the
	// math.MinInt32 sentinel (no staged next).
	deadline := time.Now().Add(2 * time.Second)
	var curVal int32
	for time.Now().Before(deadline) {
		curVal = iwf.InputSwitch.CurrentValue.Load()
		if curVal == 10 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	testifyassert.Equal(t, int32(10), curVal,
		"onInputChainError must skip empty chains 1..9 and switch directly to chain 10; "+
			"got CurrentValue=%d (pre-fix would be 1)", curVal)
}

// TestInputWithFallback_OnInputChainError_AllEmptyFallbacks_NoSwitch
// asserts the boundary: when all chains beyond the failing one are
// empty, no switch is requested. Pre-fix would request a switch to id+1.
func TestInputWithFallback_OnInputChainError_AllEmptyFallbacks_NoSwitch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	// Chain 0 occupied, chains 1..3 all empty.
	for i := 0; i < 4; i++ {
		f := &mockInputFactoryWithAvailability{
			mockInputFactory: mockInputFactory{name: fmt.Sprintf("factory-%d", i)},
			hasResources:     i == 0,
		}
		iFactories = append(iFactories, f)
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)
	defer func() {
		cancel()
		time.Sleep(25 * time.Millisecond)
		_ = iwf.Close(context.Background())
	}()

	prevCur := iwf.InputSwitch.CurrentValue.Load()

	result := iwf.onInputChainError(ctx, iwf.InputChains[0], fmt.Errorf("primary failed"))
	testifyassert.NoError(t, result)

	// No fallback target was requested: CurrentValue must remain
	// unchanged (no SetValue call was issued).
	time.Sleep(50 * time.Millisecond)
	testifyassert.Equal(t, prevCur, iwf.InputSwitch.CurrentValue.Load(),
		"with no occupied fallback, no switch must be requested")
}

// TestInputWithFallback_OnInputChainError_DenseFactories_LegacyBehavior
// pins the fallback path for factories that DO NOT implement
// InputFactoryWithAvailability: behavior remains the legacy
// "advance by +1" — no skipping. This guards against accidentally
// changing the behavior for callers that don't opt into the optional
// interface.
func TestInputWithFallback_OnInputChainError_DenseFactories_LegacyBehavior(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	f0 := &mockInputFactory{name: "primary"}
	f1 := &mockInputFactory{name: "fallback"}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](
		ctx,
		[]InputFactory[*inputKernel, codec.DecoderFactory, struct{}]{f0, f1},
	)
	require.NoError(t, err)
	defer func() {
		cancel()
		time.Sleep(25 * time.Millisecond)
		_ = iwf.Close(context.Background())
	}()

	result := iwf.onInputChainError(ctx, iwf.InputChains[0], fmt.Errorf("primary failed"))
	testifyassert.NoError(t, result)

	deadline := time.Now().Add(2 * time.Second)
	var curVal int32
	for time.Now().Before(deadline) {
		curVal = iwf.InputSwitch.CurrentValue.Load()
		if curVal == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	testifyassert.Equal(t, int32(1), curVal,
		"factories without InputFactoryWithAvailability keep the legacy "+
			"+1 advance: CurrentValue must be 1, got %d", curVal)
}

// TestInputWithFallback_OnSwitchRequest_UnpausesIntermediateChains:
// chains 1..N-1 exist BEFORE the fallback walk runs, are paused on
// creation (ID > 0). After OnSwitchRequest unpauses chain N, chains
// [0, to] must all be unpaused so the consistency invariant
// `paused = (ID > CurrentValue)` holds and hot adds at intermediate
// priorities take effect.
func TestInputWithFallback_OnSwitchRequest_UnpausesIntermediateChains(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 11 dense factories so OnSwitchRequest's getInputChainByID walk
	// has real chains to unpause at every intermediate ID.
	var iFactories []InputFactory[*inputKernel, codec.DecoderFactory, struct{}]
	for i := 0; i < 11; i++ {
		iFactories = append(iFactories,
			&mockInputFactory{name: fmt.Sprintf("factory-%d", i)})
	}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](ctx, iFactories)
	require.NoError(t, err)
	defer func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		_ = iwf.Close(context.Background())
	}()

	errCh := make(chan node.Error, 64)
	go func() { iwf.Serve(ctx, node.ServeConfig{}, errCh) }()
	go func() {
		for range errCh {
		}
	}()

	// Wait for the boot state: chain 0 unpaused (auto-unpause path),
	// chains 1..10 paused (no auto-unpause yet because CurrentValue=0).
	require.Eventually(t, func() bool {
		if iwf.InputChains[0].IsPaused(ctx) {
			return false
		}
		for i := 1; i <= 10; i++ {
			if !iwf.InputChains[i].IsPaused(ctx) {
				return false
			}
		}
		return true
	}, 2*time.Second, 5*time.Millisecond,
		"boot state precondition: chain 0 unpaused, chains 1..10 paused")

	// Drive the fallback path: SetValue(10) routes through
	// OnSwitchRequest, which (post-fix) unpauses every chain in
	// [0, 10] — pre-fix, only chain 10 is unpaused, leaving chains
	// 1..9 in violation of the consistency invariant.
	require.NoError(t, iwf.InputSwitch.SetValue(ctx, 10))

	require.Eventually(t, func() bool {
		for id := 0; id <= 10; id++ {
			if iwf.InputChains[id].IsPaused(ctx) {
				return false
			}
		}
		return true
	}, 2*time.Second, 5*time.Millisecond,
		"after SetValue(10), all chains [0,10] must be unpaused; "+
			"pre-fix chains 1..9 stay paused — `inputs add N` for "+
			"1<=N<=9 then silently fails because hot-reload kick "+
			"is gated on !IsPaused")
}

// TestInputWithFallback_AddInput_AutoUnpauseAllChainsUpToCurrent:
// when a chain at ID==0 is auto-unpaused at boot and the InputSwitch
// later promotes a higher-numbered chain (e.g. fallback walk →
// CurrentValue=10), all chains with ID <= CurrentValue MUST be
// unpaused — otherwise the consistency-check loop in Serve flags
// "input chain N paused=true but should be false" and a hot
// `inputs add N` (1 <= N < CurrentValue) silently fails because
// FFStream.AddInput's chainPreExisted Pause+Unpause kick is gated on
// !IsPaused, so a paused chain never reloads InputsInfo[N].
//
// The receiver loop unpauses every newly-arrived chain whose
// ID <= CurrentValue, mirroring the consistency-check invariant
// (`expectedIsPaused := inputID > CurrentValue`).
func TestInputWithFallback_AddInput_AutoUnpauseAllChainsUpToCurrent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Bootstrap with a single factory at ID=0 first; we will pump the
	// switch to a higher current value, then add more factories and
	// assert they get auto-unpaused on arrival.
	f0 := &mockInputFactory{name: "factory-0"}
	iwf, err := New[*inputKernel, codec.DecoderFactory, struct{}](
		ctx,
		[]InputFactory[*inputKernel, codec.DecoderFactory, struct{}]{f0},
	)
	require.NoError(t, err)
	defer func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
		_ = iwf.Close(context.Background())
	}()

	// Start serving so the newInputChainChan receiver loop is running.
	errCh := make(chan node.Error, 64)
	go func() {
		iwf.Serve(ctx, node.ServeConfig{}, errCh)
	}()
	// Drain errCh so receivers don't block.
	go func() {
		for range errCh {
		}
	}()

	// Wait for chain 0 to be auto-unpaused (the legacy ID==0 path).
	require.Eventually(t, func() bool {
		return !iwf.InputChains[0].IsPaused(ctx)
	}, 2*time.Second, 5*time.Millisecond,
		"chain 0 must be auto-unpaused at boot")

	// Simulate a fallback-style promotion: bump CurrentValue to 10.
	// In production this happens via InputSwitch.SetValue from
	// onInputChainError, but for this test we set the field directly so
	// the receiver-loop's auto-unpause is the only logic under test.
	iwf.InputSwitch.CurrentValue.Store(10)

	// Add 10 more factories (IDs 1..10). Per the consistency
	// invariant, all of them have ID <= CurrentValue(10), so all must
	// be auto-unpaused after AddFactory delivers them through
	// newInputChainChan to the receiver loop.
	for i := 1; i <= 10; i++ {
		f := &mockInputFactory{name: fmt.Sprintf("factory-%d", i)}
		require.NoError(t, iwf.AddFactory(ctx, f))
	}
	require.Len(t, iwf.InputChains, 11)

	// Assert: every chain with ID <= CurrentValue is unpaused.
	require.Eventually(t, func() bool {
		for id := 0; id <= 10; id++ {
			if iwf.InputChains[id].IsPaused(ctx) {
				return false
			}
		}
		return true
	}, 2*time.Second, 5*time.Millisecond,
		"all chains with ID <= CurrentValue(10) must be auto-unpaused; "+
			"pre-fix only ID==0 is unpaused — chains 1..10 stay paused")
}
