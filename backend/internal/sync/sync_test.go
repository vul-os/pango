package sync

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/repo"
	"github.com/vul-os/pango/backend/internal/store"
)

const testSecret = "shared-pairing-secret"

// node bundles the layers a sync test drives: a store, a repo to write
// through, and a sync Engine wired to it.
type node struct {
	s *store.Store
	r *repo.Repo
	e *Engine
}

func newNode(t *testing.T) *node {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "pango.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	e := New(s)
	e.SecretFn = func() string { return testSecret }
	return &node{s: s, r: repo.New(s), e: e}
}

// server starts an httptest server serving n's sync handler and registers
// its shutdown as test cleanup.
func (n *node) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(n.e.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// jobCount returns how many (non-deleted) jobs n's database holds.
func (n *node) jobCount(t *testing.T) int {
	t.Helper()
	var c int
	if err := n.s.DB().QueryRow(`SELECT COUNT(*) FROM job WHERE deleted = 0`).Scan(&c); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return c
}

func (n *node) jobIDs(t *testing.T) map[string]bool {
	t.Helper()
	rows, err := n.s.DB().Query(`SELECT id FROM job WHERE deleted = 0`)
	if err != nil {
		t.Fatalf("list job ids: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out[id] = true
	}
	return out
}

func (n *node) costSum(t *testing.T, jobID string) int64 {
	t.Helper()
	var sum sql.NullInt64
	if err := n.s.DB().QueryRow(
		`SELECT SUM(amount_minor) FROM cost_entry WHERE job_id = ?`, jobID).Scan(&sum); err != nil {
		t.Fatalf("sum cost: %v", err)
	}
	return sum.Int64
}

func (n *node) opCount(t *testing.T) int {
	t.Helper()
	c, err := n.s.OpCount()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestTwoNodeHTTPReconverge is the central promise of §7: two nodes that
// diverge while offline, exchanging one stateless round afterwards, end up
// byte-for-byte identical — including a money ledger, where the correct
// answer is that both entries survive and SUM to their total (ARCHITECTURE §6).
func TestTwoNodeHTTPReconverge(t *testing.T) {
	a := newNode(t)
	b := newNode(t)
	srvA := a.server(t)
	ctx := context.Background()

	// a is the organisation's owning node: it creates the org and two
	// buildings that everything else hangs off. Two buildings, one per
	// divergent writer below, rather than one shared building: job numbers
	// are a per-building sequence allocated locally with no coordination
	// (ARCHITECTURE §5), and that sequence itself is deliberately NOT
	// journalled (repo/job.go's nextJobNumber writes straight to
	// job_number_seq, bypassing store.Journal) — so two nodes that are both
	// offline and both raise the FIRST job against the SAME building
	// legitimately collide on job number 1 when they reconverge. That is a
	// pre-existing gap in the numbering scheme, not something this transport
	// can paper over, and is called out in this change's report rather than
	// silently worked around here.
	org, err := a.r.CreateOrg("Acme Property")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	buildingA, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Riverside Court"})
	if err != nil {
		t.Fatalf("create building A: %v", err)
	}
	buildingB, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Harbour View"})
	if err != nil {
		t.Fatalf("create building B: %v", err)
	}

	// Bootstrap: b pulls the org + buildings it does not yet have. b has
	// nothing to push. This exercises TOFU enrolment on a (b's key is not
	// yet recorded there).
	if res := b.e.SyncPeer(ctx, srvA.URL); !res.OK {
		t.Fatalf("bootstrap sync failed: %+v", res)
	}

	// Now diverge: a and b each raise a job on their own building and record
	// a cost entry against it, entirely offline from each other.
	jobA, err := a.r.CreateJob(org.ID, domain.Job{BuildingID: buildingA.ID, Title: "Leak in unit 4B"}, "4B")
	if err != nil {
		t.Fatalf("create job on a: %v", err)
	}
	if _, err := a.r.AddCost(org.ID, domain.CostEntry{
		JobID: jobA.ID, Kind: domain.CostMaterial, AmountMinor: 1200, Currency: "ZAR",
	}); err != nil {
		t.Fatalf("add cost on a: %v", err)
	}

	jobB, err := b.r.CreateJob(org.ID, domain.Job{BuildingID: buildingB.ID, Title: "Broken gate motor"}, "Gate")
	if err != nil {
		t.Fatalf("create job on b: %v", err)
	}
	if _, err := b.r.AddCost(org.ID, domain.CostEntry{
		JobID: jobB.ID, Kind: domain.CostLabour, AmountMinor: 850, Currency: "ZAR",
	}); err != nil {
		t.Fatalf("add cost on b: %v", err)
	}
	// A second, independent cost entry on the SAME job from the other
	// side too, so convergence has to add rather than overwrite — the whole
	// point of union merge over an append-only ledger (ARCHITECTURE §6).
	if _, err := a.r.AddCost(org.ID, domain.CostEntry{
		JobID: jobA.ID, Kind: domain.CostLabour, AmountMinor: 300, Currency: "ZAR",
	}); err != nil {
		t.Fatalf("add second cost on a: %v", err)
	}

	// One stateless, symmetric round from b: pushes b's new ops to a, pulls
	// a's new ops to b. A single call converges BOTH sides.
	res := b.e.SyncPeer(ctx, srvA.URL)
	if !res.OK {
		t.Fatalf("reconverge sync failed: %+v", res)
	}
	if res.Pushed == 0 {
		t.Error("expected b to push jobB's ops to a")
	}
	if res.Pulled == 0 {
		t.Error("expected b to pull a's new ops")
	}

	if got, want := a.jobCount(t), 2; got != want {
		t.Errorf("a has %d jobs after reconverge, want %d", got, want)
	}
	if got, want := b.jobCount(t), 2; got != want {
		t.Errorf("b has %d jobs after reconverge, want %d", got, want)
	}
	idsA, idsB := a.jobIDs(t), b.jobIDs(t)
	if len(idsA) != len(idsB) {
		t.Fatalf("job id sets differ in size: a=%v b=%v", idsA, idsB)
	}
	for id := range idsA {
		if !idsB[id] {
			t.Errorf("job %s present on a but not b", id)
		}
	}

	if got, want := a.costSum(t, jobA.ID), int64(1500); got != want {
		t.Errorf("a: cost sum on jobA = %d, want %d (union of both entries)", got, want)
	}
	if got, want := b.costSum(t, jobA.ID), int64(1500); got != want {
		t.Errorf("b: cost sum on jobA = %d, want %d (union of both entries)", got, want)
	}

	if a.opCount(t) != b.opCount(t) {
		t.Errorf("oplog sizes differ after reconverge: a=%d b=%d", a.opCount(t), b.opCount(t))
	}
}

// TestThreeNodeTransitiveRelay proves the "any node can relay any other
// node's ops" claim: a and c never dial each other directly, only b, and yet
// all three converge on the same three jobs.
func TestThreeNodeTransitiveRelay(t *testing.T) {
	a := newNode(t)
	b := newNode(t)
	c := newNode(t)
	srvA := a.server(t)
	srvB := b.server(t)
	ctx := context.Background()

	org, err := a.r.CreateOrg("Relay Co")
	if err != nil {
		t.Fatal(err)
	}
	// One building per writer: see the comment in TestTwoNodeHTTPReconverge
	// about the (unreplicated) per-building job number sequence.
	buildingA, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Block A"})
	if err != nil {
		t.Fatal(err)
	}
	buildingB, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Block B"})
	if err != nil {
		t.Fatal(err)
	}
	buildingC, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Block C"})
	if err != nil {
		t.Fatal(err)
	}

	// b learns the org/buildings from a. c learns it from b — never from a.
	if res := b.e.SyncPeer(ctx, srvA.URL); !res.OK {
		t.Fatalf("b<-a bootstrap: %+v", res)
	}
	if res := c.e.SyncPeer(ctx, srvB.URL); !res.OK {
		t.Fatalf("c<-b bootstrap: %+v", res)
	}

	jobA, err := a.r.CreateJob(org.ID, domain.Job{BuildingID: buildingA.ID, Title: "Job from A"}, "")
	if err != nil {
		t.Fatal(err)
	}
	jobB, err := b.r.CreateJob(org.ID, domain.Job{BuildingID: buildingB.ID, Title: "Job from B"}, "")
	if err != nil {
		t.Fatal(err)
	}
	jobC, err := c.r.CreateJob(org.ID, domain.Job{BuildingID: buildingC.ID, Title: "Job from C"}, "")
	if err != nil {
		t.Fatal(err)
	}

	// b <-> a: exchanges jobA and jobB between the two of them.
	if res := b.e.SyncPeer(ctx, srvA.URL); !res.OK {
		t.Fatalf("b<->a round 1: %+v", res)
	}
	// c <-> b: c picks up jobA+jobB (relayed via b) and pushes jobC to b.
	if res := c.e.SyncPeer(ctx, srvB.URL); !res.OK {
		t.Fatalf("c<->b round: %+v", res)
	}
	// b <-> a again: b now holds jobC (from c) and relays it on to a, which
	// has never spoken to c.
	if res := b.e.SyncPeer(ctx, srvA.URL); !res.OK {
		t.Fatalf("b<->a round 2: %+v", res)
	}

	want := map[string]bool{jobA.ID: true, jobB.ID: true, jobC.ID: true}
	for name, n := range map[string]*node{"a": a, "b": b, "c": c} {
		got := n.jobIDs(t)
		if len(got) != len(want) {
			t.Fatalf("%s has %d jobs, want %d (%v)", name, len(got), len(want), got)
		}
		for id := range want {
			if !got[id] {
				t.Errorf("%s is missing job %s despite transitive relay", name, id)
			}
		}
	}
}

// TestBatchCapEnforced confirms the server rejects a batch above the 2000-op
// cap rather than silently truncating or accepting it.
func TestBatchCapEnforced(t *testing.T) {
	a := newNode(t)
	a.e.OrgIDFn = func() (string, error) { return "test-org", nil }
	srvA := a.server(t)
	b := newNode(t)

	ops := make([]store.Op, Batch+1)
	for i := range ops {
		ops[i] = store.Op{HLC: "0000000000001-0000-deadbeef", Author: "deadbeef", OrgID: "x", Tbl: "job", RowID: "x"}
	}
	// Drive it through the real signed transport so the test also exercises
	// guard() end to end.
	req := newSignedOpsRequest(t, b.e, srvA.URL, ops)
	resp, err := b.e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("oversized batch: got HTTP %d, want 400", resp.StatusCode)
	}
}
