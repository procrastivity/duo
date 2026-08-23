// Package domain is duo's identity and lifecycle kernel: workspaces, Duo
// sessions, runtime instances, agent actors, host attachments, correlation
// records, the active-claim index, and the lifecycle fact history.
//
// Its normative source is duo-vnext-decision-01-identity-lifecycle.md §4–§5
// in the planning repository. The three rules that shape every type here:
//
//   - A Duo ID is issued by this authority and is never derived from an
//     external identifier. A pane, PID, transcript path, working directory,
//     or agent-runtime session ID is a correlation, never an identity.
//   - Enrollment is one atomic operation against an active-claim index.
//     Repeating it returns the same Duo session; overlapping evidence
//     enrolls nothing.
//   - Exit is final for a runtime instance. A replacement process in the
//     same pane always gets a new runtime-instance ID.
//
// Per the Go architecture's §3 responsibility table the domain kernel owns
// no SQL, no JSON, and no adapter protocol. Durability therefore arrives
// through the Repository interface, whose only implementation
// (internal/domain/storerepo) speaks internal/store's §4.2 transaction
// boundaries. The kernel keeps the whole model in memory and rebuilds it by
// replaying the durable fact log; see docs/domain/decisions.md for why, and
// for the boundary each verb commits through.
package domain
