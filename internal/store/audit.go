package store

import (
	"context"
	"fmt"
)

// Audit is the caller-supplied half of an audit row. The store stamps the
// rest: the writer incarnation, the boundary operation, and the record time
// come from the transaction, so a caller cannot mislabel them.
type Audit struct {
	Actor  string
	Target string
	Reason string
	Detail string
}

// AppendAudit writes one audit row inside the surrounding boundary, stamped
// with this writer's incarnation and the boundary's operation name.
func (t *Tx) AppendAudit(a Audit) error {
	if _, err := t.tx.ExecContext(context.Background(),
		`INSERT INTO audit_log (incarnation, operation, actor, target, reason, detail, recorded_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.s.incarnation, t.op, a.Actor, a.Target, a.Reason, a.Detail, t.now); err != nil {
		return fmt.Errorf("store: %s: audit append: %w", t.op, err)
	}
	return nil
}
