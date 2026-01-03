// quirks.go defines flags for handling codec-specific bugs and behaviors.

package codec

import "strings"

type Quirks uint64

const (
	QuirkBuggyFlushBuffers Quirks = 1 << iota // TODO: delete me, if not set in codec.go
)

func (f Quirks) HasAll(flag Quirks) bool {
	return f&flag == flag
}

func (f Quirks) HasAny(flag Quirks) bool {
	return f&flag != 0
}

func (f *Quirks) Set(flag Quirks) {
	*f |= flag
}

func (f *Quirks) Unset(flag Quirks) {
	*f &^= flag
}

func (f Quirks) String() string {
	var s []string
	return strings.Join(s, "|")
}
