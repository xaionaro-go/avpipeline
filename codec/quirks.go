package codec

import "strings"

type Quirks uint64

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
