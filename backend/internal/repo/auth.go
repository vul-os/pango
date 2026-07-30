package repo

// Organisations, operator accounts and sessions.
//
// This is the aggregate that decides what org_id every other call in this
// package is scoped to, so it is the one place where getting it wrong is a
// tenancy breach rather than a bug. The rule (§11): the org_id used for
// scoping is read from the session row, never from anything the client sent.
//
// Session tokens are stored as SHA-256 hashes. A database file that walks out
// of an office on a stolen laptop then yields no usable sessions, only the
// hashes of expired ones.

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

// ErrBadCredentials is returned for both an unknown email and a wrong password.
// One error for both cases: distinguishing them turns the login form into a
// list of who works here.
var ErrBadCredentials = errors.New("invalid email or password")

// SessionTTL is how long a session lasts. Thirty days, because the target
// deployment is a tablet carried around a building by someone who should not be
// re-authenticating in a basement with no signal — a short TTL here would push
// people to disable auth entirely, which is worse.
const SessionTTL = 30 * 24 * time.Hour

// dummyHash is compared against when no user matches, so a failed login costs
// the same time whether or not the email exists. Without it, response latency
// is an account-enumeration oracle. It is a real bcrypt hash of a random value.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// CreateOrg inserts an organisation.
func (r *Repo) CreateOrg(name string) (domain.Organisation, error) {
	if strings.TrimSpace(name) == "" {
		return domain.Organisation{}, errors.New("organisation name is required")
	}
	o := domain.Organisation{ID: store.NewID(), Name: name, CreatedAt: store.Now()}
	err := r.s.Tx(func(tx *sql.Tx) error {
		hlc, err := r.s.Journal(tx, o.ID, "organisation", o.ID, o, false)
		if err != nil {
			return err
		}
		o.HLC = hlc
		_, err = tx.Exec(
			`INSERT INTO organisation (id, name, hlc, deleted, created_at) VALUES (?, ?, ?, 0, ?)`,
			o.ID, o.Name, o.HLC, o.CreatedAt)
		return err
	})
	if err != nil {
		return domain.Organisation{}, err
	}
	return o, nil
}

// GetOrg returns an organisation by id.
func (r *Repo) GetOrg(id string) (domain.Organisation, error) {
	var o domain.Organisation
	var deleted int
	err := r.s.DB().QueryRow(
		`SELECT id, name, hlc, deleted, created_at FROM organisation WHERE id = ?`, id).
		Scan(&o.ID, &o.Name, &o.HLC, &deleted, &o.CreatedAt)
	if err == sql.ErrNoRows {
		return domain.Organisation{}, ErrNotFound
	}
	if err != nil {
		return domain.Organisation{}, err
	}
	o.Deleted = deleted != 0
	return o, nil
}

// CreateUser inserts an operator account into an existing organisation.
func (r *Repo) CreateUser(orgID, email, password, name, role string) (domain.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || !strings.Contains(email, "@") {
		return domain.User{}, errors.New("a valid email is required")
	}
	// Long enough to resist an offline guess against a stolen file, short
	// enough that people do not write it on the monitor.
	if len(password) < 10 {
		return domain.User{}, errors.New("password must be at least 10 characters")
	}
	if _, err := r.GetOrg(orgID); err != nil {
		return domain.User{}, err
	}
	if role == "" {
		role = "manager"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	u := domain.User{
		ID:        store.NewID(),
		OrgID:     orgID,
		Email:     email,
		Name:      name,
		Role:      role,
		CreatedAt: store.Now(),
	}
	// Not journalled: credentials are local to the node (see 1_core.sql).
	if _, err := r.s.DB().Exec(
		`INSERT INTO app_user (id, org_id, email, password_hash, name, role, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.OrgID, u.Email, string(hash), u.Name, u.Role, u.CreatedAt); err != nil {
		return domain.User{}, fmt.Errorf("%w: email may already be registered", ErrConflict)
	}
	return u, nil
}

// UserCount reports how many operator accounts exist. Used to detect first run.
func (r *Repo) UserCount() (int, error) {
	var n int
	err := r.s.DB().QueryRow("SELECT COUNT(*) FROM app_user").Scan(&n)
	return n, err
}

// Authenticate checks an email and password and returns the user.
func (r *Repo) Authenticate(email, password string) (domain.User, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	var u domain.User
	var hash string
	err := r.s.DB().QueryRow(
		`SELECT id, org_id, email, name, role, created_at, password_hash
		 FROM app_user WHERE lower(email) = ?`, email).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &hash)
	if err == sql.ErrNoRows {
		// Burn the same time a real comparison would take.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return domain.User{}, ErrBadCredentials
	}
	if err != nil {
		return domain.User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return domain.User{}, ErrBadCredentials
	}
	return u, nil
}

// CreateSession mints a session token for a user and returns the plaintext
// token. Only its hash is stored, so this is the one and only moment the token
// exists in a form that can be presented.
func (r *Repo) CreateSession(u domain.User) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now().UTC()
	_, err := r.s.DB().Exec(
		`INSERT INTO session (token_hash, user_id, org_id, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		hashToken(token), u.ID, u.OrgID,
		now.Format(time.RFC3339Nano), now.Add(SessionTTL).Format(time.RFC3339Nano))
	if err != nil {
		return "", err
	}
	return token, nil
}

// SessionUser resolves a session token to its user, or ErrBadCredentials.
//
// Expiry is checked here rather than by a sweeper, so an expired session is
// dead the moment it expires even if the process has been running untouched for
// a year on a NAS in a cupboard.
func (r *Repo) SessionUser(token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, ErrBadCredentials
	}
	var u domain.User
	var expires string
	err := r.s.DB().QueryRow(
		`SELECT u.id, u.org_id, u.email, u.name, u.role, u.created_at, s.expires_at
		 FROM session s JOIN app_user u ON u.id = s.user_id
		 WHERE s.token_hash = ?`, hashToken(token)).
		Scan(&u.ID, &u.OrgID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &expires)
	if err == sql.ErrNoRows {
		return domain.User{}, ErrBadCredentials
	}
	if err != nil {
		return domain.User{}, err
	}
	exp, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().UTC().After(exp) {
		return domain.User{}, ErrBadCredentials
	}
	return u, nil
}

// DeleteSession revokes a session.
func (r *Repo) DeleteSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := r.s.DB().Exec("DELETE FROM session WHERE token_hash = ?", hashToken(token))
	return err
}

// PurgeExpiredSessions removes sessions past their expiry. Housekeeping only —
// SessionUser already refuses them.
func (r *Repo) PurgeExpiredSessions() error {
	_, err := r.s.DB().Exec(
		"DELETE FROM session WHERE expires_at < ?", time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
