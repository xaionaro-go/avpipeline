// annexb.go provides common functionality for parsing Annex-B sequences.

package extradata

func SplitAnnexB(b []byte) [][]byte {
	var nalus [][]byte
	n := len(b)
	i := 0

	for {
		start := FindStartCode(b, i)
		if start < 0 {
			break
		}

		// length of start code (3 or 4 bytes)
		scLen := 3
		if start+3 < n &&
			b[start] == 0 && b[start+1] == 0 &&
			b[start+2] == 0 && b[start+3] == 1 {
			scLen = 4
		}

		next := FindStartCode(b, start+scLen)
		end := n
		if next >= 0 {
			end = next
		}

		if start+scLen >= end {
			if next < 0 {
				break
			}
			i = next
			continue
		}

		nalu := b[start+scLen : end]
		if len(nalu) > 0 {
			cloned := make([]byte, len(nalu))
			copy(cloned, nalu)
			nalus = append(nalus, cloned)
		}

		if next < 0 {
			break
		}
		i = next
	}

	return nalus
}

func FindStartCode(b []byte, start int) int {
	n := len(b)
	for i := start; i+3 <= n; i++ {
		if b[i] == 0 && b[i+1] == 0 {
			// 00 00 01
			if b[i+2] == 1 {
				return i
			}
			// 00 00 00 01
			if i+4 <= n && b[i+2] == 0 && b[i+3] == 1 {
				return i
			}
		}
	}
	return -1
}
