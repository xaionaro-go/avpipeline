package streammux

import (
	"github.com/xaionaro-go/avpipeline/kernel/boilerplate"
)

type InputHandler struct{}

var _ boilerplate.CustomHandlerWithContextFormat = (*InputHandler)(nil)

func (h *InputHandler) String() string {
	return "StreamMuxInput"
}
