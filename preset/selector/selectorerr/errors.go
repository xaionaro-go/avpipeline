package selectorerr

import "errors"

var (
	ErrInvalidConfig    = errors.New("invalid selector config")
	ErrMemberNotFound   = errors.New("selector member not found")
	ErrRouteNotFound    = errors.New("selector route not found")
	ErrAlreadyPreferred = errors.New("selector already preferred")
	ErrInvalidRoutePlan = errors.New("invalid selector route plan")
)
