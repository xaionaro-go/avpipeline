// Package sockinfo provides utilities to retrieve socket information.
package sockinfo

import (
	"time"
)

type SockInfo interface {
	GetRTT() (time.Duration, error)
}
