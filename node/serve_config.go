// serve_config.go defines the configuration for serving a node.

package node

type ServeConfig struct {
	FrameDropAudio bool
	FrameDropVideo bool
	FrameDropOther bool

	DebugData any
}
