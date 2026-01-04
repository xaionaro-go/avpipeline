// linesize.go provides conversion functions for linesize between Protobuf and Go.

// Package libavnolibav provides conversion functions between Protobuf and Go for libav types without libav dependency.
package libavnolibav

type Linesize [8]uint32

func LinesizeFromProtobuf(input []uint32) Linesize {
	if len(input) != 8 {
		panic("invalid linesize length")
	}
	var ls Linesize
	for i := 0; i < 8 && i < len(input); i++ {
		ls[i] = input[i]
	}
	return ls
}

func LinesizeFromGo(input [8]int) Linesize {
	var ls Linesize
	for i := range 8 {
		ls[i] = uint32(input[i])
	}
	return ls
}

func (f Linesize) Protobuf() []uint32 {
	var ls []uint32
	for i := range 8 {
		ls = append(ls, f[i])
	}
	return ls
}

func (f Linesize) Go() [8]int {
	var ls [8]int
	for i := range 8 {
		ls[i] = int(f[i])
	}
	return ls
}
