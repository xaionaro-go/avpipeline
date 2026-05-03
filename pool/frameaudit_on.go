//go:build frameaudit

// frameaudit_on.go provides deterministic detection of the slab-aliasing
// UAF the runtime stress test reproduces probabilistically. Every Get
// records `uintptr(f.UnsafePointer())` -> `*T`; if a fresh Get returns a
// different Go wrapper for the same C slab, we panic in Go before any
// cgo deref. Build with `-tags=frameaudit`.

package pool

import (
	"fmt"
	"sync"
	"unsafe"
)

type unsafePointerer interface {
	UnsafePointer() unsafe.Pointer
}

var (
	auditMu sync.Mutex
	audit   = map[uintptr]any{}
)

func auditGet[T any](v *T) {
	up, ok := any(v).(unsafePointerer)
	if !ok {
		return
	}
	key := uintptr(up.UnsafePointer())
	if key == 0 {
		return
	}
	auditMu.Lock()
	defer auditMu.Unlock()
	if prev, exists := audit[key]; exists && any(v) != prev {
		panic(fmt.Sprintf(
			"pool slab-alias detected: same C pointer %#x wrapped by two Go objects (%p vs %p)",
			key, prev, v))
	}
	audit[key] = any(v)
}

func auditPut[T any](v *T) {
	up, ok := any(v).(unsafePointerer)
	if !ok {
		return
	}
	key := uintptr(up.UnsafePointer())
	if key == 0 {
		return
	}
	auditMu.Lock()
	delete(audit, key)
	auditMu.Unlock()
}
