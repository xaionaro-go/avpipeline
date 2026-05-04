package fanout

import (
	"context"
	"errors"

	"github.com/xaionaro-go/avpipeline/preset/selector/attachment"
	"github.com/xaionaro-go/avpipeline/preset/selector/eviction"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/route"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
)

var (
	ErrMissingRoutes                = errors.New("missing routes")
	ErrMissingMembers               = errors.New("missing members")
	ErrMissingAttachments           = errors.New("missing attachments")
	ErrMissingMemberIDs             = errors.New("missing member ids")
	ErrMissingCreationPlanner       = errors.New("missing creation planner")
	ErrMissingPreferredRoutePlanner = errors.New("missing preferred route planner")
	ErrMissingDifferentOutputPolicy = errors.New("missing different output policy")
	ErrMissingPreferenceSwitcher    = errors.New("missing preference switcher")
	ErrMissingEviction              = errors.New("missing eviction")
)

type EvictionHandler[K comparable, M any] interface {
	Evict(ctx context.Context, dead member.Entry[K, M]) (eviction.Result, error)
}

type RetryTracker[K comparable] interface {
	Tick(ctx context.Context, current func(context.Context, id.RouteID) (id.MemberID, bool)) error
}

type Config[K comparable, M any] struct {
	Routes                *route.Registry
	Members               *member.Registry[K, M]
	Attachments           *attachment.Index
	MemberIDs             member.Allocator
	CreationPlanner       CreationPlanner[K]
	PreferredRoutePlanner PreferredRoutePlanner[K]
	DifferentOutputPolicy DifferentOutputPolicy
	PreferenceSwitcher    *PreferenceSwitcher[K, M]
	Eviction              EvictionHandler[K, M]
	RetryTracker          RetryTracker[K]
	SafeKeyFormatter      safekey.Formatter[K]
}

func validateConfig[K comparable, M any](
	cfg Config[K, M],
) error {
	var errs []error
	if cfg.Routes == nil {
		errs = append(errs, selectorerr.InvalidConfig("routes", ErrMissingRoutes))
	}
	if cfg.Members == nil {
		errs = append(errs, selectorerr.InvalidConfig("members", ErrMissingMembers))
	}
	if cfg.Attachments == nil {
		errs = append(errs, selectorerr.InvalidConfig("attachments", ErrMissingAttachments))
	}
	if cfg.MemberIDs == nil {
		errs = append(errs, selectorerr.InvalidConfig("member ids", ErrMissingMemberIDs))
	}
	if cfg.CreationPlanner == nil {
		errs = append(errs, selectorerr.InvalidConfig("creation planner", ErrMissingCreationPlanner))
	}
	if cfg.PreferredRoutePlanner == nil {
		errs = append(errs, selectorerr.InvalidConfig("preferred route planner", ErrMissingPreferredRoutePlanner))
	}
	if cfg.DifferentOutputPolicy == nil {
		errs = append(errs, selectorerr.InvalidConfig("different output policy", ErrMissingDifferentOutputPolicy))
	}
	if cfg.PreferenceSwitcher == nil {
		errs = append(errs, selectorerr.InvalidConfig("preference switcher", ErrMissingPreferenceSwitcher))
	}
	if cfg.Eviction == nil {
		errs = append(errs, selectorerr.InvalidConfig("eviction", ErrMissingEviction))
	}

	return errors.Join(errs...)
}
