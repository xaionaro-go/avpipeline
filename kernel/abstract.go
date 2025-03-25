package kernel

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/types"
)

type Abstract interface {
	SendInputer
	fmt.Stringer
	types.Closer
	CloseChaner
	Generator
}

type CloseChaner interface {
	CloseChan() <-chan struct{}
}

/*

== for easier copy&paste ==

func () SendInputPacket(
	ctx context.Context,
	input packet.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
}
func () SendInputFrame(
	ctx context.Context,
	input frame.Input,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
 }
func () String() string {
}
func () Close(context.Context) error {
}
func () CloseChan() <-chan struct{} {
}
func () Generate(
	ctx context.Context,
	outputPacketsCh chan<- packet.Output,
	outputFramesCh chan<- frame.Output,
) error {
}

*/
