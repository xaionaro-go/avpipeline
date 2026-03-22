//go:build android

package android

import "fmt"

type ErrTermuxAPIUnavailable struct {
	Err error
}

func (e ErrTermuxAPIUnavailable) Error() string {
	if e.Err == nil {
		return "termux api is unavailable"
	}
	return fmt.Sprintf("termux api is unavailable: %v", e.Err)
}

func (e ErrTermuxAPIUnavailable) Unwrap() error {
	return e.Err
}

type ErrTermuxAPICommandFailed struct {
	Command string
	Message string
	Err     error
}

func (e ErrTermuxAPICommandFailed) Error() string {
	if e.Err == nil && e.Message == "" {
		return fmt.Sprintf("termux api command failed: %s", e.Command)
	}
	if e.Err == nil {
		return fmt.Sprintf("termux api command failed: %s: %s", e.Command, e.Message)
	}
	if e.Message == "" {
		return fmt.Sprintf("termux api command failed: %s: %v", e.Command, e.Err)
	}
	return fmt.Sprintf("termux api command failed: %s: %s: %v", e.Command, e.Message, e.Err)
}

func (e ErrTermuxAPICommandFailed) Unwrap() error {
	return e.Err
}

type ErrTermuxAPIResponse struct {
	Message string
	Err     error
}

func (e ErrTermuxAPIResponse) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("termux api invalid response: %s", e.Message)
	}
	if e.Message == "" {
		return fmt.Sprintf("termux api invalid response: %v", e.Err)
	}
	return fmt.Sprintf("termux api invalid response: %s: %v", e.Message, e.Err)
}

func (e ErrTermuxAPIResponse) Unwrap() error {
	return e.Err
}
