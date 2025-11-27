package condition

import (
	"context"
	"fmt"

	"github.com/xaionaro-go/avpipeline/frame"
)

type PanicfT struct {
	Format string
	Args   []any
}

var _ Condition = PanicfT{}

func Panicf(format string, args ...any) PanicfT {
	return PanicfT{
		Format: format,
		Args:   args,
	}
}

func (p PanicfT) String() string {
	return fmt.Sprintf("Panicf(%q, %v)", p.Format, p.Args)
}

func (p PanicfT) Match(
	ctx context.Context,
	f frame.Input,
) bool {
	panic(fmt.Sprintf(p.Format, p.Args...))
}
