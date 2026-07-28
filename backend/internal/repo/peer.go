package repo

// Peers: nodes this one has been told to sync with.
//
// Enrolment is manual (§7). There is no mDNS, no DHT and no rendezvous server,
// which is a deliberate refusal rather than a missing feature: automatic
// discovery means a node's replica set is decided by whatever else is on the
// network, and "whatever else is on the network" at a managing agent's office
// includes tenants' phones on the guest wifi.
//
// A peer row with a pubkey and no URL is inbound-only: a node that paired with
// us, which we do not dial. Keeping the row is what gives an operator something
// to delete in order to revoke it.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vul-os/propfix/backend/internal/domain"
	"github.com/vul-os/propfix/backend/internal/store"
)

const peerCols = `id, org_id, name, url, pubkey, enabled, last_sync_at, last_status, created_at`

// SavePeer inserts or updates an enrolled peer.
func (r *Repo) SavePeer(orgID string, p domain.Peer) (domain.Peer, error) {
	if strings.TrimSpace(p.Name) == "" {
		return domain.Peer{}, errors.New("peer name is required")
	}
	if p.URL != "" && !strings.HasPrefix(p.URL, "http://") && !strings.HasPrefix(p.URL, "https://") {
		return domain.Peer{}, errors.New("peer URL must start with http:// or https://")
	}
	p.OrgID = orgID
	if p.ID == "" {
		p.ID = store.NewID()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = store.Now()
	}

	// Peers are local wiring, not replicated data: a node's list of who it
	// dials is a property of where that node sits on a network, and copying it
	// to every peer would turn one enrolment into a mesh-wide one.
	_, err := r.s.DB().Exec(
		`INSERT INTO peer (`+peerCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name, url = excluded.url, enabled = excluded.enabled`,
		p.ID, p.OrgID, p.Name, p.URL, p.PubKey, boolToInt(p.Enabled),
		p.LastSyncAt, p.LastStatus, p.CreatedAt)
	if err != nil {
		return domain.Peer{}, err
	}
	return p, nil
}

// ListPeers returns the org's enrolled peers.
func (r *Repo) ListPeers(orgID string) ([]domain.Peer, error) {
	rows, err := r.s.DB().Query(
		`SELECT `+peerCols+` FROM peer WHERE org_id = ? ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Peer{}
	for rows.Next() {
		var p domain.Peer
		var enabled int
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.URL, &p.PubKey, &enabled,
			&p.LastSyncAt, &p.LastStatus, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeletePeer removes an enrolment. This is a real DELETE, not a tombstone:
// revocation must take effect on this node immediately and must not be
// replicated back by a peer that disagrees.
func (r *Repo) DeletePeer(orgID, id string) error {
	res, err := r.s.DB().Exec("DELETE FROM peer WHERE id = ? AND org_id = ?", id, orgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
