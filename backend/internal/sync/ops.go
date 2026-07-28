package sync

// Reading and applying the oplog (docs/SYNC.md §6): version vectors, the
// push/pull deltas they drive, and the idempotent apply path shared by the
// HTTP transport and the folder transport.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/vul-os/propfix/backend/internal/store"
)

// OpsAfter returns ops that a peer holding vector lacks: for each op, it is
// included if its author is absent from vector or its HLC is newer than that
// author's entry. Ordering is ascending HLC, so a caller can stop at the
// first short batch and a multi-round push never sends the same op twice.
func (e *Engine) OpsAfter(vector map[string]string, limit int) ([]store.Op, error) {
	rows, err := e.s.DB().Query(
		`SELECT hlc, author, org_id, tbl, row_id, deleted, payload, cose
		 FROM oplog ORDER BY hlc ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Op
	for rows.Next() {
		op, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		if op.HLC > vector[op.Author] {
			out = append(out, op)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, rows.Err()
}

// OwnOpsAfter returns this node's own ops minted after hwm (a high-water HLC
// mark), oldest first. Used by the folder transport's incremental export.
func (e *Engine) OwnOpsAfter(hwm string) ([]store.Op, error) {
	rows, err := e.s.DB().Query(
		`SELECT hlc, author, org_id, tbl, row_id, deleted, payload, cose
		 FROM oplog WHERE author = ? AND hlc > ? ORDER BY hlc ASC`, e.NodeID(), hwm)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Op
	for rows.Next() {
		op, err := scanOp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

func scanOp(rows *sql.Rows) (store.Op, error) {
	var op store.Op
	var deleted int
	var payload string
	var cose sql.NullString
	if err := rows.Scan(&op.HLC, &op.Author, &op.OrgID, &op.Tbl, &op.RowID,
		&deleted, &payload, &cose); err != nil {
		return store.Op{}, err
	}
	op.Deleted = deleted != 0
	op.Payload = json.RawMessage(payload)
	op.Cose = cose.String
	return op, nil
}

// ApplyOps journals and materialises a batch of remote ops, idempotently:
// applying the same op twice is a no-op (INSERT OR IGNORE keyed on hlc). Ops
// belonging to an organisation this node does not (yet) hold are dropped —
// the safety net against two unrelated organisations sharing a pairing
// secret by accident (docs/SYNC.md §8 threat table); an "organisation" op is
// the one exception, since it is what makes an org known in the first place.
//
// Ops can arrive out of their causal order — a batch boundary or an
// interleaved folder import landing a "job" row before the "building" op it
// references — so ApplyOps retries what it could not yet place, in bounded
// passes, rather than failing the whole batch or a single peer's stream
// permanently on one out-of-order row.
func (e *Engine) ApplyOps(ops []store.Op) (int, error) {
	pending := ops
	applied := 0
	for len(pending) > 0 {
		var deferred []store.Op
		for _, op := range pending {
			err := e.applyOne(op)
			switch {
			case err == nil:
				e.s.Observe(op.HLC)
				applied++
			case isRetryable(err):
				deferred = append(deferred, op)
			default:
				return applied, err
			}
		}
		if len(deferred) == len(pending) {
			// No progress this pass: whatever remains cannot be placed (a
			// genuinely missing dependency, or a foreign org). Stop rather
			// than spin to no purpose.
			break
		}
		pending = deferred
	}
	return applied, nil
}

// errOrgUnknown signals a batch op whose organisation this node does not (yet)
// hold. It is retryable: a later pass over the same batch may have applied the
// organisation row by then (see isRetryable).
var errOrgUnknown = errors.New("organisation not locally known")

func (e *Engine) applyOne(op store.Op) error {
	known, err := e.orgKnown(op.OrgID, op.Tbl)
	if err != nil {
		return err
	}
	if !known {
		return errOrgUnknown
	}
	return e.s.Tx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO oplog (hlc, author, org_id, tbl, row_id, deleted, payload, cose, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			op.HLC, op.Author, op.OrgID, op.Tbl, op.RowID, boolToInt(op.Deleted),
			string(op.Payload), op.Cose, store.Now()); err != nil {
			return err
		}
		return applyToTable(tx, op)
	})
}

func (e *Engine) orgKnown(orgID, tbl string) (bool, error) {
	if tbl == "organisation" {
		return true, nil
	}
	var n int
	err := e.s.DB().QueryRow(`SELECT COUNT(*) FROM organisation WHERE id = ?`, orgID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// isRetryable reports whether err is a condition a later pass over the same
// batch might resolve: a dependency (the organisation, or a foreign-key
// referent such as the building a job points at) that simply has not been
// applied yet.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if err == errOrgUnknown {
		return true
	}
	return strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
