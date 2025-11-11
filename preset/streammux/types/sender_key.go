package types

import (
	"fmt"
	"sort"

	codectypes "github.com/xaionaro-go/avpipeline/codec/types"
)

type SenderKey struct {
	AudioCodec codectypes.Name
	VideoCodec codectypes.Name
	Resolution codectypes.Resolution
}

func (k SenderKey) String() string {
	return fmt.Sprintf(
		"%s/%dx%d | %s",
		k.VideoCodec, k.Resolution.Width, k.Resolution.Height,
		k.AudioCodec,
	)
}

func (k SenderKey) Compare(b SenderKey) int {
	if k.VideoCodec != b.VideoCodec {
		if k.VideoCodec == codectypes.NameCopy {
			return 1 // lossless is better even despite lower width/height
		}
		if b.VideoCodec == codectypes.NameCopy {
			return -1
		}
		if k.VideoCodec > b.VideoCodec { // TODO: determine better codec by some other way?
			return 1
		}
		return -1
	}
	resK := k.Resolution.Width * k.Resolution.Height
	resB := b.Resolution.Width * b.Resolution.Height
	if resK != resB {
		if resK > resB {
			return 1
		}
		return -1
	}
	if k.AudioCodec != b.AudioCodec {
		if k.AudioCodec == codectypes.NameCopy {
			return 1
		}
		if b.AudioCodec == codectypes.NameCopy {
			return -1
		}
		if k.AudioCodec > b.AudioCodec { // TODO: determine better codec by some other way?
			return 1
		}
		return -1
	}
	return 0
}

type SenderKeys []SenderKey

func (s SenderKeys) Sort() {
	sort.Sort(s)
}

func (s SenderKeys) Len() int {
	return len(s)
}

func (s SenderKeys) Less(i, j int) bool {
	return s[i].Compare(s[j]) < 0
}

func (s SenderKeys) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
