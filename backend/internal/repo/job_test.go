package repo

// Defect coverage: two nodes offline from the same building both allocate job
// number 1, then reconverge (docs/SYNC.md "Job numbers under divergence").
//
// This drives the real sync transport (package sync) rather than poking the
// database directly, so it exercises the exact path production traffic takes:
// repo.CreateJob's local allocation, store.Journal's oplog, and the
// materialisation trigger in store/migrations/200_maintenance.sql —
// nothing here is a simulation of the bug, it is the bug's own reproduction.

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
	"github.com/vul-os/pango/backend/internal/sync"
)

// syncNode bundles a store, a repo and a sync engine over it — one simulated
// Pango install.
type syncNode struct {
	s *store.Store
	r *Repo
	e *sync.Engine
}

func newSyncNode(t *testing.T) *syncNode {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	e := sync.New(s)
	e.SecretFn = func() string { return "test-pairing-secret" }
	return &syncNode{s: s, r: New(s), e: e}
}

func (n *syncNode) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(n.e.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// TestFirstJobCollisionReconverges is THE regression test for the bug: two
// nodes, both offline, both raise the FIRST job against the SAME building —
// so both mint number 1 with no way to know the other exists — then
// reconverge. Before the fix, the second node's job row fails to insert on
// the peer holding job number 1 already, with a UNIQUE(building_id, number)
// violation that aborts the whole sync round. After the fix, both jobs
// survive on both nodes with distinct numbers and no error.
func TestFirstJobCollisionReconverges(t *testing.T) {
	a := newSyncNode(t)
	b := newSyncNode(t)
	srvA := a.server(t)
	ctx := context.Background()

	org, err := a.r.CreateOrg("Meridian Property")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	building, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Riverside Court"})
	if err != nil {
		t.Fatalf("create building: %v", err)
	}

	// b learns the org and building from a. b has nothing to push yet.
	if res := b.e.SyncPeer(ctx, srvA.URL); !res.OK {
		t.Fatalf("bootstrap sync failed: %+v", res)
	}

	// Now diverge: a and b are offline from each other and each raises the
	// FIRST job against the ONE building they share. Both allocate number 1,
	// entirely locally, entirely legitimately from where each node sits.
	jobA, err := a.r.CreateJob(org.ID, domain.Job{BuildingID: building.ID, Title: "Leak in unit 4B"}, "4B")
	if err != nil {
		t.Fatalf("create job on a: %v", err)
	}
	if jobA.Number != 1 {
		t.Fatalf("job on a: number = %d, want 1", jobA.Number)
	}
	jobB, err := b.r.CreateJob(org.ID, domain.Job{BuildingID: building.ID, Title: "Broken gate motor"}, "Gate")
	if err != nil {
		t.Fatalf("create job on b: %v", err)
	}
	if jobB.Number != 1 {
		t.Fatalf("job on b: number = %d, want 1", jobB.Number)
	}

	// One stateless, symmetric round from b: pushes jobB to a, pulls jobA to
	// b. This must succeed — the whole point of the fix — not fail the batch
	// with a UNIQUE constraint violation.
	res := b.e.SyncPeer(ctx, srvA.URL)
	if !res.OK {
		t.Fatalf("reconverge sync failed: %+v", res)
	}

	for name, n := range map[string]*syncNode{"a": a, "b": b} {
		gotA, err := n.r.GetJob(org.ID, jobA.ID)
		if err != nil {
			t.Fatalf("%s: get jobA: %v", name, err)
		}
		gotB, err := n.r.GetJob(org.ID, jobB.ID)
		if err != nil {
			t.Fatalf("%s: get jobB: %v", name, err)
		}
		if gotA.Number == gotB.Number {
			t.Errorf("%s: jobA and jobB both hold number %d after reconverge — collision not resolved",
				name, gotA.Number)
		}
	}

	// Both nodes must have made the SAME decision: the pairwise case is
	// deterministic regardless of which side resolved the collision locally
	// (see the trigger's doc comment for why).
	finalA, err := a.r.GetJob(org.ID, jobA.ID)
	if err != nil {
		t.Fatal(err)
	}
	finalB, err := a.r.GetJob(org.ID, jobB.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherA, err := b.r.GetJob(org.ID, jobA.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherB, err := b.r.GetJob(org.ID, jobB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalA.Number != otherA.Number {
		t.Errorf("jobA number diverged: a=%d b=%d", finalA.Number, otherA.Number)
	}
	if finalB.Number != otherB.Number {
		t.Errorf("jobB number diverged: a=%d b=%d", finalB.Number, otherB.Number)
	}

	// A job's own creation order settles the outcome: jobA was minted first
	// (its HLC is causally earlier), so it keeps number 1 and jobB is the one
	// that moves.
	if finalA.Number != 1 {
		t.Errorf("jobA (created first) should keep number 1, got %d", finalA.Number)
	}
	if finalB.Number != 2 {
		t.Errorf("jobB (created second, the collision) should be bumped to number 2, got %d", finalB.Number)
	}
}
