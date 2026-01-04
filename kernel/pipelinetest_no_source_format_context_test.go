// pipelinetest_no_source_format_context_test.go contains tests for the pipeline without source format context.

package kernel_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/avpipeline"
	"github.com/xaionaro-go/avpipeline/kernel"
	"github.com/xaionaro-go/avpipeline/node"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/secret"
)

// mockRetryableNotReady simulates a Retryable where the kernel is not ready
// (e.g., Factory blocked due to poor connection). WithOutputFormatContext
// doesn't call the callback in this state.
type mockRetryableNotReady struct{}

func (m *mockRetryableNotReady) WithOutputFormatContext(
	ctx context.Context,
	callback func(*astiav.FormatContext),
) {
	// Intentionally not calling callback - simulates kernel not ready
}

func (m *mockRetryableNotReady) String() string {
	return "mockRetryableNotReady"
}

var _ packet.Source = (*mockRetryableNotReady)(nil)

// generatorKernel generates packets with a custom Source.
type generatorKernel struct {
	Source      packet.Source
	Stream      *astiav.Stream
	PacketCount int
	generated   int
	closeCh     chan struct{}
}

func newGeneratorKernel(source packet.Source, stream *astiav.Stream, count int) *generatorKernel {
	return &generatorKernel{
		Source:      source,
		Stream:      stream,
		PacketCount: count,
		closeCh:     make(chan struct{}),
	}
}

func (g *generatorKernel) String() string {
	return "generatorKernel"
}

func (g *generatorKernel) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	return nil
}

func (g *generatorKernel) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	for g.generated < g.PacketCount {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.closeCh:
			return nil
		default:
		}

		pkt := packet.Pool.Get()
		pkt.SetStreamIndex(0)

		pktOut := packet.BuildOutput(pkt, &packet.StreamInfo{
			Stream: g.Stream,
			Source: g.Source,
		})
		select {
		case outputCh <- packetorframe.OutputUnion{
			Packet: &pktOut,
		}:
			g.generated++
		case <-ctx.Done():
			pkt.Free()
			return ctx.Err()
		case <-g.closeCh:
			pkt.Free()
			return nil
		}
	}
	return nil
}

func (g *generatorKernel) Close(ctx context.Context) error {
	select {
	case <-g.closeCh:
	default:
		close(g.closeCh)
	}
	return nil
}

func (g *generatorKernel) CloseChan() <-chan struct{} {
	return g.closeCh
}

func (g *generatorKernel) GetObjectID() types.ObjectID {
	return types.GetObjectID(g)
}

var _ kernel.Abstract = (*generatorKernel)(nil)

// TestPipeline_ErrNoSourceFormatContext tests the production scenario at pipeline level:
// 1. Generator node creates packets with Source = mockRetryableNotReady
// 2. Packets flow through node connections to Output node
// 3. Output.SendInput calls Source.WithOutputFormatContext
// 4. mockRetryableNotReady.WithOutputFormatContext doesn't call callback
// 5. ErrNoSourceFormatContext is raised (or ignored with option)
func TestPipeline_ErrNoSourceFormatContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create output with null format
	output, err := kernel.NewOutputFromURL(ctx, "", secret.New(""), kernel.OutputConfig{
		IgnoreNoSourceFormatCtxErrors: true,
		CustomOptions: types.DictionaryItems{{
			Key:   "f",
			Value: "null",
		}},
	})
	require.NoError(t, err)
	defer output.Close(ctx)

	stream := output.FormatContext.NewStream(astiav.FindEncoder(astiav.CodecIDH264))

	// Create generator kernel with mock source
	mockSource := &mockRetryableNotReady{}
	generator := newGeneratorKernel(mockSource, stream, 5)

	// Create nodes
	generatorNode := node.NewFromKernel(ctx, generator)
	outputNode := node.NewFromKernel(ctx, output)

	// Connect: generator -> output (packets flow through node connection)
	generatorNode.AddPushTo(ctx, outputNode)

	// Setup error channel
	errCh := make(chan node.Error, 100)

	// Run pipeline
	var wg sync.WaitGroup
	wg.Add(1)
	observability.Go(ctx, func(ctx context.Context) {
		defer wg.Done()
		avpipeline.Serve(ctx, avpipeline.ServeConfig{}, errCh, generatorNode)
	})

	// Wait for generator to finish
	for generator.generated < generator.PacketCount {
		select {
		case <-ctx.Done():
			t.Fatal("context cancelled before generator finished")
		default:
		}
	}

	// Stop pipeline
	cancel()
	wg.Wait()

	// Drain error channel
	close(errCh)
	for nodeErr := range errCh {
		var noSrcErr kernel.ErrNoSourceFormatContext
		require.False(t, errors.As(nodeErr.Err, &noSrcErr),
			"should not see ErrNoSourceFormatContext with ignore enabled, got: %v", nodeErr.Err)
	}
}
