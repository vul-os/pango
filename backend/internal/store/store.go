// Package store owns Pango's local database: a SQLite file, an embedded
// migration set, this node's Ed25519 identity, a hybrid logical clock and an
// append-only oplog.
//
// The design goal is that a node is complete on its own. It opens its file,
// applies its own schema, mints its own identity and accepts writes — with no
// network, no control plane and no account anywhere (§2). Replication is
// something a node can additionally do, never something it needs in order to
// work.
package store

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Op is one journalled mutation: the unit of replication.
//
// Author is the minting node's Ed25519 public key (hex) and is also the HLC
// tie-break field, so an op's position in the total order travels with the op
// rather than being recomputed by whoever received it (§7).
//
// Cose, when non-empty, is the op re-expressed as a signed DMTAP-SYNC envelope.
// It is carried and relayed untouched by a node that has no merge engine
// installed, so a mixed fleet does not lose the envelopes it cannot read.
type Op struct {
	HLC     string          `json:"hlc"`
	Author  string          `json:"author"`
	OrgID   string          `json:"org_id"`
	Tbl     string          `json:"tbl"`
	RowID   string          `json:"row_id"`
	Deleted bool            `json:"deleted"`
	Payload json.RawMessage `json:"payload"`
	Cose    string          `json:"cose,omitempty"`
}

// Merger is the optional external merge authority: DMTAP-SYNC (§7). With none
// installed the built-in HLC engine decides, which is the default and the only
// path exercised today.
//
// It exists as a seam now, before any sync transport is built, because the
// choice of merge engine has to be made at boot and never mixed: two engines
// with different total orders cannot share a replica set, and discovering that
// after a fleet is deployed is not a recoverable position. Having the seam
// early keeps every write path already shaped to route through it.
type Merger interface {
	// Mint expresses a locally authored op as a signed envelope (hex).
	Mint(op Op) (string, error)
	// Ingest records an op authored elsewhere. A refusal is an error: the
	// engine fails closed rather than merging unverified state.
	Ingest(op Op) error
	// Resolve returns the winning payload for a row, the winner's HLC, and
	// whether the winner is a deletion. ok is false when the engine holds no
	// opinion.
	Resolve(tbl, rowID string) (payload json.RawMessage, hlc string, deleted bool, ok bool)
	// NoteLegacy records that an op arrived with no envelope, so a fleet
	// running two algebras at once is visible rather than silently
	// half-merged.
	NoteLegacy()
}

// Store owns the database, the identity and the clock. Safe for concurrent use.
type Store struct {
	mu     sync.Mutex
	db     *sql.DB
	clock  *HLC
	priv   ed25519.PrivateKey
	pub    ed25519.PublicKey
	merger Merger
}

// Open opens (or creates) the database at path, applies migrations, loads or
// mints this node's identity, and seeds the clock past everything already
// journalled. Pass ":memory:" for an ephemeral database (demo mode, tests).
func Open(path string) (*Store, error) {
	memory := path == "" || path == ":memory:"

	// Create the file ourselves at 0600 before SQLite can create it at
	// whatever the process umask allows (§11). A maintenance database holds
	// tenant names, addresses and access notes; it is not world-readable.
	if !memory {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create database: %w", err)
		}
		f.Close()
	}

	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	if memory {
		dsn = "file::memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	} else {
		dsn += "&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One writer. WAL serves concurrent readers, and a single connection
	// removes SQLITE_BUSY as a class of bug on the cheap hardware this is
	// expected to run on.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	// WAL creates sidecar files; they hold the same data and need the same
	// permissions.
	if !memory {
		for _, p := range []string{path, path + "-wal", path + "-shm"} {
			if _, err := os.Stat(p); err == nil {
				_ = os.Chmod(p, 0o600)
			}
		}
	}

	s := &Store{db: db}
	if err := s.ensureIdentity(); err != nil {
		db.Close()
		return nil, fmt.Errorf("node identity: %w", err)
	}

	var maxHLC sql.NullString
	_ = db.QueryRow("SELECT MAX(hlc) FROM oplog").Scan(&maxHLC)
	s.clock = NewHLC(s.PublicKeyHex(), maxHLC.String)
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for the repo and report layers. They own their own SQL;
// this package deliberately does not grow a query for every aggregate (§9).
func (s *Store) DB() *sql.DB { return s.db }

// SetMerger installs an external merge authority. nil restores the built-in
// engine. Call before serving traffic — never while running (§7).
func (s *Store) SetMerger(m Merger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.merger = m
}

// Merger returns the installed merge authority, or nil for the built-in engine.
func (s *Store) Merger() Merger {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.merger
}

// Now is the canonical timestamp spelling: RFC3339 in UTC. Stored as text
// because it sorts lexically, groups by day with a substring, and is readable
// when someone opens the file with the sqlite3 CLI at 2am.
func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Observe folds a remote timestamp into the local clock.
func (s *Store) Observe(hlc string) { s.clock.Observe(hlc) }

// Tx runs fn inside a transaction, committing on success and rolling back on
// error or panic.
//
// Every repo write goes through here so the row and its oplog entry commit
// together. A row that exists without its op would never replicate; an op
// without its row would replicate a write this node cannot itself see.
func (s *Store) Tx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Journal appends an op describing a mutation, inside the caller's transaction,
// and returns the HLC stamp to write onto the row.
//
// It is called by every repo write, even though no sync transport exists yet,
// because a journal that starts later than the data is a journal with a hole in
// it: the rows written before sync was switched on would be invisible to every
// peer and would silently never converge.
func (s *Store) Journal(tx *sql.Tx, orgID, tbl, rowID string, payload any, deleted bool) (string, error) {
	s.mu.Lock()
	merger := s.merger
	s.mu.Unlock()

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	op := Op{
		HLC:     s.clock.Tick(),
		Author:  s.PublicKeyHex(),
		OrgID:   orgID,
		Tbl:     tbl,
		RowID:   rowID,
		Deleted: deleted,
		Payload: raw,
	}
	if merger != nil {
		// Signing failure aborts the mutation rather than journalling a write
		// the engine would not accept: that is how two states drift apart.
		cose, err := merger.Mint(op)
		if err != nil {
			return "", fmt.Errorf("merger mint: %w", err)
		}
		op.Cose = cose
	}
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO oplog (hlc, author, org_id, tbl, row_id, deleted, payload, cose, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		op.HLC, op.Author, op.OrgID, op.Tbl, op.RowID, boolToInt(op.Deleted),
		string(op.Payload), op.Cose, Now()); err != nil {
		return "", err
	}
	return op.HLC, nil
}

// Vector is this node's knowledge: the newest HLC seen per authoring node. It
// is derived from the oplog rather than stored per peer, which is what makes a
// sync round stateless and symmetric — any node can relay any other node's ops
// without either of them remembering the exchange (§7).
func (s *Store) Vector() (map[string]string, error) {
	rows, err := s.db.Query("SELECT author, MAX(hlc) FROM oplog GROUP BY author")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var author, hlc string
		if err := rows.Scan(&author, &hlc); err != nil {
			return nil, err
		}
		out[author] = hlc
	}
	return out, rows.Err()
}

// MaxJournalledHLC is the newest stamp in this node's own journal, or "" for an
// empty oplog. It is what a clock is seeded past on open, and what an external
// merge engine's clock has to be seeded past too so the two start level.
func (s *Store) MaxJournalledHLC() string {
	var max sql.NullString
	_ = s.db.QueryRow("SELECT MAX(hlc) FROM oplog").Scan(&max)
	return max.String
}

// OpCount reports how many ops this node holds. Used by status surfaces and
// tests.
func (s *Store) OpCount() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM oplog").Scan(&n)
	return n, err
}

// ── settings ────────────────────────────────────────────────────────────────

func (s *Store) getSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// GetSetting returns a setting value, or "" if unset.
func (s *Store) GetSetting(key string) string {
	v, _ := s.getSetting(key)
	return v
}

// SetSetting upserts a setting.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SplitList splits a comma/space/newline separated string, trimming blanks.
func SplitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
	})
}
