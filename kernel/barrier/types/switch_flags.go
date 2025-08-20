package types

type SwitchFlags uint64

const (
	// If set then it will pass the first packet after a switch to both directions (old and new ones).
	FlagSwitchFirstPacketAfterSwitchPassBothOutputs SwitchFlags = 1 << iota

	// If set then the KeepUnless will not switch due a packet received to a non-active SwitchOutput
	FlagSwitchForbidTakeoverInKeepUnless
)

func (f SwitchFlags) HasAll(flag SwitchFlags) bool {
	return f&flag == flag
}

func (f SwitchFlags) HasAny(flag SwitchFlags) bool {
	return f&flag != 0
}

func (f *SwitchFlags) Set(flag SwitchFlags) {
	*f |= flag
}

func (f *SwitchFlags) Unset(flag SwitchFlags) {
	*f &^= flag
}
