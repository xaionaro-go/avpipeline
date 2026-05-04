package fanout

import (
	"fmt"

	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

type ErrAllAlreadyPreferred struct {
	RouteIDs []id.RouteID
}

func (e ErrAllAlreadyPreferred) Error() string {
	return fmt.Sprintf("routes %v are already preferred", e.RouteIDs)
}

func (e ErrAllAlreadyPreferred) Unwrap() error {
	return selectorerr.ErrAlreadyPreferred
}
