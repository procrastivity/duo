package store

// substrateV1 is the Stage 0 substrate schema: the writer lease, the durable
// work queue with its attempt records (§4.3 prepare/attempt/reconcile), the
// semantic stream log, and the audit log. Domain tables (identity, sessions,
// correlations) are a Stage 1 migration, not part of the substrate.
//
// Timestamps are TEXT in the fixed layout timestampLayout, which sorts
// lexically, so SQL and Go can compare them without parsing.
var substrateV1 = migration{
	version: 1,
	name:    "substrate",
	stmts: []string{
		// One row, id fixed at 1: the exclusive authority-writer lease.
		// Ownership is fenced inside every write transaction, so a
		// takeover invalidates the usurped writer's pending commits.
		`CREATE TABLE writer_lease (
			id          INTEGER PRIMARY KEY CHECK (id = 1),
			incarnation TEXT    NOT NULL,
			pid         INTEGER NOT NULL,
			hostname    TEXT    NOT NULL,
			acquired_at TEXT    NOT NULL,
			expires_at  TEXT    NOT NULL
		) STRICT`,

		// Durable work items. 'indeterminate' is terminal-by-default: an
		// item whose attempt was marked unknown_effect; nothing claims it
		// automatically (§4.3).
		`CREATE TABLE work_queue (
			id               INTEGER PRIMARY KEY,
			kind             TEXT NOT NULL,
			payload          TEXT NOT NULL,
			state            TEXT NOT NULL DEFAULT 'pending'
				CHECK (state IN ('pending', 'claimed', 'done', 'failed', 'indeterminate')),
			attempts         INTEGER NOT NULL DEFAULT 0,
			claimed_by       TEXT,
			claim_expires_at TEXT,
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL
		) STRICT`,
		`CREATE INDEX work_queue_claimable ON work_queue (state, id)`,

		// One row per claim, created durably before the external call runs.
		// A row still 'started' after its incarnation died is the §4.3
		// unresolved case: reconcile or mark unknown_effect, never retry
		// blind. external_token is §4.3's retained external attempt
		// identity: minted and committed with the claim, so a writer that
		// died mid-call still leaves the identity a result lookup needs.
		`CREATE TABLE work_attempt (
			id             INTEGER PRIMARY KEY,
			work_id        INTEGER NOT NULL REFERENCES work_queue (id),
			attempt        INTEGER NOT NULL,
			incarnation    TEXT    NOT NULL,
			external_token TEXT    NOT NULL,
			state          TEXT    NOT NULL DEFAULT 'started'
				CHECK (state IN ('started', 'succeeded', 'failed', 'unknown_effect')),
			result         TEXT,
			started_at     TEXT NOT NULL,
			finished_at    TEXT,
			UNIQUE (work_id, attempt),
			UNIQUE (external_token)
		) STRICT`,

		// Append-only per-stream ordered records. (stream, item_id) is the
		// dedup key; (stream, seq) is the read cursor.
		`CREATE TABLE stream_log (
			stream      TEXT    NOT NULL,
			seq         INTEGER NOT NULL,
			item_id     TEXT    NOT NULL,
			payload     TEXT    NOT NULL,
			recorded_at TEXT    NOT NULL,
			PRIMARY KEY (stream, seq),
			UNIQUE (stream, item_id)
		) STRICT`,

		// Audit rows are stamped with the writing incarnation and the
		// transaction-boundary operation by Tx.AppendAudit; callers supply
		// only the actor-facing fields.
		`CREATE TABLE audit_log (
			id          INTEGER PRIMARY KEY,
			incarnation TEXT NOT NULL,
			operation   TEXT NOT NULL,
			actor       TEXT NOT NULL,
			target      TEXT NOT NULL DEFAULT '',
			reason      TEXT NOT NULL DEFAULT '',
			detail      TEXT NOT NULL DEFAULT '',
			recorded_at TEXT NOT NULL
		) STRICT`,
	},
}
