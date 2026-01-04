// in.go implements a condition that checks if a node is in a given set.

package condition

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/node"
)

type In []node.Abstract

var _ Condition = (In)(nil)

func (s In) String() string {
	var result []string
	for _, n := range s {
		if stringer, ok := n.(fmt.Stringer); ok {
			result = append(result, stringer.String())
		} else {
			result = append(result, fmt.Sprintf("%T", n))
		}
	}
	return fmt.Sprintf("(%s)", strings.Join(result, "|"))
}

func (s In) Match(
	ctx context.Context,
	node node.Abstract,
) bool {
	for _, item := range s {
		if node == item {
			return true
		}
	}
	return false
}
