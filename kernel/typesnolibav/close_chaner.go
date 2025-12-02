package typesnolibav

type CloseChaner interface {
	CloseChan() <-chan struct{}
}
