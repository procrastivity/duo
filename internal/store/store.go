package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// migration is one numbered, all-or-nothing schema unit. Never edit a
// shipped entry; append.
type migration struct {
	version int
	name    string
	stmts   []string
}

// register is the ordered migration register, embedded in the binary. Its
// last entry is the version a fresh store is created at. It is empty until
// duo's first durable schema is designed — the machinery is exercised
// against a fixture register in store_test.go, so the first real migration
// lands as an increment, never a retrofit.
var register = []migration{}

// latestVersion is the highest migration this binary carries.
func latestVersion(reg []migration) int {
	if len(reg) == 0 {
		return 0
	}
	return reg[len(reg)-1].version
}

// Store is duo's store handle.
type Store struct {
	db      *sql.DB
	path    string
	version int
}

// Open opens the store at path, creating and migrating it if needed.
func Open(path string) (*Store, error) {
	return openAt(path, register, latestVersion(register))
}

// openAt is Open pinned to a register and a target version. The injection
// exists so a test can populate a database under one version and reopen it
// under the next.
func openAt(path string, reg []migration, target int) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("store: preparing %s: %w", filepath.Dir(path), err)
	}

	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(5000)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// One connection means one writer and no lock contention to reason
	// about; nothing in the chassis wants concurrency.
	db.SetMaxOpenConns(1)

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connect %s: %w", path, err)
	}
	version, err := migrate(ctx, db, reg, target)
	if err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db, path: path, version: version}, nil
}

// Version reports the schema version the open database is at.
func (s *Store) Version() int { return s.version }

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

// migrate brings db up to target and returns the version reached.
// Migrations are versioned, forward-only, numbered, and embedded in the
// binary; downgrades are refused.
func migrate(ctx context.Context, db *sql.DB, reg []migration, target int) (int, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL
	) STRICT`); err != nil {
		return 0, fmt.Errorf("store: migration bookkeeping: %w", err)
	}

	current, err := schemaVersion(ctx, db)
	if err != nil {
		return 0, err
	}
	if current > target {
		return current, fmt.Errorf(
			"store: database is at schema v%d and this duo understands v%d; forward-only migrations mean there is no downgrade path",
			current, target)
	}

	for _, m := range reg {
		if m.version <= current || m.version > target {
			continue
		}
		if err := applyMigration(ctx, db, m); err != nil {
			return current, err
		}
		current = m.version
	}
	return current, nil
}

// applyMigration runs one migration as a single transaction: its statements
// plus its bookkeeping row commit together or not at all.
func applyMigration(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: migration v%d (%s): %w", m.version, m.name, err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range m.stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("store: migration v%d (%s): %w", m.version, m.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`,
		m.version, m.name); err != nil {
		return fmt.Errorf("store: migration v%d (%s) bookkeeping: %w", m.version, m.name, err)
	}
	return tx.Commit()
}

// schemaVersion reads the highest applied migration version, 0 for a fresh
// database.
func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v sql.NullInt64
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, fmt.Errorf("store: reading schema version: %w", err)
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}
