package fanin

import (
	"errors"
	"time"

	"github.com/xaionaro-go/avpipeline/preset/selector/availability"
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/member"
	"github.com/xaionaro-go/avpipeline/preset/selector/orphanretry"
	"github.com/xaionaro-go/avpipeline/preset/selector/safekey"
	"github.com/xaionaro-go/avpipeline/preset/selector/selectorerr"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchprogress"
)

var (
	ErrMissingRouteID     = errors.New("missing route id")
	ErrMissingPair        = errors.New("missing switch pair")
	ErrMissingMembers     = errors.New("missing members")
	ErrMissingGate        = errors.New("missing switch progress gate")
	ErrMissingAsyncErrors = errors.New("missing async error handler")
	ErrMissingPolicy      = errors.New("missing recreate policy")
	ErrMissingTracker     = errors.New("missing recreate tracker")
	ErrMissingRecreate    = errors.New("missing recreate hook")
)

type RecreateConfig[K comparable] struct {
	Policy                orphanretry.Policy[K]
	Tracker               *orphanretry.Tracker[K]
	Recreate              orphanretry.RecreateFunc[K]
	RecordOnNoFallback    bool
	RecordOnSwitchFailure bool
}

type Config[K comparable, M Member] struct {
	RouteID          id.RouteID
	Pair             *switchpair.Pair
	Members          *member.Registry[K, M]
	Gate             *switchprogress.Gate
	AsyncErrors      AsyncErrorHandler
	SafeKeyFormatter safekey.Formatter[K]
	Recreate         *RecreateConfig[K]
	PriorityPolicy   *PriorityPolicy[K, M]
	PausePlanner     *PausePlanner[K, M]
	OpenPromotion    *OpenPromotion[K, M]
	FallbackHandler  *FallbackHandler[K, M]
	RecreateRecovery *RecreateRecovery[K, M]
}

func validateConfig[K comparable, M Member](
	cfg Config[K, M],
) error {
	var errs []error
	if cfg.RouteID == "" {
		errs = append(errs, selectorerr.InvalidConfig("routeID", ErrMissingRouteID))
	}
	if cfg.Pair == nil {
		errs = append(errs, selectorerr.InvalidConfig("pair", ErrMissingPair))
	}
	if cfg.Members == nil {
		errs = append(errs, selectorerr.InvalidConfig("members", ErrMissingMembers))
	}
	if cfg.Gate == nil {
		errs = append(errs, selectorerr.InvalidConfig("gate", ErrMissingGate))
	}
	if cfg.AsyncErrors == nil {
		errs = append(errs, selectorerr.InvalidConfig("asyncErrors", ErrMissingAsyncErrors))
	}
	if cfg.Recreate != nil {
		if cfg.Recreate.Policy == nil {
			errs = append(errs, selectorerr.InvalidConfig("recreate.Policy", ErrMissingPolicy))
		}
		if cfg.Recreate.Recreate == nil {
			errs = append(errs, selectorerr.InvalidConfig("recreate.Recreate", ErrMissingRecreate))
		}
	}

	return errors.Join(errs...)
}

func configuredTracker[K comparable](
	cfg RecreateConfig[K],
) (*orphanretry.Tracker[K], error) {
	if cfg.Tracker != nil {
		return cfg.Tracker, nil
	}

	return orphanretry.NewTracker[K](
		cfg.Policy,
		time.Now,
		cfg.Recreate,
	)
}

func candidateAvailability(
	source availability.Source,
) availability.Candidate {
	return availability.Present(source)
}
