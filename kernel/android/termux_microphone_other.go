//go:build !android

package android

import (
	"context"
	"fmt"
	"time"

	"github.com/xaionaro-go/avpipeline/helpers/closuresignaler"
	"github.com/xaionaro-go/avpipeline/kernel"
	kerneltypes "github.com/xaionaro-go/avpipeline/kernel/types"
	"github.com/xaionaro-go/avpipeline/packetorframe"
	globaltypes "github.com/xaionaro-go/avpipeline/types"
)

type TermuxMicrophoneConfig struct {
	FilePath      string
	Limit         time.Duration
	Encoder       string
	BitrateKbps   int
	SampleRate    int
	Channels      int
	PollInterval  time.Duration
	DialTimeout   time.Duration
	DeleteOnClose bool
	InputConfig   kernel.InputConfig
}

type TermuxMicrophone struct {
	*closuresignaler.ClosureSignaler
	Config TermuxMicrophoneConfig
}

var _ kernel.Abstract = (*TermuxMicrophone)(nil)

func NewTermuxMicrophone(ctx context.Context, cfg TermuxMicrophoneConfig) *TermuxMicrophone {
	_ = ctx
	return &TermuxMicrophone{ClosureSignaler: closuresignaler.New(), Config: cfg}
}

func (k *TermuxMicrophone) GetObjectID() globaltypes.ObjectID {
	return globaltypes.GetObjectID(k)
}

func (k *TermuxMicrophone) String() string {
	if k == nil {
		return "TermuxMicrophone(<nil>)"
	}
	return "TermuxMicrophone"
}

func (k *TermuxMicrophone) Close(ctx context.Context) error {
	_ = ctx
	if k != nil && k.ClosureSignaler != nil {
		k.ClosureSignaler.Close(ctx)
	}
	return nil
}

func (k *TermuxMicrophone) SendInput(
	ctx context.Context,
	input packetorframe.InputUnion,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_ = ctx
	_ = input
	_ = outputCh
	return kerneltypes.ErrUnexpectedInputType{}
}

func (k *TermuxMicrophone) Generate(
	ctx context.Context,
	outputCh chan<- packetorframe.OutputUnion,
) error {
	_ = ctx
	_ = outputCh
	return fmt.Errorf("termux microphone is only supported on android")
}
