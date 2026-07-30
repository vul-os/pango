package store

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// Pango's schema is create-only from here on: a new migration may CREATE, and
// that is all.
//
// What it buys is that the SQL in migrations/ reads as the schema. A reader who
// wants to know the shape of `job` reads one CREATE TABLE, rather than
// reconstructing it by replaying four files and holding the intermediate states
// in their head.
//
// DELETE is refused for a second reason: the oplog is append-only and deletion is
// a tombstone (`deleted INTEGER`), so a migration that removed rows would be
// removing history that peers still hold and will replicate straight back.
//
// UPDATE is deliberately NOT forbidden — trg_job_number_dedupe's body is built
// from UPDATE statements, and a trigger that fires on future inserts is not a
// data migration.
//
// # Why there is a grandfather list rather than a clean tree
//
// 201 and 301 are ALTER-based, and they stay that way. The tempting fix — fold
// them back into 200 and 300 and delete the files — is unsafe, and the reason is
// worth stating plainly because it will be tempting again:
//
// migrate.go records applied state by VERSION NUMBER ALONE (`applied[m.version]`,
// migrate.go:110). There is no content hash. So on a database that applied 200
// but not yet 201, folding 201's body into 200 means the runner sees version 200
// already recorded, skips it, finds no 201 to run — and the `created_hlc` column,
// the job-number dedupe trigger and the inspection→job link are never created.
// No error. The database simply runs on, missing three pieces of schema, until
// something reads a column that was never added.
//
// The claim that would make folding safe is "no database anywhere has ever had
// the older shape". That claim is unverifiable — 201 and 301 are in committed
// history (87fcdc9, c43fd4c), install.sh is real, and docs/CLOUD-NODE.md tells
// people how to run a node — so it cannot be relied on. An unverifiable premise
// guarding a silent failure is not a trade worth taking for a tidier diff.
//
// So: applied ids are history and are never renumbered, folded or deleted. The
// list below is FROZEN. It may shrink if a migration is genuinely retired; it
// must never grow, and the length assertion below enforces that.
func TestMigrationsAreCreateOnly(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations embedded — the embed.FS pattern is broken")
	}

	// Whole words only, and case-insensitively: `deleted` is a column name in
	// almost every table here and must not trip the DELETE check.
	forbidden := map[string]*regexp.Regexp{
		"ALTER":  regexp.MustCompile(`(?i)\bALTER\b`),
		"DROP":   regexp.MustCompile(`(?i)\bDROP\b`),
		"DELETE": regexp.MustCompile(`(?i)\bDELETE\b`),
	}

	// FROZEN. Keyed "<version>_<name>.sql:<keyword>". Every entry is a migration
	// that was already applied by real databases before the create-only rule
	// existed, so it can never be edited. See the doc comment above.
	grandfathered := map[string]bool{
		"201_job_number_dedupe.sql:DROP":    true,
		"201_job_number_dedupe.sql:ALTER":   true,
		"301_inspection_job_link.sql:ALTER": true,
	}
	const wantGrandfathered = 3

	seen := map[string]bool{}
	for _, m := range migrations {
		for i, line := range strings.Split(stripSQLComments(m.sql), "\n") {
			for keyword, re := range forbidden {
				if !re.MatchString(line) {
					continue
				}
				key := fmt.Sprintf("%d_%s.sql:%s", m.version, m.name, keyword)
				if grandfathered[key] {
					seen[key] = true
					continue
				}
				t.Errorf("%d_%s.sql:%d uses %s — new migrations are create-only:\n\t%s",
					m.version, m.name, i+1, keyword, strings.TrimSpace(line))
			}
		}
	}

	// A grandfather list is only safe if it cannot quietly absorb new debt. Two
	// assertions: the count is what we froze it at, and every entry still
	// corresponds to SQL that is actually present.
	if len(grandfathered) != wantGrandfathered {
		t.Errorf("grandfather list has %d entries, frozen at %d — an applied migration may "+
			"be edited, but the list must never grow to accommodate a NEW ALTER/DROP",
			len(grandfathered), wantGrandfathered)
	}
	for key := range grandfathered {
		if !seen[key] {
			t.Errorf("grandfathered %q matched nothing — the migration was edited or removed; "+
				"drop the entry rather than leaving the list describing a tree that no longer exists", key)
		}
	}
}

// stripSQLComments blanks out `--` line comments and /* */ blocks, preserving
// line count so a reported line number still points at the right line.
//
// Without this the test is worthless: 1_core.sql's own comment explains that
// "deleted is a tombstone rather than a DELETE", and several comments name the
// ALTER-based migrations this rule replaced.
func stripSQLComments(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))

	inBlock := false
	for _, line := range strings.Split(sql, "\n") {
		for i := 0; i < len(line); i++ {
			switch {
			case inBlock:
				if strings.HasPrefix(line[i:], "*/") {
					inBlock = false
					i++
				}
			case strings.HasPrefix(line[i:], "/*"):
				inBlock = true
				i++
			case strings.HasPrefix(line[i:], "--"):
				i = len(line) // rest of the line is a comment
			default:
				b.WriteByte(line[i])
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestMigrationsCreateOnlyGuardCatchesViolations proves the guard above can
// fail. A rule enforced by a test that cannot fail is not enforced, and the
// comment-stripping makes that a live risk: strip too much and every migration
// looks clean.
func TestMigrationsCreateOnlyGuardCatchesViolations(t *testing.T) {
	cases := map[string]struct {
		sql   string
		dirty bool
	}{
		"bare alter":            {"ALTER TABLE job ADD COLUMN x TEXT;", true},
		"lowercase drop":        {"drop index idx_job_building_number;", true},
		"delete statement":      {"DELETE FROM job WHERE deleted = 1;", true},
		"alter after code":      {"CREATE TABLE t (a TEXT);\nALTER TABLE t ADD COLUMN b TEXT;", true},
		"alter in line comment": {"-- this replaces an ALTER TABLE we used to run", false},
		"drop in block comment": {"/* we no longer DROP INDEX here */", false},
		"deleted column":        {"CREATE TABLE t (deleted INTEGER NOT NULL DEFAULT 0);", false},
		"trigger with update":   {"CREATE TRIGGER x AFTER INSERT ON job BEGIN UPDATE job SET n = 1; END;", false},
	}

	res := map[string]*regexp.Regexp{
		"ALTER":  regexp.MustCompile(`(?i)\bALTER\b`),
		"DROP":   regexp.MustCompile(`(?i)\bDROP\b`),
		"DELETE": regexp.MustCompile(`(?i)\bDELETE\b`),
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stripped := stripSQLComments(tc.sql)
			var hit bool
			for _, re := range res {
				if re.MatchString(stripped) {
					hit = true
				}
			}
			if hit != tc.dirty {
				t.Errorf("stripped %q → violation=%v, want %v", stripped, hit, tc.dirty)
			}
		})
	}
}
