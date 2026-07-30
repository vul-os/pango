package sync

// MergeClasses is a hand-written list, and the failure mode of a hand-written
// list is that the schema grows past it. A table added by a migration and given
// no entry would fall through ClassOf to ClassUnknown, which applyToTable reads
// as "not a union table" — so it would silently acquire last-writer-wins and
// stay that way until someone noticed two concurrent entries had become one.
//
// These tests hold the list against the live schema in both directions.

import (
	"path/filepath"
	"testing"

	"github.com/vul-os/pango/backend/internal/store"
)

// replicatedTables reads the tables the schema itself marks as replicated: an
// `id` primary key to address a row by, and an `hlc` column to place the write
// in the total order. oplog carries an hlc but addresses rows by `row_id`; it is
// the journal, not a replicated table, and is excluded by the `id` requirement.
func replicatedTables(t *testing.T) map[string]map[string]bool {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	rows, err := s.DB().Query(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list tables: %v", err)
	}

	out := map[string]map[string]bool{}
	for _, n := range names {
		cols, err := tableColumns(s.DB(), n)
		if err != nil {
			t.Fatalf("columns of %s: %v", n, err)
		}
		set := map[string]bool{}
		for _, c := range cols {
			set[c.name] = true
		}
		if set["id"] && set["hlc"] {
			out[n] = set
		}
	}
	if len(out) == 0 {
		t.Fatal("found no replicated tables at all — this test would pass by checking nothing")
	}
	return out
}

// Every replicated table in the schema must be classified. This is the
// direction that catches a migration that landed without a merge rule.
func TestEveryReplicatedTableIsClassified(t *testing.T) {
	for tbl := range replicatedTables(t) {
		if ClassOf(tbl) == ClassUnknown {
			t.Errorf("table %q has an id and an hlc column, so it replicates, but MergeClasses "+
				"gives it no rule — it would default to last-writer-wins. Classify it in "+
				"classes.go and record the choice in docs/SYNC.md §3 and in "+
				"internal/sync/substrate's package doc.", tbl)
		}
	}
}

// And the other direction: an entry naming a table that no longer exists, or
// that does not carry the columns its class needs.
func TestClassifiedTablesMatchTheSchema(t *testing.T) {
	schema := replicatedTables(t)
	for tbl, class := range MergeClasses {
		cols, ok := schema[tbl]
		if !ok {
			t.Errorf("MergeClasses classifies %q, which is not a replicated table in the "+
				"current schema", tbl)
			continue
		}
		switch class {
		case ClassRegister:
			// A register's deletion is a replicated flag; without the column
			// there is no way to express one and applyToTable would write the
			// op's Deleted into nothing.
			if !cols["deleted"] {
				t.Errorf("%q is a register but has no `deleted` column, so a replicated "+
					"soft delete has nowhere to land", tbl)
			}
		case ClassUnion:
			// The converse is the load-bearing one: a `deleted` column on an
			// add-only table would offer a retraction the union merge cannot
			// carry, and the row would come back on the next peer that never
			// saw it (docs/SYNC.md §4 — a correction is a new row).
			if cols["deleted"] {
				t.Errorf("%q is add-only but carries a `deleted` column. Union merge cannot "+
					"replicate a retraction; either the column is a mistake or the table is "+
					"really a register", tbl)
			}
		}
	}
}
