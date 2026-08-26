// Package storerepo is the one implementation of domain.Repository: it
// persists the identity and lifecycle kernel's facts and active claims
// through internal/store's §4.2 transaction boundaries.
//
// It exists as a separate package so that the kernel keeps none of what the
// Go architecture's §3 responsibility table forbids it — no SQL, no JSON, no
// transaction handle. Everything wire-shaped lives here.
//
// The durable representation is the store's append-only stream log rather
// than domain tables of its own. docs/domain/decisions.md records why, what
// that buys (the active-claim index becomes a test-and-set the store
// enforces), and what it costs.
package storerepo

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/store"
)

// The two domain-owned streams. Both names carry a version: the payloads are
// a durable format, and a change to their shape has to be a new stream, not
// a reinterpretation of the old one.
const (
	// factStream is the ordered lifecycle fact log the kernel replays.
	factStream = "duo.domain.fact/v1"
	// claimStream is the active-claim index. Its item ID is the claim key
	// plus its generation, so inserting one is the test-and-set that makes
	// a double claim impossible even if the caller's index is stale.
	claimStream = "duo.domain.claim/v1"
	// loadPage bounds one read of the fact log.
	loadPage = 500
)

// Repo persists the identity and lifecycle domain in a duo authority store.
type Repo struct {
	s *store.Store

	// beforeCommit, when set, runs after a boundary's body and before its
	// commit. Test-only crash-injection hook, mirroring the store's own;
	// production never sets it.
	beforeCommit func() error
}

// New returns a repository over an authority-writer store handle.
func New(s *store.Store) *Repo { return &Repo{s: s} }

// Incarnation reports the writer incarnation the store minted when this
// handle acquired the authority lease. It satisfies domain.IncarnationReporter,
// which is how the kernel stamps §4.4's per-restart incarnation onto every
// fact it records.
func (r *Repo) Incarnation() string { return r.s.Incarnation() }

// Load returns every recorded fact in commit order.
func (r *Repo) Load(ctx context.Context) ([]domain.Fact, error) {
	var (
		out   []domain.Fact
		after int64
	)
	for {
		items, err := r.s.ReadStream(ctx, factStream, after, loadPage)
		if err != nil {
			return nil, fmt.Errorf("storerepo: loading the fact log: %w", err)
		}
		for _, it := range items {
			f, err := decodeFact(it.Payload)
			if err != nil {
				return nil, fmt.Errorf("storerepo: decoding fact %s: %w", it.ItemID, err)
			}
			out = append(out, f)
			after = it.Seq
		}
		if len(items) < loadPage {
			return out, nil
		}
	}
}

// CommitIdentity commits an identity or lifecycle change through the
// enrollment boundary.
func (r *Repo) CommitIdentity(ctx context.Context, c domain.Change) error {
	return r.commit(ctx, r.s.EnrollmentTx, c)
}

// CommitLaunchResolution commits a launch through the launch-resolution
// boundary.
func (r *Repo) CommitLaunchResolution(ctx context.Context, c domain.Change) error {
	return r.commit(ctx, r.s.LaunchResolutionTx, c)
}

// CommitCommandAcceptance commits an accepted lifecycle command through the
// command-acceptance boundary.
func (r *Repo) CommitCommandAcceptance(ctx context.Context, c domain.Change) error {
	return r.commit(ctx, r.s.CommandAcceptTx, c)
}

// CommitCommandTransition commits one prompt-command transition through the
// command-transition boundary.
func (r *Repo) CommitCommandTransition(ctx context.Context, c domain.Change) error {
	return r.commit(ctx, r.s.CommandTransitionTx, c)
}

// CommitObservation commits accepted process or host evidence through the
// observation boundary.
func (r *Repo) CommitObservation(ctx context.Context, c domain.Change) error {
	return r.commit(ctx, r.s.ObservationTx, c)
}

// InjectBeforeCommit is a test-only crash-injection hook. A non-nil fn
// runs after the boundary body writes and before the store commits,
// mirroring store.Store's beforeCommit. Production never calls it.
func (r *Repo) InjectBeforeCommit(fn func() error) {
	r.beforeCommit = fn
}

// commit writes one Change inside one store boundary: claims first, so a
// contested claim aborts before anything else is written, then the facts,
// then the audit row. Everything commits together or not at all.
func (r *Repo) commit(
	ctx context.Context,
	boundary func(context.Context, func(*store.Tx) error) error,
	c domain.Change,
) error {
	return boundary(ctx, func(tx *store.Tx) error {
		for _, t := range c.Claims {
			payload, err := encodeToken(t)
			if err != nil {
				return err
			}
			_, inserted, err := tx.AppendStreamItem(claimStream, tokenID(t), payload)
			if err != nil {
				return fmt.Errorf("storerepo: seizing claim %s: %w", t.Ref, err)
			}
			if !inserted {
				// The durable index already holds this entry. Whatever the
				// caller believed, this transaction commits nothing.
				return fmt.Errorf("%w: %s generation %d", domain.ErrClaimTaken, t.Ref, t.Generation)
			}
		}
		for _, f := range c.Facts {
			payload, err := encodeFact(f)
			if err != nil {
				return err
			}
			_, inserted, err := tx.AppendStreamItem(factStream, string(f.ID), payload)
			if err != nil {
				return fmt.Errorf("storerepo: appending fact %s: %w", f.ID, err)
			}
			if !inserted {
				return fmt.Errorf("storerepo: fact id %s is already recorded", f.ID)
			}
		}
		if c.Audit != (domain.AuditEntry{}) {
			if err := tx.AppendAudit(store.Audit{
				Actor:  c.Audit.Actor,
				Target: c.Audit.Target,
				Reason: c.Audit.Reason,
				Detail: c.Audit.Detail,
			}); err != nil {
				return err
			}
		}
		if r.beforeCommit != nil {
			return r.beforeCommit()
		}
		return nil
	})
}

// tokenID is one active-claim entry's durable identity: the claim reference
// plus the generation it is being taken at. The generation is what lets a
// key be re-taken after its holder exited and released it, while a repeat of
// the *current* generation collides and refuses.
func tokenID(t domain.ClaimToken) string {
	return t.Ref.String() + "#" + strconv.Itoa(t.Generation)
}

func encodeToken(t domain.ClaimToken) (string, error) {
	payload, err := json.Marshal(wireToken{
		Kind:       string(t.Ref.Kind),
		Key:        string(t.Ref.Key),
		Generation: t.Generation,
		Session:    string(t.Session),
		Instance:   string(t.Instance),
	})
	if err != nil {
		return "", fmt.Errorf("storerepo: encoding claim %s: %w", t.Ref, err)
	}
	return string(payload), nil
}

func encodeFact(f domain.Fact) (string, error) {
	payload, err := json.Marshal(toWire(f))
	if err != nil {
		return "", fmt.Errorf("storerepo: encoding fact %s: %w", f.ID, err)
	}
	return string(payload), nil
}

func decodeFact(payload string) (domain.Fact, error) {
	var w wireFact
	if err := json.Unmarshal([]byte(payload), &w); err != nil {
		return domain.Fact{}, err
	}
	return fromWire(w), nil
}
