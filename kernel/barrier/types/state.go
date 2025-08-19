package types

type State int

const (
	UndefinedState = State(iota)
	StatePass
	StateBlock
	StateDrop
	EndOfState
)
