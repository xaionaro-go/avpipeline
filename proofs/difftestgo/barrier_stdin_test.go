// barrier_stdin_test.go reads barrier test vectors from a file and prints
// results in the same format as the Lean DiffTest, enabling automated diffing.
//
// Usage: go test ./proofs/difftestgo/ -run TestBarrierFromFile -v -args /tmp/comprehensive_barrier_tests.txt
package difftestgo

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestBarrierFromFile(t *testing.T) {
	args := os.Args
	var inputFile string
	for i, arg := range args {
		if arg == "-args" || arg == "--args" {
			if i+1 < len(args) {
				inputFile = args[i+1]
			}
		}
		// Also check if it looks like a file path after -test.run
		if strings.HasSuffix(arg, ".txt") && !strings.HasPrefix(arg, "-") {
			inputFile = arg
		}
	}

	if inputFile == "" {
		inputFile = os.Getenv("BARRIER_TEST_FILE")
	}
	if inputFile == "" {
		t.Skip("No input file provided. Set BARRIER_TEST_FILE or pass -args <file>")
	}

	f, err := os.Open(inputFile)
	if err != nil {
		t.Fatalf("Cannot open %s: %v", inputFile, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 7 || fields[0] != "switch" {
			t.Errorf("bad line: %s", line)
			continue
		}

		currentValue, _ := strconv.ParseInt(fields[1], 10, 32)
		var nextValue int64
		if fields[2] == "none" {
			nextValue = math.MinInt32
		} else {
			nextValue, _ = strconv.ParseInt(fields[2], 10, 32)
		}
		outputID, _ := strconv.ParseInt(fields[3], 10, 32)
		mediaType := fields[4]
		isKeyFrame := fields[5] == "true"
		keepUnless := fields[6]

		tc := switchTestCase{
			name:         line,
			currentValue: int32(currentValue),
			nextValue:    int32(nextValue),
			outputID:     int32(outputID),
			mediaType:    mediaType,
			isKeyFrame:   isKeyFrame,
			keepUnless:   keepUnless,
			expected:     "", // we don't check expected here
		}

		got := runSwitchTestCase(t, tc)
		fmt.Println(got)
	}
}
