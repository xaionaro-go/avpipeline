package route

import (
	"github.com/xaionaro-go/avpipeline/preset/selector/id"
	"github.com/xaionaro-go/avpipeline/preset/selector/switchpair"
)

type State struct {
	ID   id.RouteID
	Pair *switchpair.Pair
}
