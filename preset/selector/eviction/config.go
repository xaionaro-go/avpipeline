package eviction

import (
	"context"
	"errors"

	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

var (
	ErrMissingMembers     = errors.New("missing members")
	ErrMissingRoutes      = errors.New("missing routes")
	ErrMissingAttachments = errors.New("missing attachments")
	ErrMissingRecommit    = errors.New("missing recommit")
)

type RetryTracker[K comparable] interface {
	RecordDemotion(ctx context.Context, routeID id.RouteID, storageKey K)
}

type Config[K comparable, M any] struct {
	Members          *member.Registry[K, M]
	Routes           *route.Registry
	Attachments      *attachment.Index
	RetryTracker     RetryTracker[K]
	Recommit         RecommitFunc[K]
	Recreate         RecreateFunc[K]
	SafeKeyFormatter safekey.Formatter[K]
}

func validateConfig[K comparable, M any](
	cfg Config[K, M],
) error {
	var errs []error
	if cfg.Members == nil {
		errs = append(errs, selectorerr.InvalidConfig("members", ErrMissingMembers))
	}
	if cfg.Routes == nil {
		errs = append(errs, selectorerr.InvalidConfig("routes", ErrMissingRoutes))
	}
	if cfg.Attachments == nil {
		errs = append(errs, selectorerr.InvalidConfig("attachments", ErrMissingAttachments))
	}
	if cfg.Recommit == nil {
		errs = append(errs, selectorerr.InvalidConfig("recommit", ErrMissingRecommit))
	}

	return errors.Join(errs...)
}
