// sock_info.go defines the SockInfo interface for retrieving socket information.

// Package sockinfo provides utilities to retrieve socket information.
package sockinfo

import (
	"time"
)

type SockInfo interface {
	GetRTT() (time.Duration, error)
}
