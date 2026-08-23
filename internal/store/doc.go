// Package store is duo's store: the single SQLite database that holds
// everything durable, and the data-access layer every verb reads and writes
// through.
//
// The chassis ships only the open/migrate scaffold: a pure-Go SQLite open
// (modernc.org/sqlite — the Makefile's CGO_ENABLED=0 static-binary
// commitment forbids a cgo driver) and versioned, forward-only, numbered
// migrations embedded in the binary. The database records the versions it
// has applied; at store-open the binary applies every migration newer than
// that, and downgrades are refused. wip's store shape (event-sourced log,
// projections, envelope stamping) is domain code and was deliberately not
// copied — duo's own durable schema is a design decision for the planning
// program, not a chassis import.
package store
