// close_chaner.go defines the CloseChaner interface for components that can signal their closure via a channel.

package typesnolibav

type CloseChaner interface {
	CloseChan() <-chan struct{}
}
