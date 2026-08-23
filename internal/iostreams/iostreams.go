// Package iostreams carries the stdout/stderr writer pair every verb
// constructor receives, so the stdout-carries-exactly-one-thing discipline is
// structural (threaded in) rather than a matter of remembering not to call
// fmt.Println.
package iostreams

import (
	"io"
	"os"
)

// Streams is the writer pair threaded into every verb constructor.
type Streams struct {
	Out io.Writer
	Err io.Writer
}

// System returns the real process streams.
func System() *Streams {
	return &Streams{Out: os.Stdout, Err: os.Stderr}
}
