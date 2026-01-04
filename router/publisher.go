// publisher.go defines the Publisher interface for route sources.

package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/xaionaro-go/avpipeline/node"
)

type Publisher[T any] interface {
	fmt.Stringer
	Close(context.Context) error
	GetInputNode(ctx context.Context) node.Abstract
	GetOutputRoute(ctx context.Context) *Route[T]
	GetPublishMode(ctx context.Context) PublishMode
}

type Publishers[T any] []Publisher[T]

func (s Publishers[T]) String() string {
	switch len(s) {
	case 0:
		return "NONE"
	case 1:
		return s[0].String()
	}

	var result []string
	for _, publisher := range s {
		result = append(result, publisher.String())
	}

	return "[" + strings.Join(result, ",") + "]"
}
