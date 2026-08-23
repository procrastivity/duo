package store

import (
	"context"
	"fmt"
)

// StreamItem is one semantic stream record.
type StreamItem struct {
	Seq        int64
	ItemID     string
	Payload    string
	RecordedAt string
}

// AppendStreamItem appends one record to a stream inside the surrounding
// boundary (§4.2 commits stream items with their observation). itemID is the
// stream-scoped dedup key: re-appending an existing itemID writes nothing and
// returns the original sequence with inserted=false. Sequences are dense and
// per-stream; the log is append-only.
func (t *Tx) AppendStreamItem(stream, itemID, payload string) (seq int64, inserted bool, err error) {
	ctx := context.Background()
	res, err := t.tx.ExecContext(ctx,
		`INSERT INTO stream_log (stream, seq, item_id, payload, recorded_at)
		 VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM stream_log WHERE stream = ?), ?, ?, ?)
		 ON CONFLICT (stream, item_id) DO NOTHING`,
		stream, stream, itemID, payload, t.now)
	if err != nil {
		return 0, false, fmt.Errorf("store: %s: stream append: %w", t.op, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, false, fmt.Errorf("store: %s: stream append: %w", t.op, err)
	}
	if err := t.tx.QueryRowContext(ctx,
		`SELECT seq FROM stream_log WHERE stream = ? AND item_id = ?`,
		stream, itemID).Scan(&seq); err != nil {
		return 0, false, fmt.Errorf("store: %s: stream append: %w", t.op, err)
	}
	return seq, n > 0, nil
}

// ReadStream returns up to limit records of one stream with seq > afterSeq,
// in sequence order. afterSeq=0 reads from the start; the last returned Seq
// is the resume cursor.
func (s *Store) ReadStream(ctx context.Context, stream string, afterSeq int64, limit int) ([]StreamItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, item_id, payload, recorded_at FROM stream_log
		 WHERE stream = ? AND seq > ? ORDER BY seq LIMIT ?`,
		stream, afterSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("store: reading stream %s: %w", stream, err)
	}
	defer func() { _ = rows.Close() }()

	var out []StreamItem
	for rows.Next() {
		var it StreamItem
		if err := rows.Scan(&it.Seq, &it.ItemID, &it.Payload, &it.RecordedAt); err != nil {
			return nil, fmt.Errorf("store: reading stream %s: %w", stream, err)
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: reading stream %s: %w", stream, err)
	}
	return out, nil
}
