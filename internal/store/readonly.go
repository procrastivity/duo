package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
)

// OpenReadOnly opens an existing authority database without acquiring the
// writer lease or changing the database or its filesystem. The SQLite
// mode=ro URI is the physical enforcement boundary; query_only is a second
// guard against accidental writes through this connection. immutable is
// deliberately not used, because this reader must observe commits made by a
// concurrent WAL writer.
func OpenReadOnly(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("store: resolving read-only path %s: %w", path, err)
	}
	u := url.URL{Scheme: "file", Path: abs}
	query := u.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	u.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("store: open read-only %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	closeOnError := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("store: connect read-only %s: %w", path, err))
	}
	version, err := schemaVersion(ctx, db)
	if err != nil {
		return closeOnError(err)
	}
	want := latestVersion(register)
	if version != want {
		return closeOnError(fmt.Errorf(
			"store: database is at unsupported schema v%d; this duo read-only projection requires v%d",
			version, want,
		))
	}

	return &Store{db: db, path: path, version: version}, nil
}
