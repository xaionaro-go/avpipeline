// frame.go defines the Frame interface for media frames.

// Package frame provides types and utilities for handling media frames.
package frame

type Frame interface {
	Input | Output
}
