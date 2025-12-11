package inputwithfallback

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/kernel"
)

type InputFactory[K kernel.Abstract] interface {
	fmt.Stringer
	NewInput(
		ctx context.Context,
		inputID InputID,
	) (K, error)
}
