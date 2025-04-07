package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/xaionaro-go/avpipeline/frame"
	"github.com/xaionaro-go/avpipeline/packet"
	"github.com/xaionaro-go/observability"
	"github.com/xaionaro-go/xsync"
)

type Sequence[T Abstract] struct {
	*closeChan
	Locker  xsync.Mutex
	Kernels []T
}

var _ Abstract = (*Sequence[Abstract])(nil)

func NewSequence[T Abstract](kernels ...T) *Sequence[T] {
	return &Sequence[T]{
		closeChan: newCloseChan(),
		Kernels:   kernels,
	}
}

func (s *Sequence[Abstract]) SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &s.Locker, s.sendInputPacket, ctx, input, outputPacketsCh, outputFramesCh)
}

func (s *Sequence[Abstract]) sendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return s.sendInput(ctx, ptr(input), nil, outputPacketsCh, outputFramesCh)
}

func (s *Sequence[Abstract]) SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return xsync.DoA4R1(ctx, &s.Locker, s.sendInputFrame, ctx, input, outputPacketsCh, outputFramesCh)
}

func (s *Sequence[Abstract]) sendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	return s.sendInput(ctx, nil, ptr(input), outputPacketsCh, outputFramesCh)
}

func (s *Sequence[Abstract]) sendInput(
	ctx context.Context,
	inputPacket *packet.Input,
	inputFrame *frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
	if len(s.Kernels) == 0 {
		return nil
	}
	if inputPacket != nil && inputFrame != nil {
		return fmt.Errorf("internal error: inputPacket != nil && inputFrame != nil")
	}
	var (
		firstOutPacketCh chan<- packet.Output
		firstOutFrameCh  chan<- frame.Output
		nextInPacketCh   chan packet.Output
		nextInFrameCh    chan frame.Output
	)
	if len(s.Kernels) == 1 {
		firstOutPacketCh = outputPacketsCh
		firstOutFrameCh = outputFramesCh
	} else {
		packetCh, frameCh := make(chan packet.Output), make(chan frame.Output)
		firstOutPacketCh, nextInPacketCh = packetCh, packetCh
		firstOutFrameCh, nextInFrameCh = frameCh, frameCh
	}

	errCh := make(chan error, 10)
	var wg sync.WaitGroup
	for idx, k := range s.Kernels[1:] {
		{
			k := k
			curInPacketCh, curInFrameCh := nextInPacketCh, nextInFrameCh
			nextInPacketCh, nextInFrameCh = make(chan packet.Output), make(chan frame.Output)
			var (
				curOutPacketCh chan<- packet.Output
				curOutFrameCh  chan<- frame.Output
			)
			if idx == len(s.Kernels[1:])-1 {
				curOutPacketCh, curOutFrameCh = outputPacketsCh, outputFramesCh
			} else {
				curOutPacketCh, curOutFrameCh = nextInPacketCh, nextInFrameCh
			}
			var kernelWG sync.WaitGroup
			wg.Add(1)
			kernelWG.Add(1)
			observability.Go(ctx, func() {
				defer wg.Done()
				defer kernelWG.Done()
				for pkt := range curInPacketCh {
					errCh <- k.SendInputPacket(ctx, packet.Input(pkt), curOutPacketCh, curOutFrameCh)
				}
			})
			wg.Add(1)
			kernelWG.Add(1)
			observability.Go(ctx, func() {
				defer wg.Done()
				defer kernelWG.Done()
				for pkt := range curInFrameCh {
					errCh <- k.SendInputFrame(ctx, frame.Input(pkt), curOutPacketCh, curOutFrameCh)
				}
			})
			if idx != len(s.Kernels[1:])-1 {
				observability.Go(ctx, func() {
					kernelWG.Wait()
					close(curOutPacketCh)
					close(curOutFrameCh)
				})
			}
		}
	}
	observability.Go(ctx, func() {
		wg.Wait()
		close(errCh)
	})

	var err error
	if inputPacket != nil {
		err = s.Kernels[0].SendInputPacket(ctx, *inputPacket, firstOutPacketCh, firstOutFrameCh)
	}
	if inputFrame != nil {
		err = s.Kernels[0].SendInputFrame(ctx, *inputFrame, firstOutPacketCh, firstOutFrameCh)
	}
	if err != nil {
		return fmt.Errorf("unable to send to the first kernel: %w", err)
	}
	close(firstOutPacketCh)
	close(firstOutFrameCh)

	var errs []error
	for err := range errCh {
		if err == nil {
			continue
		}
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (s *Sequence[Abstract]) String() string {
	var result []string
	for _, node := range s.Kernels {
		result = append(result, node.String())
	}
	return fmt.Sprintf("Sequence(\n\t%s,\n)", strings.Join(result, ",\n\t"))
}

func (s *Sequence[Abstract]) Close(ctx context.Context) error {
	s.closeChan.Close(ctx)
	var result []error
	for idx, node := range s.Kernels {
		err := node.Close(ctx)
		if err != nil {
			result = append(result, fmt.Errorf("unable to close node#%d:%T: %w", idx, node, err))
		}
	}
	return errors.Join(result...)
}

func (s *Sequence[Abstract]) Generate(
	ctx context.Context,
	_ chan<- packet.Output,
	_ chan<- frame.Output,
) error {
	ctx, cancelFn := context.WithCancel(ctx)
	defer cancelFn()
	outputPacketsCh := make(chan packet.Output, 1)
	outputFramesCh := make(chan frame.Output, 1)

	errCh := make(chan error, 1)

	var kernelWG sync.WaitGroup
	for _, k := range s.Kernels {
		kernelWG.Add(1)
		observability.Go(ctx, func() {
			defer kernelWG.Done()
			errCh <- k.Generate(ctx, outputPacketsCh, outputFramesCh)
		})
	}
	observability.Go(ctx, func() {
		kernelWG.Wait()
		close(outputPacketsCh)
		close(outputFramesCh)
	})

	var readerWG sync.WaitGroup
	readerWG.Add(1)
	observability.Go(ctx, func() {
		defer readerWG.Done()
		for _ = range outputPacketsCh {
			errCh <- fmt.Errorf("generators are not supported in Sequence, yet")
		}
	})
	readerWG.Add(1)
	observability.Go(ctx, func() {
		defer readerWG.Done()
		for _ = range outputFramesCh {
			errCh <- fmt.Errorf("generators are not supported in Sequence, yet")
		}
	})
	observability.Go(ctx, func() {
		readerWG.Wait()
		close(errCh)
	})

	var errs []error
	for err := range errCh {
		if err == nil {
			continue
		}
		cancelFn()
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
