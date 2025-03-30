package sort

import (
	"sort"

	"github.com/asticode/go-astiav"
)

type OrderedStream struct {
	Stream *astiav.Stream
	Order  int64
}

type OrderedStreams []OrderedStream

var _ sort.Interface = (OrderedStreams)(nil)

func (s OrderedStreams) Len() int {
	return len(s)
}

func (s OrderedStreams) Less(i, j int) bool {
	return s[i].Order < s[j].Order
}

func (s OrderedStreams) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
