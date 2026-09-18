package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadOnlyDoesNotCreateMissingDatabaseOrParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "missing")
	path := filepath.Join(parent, "duo.db")

	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("OpenReadOnly(missing) succeeded")
	}
	if _, err := os.Stat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing parent changed: stat error = %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("OpenReadOnly created filesystem entries: %v", entryNames(entries))
	}
}

func TestOpenReadOnlyIsQueryOnlyAndDoesNotMutateDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "duo.db")
	writable, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := writable.Close(); err != nil {
		t.Fatalf("Close writable: %v", err)
	}
	before := databaseSnapshot(t, path)

	reader, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	var queryOnly int
	if err := reader.db.QueryRowContext(context.Background(), `PRAGMA query_only`).Scan(&queryOnly); err != nil {
		t.Fatalf("PRAGMA query_only: %v", err)
	}
	if queryOnly != 1 {
		t.Fatalf("PRAGMA query_only = %d, want 1", queryOnly)
	}
	if _, err := reader.db.ExecContext(context.Background(), `CREATE TABLE forbidden (id INTEGER)`); err == nil {
		t.Fatal("write through read-only database handle succeeded")
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Close reader: %v", err)
	}
	after := databaseSnapshot(t, path)
	if after != before {
		t.Fatalf("read-only open mutated database:\nbefore: %+v\nafter:  %+v", before, after)
	}
}

func TestOpenReadOnlySeesConcurrentWALCommits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")
	writer, err := Open(path)
	if err != nil {
		t.Fatalf("Open writer: %v", err)
	}
	defer func() { _ = writer.Close() }()
	reader, err := OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = reader.Close() }()

	ctx := context.Background()
	items, err := reader.ReadStream(ctx, "concurrent", 0, 10)
	if err != nil {
		t.Fatalf("initial ReadStream: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("initial stream has %d items", len(items))
	}
	if _, err := writer.db.ExecContext(ctx, `
		INSERT INTO stream_log (stream, seq, item_id, payload, recorded_at)
		VALUES ('concurrent', 1, 'item-1', 'committed', '2026-09-18T00:00:00.000Z')`); err != nil {
		t.Fatalf("concurrent writer commit: %v", err)
	}
	items, err = reader.ReadStream(ctx, "concurrent", 0, 10)
	if err != nil {
		t.Fatalf("ReadStream after commit: %v", err)
	}
	if len(items) != 1 || items[0].Payload != "committed" {
		t.Fatalf("reader did not see committed WAL item: %+v", items)
	}
}

func TestOpenReadOnlyRejectsUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")
	writable, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := writable.db.ExecContext(context.Background(), `
		INSERT INTO schema_migrations (version, name, applied_at)
		VALUES (?, 'future', '2026-09-18T00:00:00.000Z')`, latestVersion(register)+1); err != nil {
		t.Fatalf("install future schema marker: %v", err)
	}
	if err := writable.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("OpenReadOnly accepted a future schema")
	}
}

type fileState struct {
	Mode    os.FileMode
	Size    int64
	ModTime int64
	Digest  [sha256.Size]byte
}

func databaseSnapshot(t *testing.T, path string) fileState {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%s): %v", path, err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return fileState{
		Mode: info.Mode(), Size: info.Size(), ModTime: info.ModTime().UnixNano(),
		Digest: sha256.Sum256(contents),
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}
