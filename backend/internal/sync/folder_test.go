package sync

import (
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
)

// TestFolderReconverge proves the sneakernet path: two nodes that never open
// a socket to each other, only a shared directory (standing in for a synced
// drive or a USB stick carried between sites), converge on identical state.
func TestFolderReconverge(t *testing.T) {
	dir := t.TempDir()
	a := newNode(t)
	b := newNode(t)

	org, err := a.r.CreateOrg("Sneakernet Co")
	if err != nil {
		t.Fatal(err)
	}
	// Two buildings, one per divergent writer: see the comment in
	// sync_test.go's TestTwoNodeHTTPReconverge about the (unreplicated)
	// per-building job number sequence.
	buildingA, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Offline Block A"})
	if err != nil {
		t.Fatal(err)
	}
	buildingB, err := a.r.CreateBuilding(org.ID, domain.Building{Name: "Offline Block B"})
	if err != nil {
		t.Fatal(err)
	}

	// Round 1: a writes its file; b imports it (learning the org/buildings)
	// and writes its own (empty) file.
	if res := a.e.FolderSync(dir); res.Error != "" {
		t.Fatalf("a folder sync 1: %+v", res)
	}
	if res := b.e.FolderSync(dir); res.Error != "" {
		t.Fatalf("b folder sync 1: %+v", res)
	}
	if _, err := b.r.GetBuilding(org.ID, buildingA.ID); err != nil {
		t.Fatalf("b does not have building A after importing a's file: %v", err)
	}

	// Diverge: each side raises a job, offline from each other, communicating
	// only through files dropped into dir.
	jobA, err := a.r.CreateJob(org.ID, domain.Job{BuildingID: buildingA.ID, Title: "From A"}, "")
	if err != nil {
		t.Fatal(err)
	}
	jobB, err := b.r.CreateJob(org.ID, domain.Job{BuildingID: buildingB.ID, Title: "From B"}, "")
	if err != nil {
		t.Fatal(err)
	}

	// Round 2: both export their new job, then both import what the other
	// wrote. Two calls each side is enough regardless of write/import
	// ordering, because the files are append-only and applying twice is a
	// no-op.
	if res := a.e.FolderSync(dir); res.Error != "" {
		t.Fatalf("a folder sync 2: %+v", res)
	}
	if res := b.e.FolderSync(dir); res.Error != "" {
		t.Fatalf("b folder sync 2: %+v", res)
	}
	if res := a.e.FolderSync(dir); res.Error != "" {
		t.Fatalf("a folder sync 3: %+v", res)
	}

	for name, n := range map[string]*node{"a": a, "b": b} {
		if _, err := n.r.GetJob(org.ID, jobA.ID); err != nil {
			t.Errorf("%s missing jobA after folder reconverge: %v", name, err)
		}
		if _, err := n.r.GetJob(org.ID, jobB.ID); err != nil {
			t.Errorf("%s missing jobB after folder reconverge: %v", name, err)
		}
	}
	if got, want := a.jobCount(t), 2; got != want {
		t.Errorf("a has %d jobs, want %d", got, want)
	}
	if got, want := b.jobCount(t), 2; got != want {
		t.Errorf("b has %d jobs, want %d", got, want)
	}

	// Idempotence: running the same round again changes nothing and errors
	// on nothing (§9: "it does not matter how often the stick is carried").
	if res := a.e.FolderSync(dir); res.Error != "" || res.Exported != 0 {
		t.Errorf("re-running folder sync on a should be a no-op, got %+v", res)
	}
}
