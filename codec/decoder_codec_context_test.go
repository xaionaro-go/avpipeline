package codec

import (
	"context"
	"testing"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/stretchr/testify/require"
	"github.com/xaionaro-go/observability"
)

func TestDecoder_CodecContextIfAvailableDoesNotBlockWhenLocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(320)
	cp.SetHeight(240)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dec.Close(ctx)) })

	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	observability.Go(ctx, func(ctx context.Context) {
		done <- dec.LockDo(ctx, func(ctx context.Context, dl *DecoderLocked) error {
			close(locked)
			<-release
			return nil
		})
	})

	requireSignal(t, locked)

	cc, ok := dec.CodecContextIfAvailable(ctx)
	require.False(t, ok)
	require.Nil(t, cc)

	close(release)
	require.NoError(t, requireResult(t, done))

	cc, ok = dec.CodecContextIfAvailable(ctx)
	require.True(t, ok)
	require.NotNil(t, cc)
}

func TestNaiveDecoderFactory_GetResourcesDoesNotWaitForLockedDecoder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cp := astiav.AllocCodecParameters()
	t.Cleanup(cp.Free)
	cp.SetMediaType(astiav.MediaTypeVideo)
	cp.SetCodecID(astiav.CodecIDH264)
	cp.SetWidth(320)
	cp.SetHeight(240)

	dec, err := NewDecoder(ctx, DecoderInput{CodecParameters: cp})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, dec.Close(ctx)) })

	locked := make(chan struct{})
	release := make(chan struct{})
	lockDone := make(chan error, 1)

	observability.Go(ctx, func(ctx context.Context) {
		lockDone <- dec.LockDo(ctx, func(ctx context.Context, dl *DecoderLocked) error {
			close(locked)
			<-release
			return nil
		})
	})
	requireSignal(t, locked)

	resourcesDone := make(chan *Resources, 1)
	factory := &NaiveDecoderFactory{VideoDecoders: []*Decoder{dec}}
	observability.Go(ctx, func(ctx context.Context) {
		resourcesDone <- factory.GetResources(
			ctx,
			true,
			cp,
			astiav.NewRational(1, 30),
			EncoderFactoryOptionGetDecoderer{GetDecoderer: staticDecoderGetter{decoder: dec}},
		)
	})

	require.Nil(t, requireResult(t, resourcesDone))

	close(release)
	require.NoError(t, requireResult(t, lockDone))
}

type staticDecoderGetter struct {
	decoder *Decoder
}

func (g staticDecoderGetter) GetDecoder() *Decoder {
	return g.decoder
}

func requireSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	select {
	case <-ch:
	case <-timer.C:
		t.Fatal("timed out waiting for signal")
	}
}

func requireResult[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	select {
	case result := <-ch:
		return result
	case <-timer.C:
		t.Fatal("timed out waiting for result")
	}

	var zero T
	return zero
}
