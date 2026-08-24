package launch

// Support answers, from installed evidence alone, whether one materialized
// launch tuple is supported. It is §7.1's third consulted input: "the
// digests and revisions of accepted immutable adapter conformance records
// needed to establish that each declared launch tuple is supported".
//
// An implementation must be a lookup over an immutable, digest-addressed
// record — never a probe. §7.1 forbids resolution from reading current
// reachability, process presence, adapter health, or a probe run for this
// launch; a Support implementation that called an adapter would move launch
// resolution onto the rejected live-state rung (§7.5) without the dated
// contract change that rung requires.
// Under duo.config/v3 an implementation keys on Tuple.SupportKey() —
// (host kind, host version, agent-runtime kind) — and on nothing else. That
// is the thread-5 re-key: config names are gone, and what an installed
// conformance record is about is which host software at which pinned
// version this build drives, and which agent runtime it starts. Two
// variants that differ only in model line, model family, or provider are
// one evidence key, and so are two workspaces bound to two sockets of the
// same kind.
type Support interface {
	// Supported reports the verdict for one tuple. It must be pure: the
	// same tuple and the same installed evidence must produce the same
	// verdict, because §7.2's determinism contract makes the consulted
	// digests part of the resolution's replayable inputs.
	Supported(t Tuple) Verdict
}

// Verdict is one Support answer.
type Verdict struct {
	// OK reports that installed evidence supports the tuple.
	OK bool
	// RecordDigest identifies the immutable conformance record consulted.
	// It is recorded on the launch-resolution record whether or not the
	// verdict was positive, because §7.2 makes the consulted digests part
	// of what a replay needs.
	RecordDigest string
	// Reason is the stable elimination reason when OK is false. Empty
	// falls back to ReasonNoConformanceEvidence.
	Reason string
}

// SupportFunc adapts a function to Support.
type SupportFunc func(Tuple) Verdict

// Supported implements Support.
func (f SupportFunc) Supported(t Tuple) Verdict { return f(t) }

// AllSupported is the deliberate "this installation's declarations are all
// launch-supported" implementation, carrying the digest of the record that
// says so.
//
// It exists so that the permissive case is something an installation opts
// into by name, with a digest attached, rather than something the resolver
// falls back to when no Support is wired. Silently defaulting to permissive
// would collapse §7.1's accepted rung (configuration plus installed
// evidence) into the configuration-only rung §7.5 rejects, and it would do
// it invisibly.
type AllSupported struct {
	RecordDigest string
}

// Supported implements Support.
func (a AllSupported) Supported(Tuple) Verdict {
	return Verdict{OK: true, RecordDigest: a.RecordDigest}
}
