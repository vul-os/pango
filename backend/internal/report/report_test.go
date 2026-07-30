package report

// Report tests.
//
// The case that matters most here is the join fan-out: a job with three cost
// entries and two time entries, summed by a query that joins both ledgers at
// once, reports six times the cost and six times the minutes. It looks
// plausible on a two-row fixture and is catastrophic on real data, so the
// fixture below deliberately uses different counts of each.

import (
	"path/filepath"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/repo"
	"github.com/vul-os/pango/backend/internal/store"
)

func testReporter(t *testing.T) (*repo.Repo, *Reporter) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := repo.New(s)
	return r, New(r.DB())
}

func TestTotalsAreSummedNotMultipliedByJoins(t *testing.T) {
	r, rep := testReporter(t)

	org, err := r.CreateOrg("Meridian")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.CreateBuilding(org.ID, domain.Building{Name: "Riverside Court"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := r.CreateJob(org.ID, domain.Job{BuildingID: b.ID, Title: "Leaking mixer"}, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}

	// Three cost entries and two time entries: different counts, so a join
	// fan-out would produce an obviously wrong number rather than a coincidence.
	for _, amount := range []domain.Money{45000, 28550, 90000} {
		if _, err := r.AddCost(org.ID, domain.CostEntry{JobID: j.ID, AmountMinor: amount}); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []int64{45, 75} {
		if _, err := r.AddTime(org.ID, domain.TimeEntry{JobID: j.ID, Minutes: m}); err != nil {
			t.Fatal(err)
		}
	}

	jobs, err := rep.ByJob(org.ID, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d job totals, want 1", len(jobs))
	}
	if jobs[0].CostMinor != 163550 {
		t.Errorf("job cost = %d, want 163550 (a join fan-out would give a multiple)", jobs[0].CostMinor)
	}
	if jobs[0].Minutes != 120 {
		t.Errorf("job minutes = %d, want 120", jobs[0].Minutes)
	}

	buildings, err := rep.ByBuilding(org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(buildings) != 1 {
		t.Fatalf("got %d buildings, want 1", len(buildings))
	}
	if buildings[0].CostMinor != 163550 || buildings[0].Minutes != 120 {
		t.Errorf("building totals = %d minor / %d min, want 163550 / 120",
			buildings[0].CostMinor, buildings[0].Minutes)
	}
	if buildings[0].Jobs != 1 || buildings[0].OpenJobs != 1 {
		t.Errorf("building job counts = %d/%d, want 1/1", buildings[0].Jobs, buildings[0].OpenJobs)
	}

	units, err := rep.ByUnit(org.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("got %d units, want 1", len(units))
	}
	if units[0].CostMinor != 163550 || units[0].Minutes != 120 {
		t.Errorf("unit totals = %d minor / %d min, want 163550 / 120",
			units[0].CostMinor, units[0].Minutes)
	}
}

// The whole point of §4.1: three spellings of one door produce ONE unit row
// carrying the full spend, rather than three rows each understating it.
func TestUnitTotalsAreNotFragmentedBySpelling(t *testing.T) {
	r, rep := testReporter(t)

	org, err := r.CreateOrg("Meridian")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.CreateBuilding(org.ID, domain.Building{Name: "Riverside Court"})
	if err != nil {
		t.Fatal(err)
	}

	for _, spelling := range []string{"Flat 3A", "3a", "3 A"} {
		j, err := r.CreateJob(org.ID, domain.Job{BuildingID: b.ID, Title: "job"}, spelling)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.AddCost(org.ID, domain.CostEntry{JobID: j.ID, AmountMinor: 10000}); err != nil {
			t.Fatal(err)
		}
	}

	units, err := rep.ByUnit(org.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("per-unit report has %d rows, want 1 — spend is fragmented across spellings", len(units))
	}
	if units[0].Jobs != 3 {
		t.Errorf("unit jobs = %d, want 3", units[0].Jobs)
	}
	if units[0].CostMinor != 30000 {
		t.Errorf("unit cost = %d, want 30000 — the full spend for the door", units[0].CostMinor)
	}
}

// A correction reduces the reported total without removing the original entry.
func TestReportedTotalIncludesNegativeCorrection(t *testing.T) {
	r, rep := testReporter(t)

	org, err := r.CreateOrg("Meridian")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.CreateBuilding(org.ID, domain.Building{Name: "Riverside Court"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := r.CreateJob(org.ID, domain.Job{BuildingID: b.ID, Title: "Leaking mixer"}, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}

	for _, amount := range []domain.Money{45000, 90000, -45000} {
		if _, err := r.AddCost(org.ID, domain.CostEntry{JobID: j.ID, AmountMinor: amount}); err != nil {
			t.Fatal(err)
		}
	}

	jobs, err := rep.ByJob(org.ID, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].CostMinor != 90000 {
		t.Errorf("cost after correction = %d, want 90000", jobs[0].CostMinor)
	}
	// All three entries remain: the report nets them, it does not delete one.
	if jobs[0].Entries != 3 {
		t.Errorf("entries = %d, want 3 — the audit trail must be complete", jobs[0].Entries)
	}
}

func TestStatusAndTimeline(t *testing.T) {
	r, rep := testReporter(t)

	org, err := r.CreateOrg("Meridian")
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.CreateBuilding(org.ID, domain.Building{Name: "Riverside Court"})
	if err != nil {
		t.Fatal(err)
	}

	// Two open, one closed, one cancelled.
	for i, transitions := range [][]string{
		{},
		{domain.StatusTriaged},
		{domain.StatusAssigned, domain.StatusInProgress, domain.StatusResolved, domain.StatusClosed},
		{domain.StatusCancelled},
	} {
		j, err := r.CreateJob(org.ID, domain.Job{BuildingID: b.ID, Title: "job"}, "Flat 3A")
		if err != nil {
			t.Fatalf("job %d: %v", i, err)
		}
		for _, st := range transitions {
			if _, err := r.SetJobStatus(org.ID, j.ID, st, "", ""); err != nil {
				t.Fatal(err)
			}
		}
	}

	status, err := rep.Status(org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Total != 4 {
		t.Errorf("total = %d, want 4", status.Total)
	}
	if status.Open != 2 {
		t.Errorf("open = %d, want 2 (reported + triaged)", status.Open)
	}
	if status.Closed != 2 {
		t.Errorf("closed = %d, want 2 (closed + cancelled are both terminal)", status.Closed)
	}
	if status.ByStatus[domain.StatusClosed] != 1 || status.ByStatus[domain.StatusCancelled] != 1 {
		t.Errorf("by-status breakdown wrong: %v", status.ByStatus)
	}

	timeline, err := rep.Timeline(org.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(timeline) == 0 {
		t.Fatal("timeline is empty")
	}
	var created, closed int64
	for _, p := range timeline {
		created += p.Created
		closed += p.Closed
	}
	if created != 4 {
		t.Errorf("timeline created = %d, want 4", created)
	}
	if closed != 2 {
		t.Errorf("timeline closed = %d, want 2", closed)
	}
	// Days must come out in order for a chart to be drawable straight from it.
	for i := 1; i < len(timeline); i++ {
		if timeline[i].Day < timeline[i-1].Day {
			t.Fatalf("timeline out of order: %s then %s", timeline[i-1].Day, timeline[i].Day)
		}
	}
}

// Reports are scoped to one organisation. A report that leaked across the
// boundary would hand a competitor's spend to a managing agent as a chart.
func TestReportsAreOrgScoped(t *testing.T) {
	r, rep := testReporter(t)

	orgA, err := r.CreateOrg("Meridian")
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := r.CreateOrg("Cornerstone")
	if err != nil {
		t.Fatal(err)
	}

	for _, spec := range []struct {
		org    string
		name   string
		amount domain.Money
	}{
		{orgA.ID, "Riverside Court", 10000},
		{orgB.ID, "Harbour View", 99999},
	} {
		b, err := r.CreateBuilding(spec.org, domain.Building{Name: spec.name})
		if err != nil {
			t.Fatal(err)
		}
		j, err := r.CreateJob(spec.org, domain.Job{BuildingID: b.ID, Title: "job"}, "Flat 1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.AddCost(spec.org, domain.CostEntry{JobID: j.ID, AmountMinor: spec.amount}); err != nil {
			t.Fatal(err)
		}
	}

	buildings, err := rep.ByBuilding(orgA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(buildings) != 1 || buildings[0].Name != "Riverside Court" {
		t.Fatalf("org A's building report leaked: %+v", buildings)
	}
	if buildings[0].CostMinor != 10000 {
		t.Errorf("org A cost = %d, want 10000 — org B's spend must not be included", buildings[0].CostMinor)
	}

	units, err := rep.ByUnit(orgA.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("org A sees %d units, want 1", len(units))
	}

	jobs, err := rep.ByJob(orgA.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("org A sees %d jobs, want 1", len(jobs))
	}

	status, err := rep.Status(orgA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Total != 1 {
		t.Errorf("org A status total = %d, want 1", status.Total)
	}
}
