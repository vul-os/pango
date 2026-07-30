package repo

// Repo tests. Two things get the most attention here, because they are the two
// that fail silently in production:
//
//  1. Append-only money. A stored total would lose a concurrent write with no
//     error at all, so the tests assert that SUM includes every entry and that
//     a negative correction reduces the total rather than editing anything.
//  2. Organisation isolation. A missing WHERE org_id would leak one managing
//     agent's portfolio to another and look completely normal from the inside.

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/store"
)

func testRepo(t *testing.T) *Repo {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return New(s)
}

// newOrg creates an organisation with a building, and returns both ids.
func newOrg(t *testing.T, r *Repo, name string) (orgID, buildingID string) {
	t.Helper()
	org, err := r.CreateOrg(name)
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	b, err := r.CreateBuilding(org.ID, domain.Building{Name: name + " Court"})
	if err != nil {
		t.Fatalf("create building: %v", err)
	}
	return org.ID, b.ID
}

func newJob(t *testing.T, r *Repo, orgID, buildingID, unitLabel, title string) domain.Job {
	t.Helper()
	j, err := r.CreateJob(orgID, domain.Job{BuildingID: buildingID, Title: title}, unitLabel)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	return j
}

// ── units ───────────────────────────────────────────────────────────────────

// The §4.1 failure, at the repo level: three spellings of one door must produce
// one unit, so the per-unit cost report is not fragmented across them.
func TestEnsureUnitCollapsesSpellings(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")

	var ids []string
	for _, label := range []string{"Flat 3A", "3A", "flat 3a", "3 A", "No. 3a"} {
		u, err := r.EnsureUnit(org, building, label)
		if err != nil {
			t.Fatalf("EnsureUnit(%q): %v", label, err)
		}
		ids = append(ids, u.ID)
		if u.Key != "3a" {
			t.Errorf("EnsureUnit(%q).Key = %q, want 3a", label, u.Key)
		}
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[0] {
			t.Fatalf("spelling %d created a second unit: %s vs %s", i, ids[i], ids[0])
		}
	}

	units, err := r.ListUnits(org, building)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("building has %d units, want 1", len(units))
	}
	// The first spelling seen keeps the display label, so the name does not
	// flicker as different people touch the same unit.
	if units[0].Label != "Flat 3A" {
		t.Errorf("label = %q, want the first spelling %q", units[0].Label, "Flat 3A")
	}
}

func TestEnsureUnitRespectsMixedUseScheme(t *testing.T) {
	r := testRepo(t)
	org, _ := newOrg(t, r, "Oakmead")
	b, err := r.CreateBuilding(org, domain.Building{
		Name: "Oakmead Mews", UnitScheme: domain.SchemeMixedUse,
	})
	if err != nil {
		t.Fatal(err)
	}

	shop, err := r.EnsureUnit(org, b.ID, "Shop 2")
	if err != nil {
		t.Fatal(err)
	}
	flat, err := r.EnsureUnit(org, b.ID, "Flat 2")
	if err != nil {
		t.Fatal(err)
	}
	if shop.ID == flat.ID {
		t.Fatal("mixed-use building merged Shop 2 and Flat 2 into one unit")
	}
}

// ── append-only money and hours (§6) ────────────────────────────────────────

func TestCostIsSummedNotStored(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	j := newJob(t, r, org, building, "Flat 3A", "Leaking mixer")

	entries := []domain.CostEntry{
		{JobID: j.ID, Kind: domain.CostCallout, AmountMinor: 45000},
		{JobID: j.ID, Kind: domain.CostMaterial, AmountMinor: 28550},
		{JobID: j.ID, Kind: domain.CostLabour, AmountMinor: 90000},
	}
	for _, e := range entries {
		if _, err := r.AddCost(org, e); err != nil {
			t.Fatal(err)
		}
	}

	costs, err := r.ListCosts(org, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(costs) != 3 {
		t.Fatalf("got %d cost entries, want 3", len(costs))
	}
	var total domain.Money
	for _, c := range costs {
		total += c.AmountMinor
	}
	if total != 163550 {
		t.Fatalf("total = %d, want 163550", total)
	}
}

// A correction is a new entry with a negative amount, never an edit (§6). This
// is what keeps the audit trail complete by construction.
func TestNegativeCorrectionReducesTotalWithoutEditing(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	j := newJob(t, r, org, building, "Flat 3A", "Leaking mixer")

	if _, err := r.AddCost(org, domain.CostEntry{
		JobID: j.ID, Kind: domain.CostCallout, Description: "Call-out fee", AmountMinor: 45000,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddCost(org, domain.CostEntry{
		JobID: j.ID, Kind: domain.CostLabour, Description: "2h labour", AmountMinor: 90000,
	}); err != nil {
		t.Fatal(err)
	}
	// The call-out is waived: a new entry, not an edit of the original.
	if _, err := r.AddCost(org, domain.CostEntry{
		JobID: j.ID, Kind: domain.CostCallout, Description: "Call-out waived", AmountMinor: -45000,
	}); err != nil {
		t.Fatal(err)
	}

	costs, err := r.ListCosts(org, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	// All three entries survive: the original charge is still in the record.
	if len(costs) != 3 {
		t.Fatalf("got %d entries, want 3 — a correction must not remove the original", len(costs))
	}
	var total domain.Money
	var sawOriginal, sawCorrection bool
	for _, c := range costs {
		total += c.AmountMinor
		if c.AmountMinor == 45000 {
			sawOriginal = true
		}
		if c.AmountMinor == -45000 {
			sawCorrection = true
		}
	}
	if !sawOriginal || !sawCorrection {
		t.Error("audit trail incomplete: both the charge and its reversal must remain")
	}
	if total != 90000 {
		t.Fatalf("total after correction = %d, want 90000", total)
	}
}

func TestZeroAmountsAreRejected(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	j := newJob(t, r, org, building, "Flat 3A", "Leaking mixer")

	if _, err := r.AddCost(org, domain.CostEntry{JobID: j.ID, AmountMinor: 0}); err == nil {
		t.Error("a zero cost entry should be rejected — job_event is the comment field")
	}
	if _, err := r.AddTime(org, domain.TimeEntry{JobID: j.ID, Minutes: 0}); err == nil {
		t.Error("a zero time entry should be rejected")
	}
}

func TestTimeIsSummedIncludingCorrections(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	j := newJob(t, r, org, building, "Flat 3A", "Leaking mixer")

	for _, m := range []int64{45, 75, -20} { // the -20 is a correction
		if _, err := r.AddTime(org, domain.TimeEntry{JobID: j.ID, Minutes: m}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := r.ListTime(org, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d time entries, want 3", len(entries))
	}
	var total int64
	for _, e := range entries {
		total += e.Minutes
	}
	if total != 100 {
		t.Fatalf("minutes = %d, want 100", total)
	}
}

// ── job numbering and status ────────────────────────────────────────────────

// Numbers are per building (§5), so two buildings both start at 1 and neither
// needs to coordinate with the other.
func TestJobNumbersAreNamespacedPerBuilding(t *testing.T) {
	r := testRepo(t)
	org, buildingA := newOrg(t, r, "Meridian")
	buildingB, err := r.CreateBuilding(org, domain.Building{Name: "Harbour View"})
	if err != nil {
		t.Fatal(err)
	}

	for i := int64(1); i <= 3; i++ {
		j := newJob(t, r, org, buildingA, "1", "job")
		if j.Number != i {
			t.Errorf("building A job %d has number %d", i, j.Number)
		}
	}
	for i := int64(1); i <= 2; i++ {
		j, err := r.CreateJob(org, domain.Job{BuildingID: buildingB.ID, Title: "job"}, "1")
		if err != nil {
			t.Fatal(err)
		}
		if j.Number != i {
			t.Errorf("building B job %d has number %d, want %d — sequences must be per building", i, j.Number, i)
		}
	}
}

func TestJobStatusTransitions(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	j := newJob(t, r, org, building, "Flat 3A", "Leaking mixer")

	for _, st := range []string{domain.StatusTriaged, domain.StatusAssigned, domain.StatusInProgress, domain.StatusResolved, domain.StatusClosed} {
		updated, err := r.SetJobStatus(org, j.ID, st, "", "")
		if err != nil {
			t.Fatalf("transition to %s: %v", st, err)
		}
		if updated.Status != st {
			t.Fatalf("status = %s, want %s", updated.Status, st)
		}
	}

	closed, err := r.GetJob(org, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if closed.ClosedAt == "" {
		t.Error("closing a job must stamp closed_at")
	}

	// Closed → triaged is not in the graph; only an explicit reopen is.
	if _, err := r.SetJobStatus(org, j.ID, domain.StatusTriaged, "", ""); !errors.Is(err, ErrConflict) {
		t.Errorf("illegal transition error = %v, want ErrConflict", err)
	}

	// Reopening clears closed_at, so the job counts as open work again.
	reopened, err := r.SetJobStatus(org, j.ID, domain.StatusInProgress, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ClosedAt != "" {
		t.Error("reopening a job must clear closed_at")
	}

	// Every transition is recorded on the thread.
	events, err := r.ListEvents(org, j.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 6 {
		t.Errorf("got %d events, want one per transition", len(events))
	}
}

// ── tenant visibility (§4.3) ────────────────────────────────────────────────

func TestEventVisibilitySplit(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	j := newJob(t, r, org, building, "Flat 3A", "Leaking mixer")

	if _, err := r.AddEvent(org, domain.JobEvent{
		JobID: j.ID, Kind: "note", Body: "Contractor booked for Tuesday.",
		Visibility: domain.VisibilityPublic,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddEvent(org, domain.JobEvent{
		JobID: j.ID, Kind: "note", Body: "Recharge to owner — tenant damage.",
		Visibility: domain.VisibilityInternal,
	}); err != nil {
		t.Fatal(err)
	}

	public, err := r.ListEvents(org, j.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range public {
		if e.Visibility != domain.VisibilityPublic {
			t.Fatalf("internal note leaked to the tenant view: %q", e.Body)
		}
	}
	if len(public) != 1 {
		t.Fatalf("public events = %d, want 1", len(public))
	}

	all, err := r.ListEvents(org, j.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("internal view has %d events, want 2", len(all))
	}
}

// An event defaults to internal when no visibility is given: leaking by
// omission must be impossible.
func TestEventDefaultsToInternal(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")
	j := newJob(t, r, org, building, "Flat 3A", "Leaking mixer")

	e, err := r.AddEvent(org, domain.JobEvent{JobID: j.ID, Body: "no visibility given"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Visibility != domain.VisibilityInternal {
		t.Errorf("default visibility = %q, want internal", e.Visibility)
	}
}

// ── organisation isolation (§11) ────────────────────────────────────────────

// The legacy breach, at the repo level: org A must not be able to read, write
// or even confirm the existence of org B's rows.
func TestOrgIsolation(t *testing.T) {
	r := testRepo(t)
	orgA, buildingA := newOrg(t, r, "Meridian")
	orgB, buildingB := newOrg(t, r, "Cornerstone")

	jobA := newJob(t, r, orgA, buildingA, "Flat 3A", "A's job")
	jobB := newJob(t, r, orgB, buildingB, "Flat 9", "B's job")
	unitA, err := r.EnsureUnit(orgA, buildingA, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}

	// Reads across the boundary return ErrNotFound — never 403, which would
	// confirm the id is real and belongs to somebody.
	if _, err := r.GetBuilding(orgA, buildingB); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A read org B's building: %v", err)
	}
	if _, err := r.GetJob(orgA, jobB.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A read org B's job: %v", err)
	}
	if _, err := r.GetUnit(orgB, unitA.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("org B read org A's unit: %v", err)
	}

	// Listings never cross the boundary.
	buildings, err := r.ListBuildings(orgA)
	if err != nil {
		t.Fatal(err)
	}
	if len(buildings) != 1 || buildings[0].ID != buildingA {
		t.Fatalf("org A sees %d buildings, want only its own", len(buildings))
	}
	jobs, err := r.ListJobs(orgA, JobFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != jobA.ID {
		t.Fatalf("org A sees %d jobs, want only its own", len(jobs))
	}

	// A client-supplied building filter naming another org's building yields
	// nothing, rather than that org's jobs.
	cross, err := r.ListJobs(orgA, JobFilter{BuildingID: buildingB})
	if err != nil {
		t.Fatal(err)
	}
	if len(cross) != 0 {
		t.Fatalf("filtering by another org's building returned %d jobs", len(cross))
	}

	// Writes across the boundary fail.
	if _, err := r.AddCost(orgA, domain.CostEntry{JobID: jobB.ID, AmountMinor: 100}); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A wrote a cost onto org B's job: %v", err)
	}
	if _, err := r.AddTime(orgA, domain.TimeEntry{JobID: jobB.ID, Minutes: 30}); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A wrote time onto org B's job: %v", err)
	}
	if _, err := r.AddEvent(orgA, domain.JobEvent{JobID: jobB.ID, Body: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A wrote an event onto org B's job: %v", err)
	}
	if _, err := r.SetJobStatus(orgA, jobB.ID, domain.StatusClosed, "", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A closed org B's job: %v", err)
	}
	if _, err := r.CreateJob(orgA, domain.Job{BuildingID: buildingB, Title: "x"}, "1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A raised a job against org B's building: %v", err)
	}
	if err := r.DeleteBuilding(orgA, buildingB); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A deleted org B's building: %v", err)
	}
	if _, err := r.EnsureUnit(orgA, buildingB, "Flat 1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("org A created a unit in org B's building: %v", err)
	}

	// Org B is untouched by all of that.
	stillThere, err := r.GetJob(orgB, jobB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillThere.Status == domain.StatusClosed {
		t.Fatal("org B's job was modified across the tenancy boundary")
	}
}

// Units with the same key in different organisations are different units.
func TestUnitKeysDoNotCollideAcrossOrgs(t *testing.T) {
	r := testRepo(t)
	orgA, buildingA := newOrg(t, r, "Meridian")
	orgB, buildingB := newOrg(t, r, "Cornerstone")

	a, err := r.EnsureUnit(orgA, buildingA, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.EnsureUnit(orgB, buildingB, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("the same unit key in two organisations resolved to one unit")
	}
}

// ── auth ────────────────────────────────────────────────────────────────────

func TestAuthenticateAndSessions(t *testing.T) {
	r := testRepo(t)
	org, err := r.CreateOrg("Meridian")
	if err != nil {
		t.Fatal(err)
	}
	user, err := r.CreateUser(org.ID, "Manager@Meridian.example", "correct-horse-battery", "Manager", "owner")
	if err != nil {
		t.Fatal(err)
	}

	// Email matching is case-insensitive; a manager who capitalises their
	// address on Monday must still get in on Tuesday.
	if _, err := r.Authenticate("manager@meridian.example", "correct-horse-battery"); err != nil {
		t.Errorf("login with lowercased email failed: %v", err)
	}
	// Both failure modes return the same error, so the form is not a staff list.
	if _, err := r.Authenticate("manager@meridian.example", "wrong"); !errors.Is(err, ErrBadCredentials) {
		t.Errorf("wrong password error = %v, want ErrBadCredentials", err)
	}
	if _, err := r.Authenticate("nobody@meridian.example", "correct-horse-battery"); !errors.Is(err, ErrBadCredentials) {
		t.Errorf("unknown user error = %v, want ErrBadCredentials", err)
	}

	token, err := r.CreateSession(user)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.SessionUser(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != user.ID || got.OrgID != org.ID {
		t.Fatal("session resolved to the wrong user")
	}

	// Only the hash is stored: the plaintext token must not appear in the row.
	var stored string
	if err := r.DB().QueryRow("SELECT token_hash FROM session LIMIT 1").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token {
		t.Fatal("session token stored in plaintext — a stolen database file would yield live sessions")
	}

	if err := r.DeleteSession(token); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SessionUser(token); !errors.Is(err, ErrBadCredentials) {
		t.Error("a revoked session still resolves")
	}
	if _, err := r.SessionUser(""); !errors.Is(err, ErrBadCredentials) {
		t.Error("an empty token resolved to a user")
	}
}

func TestPasswordPolicy(t *testing.T) {
	r := testRepo(t)
	org, err := r.CreateOrg("Meridian")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateUser(org.ID, "a@b.example", "short", "X", ""); err == nil {
		t.Error("a short password was accepted")
	}
	if _, err := r.CreateUser(org.ID, "not-an-email", "correct-horse-battery", "X", ""); err == nil {
		t.Error("an invalid email was accepted")
	}
	if _, err := r.CreateUser(org.ID, "a@b.example", "correct-horse-battery", "X", ""); err != nil {
		t.Fatal(err)
	}
	// A duplicate address would make login ambiguous.
	if _, err := r.CreateUser(org.ID, "A@B.example", "correct-horse-battery", "Y", ""); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate email error = %v, want ErrConflict", err)
	}
}

// ── inspections ─────────────────────────────────────────────────────────────

func TestInspectionRequiresUnitForTenancyKinds(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")

	// An ingoing inspection with no unit could never be paired with an
	// outgoing one, so it would be evidence of nothing.
	if _, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: building, Kind: domain.InspectionIngoing,
	}, ""); err == nil {
		t.Error("an ingoing inspection without a unit was accepted")
	}
	// A routine inspection of common property legitimately has no unit.
	if _, err := r.CreateInspection(org, domain.Inspection{
		BuildingID: building, Kind: domain.InspectionRoutine,
	}, ""); err != nil {
		t.Errorf("a routine common-property inspection was rejected: %v", err)
	}
}

func TestFindingsAreAppendOnlyAndScopedToTemplate(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")

	tmpl, err := r.CreateTemplate(org, domain.InspectionTemplate{
		Name: "Move-in", Items: []domain.TemplateItem{{Label: "Flooring"}, {Label: "Windows"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := r.CreateTemplate(org, domain.InspectionTemplate{
		Name: "Other", Items: []domain.TemplateItem{{Label: "Roof"}},
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
	// A revision is a new row; the superseded one stays in the record.
	if _, err := r.AddFinding(org, domain.Finding{
		InspectionID: insp.ID, ItemID: tmpl.Items[0].ID, Condition: domain.ConditionDamage,
		Comment: "Revised after a closer look.",
	}); err != nil {
		t.Fatal(err)
	}
	findings, err := r.ListFindings(org, insp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("got %d findings, want 2 — a revision must not overwrite the original", len(findings))
	}

	// An item from a different checklist must not attach, or a comparison
	// could pair unrelated items.
	if _, err := r.AddFinding(org, domain.Finding{
		InspectionID: insp.ID, ItemID: other.Items[0].ID, Condition: domain.ConditionOK,
	}); !errors.Is(err, ErrConflict) {
		t.Errorf("cross-template finding error = %v, want ErrConflict", err)
	}

	// Templates cannot be empty: an empty checklist records nothing while
	// looking like a completed inspection.
	if _, err := r.CreateTemplate(org, domain.InspectionTemplate{Name: "Empty"}); err == nil {
		t.Error("an empty template was accepted")
	}
}

func TestTombstoneRatherThanDelete(t *testing.T) {
	r := testRepo(t)
	org, building := newOrg(t, r, "Meridian")

	if err := r.DeleteBuilding(org, building); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetBuilding(org, building); !errors.Is(err, ErrNotFound) {
		t.Error("deleted building is still readable")
	}
	// The row must survive as a tombstone: a physical delete would be undone
	// by the first peer that never saw it.
	var deleted int
	if err := r.DB().QueryRow("SELECT deleted FROM building WHERE id = ?", building).Scan(&deleted); err != nil {
		t.Fatalf("building row was physically removed: %v", err)
	}
	if deleted != 1 {
		t.Error("building was not tombstoned")
	}
}
