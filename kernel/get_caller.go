package kernel

import (
	xruntime "github.com/facebookincubator/go-belt/pkg/runtime"
)

func getCaller() (string, int) {
	cnt := 0
	return xruntime.Caller(func(pc uintptr) bool {
		if cnt >= 3 {
			return true
		}
		cnt++
		return false
	}).FileLine()
}
