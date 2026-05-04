package eviction

import "github.com/xaionaro-go/avpipeline/preset/selector/id"

type Result struct {
	DemotedRoutes []id.RouteID
	Recommitted   []id.RouteID
	Recreated     []id.RouteID
	RetryRecorded []id.RouteID
	RecreateErrs  []error
}
