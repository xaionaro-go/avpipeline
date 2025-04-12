package types

type CloseChaner interface {
	CloseChan() <-chan struct{}
}
