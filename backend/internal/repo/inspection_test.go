package repo

// Inspection repo tests beyond the two in repo_test.go
// (TestInspectionRequiresUnitForTenancyKinds,
// TestFindingsAreAppendOnlyAndScopedToTemplate): completion immutability, the
// ingoing/outgoing pairing rule across multiple tenancies, and that a job or
// unit id from outside the org cannot be attached to an inspection.

import (
	"errors"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
)

func TestInspectionRequiresValidBuilding(t *testing.T) {
	r := testRepo(t)
	org, _ := newOrg(t, r, "Meridian")

	if _, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: "does-not-exist", Kind: domain.InspectionRoutine,
	}, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("inspection against an unknown building: err = %v, want ErrNotFound", err)
	}
}

func TestInspectionRejectsUnitFromAnotherOrg(t *testing.T) {
	r := testRepo(t)
	orgA, buildingA := newOrg(t, r, "Meridian")
	orgB, buildingB := newOrg(t, r, "Highgate")

	unitB, err := r.EnsureUnit(orgB, buildingB, "Flat 1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.CreateInspection(orgA, domain.Inspection{
		BuildingID: buildingA, UnitID: unitB.ID, Kind: domain.InspectionRoutine,
	}, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("inspection with another org's unit: err = %v, want ErrNotFound", err)
	}
}

func TestInspectionOptionallyLinksAJob(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	job := newJob(t, r, org, building, "Flat 3A", "Fix the leak")

	insp, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: building, Kind: domain.InspectionRoutine, JobID: job.ID,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if insp.JobID != job.ID {
		t.Errorf("inspection job id = %q, want %q", insp.JobID, job.ID)
	}
	got, err := r.GetInspection(org, insp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.JobID != job.ID {
		t.Errorf("reloaded inspection job id = %q, want %q", got.JobID, job.ID)
	}

	// A job id from a different org must not attach — the same isolation rule
	// as everything else (§11).
	otherOrg, otherBuilding := newOrg(t, r, "Highgate")
	otherJob := newJob(t, r, otherOrg, otherBuilding, "Flat 1", "Other org's job")
	if _, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: building, Kind: domain.InspectionRoutine, JobID: otherJob.ID,
	}, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("inspection with another org's job: err = %v, want ErrNotFound", err)
	}
}

// A completed inspection is immutable: no further status change, and no new
// findings. The legacy handleCompletion() did neither of these things.
func TestCompletedInspectionIsImmutable(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	tmpl, err := r.CreateTemplate(org, domain.InspectionTemplate{
		Name: "Move-in", Items: []domain.TemplateItem{{Label: "Flooring"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	insp, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: building, TemplateID: tmpl.ID, Kind: domain.InspectionIngoing,
	}, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}

	insp, err = r.SetInspectionStatus(org, insp.ID, domain.InspectionComplete)
	if err != nil {
		t.Fatal(err)
	}
	if insp.PerformedAt == "" {
		t.Error("completing an inspection did not stamp performed_at")
	}

	// No further status change, including a no-op re-completion.
	if _, err := r.SetInspectionStatus(org, insp.ID, domain.InspectionComplete); !errors.Is(err, ErrConflict) {
		t.Errorf("re-completing a completed inspection: err = %v, want ErrConflict", err)
	}
	if _, err := r.SetInspectionStatus(org, insp.ID, domain.InspectionActive); !errors.Is(err, ErrConflict) {
		t.Errorf("reopening a completed inspection: err = %v, want ErrConflict", err)
	}

	// No new findings.
	if _, err := r.AddFinding(org, domain.Finding{
		InspectionID: insp.ID, ItemID: tmpl.Items[0].ID, Condition: domain.ConditionOK,
	}); !errors.Is(err, ErrConflict) {
		t.Errorf("finding on a completed inspection: err = %v, want ErrConflict", err)
	}
}

// A unit goes through several tenancies over its life, each with its own
// ingoing/outgoing pair. MatchingIngoing must bind each outgoing inspection to
// the ingoing one that actually preceded it — not simply "the newest ingoing
// on record", which would compare a move-out against a later tenant's move-in.
func TestMatchingIngoingBindsToThePrecedingTenancy(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")

	mk := func(kind, performedAt string) domain.Inspection {
		i, err := r.CreateInspection(org, domain.Inspection{
			BuildingID: building, Kind: kind,
		}, "Flat 3A")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.s.DB().Exec(`UPDATE inspection SET performed_at = ? WHERE id = ?`, performedAt, i.ID); err != nil {
			t.Fatal(err)
		}
		i.PerformedAt = performedAt
		return i
	}

	in1 := mk(domain.InspectionIngoing, "2024-01-01T00:00:00Z")
	out1 := mk(domain.InspectionOutgoing, "2024-06-01T00:00:00Z")
	in2 := mk(domain.InspectionIngoing, "2024-07-01T00:00:00Z")
	out2 := mk(domain.InspectionOutgoing, "2025-01-01T00:00:00Z")

	got, err := r.MatchingIngoing(org, out1)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != in1.ID {
		t.Errorf("first tenancy's outgoing matched ingoing %s, want %s", got.ID, in1.ID)
	}

	got, err = r.MatchingIngoing(org, out2)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != in2.ID {
		t.Errorf("second tenancy's outgoing matched ingoing %s, want %s", got.ID, in2.ID)
	}
}

func TestMatchingIngoingErrorsWithNoBaseline(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	out, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: building, Kind: domain.InspectionOutgoing,
	}, "Flat 9B")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.MatchingIngoing(org, out); !errors.Is(err, ErrNotFound) {
		t.Errorf("outgoing inspection with no ingoing baseline: err = %v, want ErrNotFound", err)
	}
}

// LatestFindings collapses append-only revisions to the newest observation
// per item — the read-time half of the append-only design (§6/§13).
func TestLatestFindingsReturnsNewestPerItem(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	tmpl, err := r.CreateTemplate(org, domain.InspectionTemplate{
		Name: "Move-in", Items: []domain.TemplateItem{{Label: "Flooring"}, {Label: "Windows"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	insp, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: building, TemplateID: tmpl.ID, Kind: domain.InspectionIngoing,
	}, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddFinding(org, domain.Finding{
		InspectionID: insp.ID, ItemID: tmpl.Items[0].ID, Condition: domain.ConditionOK,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddFinding(org, domain.Finding{
		InspectionID: insp.ID, ItemID: tmpl.Items[1].ID, Condition: domain.ConditionWear,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddFinding(org, domain.Finding{
		InspectionID: insp.ID, ItemID: tmpl.Items[0].ID, Condition: domain.ConditionDamage,
		Comment: "Correction: missed a scratch.",
	}); err != nil {
		t.Fatal(err)
	}

	latest, err := r.LatestFindings(org, insp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 {
		t.Fatalf("got %d latest findings, want 2 (one per item)", len(latest))
	}
	byItem := map[string]domain.Finding{}
	for _, f := range latest {
		byItem[f.ItemID] = f
	}
	if got := byItem[tmpl.Items[0].ID].Condition; got != domain.ConditionDamage {
		t.Errorf("latest finding for flooring = %q, want %q (the correction, not the original)", got, domain.ConditionDamage)
	}
	if got := byItem[tmpl.Items[1].ID].Condition; got != domain.ConditionWear {
		t.Errorf("latest finding for windows = %q, want %q", got, domain.ConditionWear)
	}
}
