package scrub

import (
	"regexp"
	"strings"
)

// PaneCommand returns shape (b): a single POSIX shell command line that
// runs command with args after scrubbing every marker from whatever
// environment the executing shell actually inherits.
//
// # Why a pane needs its own shape
//
// Herdr's workspace.create/pane.split `env` map can only *set* a
// variable in the pane's shell; verified live, it cannot unset one the
// pane inherits from the Herdr server process (docs/adapters/decisions.md,
// Herdr host adapter section, "Launch mapping is lossy, and the
// environment seam is the reason"). So a Herdr-hosted spawn that does not
// control the server's own launch (and therefore cannot use Guard on the
// server's spawn environment) has exactly one place left to remove a
// marker: inside the pane command itself, before the real command execs.
//
// # Why this returns a string, not an argv
//
// Herdr has no wire method that spawns a process in a pane from a
// structured argv. Verified live against a real 0.8.2 server: the full
// method set has no `pane.run`; the CLI convenience `herdr pane run
// <PANE_ID> <COMMAND>...` is documented as "sends text and Enter in one
// call" and is built entirely on `pane.send_text` (one string) plus a
// key press. Passing a multi-word argument (this wrapper's `-c <script>`)
// through that path reproduces exactly as if a person had typed each
// COMMAND word separated by a space and pressed enter — quoting is not
// preserved across the boundary, so an argv slice would silently break
// apart into the pane's shell's own word-splitting. The only sound shape
// for "a command for a pane" is therefore one line of text the pane's
// shell parses itself, which is what PaneCommand returns.
//
// # Shape
//
// PaneCommand returns:
//
//	env -u CLAUDECODE -u AI_AGENT -u CLAUDE_CODE_CHILD_SESSION \
//	  sh -c '<wildcard sweep>; exec "$@"' duo-scrub 'command' 'args...'
//
// with every token after the outer `env -u ...` individually
// single-quoted (POSIX single-quote escaping, see shellQuote) so the
// pane's shell reconstructs the same argv boundaries this function
// started with, however many
// spaces or special characters command or an argument contains.
//
// The outer `env -u ...` removes the exact-name markers in Markers by
// name — the literal idiom the design calls for. The wildcard markers in
// WildcardPrefixes (CLAUDE_*) cannot be named that way: env -u needs an
// exact name, and the set of CLAUDE_-prefixed variables a harness sets
// is not enumerable ahead of time (it has already changed across Claude
// Code versions this project tracked). So the inner `sh -c` script
// discovers and unsets them at exec time, against whatever the pane's
// shell actually has — not against a snapshot the caller guessed at when
// it built the line. That makes this wrapper correct even when the
// caller has no visibility at all into what the target environment
// carries, which a caller building this line ahead of time never does.
//
// Live-verified (testdata/scrub-live-2026-08-23.md): typed into a pane
// whose Herdr server carried CLAUDECODE, CLAUDE_CODE_CHILD_SESSION, and
// AI_AGENT, this wrapper's line started claude with no "Transcript
// saving is off" warning and the turn that followed produced exactly one
// transcript.
func PaneCommand(command string, args ...string) string {
	var b strings.Builder
	b.WriteString("env")
	for _, m := range Markers {
		b.WriteString(" -u ")
		b.WriteString(m)
	}
	b.WriteString(" sh -c ")
	b.WriteString(shellQuote(wildcardSweepScript()))
	b.WriteString(" duo-scrub ")
	b.WriteString(shellQuote(command))
	for _, a := range args {
		b.WriteByte(' ')
		b.WriteString(shellQuote(a))
	}
	return b.String()
}

// shellQuote wraps s in single quotes for POSIX sh, escaping any single
// quote s itself contains with the standard close-quote, escaped-quote,
// reopen-quote idiom (see the function body). Single quotes are used
// because they are the only POSIX quoting form with no exceptions: no
// character inside them is special, not even backslash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// wildcardSweepScript builds the POSIX sh script that discovers and
// unsets every currently-set variable whose name starts with one of
// WildcardPrefixes, then execs its positional parameters. It is derived
// from WildcardPrefixes rather than hand-written per prefix, so a future
// prefix addition to markers.go changes this script without a second
// edit anywhere.
//
// The discovery step shells out to `env | sed`, not a shell builtin,
// because POSIX sh has no portable "list variable names matching a
// prefix" primitive; bash's ${!prefix@} is not available in every /bin/sh
// this could run under (notably Herdr panes on any Debian/Ubuntu-style
// dash default).
func wildcardSweepScript() string {
	var b []byte
	b = append(b, "for v in $(env"...)
	for _, p := range WildcardPrefixes {
		b = append(b, " | sed -n 's/^\\("...)
		b = append(b, sedEscape(p)...)
		b = append(b, "[^=]*\\)=.*/\\1/p'"...)
	}
	b = append(b, "); do unset \"$v\"; done; exec \"$@\""...)
	return string(b)
}

// sedRegexMeta matches the BRE metacharacters sedEscape backslash-escapes.
// WildcardPrefixes only ever holds plain identifiers today (CLAUDE_), but
// escaping keeps a future prefix containing one of these safe instead of
// silently changing what the sed pattern matches.
var sedRegexMeta = regexp.MustCompile(`[.[\]*^$\\]`)

func sedEscape(prefix string) string {
	return sedRegexMeta.ReplaceAllString(prefix, `\$0`)
}
