// no_raw_pool_put_test.go enforces the rule that callers must use the
// wrapped Pool.Put (which runs ResetFunc — av_frame_unref / av_packet_unref)
// rather than the raw embedded sync.Pool.Pool.Put. Bypassing ResetFunc
// leaves dirty state (buf[i] non-NULL) in the pool entry; the next Get-er
// hits AVERROR(EINVAL) from av_frame_get_buffer because the frame already
// has buffers, and its recovery Pool.Put then calls av_frame_unref on a
// frame whose buf[i] points at memory that may have been freed by another
// owner — causing the SIGSEGV observed in production
// (resampler.New -> Pool.Put -> Frame.Unref -> av_frame_unref crash at
// addr=0xbb80).
//
// Allowed (with ResetFunc):    p.Put(item)   // p is *Pool[T]
// Not allowed (raw, no Reset): p.Pool.Put(item)
//
// This is enforced as a structural source-text test rather than a runtime
// test because the failure mode is a use-after-free in cgo that only
// reproduces on long-running production traffic with a specific codec
// configuration.

package pool_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoRawPoolPoolPutInProduction walks every .go file under the
// avpipeline module (excluding _test.go and vendor) and fails the test
// if any expression of the form `<x>.Pool.Pool.Put(...)` is found.
//
// Cases this catches (all converted to wrapped Pool.Put as part of the
// resampler-UAF fix):
//   - codec/decoder_locked.go (frame.Pool.Pool.Put on ReceiveFrame error /
//     success+nil-callback / callback error)
//   - codec/encoder_full_locked.go (packet.Pool.Pool.Put mirror for the
//     encoder's ReceivePacket loop)
//   - kernel/bitstream_filter.go (packet.Pool.Pool.Put after
//     bsf.ReceivePacket EOF/EAgain)
func TestNoRawPoolPoolPutInProduction(t *testing.T) {
	abs, err := filepath.Abs(".")
	require.NoError(t, err)
	// abs is .../avpipeline/pool — module root is one level up.
	root := filepath.Dir(abs)

	// Match `<ident>.Pool.Pool.Put(` anywhere in the line. Production
	// callers always go through a package-level Pool var (frame.Pool,
	// packet.Pool, …) so the leading identifier qualifies the match.
	re := regexp.MustCompile(`\b\w+\.Pool\.Pool\.Put\(`)

	var violations []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == "testdata" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				violations = append(violations,
					rel+":"+itoa(i+1)+"  "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	require.NoError(t, err)

	if len(violations) > 0 {
		t.Fatalf(
			"found %d production-code call(s) to raw Pool.Pool.Put — "+
				"bypassing ResetFunc leaks AVFrame/AVPacket buf[i] refs into "+
				"the pool and causes av_frame_unref/av_packet_unref UAF "+
				"crashes (see resampler.New -> Pool.Put SIGSEGV in prod). "+
				"Use wrapped Pool.Put instead:\n  %s",
			len(violations), strings.Join(violations, "\n  "),
		)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
