package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/asticode/go-astiav"
	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/logger"
	"github.com/xaionaro-go/avpipeline/packet"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/observability"
)

// Tee duplicates incoming packets and frames to multiple underlying kernels,
// and collects their outputs (including Generate).
type Tee[K Abstract] []K

var _ Abstract = (*Tee[Abstract])(nil)
var _ packet.Source = (*Tee[Abstract])(nil)
var _ packet.Sink = (*Tee[Abstract])(nil)
var _ types.OriginalPacketSourcer = (*Tee[Abstract])(nil)

// OriginalPacketSource returns the first kernel that has an actual packet source.
// All Tee children receive the same input, so they should have equivalent format contexts.
func (t Tee[K]) OriginalPacketSource() packet.Source {
	for _, k := range t {
		src, ok := any(k).(packet.Source)
		if !ok {
			continue
		}
		if result := types.GetOriginalPacketSource(src); result != nil {
			return result
		}
	}
	return nil
}

func (t Tee[K]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	errCh := make(chan error, len(t))
	var wg sync.WaitGroup
	for _, k := range t {
		k := k
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer logger.Tracef(ctx, "Tee.SendInputPacket: packet/frame sender for child kernel ended")
			pkt := input.Packet
			if pkt != nil {
				pkt = packet.CloneAsReferenced(pkt)
			}
			if err := k.SendInputPacket(ctx,
				packet.BuildInput(
					pkt,
					input.StreamInfo,
				),
				outputPacketsCh, outputFramesCh,
			); err != nil {
				errCh <- fmt.Errorf("tee send input packet error from kernel %v:%v: %w", k.GetObjectID(), k, err)
			}
		})
	}
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (t Tee[K]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	errCh := make(chan error, len(t))
	var wg sync.WaitGroup
	for _, k := range t {
		k := k
		wg.Add(1)
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			defer logger.Tracef(ctx, "Tee.SendInputFrame: packet/frame sender for child kernel ended")
			f := input.Frame
			if f != nil {
				f = frame.CloneAsReferenced(f)
			}
			if err := k.SendInputFrame(ctx,
				frame.BuildInput(
					f,
					input.Pos,
					input.StreamInfo,
				),
				outputPacketsCh, outputFramesCh,
			); err != nil {
				errCh <- fmt.Errorf("tee send input frame error from kernel %v:%v: %w", k.GetObjectID(), k, err)
			}
		})
	}
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (t Tee[K]) Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(t))
	var wg sync.WaitGroup
	for _, k := range t {
		wg.Add(1)
		k := k
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			err := k.Generate(ctx, outputPacketsCh, outputFramesCh)
			if err == nil {
				return
			}
			defer cancel()
			select {
			case errCh <- fmt.Errorf("tee generate error from kernel %v:%v: %w", k.GetObjectID(), k, err):
			case <-ctx.Done():
			}
		})
	}
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (t Tee[K]) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(unsafe.SliceData(t))
}

func (t Tee[K]) String() string {
	var parts []string
	for _, k := range t {
		parts = append(parts, k.String())
	}
	return fmt.Sprintf("Tee(%s)", strings.Join(parts, ","))
}

func (t Tee[K]) Close(ctx context.Context) error {
	if len(t) == 0 {
		return nil
	}
	errCh := make(chan error, len(t))
	var wg sync.WaitGroup
	for _, k := range t {
		wg.Add(1)
		k := k
		observability.Go(ctx, func(ctx context.Context) {
			defer wg.Done()
			if err := k.Close(ctx); err != nil {
				select {
				case errCh <- fmt.Errorf("tee close error from kernel %v:%v: %w", k.GetObjectID(), k, err):
				case <-ctx.Done():
				default:
					logger.Errorf(ctx, "Tee.Close: the error queue is full, which is supposed to be impossible; dropping error: %v", err)
				}
			}
		})
	}
	observability.Go(ctx, func(ctx context.Context) {
		wg.Wait()
		close(errCh)
	})
	var errs []error
	for err := range errCh {
		if err == nil {
			continue
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (t Tee[K]) CloseChan() <-chan struct{} {
	if len(t) == 0 {
		return nil
	}
	// collect non-nil child channels
	var chans []<-chan struct{}
	for _, k := range t {
		ch := k.CloseChan()
		if ch != nil {
			chans = append(chans, ch)
		}
	}
	if len(chans) == 0 {
		return nil
	}
	merged := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for _, ch := range chans {
			wg.Add(1)
			ch := ch
			go func() {
				defer wg.Done()
				<-ch
			}()
		}
		wg.Wait()
		close(merged)
	}()
	return merged
}

// WithOutputFormatContext implements packet.Source: calls every child
// kernel's WithOutputFormatContext that provides a format context.
func (t Tee[K]) WithOutputFormatContext(ctx context.Context, callback func(*astiav.FormatContext)) {
	for _, k := range t {
		src, ok := any(k).(packet.Source)
		if !ok {
			continue
		}
		src.WithOutputFormatContext(ctx, callback)
	}
}

// WithInputFormatContext implements packet.Sink: calls every child
// kernel's WithInputFormatContext that provides a format context.
func (t Tee[K]) WithInputFormatContext(ctx context.Context, callback func(*astiav.FormatContext)) {
	for _, k := range t {
		sink, ok := any(k).(packet.Sink)
		if !ok {
			continue
		}
		sink.WithInputFormatContext(ctx, callback)
	}
}

// NotifyAboutPacketSource implements packet.Sink: notify all children that are sinks.
func (t Tee[K]) NotifyAboutPacketSource(ctx context.Context, source packet.Source) error {
	var errs []error
	for idx, k := range t {
		sink, ok := any(k).(packet.Sink)
		if !ok {
			continue
		}
		if err := sink.NotifyAboutPacketSource(ctx, source); err != nil {
			errs = append(errs, fmt.Errorf("tee child#%d:%T: %w", idx, k, err))
		}
	}
	return errors.Join(errs...)
}
