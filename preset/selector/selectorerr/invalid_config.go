package selectorerr

import "fmt"

func InvalidConfig(
	field string,
	err error,
) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrInvalidConfig, field)
	}

	return fmt.Errorf("%w: %s: %w", ErrInvalidConfig, field, err)
}
