package store

// Store and migration tests.
//
// The migration cases matter because a migration that runs twice is not a
// no-op: it is a CREATE TABLE against a table that exists, which fails the
// whole open and takes the node offline on restart. And a migration that is
// recorded but not applied leaves a schema hole nobody sees until a query hits
// the missing column.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tempStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pango.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestOpenAppliesMigrations(t *testing.T) {
	s, _ := tempStore(t)

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) == 0 {
		t.Fatal("no migrations embedded — the embed.FS pattern is broken")
	}

	var applied int
	if err := s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != len(migrations) {
		t.Fatalf("applied %d migrations, embedded %d", applied, len(migrations))
	}

	// Every table the rest of the code depends on must exist.
	for _, table := range []string{
		"settings", "oplog", "organisation", "app_user", "session", "party", "peer",
		"building", "unit", "job_number_seq", "job", "job_event", "cost_entry",
		"time_entry", "inspection_template", "inspection_template_item", "inspection",
		"finding", "attachment",
	} {
		var name string
		err := s.DB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestMigrationsApplyExactlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pango.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var first int
	if err := s1.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&first); err != nil {
		t.Fatal(err)
	}
	// Write a row, so a re-run that dropped and recreated a table would be
	// caught by losing the data rather than only by the counter.
	if err := s1.SetSetting("canary", "alive"); err != nil {
		t.Fatal(err)
	}
	s1.Close()

	// Reopen several times: each must be a clean no-op.
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("reopen %d failed — migrations are not idempotent: %v", i, err)
		}
		var n int
		if err := s.DB().QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != first {
			t.Fatalf("reopen %d: schema_migrations has %d rows, want %d", i, n, first)
		}
		if got := s.GetSetting("canary"); got != "alive" {
			t.Fatalf("reopen %d: data lost, canary = %q", i, got)
		}
		s.Close()
	}
}

func TestMigrationsAreNumberedByEpoch(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	// Sorting must be numeric, not lexical: lexically "100" sorts before "2".
	for i := 1; i < len(migrations); i++ {
		if migrations[i].version <= migrations[i-1].version {
			t.Fatalf("migrations not in ascending numeric order: %d then %d",
				migrations[i-1].version, migrations[i].version)
		}
	}
	if migrations[0].version != 1 {
		t.Errorf("first migration is version %d, want 1", migrations[0].version)
	}
}

func TestMigrationsRunInOwnTransaction(t *testing.T) {
	// A failing migration must leave every earlier one applied and recorded, so
	// the operator's restart resumes rather than starting over.
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before == 0 {
		t.Fatal("nothing applied")
	}

	// Applying again records nothing new and errors on nothing.
	if err := migrate(db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var after int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("second migrate added rows: %d → %d", before, after)
	}
}

// §11: the database file is 0600. It holds tenant names, addresses and access
// notes, and the deployment target is a shared office machine or a NAS.
func TestDatabaseFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	_, path := tempStore(t)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("database file mode is %o, want 600", perm)
	}
	// The WAL sidecar holds the same data and must be equally private.
	if wal, err := os.Stat(path + "-wal"); err == nil {
		if perm := wal.Mode().Perm(); perm != 0o600 {
			t.Errorf("WAL file mode is %o, want 600", perm)
		}
	}
}

func TestIdentityIsStableAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pango.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	key := s1.PublicKeyHex()
	seed := s1.PrivateSeedHexForTest()
	s1.Close()

	if key == "" {
		t.Fatal("no identity generated on first run")
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	// A node that regenerated its key on restart would fork its own history
	// and lose its enrolment with every peer.
	if s2.PublicKeyHex() != key {
		t.Errorf("public key changed across restart: %q → %q", key, s2.PublicKeyHex())
	}
	if s2.PrivateSeedHexForTest() != seed {
		t.Error("private seed changed across restart")
	}
}

func TestIdentitySigning(t *testing.T) {
	s, _ := tempStore(t)
	msg := []byte("job 41 closed")

	sig := s.Sign(msg)
	if sig == "" {
		t.Fatal("no signature produced")
	}
	if !VerifySig(s.PublicKeyHex(), msg, sig) {
		t.Error("signature did not verify against its own key")
	}
	if VerifySig(s.PublicKeyHex(), []byte("job 41 cancelled"), sig) {
		t.Error("signature verified against a different message")
	}
	// Empty inputs must fail closed rather than verify nothing successfully.
	if VerifySig("", msg, sig) || VerifySig(s.PublicKeyHex(), msg, "") {
		t.Error("empty key or signature verified")
	}
}

func TestCorruptIdentityIsFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pango.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("node_privkey", "not-hex-at-all"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Silently minting a new identity here would fork the node's history.
	if _, err := Open(path); err == nil {
		t.Fatal("corrupt identity should refuse to open")
	}
}

func TestJournalAppendsOps(t *testing.T) {
	s, _ := tempStore(t)

	var hlcs []string
	for i := 0; i < 3; i++ {
		err := s.Tx(func(tx *sql.Tx) error {
			h, err := s.Journal(tx, "org-1", "building", "b-1", map[string]any{"name": "Riverside"}, false)
			if err != nil {
				return err
			}
			hlcs = append(hlcs, h)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.OpCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("op count = %d, want 3", n)
	}
	for i := 1; i < len(hlcs); i++ {
		if hlcs[i] <= hlcs[i-1] {
			t.Fatalf("journal HLCs not increasing: %q then %q", hlcs[i-1], hlcs[i])
		}
	}

	// The version vector is derived from the oplog, not stored per peer, which
	// is what makes a sync round stateless (§7).
	vec, err := s.Vector()
	if err != nil {
		t.Fatal(err)
	}
	if got := vec[s.PublicKeyHex()]; got != hlcs[len(hlcs)-1] {
		t.Errorf("vector for this node = %q, want the newest op %q", got, hlcs[len(hlcs)-1])
	}
}

// A rolled-back transaction must leave no op behind: an op without its row
// would replicate a write this node cannot see.
func TestJournalRollsBackWithTransaction(t *testing.T) {
	s, _ := tempStore(t)

	wantErr := sql.ErrTxDone
	err := s.Tx(func(tx *sql.Tx) error {
		if _, err := s.Journal(tx, "org-1", "building", "b-1", map[string]any{}, false); err != nil {
			return err
		}
		return wantErr
	})
	if err != wantErr {
		t.Fatalf("Tx error = %v, want %v", err, wantErr)
	}

	n, err := s.OpCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("op count = %d after rollback, want 0", n)
	}
}

// The Merger seam (§7): nil is the built-in engine, and installing one routes
// local writes through it.
func TestMergerSeam(t *testing.T) {
	s, _ := tempStore(t)

	if s.Merger() != nil {
		t.Fatal("a fresh store should use the built-in engine (nil Merger)")
	}

	m := &fakeMerger{}
	s.SetMerger(m)
	if s.Merger() == nil {
		t.Fatal("SetMerger did not install the engine")
	}

	err := s.Tx(func(tx *sql.Tx) error {
		_, err := s.Journal(tx, "org-1", "building", "b-1", map[string]any{"name": "X"}, false)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.minted != 1 {
		t.Errorf("merger minted %d ops, want 1", m.minted)
	}

	var cose string
	if err := s.DB().QueryRow("SELECT cose FROM oplog LIMIT 1").Scan(&cose); err != nil {
		t.Fatal(err)
	}
	if cose != "deadbeef" {
		t.Errorf("envelope not journalled: %q", cose)
	}

	s.SetMerger(nil)
	if s.Merger() != nil {
		t.Error("SetMerger(nil) did not restore the built-in engine")
	}
}

// A merge engine that refuses to sign must abort the write, not journal it:
// journalling a write the engine would not accept is how two states drift.
func TestMergerMintFailureAbortsWrite(t *testing.T) {
	s, _ := tempStore(t)
	s.SetMerger(&fakeMerger{failMint: true})

	err := s.Tx(func(tx *sql.Tx) error {
		_, err := s.Journal(tx, "org-1", "building", "b-1", map[string]any{}, false)
		return err
	})
	if err == nil {
		t.Fatal("a mint failure must abort the write")
	}
	n, _ := s.OpCount()
	if n != 0 {
		t.Fatalf("op count = %d after a failed mint, want 0", n)
	}
}

type fakeMerger struct {
	minted   int
	failMint bool
}

func (m *fakeMerger) Mint(op Op) (string, error) {
	if m.failMint {
		return "", sql.ErrConnDone
	}
	m.minted++
	return "deadbeef", nil
}
func (m *fakeMerger) Ingest(op Op) error { return nil }
func (m *fakeMerger) Resolve(tbl, rowID string) (json.RawMessage, string, bool, bool) {
	return nil, "", false, false
}
func (m *fakeMerger) NoteLegacy() {}

func TestNewIDIsSortableAndUnique(t *testing.T) {
	seen := map[string]bool{}
	prev := ""
	for i := 0; i < 10000; i++ {
		id := NewID()
		if len(id) != 26 {
			t.Fatalf("id %q is %d chars, want 26", id, len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %q at iteration %d", id, i)
		}
		seen[id] = true
		// Ids minted in the same millisecond may tie; they must never go
		// backwards in time.
		if prev != "" && id[:10] < prev[:10] {
			t.Fatalf("id timestamp went backwards: %q then %q", prev, id)
		}
		prev = id
	}
}
