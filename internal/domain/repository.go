package domain

import (
	"context"
	"errors"
)

// Change is one atomic domain commit: the facts it records, the claim tokens
// it seizes, and the audit entry that explains it. Everything in a Change
// commits together or not at all — there is no partial enrollment.
type Change struct {
	Facts  []Fact
	Claims []ClaimToken
	Audit  AuditEntry
}

// ClaimToken is one durable seizure of an active-claim index entry.
//
// The kernel decides claim ownership from its in-memory index, which is a
// replay of the same durable facts and therefore authoritative for a single
// writer. The token is the durable backstop: the repository must seize it
// with a test-and-set that fails the whole transaction if the entry is
// already held, so a stale or buggy index can refuse an enrollment but can
// never commit a double claim.
type ClaimToken struct {
	Ref        ClaimRef
	Generation int
	Session    SessionID
	Instance   InstanceID
}

// AuditEntry is the caller-facing half of an audit row. The store stamps the
// incarnation and the transaction boundary.
type AuditEntry struct {
	Actor  string
	Target string
	Reason string
	Detail string
}

// Repository is the domain's durability seam. Its four methods are the four
// §4.2 atomic boundaries this domain writes through, named for what the
// domain is committing rather than for the transaction; see
// docs/domain/decisions.md for the verb-to-boundary map.
//
// The interface takes domain records only. No SQL row, JSON document, or
// transaction handle crosses it, which is what keeps the kernel free of the
// things the Go architecture §3 forbids it to own.
type Repository interface {
	// Load returns every recorded fact in commit order. It is the only
	// read: the kernel holds the model in memory and rebuilds it by replay.
	Load(ctx context.Context) ([]Fact, error)

	// CommitIdentity commits a workspace, session, runtime-instance,
	// correlation, attachment, actor-binding, or active-claim change
	// (§4.2's enrollment boundary).
	CommitIdentity(ctx context.Context, c Change) error

	// CommitLaunchResolution commits a launch: its session and
	// runtime-instance links (§4.2's launch-resolution boundary). A failed
	// launch commits no session and no record.
	CommitLaunchResolution(ctx context.Context, c Change) error

	// CommitCommandAcceptance commits an accepted lifecycle command that is
	// a request rather than a fact — stop (§4.2's command-acceptance
	// boundary).
	CommitCommandAcceptance(ctx context.Context, c Change) error

	// CommitObservation commits accepted process or host evidence: live
	// verification, exit, and late-report handling (§4.2's observation
	// boundary).
	CommitObservation(ctx context.Context, c Change) error
}

// ErrClaimTaken reports that a claim token the kernel believed free was
// already held durably. The transaction committed nothing. It means the
// in-memory index disagreed with the store — a second kernel over one
// repository, or a bug — and the durable index won.
var ErrClaimTaken = errors.New("domain: active claim already held")
