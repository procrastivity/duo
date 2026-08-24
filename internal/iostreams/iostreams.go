// Package iostreams carries the stdin/stdout/stderr set every verb
// constructor receives, so the stdout-carries-exactly-one-thing discipline is
// structural (threaded in) rather than a matter of remembering not to call
// fmt.Println.
package iostreams

import (
	"io"
	"os"
)

// Streams is the reader/writer set threaded into every verb constructor.
//
// In is here for the one thing a verb may legitimately ask a person:
// confirmation before a durable write it could not otherwise justify. The
// cold-start host bind whose provenance is `ambient-env` is the first such
// case (notes/43 item 13's hybrid rule), and Interactive is what keeps that
// question from ever blocking a script.
type Streams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// System returns the real process streams.
func System() *Streams {
	return &Streams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
}

// InteractiveReader is a reader that knows whether a person is behind it.
//
// It exists because "is this a terminal" is not a property of io.Reader and
// cannot be asked of a *bytes.Buffer. A test supplies an implementation to
// drive the confirmed path; a real process stream is decided by its file
// mode instead.
type InteractiveReader interface {
	io.Reader
	IsTerminal() bool
}

// Interactive reports whether a prompt written to Err could actually be
// answered on In.
//
// The check is deliberately conservative: anything it cannot positively
// identify as a terminal is not one. A verb that must ask before writing
// therefore declines to write when it is unsure, which is the safe
// direction — a skipped write is recoverable, an unattended "yes" is not.
func (s *Streams) Interactive() bool {
	if s == nil || s.In == nil {
		return false
	}
	switch in := s.In.(type) {
	case InteractiveReader:
		return in.IsTerminal()
	case *os.File:
		info, err := in.Stat()
		if err != nil {
			return false
		}
		return info.Mode()&os.ModeCharDevice != 0
	default:
		return false
	}
}
