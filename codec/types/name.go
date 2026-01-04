// name.go defines the Name type for codecs.

// Package types provides common types for codecs.
package types

type Name string

const (
	NameCopy Name = "copy"
	NameRaw  Name = "raw"
)
