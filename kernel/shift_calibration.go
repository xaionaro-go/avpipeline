// shift_calibration.go implements the PTS/DTS shift calibration state
// machine used by the Input kernel to keep every shifted timestamp
// non-negative across multi-stream inputs.

package kernel

// shiftCalibration tracks the minimum raw PTS and DTS observed across
// every stream that has produced at least one packet, so the committed
// shift (target - min) never leaves a later packet with a negative
// timestamp. A shift computed from the first packet alone would leave
// later packets with lower raw DTS in negative territory, which the FLV
// muxer writes as uint32 and wraps to ~4.29e9, poisoning every
// downstream consumer.
type shiftCalibration struct {
	WantPTS         bool
	WantDTS         bool
	TargetPTS       int64
	TargetDTS       int64
	TotalStreams    int
	MaxBufferedPkts int

	streamsSeen  map[int]struct{}
	bufferedPkts int
	minRawPTS    int64
	minRawDTS    int64
	hasPTS       bool
	hasDTS       bool
	committed    bool
}

// newShiftCalibration returns a calibration tracker primed for the
// given targets. MaxBufferedPkts caps how many packets may be buffered
// before the calibration is forced to commit on the min seen so far.
func newShiftCalibration(
	wantPTS bool,
	wantDTS bool,
	targetPTS int64,
	targetDTS int64,
	totalStreams int,
	maxBufferedPkts int,
) *shiftCalibration {
	return &shiftCalibration{
		WantPTS:         wantPTS,
		WantDTS:         wantDTS,
		TargetPTS:       targetPTS,
		TargetDTS:       targetDTS,
		TotalStreams:    totalStreams,
		MaxBufferedPkts: maxBufferedPkts,
		streamsSeen:     make(map[int]struct{}, totalStreams),
	}
}

// Active reports whether the calibration still needs more input before
// committing (i.e. neither of the target shifts has been decided yet).
func (c *shiftCalibration) Active() bool {
	return !c.committed
}

// Observe records a raw packet's (streamIndex, pts, dts) and returns
// true once the calibration has gathered enough evidence to commit.
// Callers pass astiav.NoPtsValue (or any sentinel) via hasPTS/hasDTS.
func (c *shiftCalibration) Observe(
	streamIndex int,
	rawPTS int64,
	hasPTS bool,
	rawDTS int64,
	hasDTS bool,
) (shouldCommit bool) {
	if c.committed {
		return false
	}
	if c.WantPTS && hasPTS {
		if !c.hasPTS || rawPTS < c.minRawPTS {
			c.minRawPTS = rawPTS
			c.hasPTS = true
		}
	}
	if c.WantDTS && hasDTS {
		if !c.hasDTS || rawDTS < c.minRawDTS {
			c.minRawDTS = rawDTS
			c.hasDTS = true
		}
	}
	c.streamsSeen[streamIndex] = struct{}{}
	c.bufferedPkts++
	allStreamsSeen := c.TotalStreams > 0 && len(c.streamsSeen) >= c.TotalStreams
	bufferFull := c.bufferedPkts >= c.MaxBufferedPkts
	return allStreamsSeen || bufferFull
}

// Commit marks the calibration as decided and returns the shifts to
// apply. The caller is responsible for storing the returned shifts in
// the atomic fields and flushing buffered packets.
func (c *shiftCalibration) Commit() (ptsShift int64, dtsShift int64, hasPTSShift bool, hasDTSShift bool) {
	c.committed = true
	if c.WantPTS && c.hasPTS {
		ptsShift = c.TargetPTS - c.minRawPTS
		hasPTSShift = true
	}
	if c.WantDTS && c.hasDTS {
		dtsShift = c.TargetDTS - c.minRawDTS
		hasDTSShift = true
	}
	return ptsShift, dtsShift, hasPTSShift, hasDTSShift
}

// StreamsSeen returns how many distinct streams produced at least one
// packet since calibration began. Useful for diagnostic logging when
// calibration is forced to commit before every stream has contributed.
func (c *shiftCalibration) StreamsSeen() int {
	return len(c.streamsSeen)
}

// BufferedPackets returns how many packets the calibration has counted
// toward the MaxBufferedPkts cap. Also used for diagnostic logging.
func (c *shiftCalibration) BufferedPackets() int {
	return c.bufferedPkts
}
