// packet.go defines the Packet interface for media packets.

package packet

type Packet interface {
	Input | Output
}
