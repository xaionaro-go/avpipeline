// state.go defines the State type for barriers.

// Package types provides common types for barriers.
package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

type State int

const (
	UndefinedState = State(iota)
	StatePass
	StateBlock
	StateDrop
	EndOfState
)

func (s State) String() string {
	switch s {
	case UndefinedState:
		return "<undefined>"
	case StatePass:
		return "pass"
	case StateBlock:
		return "block"
	case StateDrop:
		return "drop"
	default:
		return fmt.Sprintf("unknown_%d", int(s))
	}
}

func (s State) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *State) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return fmt.Errorf("cannot unmarshal %s to State: %w", string(b), err)
	}
	st, err := StateFromString(str)
	if err != nil {
		return fmt.Errorf("cannot parse %q as State: %w", str, err)
	}
	*s = st
	return nil
}

func StateFromString(str string) (State, error) {
	str = strings.ToLower(str)
	for s := range EndOfState {
		if s.String() == str {
			return s, nil
		}
	}
	return UndefinedState, fmt.Errorf("unknown State: %q", str)
}
