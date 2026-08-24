package iostreams_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/iostreams"
)

// terminal is an InteractiveReader that claims to be one.
type terminal struct{ *strings.Reader }

func (terminal) IsTerminal() bool { return true }

// notATerminal is an InteractiveReader that says it is not one, which is a
// different answer from "I cannot tell".
type notATerminal struct{ *strings.Reader }

func (notATerminal) IsTerminal() bool { return false }

// TestInteractiveIsConservative pins the direction the check errs in. Every
// caller of it is about to decide whether to ask before a durable write, so
// "I cannot tell" must read as "do not ask, do not write": a skipped write
// is recoverable, an unattended yes is not.
func TestInteractiveIsConservative(t *testing.T) {
	pipeR, pipeW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = pipeR.Close()
		_ = pipeW.Close()
	})

	for _, tc := range []struct {
		name string
		in   any
		want bool
	}{
		{"no reader at all", nil, false},
		{"a buffer", &bytes.Buffer{}, false},
		{"a pipe, which is a file but not a terminal", pipeR, false},
		{"a reader that says it is not a terminal", notATerminal{strings.NewReader("y\n")}, false},
		{"a reader that says it is a terminal", terminal{strings.NewReader("y\n")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			streams := &iostreams.Streams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
			if tc.in != nil {
				streams.In = tc.in.(interface{ Read([]byte) (int, error) })
			}
			if got := streams.Interactive(); got != tc.want {
				t.Errorf("Interactive() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestNilStreamsIsNotInteractive keeps the check safe on the one caller
// shape a test can produce by accident.
func TestNilStreamsIsNotInteractive(t *testing.T) {
	var streams *iostreams.Streams
	if streams.Interactive() {
		t.Error("a nil Streams reports itself interactive")
	}
}
