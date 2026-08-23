package store

import (
	"context"
	"testing"
)

func appendItemT(t *testing.T, s *Store, stream, itemID, payload string) (int64, bool) {
	t.Helper()
	var (
		seq      int64
		inserted bool
	)
	if err := s.ObservationTx(context.Background(), func(tx *Tx) error {
		var err error
		seq, inserted, err = tx.AppendStreamItem(stream, itemID, payload)
		return err
	}); err != nil {
		t.Fatalf("appending %s/%s: %v", stream, itemID, err)
	}
	return seq, inserted
}

func TestStreamOrderDedupAndCursor(t *testing.T) {
	s := openAuthorityT(t)
	ctx := context.Background()

	for i, id := range []string{"a", "b", "c"} {
		seq, inserted := appendItemT(t, s, "obs", id, "payload-"+id)
		if !inserted || seq != int64(i+1) {
			t.Fatalf("append %s = (seq %d, inserted %v), want (seq %d, true)", id, seq, inserted, i+1)
		}
	}

	// Re-appending an existing stream-item ID is a durable no-op that
	// reports the original sequence.
	seq, inserted := appendItemT(t, s, "obs", "b", "payload-b-duplicate")
	if inserted || seq != 2 {
		t.Fatalf("duplicate append = (seq %d, inserted %v), want (seq 2, false)", seq, inserted)
	}
	items, err := s.ReadStream(ctx, "obs", 0, 10)
	if err != nil {
		t.Fatalf("ReadStream: %v", err)
	}
	if len(items) != 3 || items[1].Payload != "payload-b" {
		t.Fatalf("stream after duplicate = %+v, want 3 originals", items)
	}

	// Streams sequence independently.
	if seq, _ := appendItemT(t, s, "other", "a", "x"); seq != 1 {
		t.Fatalf("second stream started at seq %d, want 1", seq)
	}

	// Cursor: read two, resume from the last returned seq.
	page, err := s.ReadStream(ctx, "obs", 0, 2)
	if err != nil {
		t.Fatalf("ReadStream(page 1): %v", err)
	}
	if len(page) != 2 || page[0].ItemID != "a" || page[1].ItemID != "b" {
		t.Fatalf("page 1 = %+v, want a,b", page)
	}
	rest, err := s.ReadStream(ctx, "obs", page[1].Seq, 2)
	if err != nil {
		t.Fatalf("ReadStream(resume): %v", err)
	}
	if len(rest) != 1 || rest[0].ItemID != "c" {
		t.Fatalf("resumed page = %+v, want just c", rest)
	}
	if tail, _ := s.ReadStream(ctx, "obs", rest[0].Seq, 2); len(tail) != 0 {
		t.Fatalf("read past end = %+v, want empty", tail)
	}
}
