// session.go is the composition root for the `duo session` verb family:
// internal/registry's session.list, session.inspect, session.enroll,
// session.detach, session.reattach, and session.launch operations. It
// wires the CLI directly against internal/domain's Authority kernel (Step
// 14) and internal/launch's resolver (Step 15) — nothing here re-derives a
// rule the kernel or the resolver already owns.
//
// Recovery has no dedicated verb: decision-01 §5.1 makes "recovering" a
// derived view, not a durable state, so `session list` and `session show`
// surface it the same way they surface every other view
// (domain.Authority.View), and the ordinary detach/reattach verbs are how
// an operator acts on a recovering session. See docs/registry/decisions.md
// and notes/30 for why no separate "session recover" operation is
// registered.

package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/doctor"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/store"
)

// outputFlagName is the session family's own result-format flag.
//
// Every session CLI shape the synced contract set carries
// (contracts/fixtures/duo-external-v1/projection-cases.json: session.list
// and session.inspect's "cli.arguments") ends in "--output json", not the
// chassis's global "--json" boolean internal/cliflags documents ("the two
// global flags... a verb never redeclares either flag"). That is a real
// conflict between an already-merged, embedded contract fixture and a
// doc-comment convention two verbs (doctor, manifest) established before
// this fixture synced. This package follows the fixture — code the
// registry's own conformance tests already hold every operation to — and
// records the conflict rather than silently picking one. See
// docs/cli/decisions.md and the step-21 wip findings.
const outputFlagName = "output"

// sessionCommand builds the `duo session` parent verb and every registered
// session.* subcommand.
func sessionCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "list, inspect, enroll, launch, detach, and reattach duo sessions",
	}
	cmd.PersistentFlags().String(outputFlagName, "text", `result format: "text" or "json"`)

	cmd.AddCommand(sessionListCommand(streams))
	cmd.AddCommand(sessionShowCommand(streams))
	cmd.AddCommand(sessionEnrollCommand(streams))
	cmd.AddCommand(sessionLaunchCommand(streams))
	cmd.AddCommand(sessionDetachCommand(streams))
	cmd.AddCommand(sessionReattachCommand(streams))
	return cmd
}

// outputMode reads cmd's --output flag, validates it, and — for "json" —
// mirrors the choice onto the chassis's shared --json flag so that
// internal/cli.Execute's error-envelope selection (which reads only
// cmd.Flags().GetBool("json"), the one path a subcommand cannot otherwise
// influence) agrees with what the operator asked this verb for. See the
// outputFlagName doc comment for why the session family carries its own
// flag instead of just reading --json.
func outputMode(cmd *cobra.Command) (string, error) {
	v, err := cmd.Flags().GetString(outputFlagName)
	if err != nil {
		return "", err
	}
	switch v {
	case "", "text":
		return "text", nil
	case "json":
		// Best-effort: a merged flag set always has "json" (root registers
		// it), so this Set cannot fail in practice. Ignoring a failure here
		// would only cost the JSON error envelope on a code path that is
		// already about to fail some other way.
		_ = cmd.Flags().Set("json", "true")
		return "json", nil
	default:
		return "", duoerr.New("invalid.request",
			fmt.Sprintf("--output must be \"text\" or \"json\", not %q.", v))
	}
}

// requestID mints a diagnostic-only request identifier for a locally
// synthesized duo.external/v1 envelope. Nothing parses it; it exists so a
// CLI --output json result carries the same "request_id" field a real
// presentation-service request would.
func requestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_cli"
	}
	return "req_" + hex.EncodeToString(b[:])
}

// envelope is the duo.external/v1 success envelope every session --output
// json result is wrapped in.
type envelope struct {
	Schema    string   `json:"schema"`
	RequestID string   `json:"request_id"`
	Operation string   `json:"operation"`
	Result    any      `json:"result"`
	Warnings  []any    `json:"warnings"`
	Features  []string `json:"features"`
}

// newEnvelope wraps result in the duo.external/v1 success shape for
// operation.
func newEnvelope(operation string, result any) envelope {
	return envelope{
		Schema:    "duo.external/v1",
		RequestID: requestID(),
		Operation: operation,
		Result:    result,
		Warnings:  []any{},
		Features:  []string{},
	}
}

// --- authority wiring ---------------------------------------------------

// authorityStorePath resolves the authority store's path. Session commands
// reuse doctor's default XDG resolution: one store, one path convention,
// for every verb that opens it (docs/doctor/decisions.md).
func authorityStorePath() (string, error) {
	return doctor.DefaultStorePath()
}

// openReadAuthority opens the authority store read-only and replays it into
// a domain.Authority, without ever creating a store file that was not
// already there — mirroring doctor's probeStore discipline
// (docs/doctor/decisions.md: "a missing store is not an error ... and
// doctor never creates one just by checking"). A session list or show
// against an uninitialized installation sees zero sessions, not a
// freshly-created empty database.
func openReadAuthority(ctx context.Context) (*domain.Authority, io.Closer, error) {
	path, err := authorityStorePath()
	if err != nil {
		return nil, nil, duoerr.New("internal.store_path_unresolved", fmt.Sprintf("resolving the authority store path: %v", err))
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		a, err := domain.Open(ctx, emptyRepository{})
		if err != nil {
			return nil, nil, duoerr.New("internal.authority_open_failed", fmt.Sprintf("opening an empty authority: %v", err))
		}
		return a, nopCloser{}, nil
	} else if statErr != nil {
		return nil, nil, duoerr.New("internal.store_stat_failed", fmt.Sprintf("checking %s: %v", path, statErr))
	}

	s, err := store.Open(path)
	if err != nil {
		return nil, nil, duoerr.New("internal.store_open_failed", fmt.Sprintf("opening the authority store: %v", err))
	}
	a, err := domain.Open(ctx, storerepo.New(s))
	if err != nil {
		_ = s.Close()
		return nil, nil, duoerr.New("internal.authority_open_failed", fmt.Sprintf("replaying the authority store: %v", err))
	}
	return a, s, nil
}

// openWriteAuthority opens the authority store as the durable writer (the
// authority-writer lease, store.OpenAuthority), creating it if this is the
// first write any verb has made. Every session verb that commits a change
// (enroll, launch, detach, reattach) goes through this — never through
// openReadAuthority — so that the domain kernel's per-restart incarnation
// (decision-01 §4.4) is always minted by the same seam step 22 restarts
// through.
func openWriteAuthority(ctx context.Context) (*domain.Authority, *store.Store, error) {
	path, err := authorityStorePath()
	if err != nil {
		return nil, nil, duoerr.New("internal.store_path_unresolved", fmt.Sprintf("resolving the authority store path: %v", err))
	}
	s, err := store.OpenAuthority(path)
	if err != nil {
		var active *store.WriterActiveError
		if errors.As(err, &active) {
			return nil, nil, duoerr.New("refusal.authority_writer_active",
				fmt.Sprintf("another duo process already holds the authority-writer lease (incarnation=%s, pid=%d, host=%s, expires=%s).",
					active.Incarnation, active.PID, active.Hostname, active.ExpiresAt))
		}
		return nil, nil, duoerr.New("internal.store_open_failed", fmt.Sprintf("acquiring the authority-writer lease: %v", err))
	}
	a, err := domain.Open(ctx, storerepo.New(s))
	if err != nil {
		_ = s.Close()
		return nil, nil, duoerr.New("internal.authority_open_failed", fmt.Sprintf("replaying the authority store: %v", err))
	}
	return a, s, nil
}

// nopCloser is io.Closer for the empty-repository path, where there is no
// store handle to release.
type nopCloser struct{}

func (nopCloser) Close() error { return nil }

// emptyRepository is domain.Repository over an installation with no
// authority store yet: Load reports no facts, and nothing ever commits
// through it (openReadAuthority's callers never write).
type emptyRepository struct{}

func (emptyRepository) Load(context.Context) ([]domain.Fact, error) { return nil, nil }
func (emptyRepository) CommitIdentity(context.Context, domain.Change) error {
	return errors.New("cli: emptyRepository is read-only; no authority store exists yet")
}

func (emptyRepository) CommitLaunchResolution(context.Context, domain.Change) error {
	return errors.New("cli: emptyRepository is read-only; no authority store exists yet")
}

func (emptyRepository) CommitCommandAcceptance(context.Context, domain.Change) error {
	return errors.New("cli: emptyRepository is read-only; no authority store exists yet")
}

func (emptyRepository) CommitObservation(context.Context, domain.Change) error {
	return errors.New("cli: emptyRepository is read-only; no authority store exists yet")
}

// --- shared rendering -----------------------------------------------------

// lifecycle projects a domain.SessionState onto duo-external-v1's coarser
// "lifecycle" enum (active, archived, removed — decision-01 §5.1's
// "inactive" durable state has no separate wire lifecycle value; it is
// still "active" at that grain, with the finer distinction carried by
// "view").
func lifecycle(s domain.SessionState) string {
	switch s {
	case domain.SessionArchived:
		return "archived"
	case domain.SessionRemoved:
		return "removed"
	default:
		return "active"
	}
}

// duoerrFromDomain maps a domain kernel refusal onto the chassis's
// structured error, choosing a "refusal." prefix for a guard tripping (exit
// 3) and leaving everything else at the default user-failure exit (1).
// Codes reuse the registry's registered stable set where one applies
// (docs/registry/decisions.md); the rest are local, diagnostic-only tokens
// — the kernel's Go error values are the contract, not a wire code these
// verbs promise to keep stable.
func duoerrFromDomain(err error) *duoerr.Error {
	var conflict *domain.ConflictError
	if errors.As(err, &conflict) {
		return duoerr.New("session.enrollment_conflict", conflict.Error())
	}
	switch {
	case errors.Is(err, domain.ErrUnknownObject):
		return duoerr.New("object.not_found", err.Error())
	case errors.Is(err, domain.ErrIllegalTransition),
		errors.Is(err, domain.ErrInstanceExited),
		errors.Is(err, domain.ErrSessionState),
		errors.Is(err, domain.ErrNoCurrentInstance),
		errors.Is(err, domain.ErrNotAttested),
		errors.Is(err, domain.ErrActorBound),
		errors.Is(err, domain.ErrIntentRequired),
		errors.Is(err, domain.ErrContinuityUnverified):
		return duoerr.New("refusal.session_guard", err.Error())
	case errors.Is(err, domain.ErrWeakFingerprint), errors.Is(err, domain.ErrRootPathRequired):
		return duoerr.New("invalid.request", err.Error())
	default:
		return duoerr.New("internal.session_command_failed", err.Error())
	}
}
