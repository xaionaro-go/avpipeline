package kernel

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/types"
	"github.com/xaionaro-go/xsync"
)

type Switch struct {
	*closeChan

	Kernels []Abstract

	KernelIndex uint

	Locker xsync.RWMutex
}

var _ Abstract = (*Switch)(nil)

func NewProcessorsSwitch(
	processors ...Abstract,
) *Switch {
	return &Switch{
		closeChan: newCloseChan(),
		Kernels:   processors,
	}
}

func (sw *Switch) GetKernelIndex(
	ctx context.Context,
) uint {
	return xsync.DoR1(xsync.WithNoLogging(ctx, true), &sw.Locker, func() uint {
		return sw.KernelIndex
	})
}

func (sw *Switch) SetKernelIndex(
	ctx context.Context,
	idx uint,
) error {
	if idx >= uint(len(sw.Kernels)) {
		return fmt.Errorf("requested processor #%d, while I have only %d processor", idx+1, len(sw.Kernels))
	}
	sw.Locker.Do(ctx, func() {
		sw.KernelIndex = idx
	})
	return nil
}

func (sw *Switch) GetKernel(ctx context.Context) Abstract {
	return sw.Kernels[sw.GetKernelIndex(ctx)]
}

func (sw *Switch) String() string {
	var result []string
	for idx, node := range sw.Kernels {
		var str string
		if uint(idx) == sw.GetKernelIndex(context.Background()) {
			str = fmt.Sprintf(" *%s* ", node.String())
		} else {
			str = node.String()
		}
		result = append(result, str)
	}
	return fmt.Sprintf("Switch(%s)", strings.Join(result, "|"))
}

func (sw *Switch) Generate(ctx context.Context, outputCh chan<- types.OutputPacket) error {
	return fmt.Errorf("Switch does not support Generate, yet")
}

func (sw *Switch) SendInput(
	ctx context.Context,
	input types.InputPacket,
	outputCh chan<- types.OutputPacket,
) error {
	return sw.GetKernel(ctx).SendInput(ctx, input, outputCh)
}

func (sw *Switch) Close(
	ctx context.Context,
) error {
	sw.closeChan.Close()
	var result []error
	for idx, node := range sw.Kernels {
		err := node.Close(ctx)
		if err != nil {
			result = append(result, fmt.Errorf("unable to close node#%d:%T: %w", idx, node, err))
		}
	}
	return errors.Join(result...)
}
