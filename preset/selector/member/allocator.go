package member

import (
	"context"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
)

type Allocator interface {
	Allocate(ctx context.Context) (id.MemberID, error)
}
