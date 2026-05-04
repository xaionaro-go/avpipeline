package fanin

import "errors"

var ErrCannotPauseSoleActiveMember = errors.New("cannot pause sole active fan-in member")
