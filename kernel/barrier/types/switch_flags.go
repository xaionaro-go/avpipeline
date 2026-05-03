// switch_flags.go defines the SwitchFlags type and its flags.

package types

type SwitchFlags uint64

const (
	// If set then it will pass the first packet after a switch to both directions (old and new ones).
	SwitchFlagFirstPacketAfterSwitchPassBothOutputs SwitchFlags = 1 << iota

	// If set then the KeepUnless will not switch due a packet received to a non-active SwitchOutput
	SwitchFlagForbidTakeoverInKeepUnless

	// If set then the state of the next output will be "block".
	SwitchFlagNextOutputStateBlock

	// If set then the state of all inactive outputs will be "block".
	SwitchFlagInactiveBlock

	// If set, the Switch tracks per-chain last-emitted PTS per stream and, on
	// chain change, rebases the new chain's PTS / DTS so the output stream
	// remains a strictly-monotonic continuation of the old chain's last PTS.
	// Bridges cross-clock-domain transitions (e.g. rtmp upstream-derived PTS
	// vs builtin camera+mic monotonic-epoch PTS) that would otherwise produce
	// large forward / backward jumps at chain switches and freeze players.
	SwitchFlagBridgePTSAcrossChains
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
