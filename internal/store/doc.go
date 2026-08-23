// Package store is duo's authority store: the single SQLite database that
// holds everything durable, and the data-access layer every verb reads and
// writes through.
//
// The open/migrate scaffold is a pure-Go SQLite open (modernc.org/sqlite —
// the Makefile's CGO_ENABLED=0 static-binary commitment forbids a cgo
// driver) plus versioned, forward-only, numbered migrations embedded in the
// binary. On top of it sits the Stage 0 substrate: the exclusive
// authority-writer lease (lease.go), the eight named §4.2 transaction
// boundaries (tx.go), the durable work queue with the §4.3
// prepare/attempt/reconcile protocol (queue.go), the semantic stream log
// (stream.go), and the audit envelope (audit.go). Domain tables (identity,
// sessions, correlations) arrive as later migrations; SQL never leaves this
// package. Lease and takeover rules are documented in
// docs/store/decisions.md.
package store
