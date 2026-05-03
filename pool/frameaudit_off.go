//go:build !frameaudit

package pool

func auditGet[T any](*T) {}
func auditPut[T any](*T) {}
