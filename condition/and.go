package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/types"
)

type And []Condition

var _ Condition = (And)(nil)

func (s And) String() string {
	var result []string
	for _, cond := range s {
		result = append(result, cond.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "&"))
}

func (s And) Match(
	ctx context.Context,
	pkt types.InputPacket,
) bool {
	for _, item := range s {
		if !item.Match(ctx, pkt) {
			return false
		}
	}
	return true
}
