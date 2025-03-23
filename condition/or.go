package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/types"
)

type Or []Condition

var _ Condition = (Or)(nil)

func (s *Or) Add(item Condition) *Or {
	*s = append(*s, item)
	return s
}

func (s Or) String() string {
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "|"))
}

func (s Or) Match(
	ctx context.Context,
	pkt types.InputPacket,
) bool {
	for _, item := range s {
		if item.Match(ctx, pkt) {
			return true
		}
	}
	return false
}
