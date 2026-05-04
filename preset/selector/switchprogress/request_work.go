package switchprogress

import "sync/atomic"

type RequestWork struct {
	gate  *Gate
	token *releaseToken
}

type releaseToken struct {
	released atomic.Bool
}

func (w RequestWork) Release() {
	w.release(w.token)
}

func (w RequestWork) ReserveAsyncWork() func() {
	if w.gate == nil || w.token == nil || w.token.released.Load() {
		return func() {}
	}

	w.gate.inFlight.Add(1)
	token := &releaseToken{}
	return func() {
		w.release(token)
	}
}

func (w RequestWork) release(
	token *releaseToken,
) {
	if w.gate == nil || token == nil {
		return
	}
	if !token.released.CompareAndSwap(false, true) {
		return
	}
	w.gate.inFlight.Add(-1)
}
