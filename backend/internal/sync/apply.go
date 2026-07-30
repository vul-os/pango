package sync

// Materialising a replicated op into its domain table (docs/SYNC.md §3).
//
// This is schema-driven rather than hard-coding a column list per table: the
// column set comes from SQLite's own catalogue (pragma_table_info), and every
// non-special column is read out of the op's JSON payload with json_extract,
// using the same field name for both. That equivalence already holds
// throughout repo/ — every domain struct's JSON tag equals its SQL column
// name (e.g. domain.Job.AssigneeID is `json:"assignee_party_id"`, and job's
// column is `assignee_party_id`) — so this package does not need its own copy
// of every aggregate's shape, and does not go stale when a migration adds a
// column.
//
// id, org_id, hlc and deleted are the four exceptions: they always come from
// the Op itself, never the payload. In particular the payload's own embedded
// "hlc" field is always empty — store.Journal marshals the payload *before*
// the caller assigns the HLC it returns onto the struct (see every
// repo/*.go write) — so op.HLC (the Op's own field) is the only place the
// true stamp lives.

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/vul-os/pango/backend/internal/store"
)

type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// column is one entry from SQLite's own table catalogue.
type column struct {
	name    string
	notNull bool
}

// tableColumns returns tbl's columns, in declaration order, from SQLite's own
// catalogue.
func tableColumns(q queryer, tbl string) ([]column, error) {
	rows, err := q.Query(`SELECT name, "notnull" FROM pragma_table_info(?)`, tbl)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []column
	for rows.Next() {
		var c column
		var notNull int
		if err := rows.Scan(&c.name, &notNull); err != nil {
			return nil, err
		}
		c.notNull = notNull != 0
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// applyToTable materialises one op into its domain table, following the
// merge rule for that table: union tables insert-or-ignore by id; everything
// else is last-writer-wins by HLC, upserted only when the incoming stamp is
// newer than what is already stored — which is how "building is the single
// writer" (ARCHITECTURE.md §5) plays out in practice among the building's own
// devices: there is no separate authority mechanism beyond the HLC order
// itself, because within one organisation any conflicting write is a genuine
// human race that the newest stamp resolves the same way every other
// reference-data conflict does.
func applyToTable(tx *sql.Tx, op store.Op) error {
	if op.Tbl == "" {
		return nil
	}
	cols, err := tableColumns(tx, op.Tbl)
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		// Unknown table: forward compatibility with a newer peer's schema.
		// Ignored silently rather than failing the whole batch.
		return nil
	}
	if !containsCol(cols, "id") {
		return nil
	}

	colNames := make([]string, 0, len(cols))
	exprs := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols)*2)
	for _, c := range cols {
		colNames = append(colNames, c.name)
		switch c.name {
		case "id":
			exprs = append(exprs, "?")
			args = append(args, op.RowID)
		case "org_id":
			exprs = append(exprs, "?")
			args = append(args, op.OrgID)
		case "hlc":
			exprs = append(exprs, "?")
			args = append(args, op.HLC)
		case "deleted":
			exprs = append(exprs, "?")
			args = append(args, boolToInt(op.Deleted))
		default:
			expr := "json_extract(?, '$.' || ?)"
			if !c.notNull {
				// Every nullable column in this schema is an optional
				// foreign key (a job's assignee, a cost entry's party).
				// domain structs hold those as plain Go strings, which
				// json.Marshal renders as "" rather than null when unset —
				// repo/*.go's direct writers convert that through their own
				// nullable() helper; json_extract has no such concept, so an
				// empty string would otherwise be inserted literally and
				// trip the column's FOREIGN KEY constraint instead of
				// leaving the relationship unset.
				expr = "NULLIF(" + expr + ", '')"
			}
			exprs = append(exprs, expr)
			args = append(args, string(op.Payload), c.name)
		}
	}
	colList := strings.Join(colNames, ", ")
	valList := strings.Join(exprs, ", ")

	if ClassOf(op.Tbl) == ClassUnion || !containsCol(cols, "hlc") {
		// No HLC column means this apply path has no way to arbitrate a
		// conflict for that table (none exists in the current schema, but a
		// future one should fail safe rather than silently overwrite).
		q := fmt.Sprintf(`INSERT OR IGNORE INTO %s (%s) VALUES (%s)`, op.Tbl, colList, valList)
		_, err := tx.Exec(q, args...)
		return err
	}

	var setClauses []string
	for _, c := range cols {
		if c.name == "id" {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = excluded.%s", c.name, c.name))
	}
	q := fmt.Sprintf(
		`INSERT INTO %s (%s) VALUES (%s)
		 ON CONFLICT(id) DO UPDATE SET %s WHERE excluded.hlc > %s.hlc`,
		op.Tbl, colList, valList, strings.Join(setClauses, ", "), op.Tbl)
	_, err = tx.Exec(q, args...)
	return err
}

func containsCol(cols []column, name string) bool {
	for _, c := range cols {
		if c.name == name {
			return true
		}
	}
	return false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
