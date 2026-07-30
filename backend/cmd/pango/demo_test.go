package main

// The demo dataset is a shipped surface (§12): it is what the screenshotter
// runs against and the first thing a contributor sees, so a seeding failure is
// a broken front door rather than a broken test fixture. It is also the only
// place the whole stack is exercised end to end in one call.

import (
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/repo"
	"github.com/vul-os/pango/backend/internal/report"
	"github.com/vul-os/pango/backend/internal/store"
)

func TestSeedDemo(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := repo.New(s)

	creds, err := seedDemo(r)
	if err != nil {
		t.Fatalf("seedDemo: %v", err)
	}

	// The advertised credentials must actually work — a demo that prints a
	// login nobody can use is worse than no demo.
	user, err := r.Authenticate(creds.Email, creds.Password)
	if err != nil {
		t.Fatalf("demo credentials do not work: %v", err)
	}
	org := user.OrgID

	buildings, err := r.ListBuildings(org)
	if err != nil {
		t.Fatal(err)
	}
	if len(buildings) < 3 {
		t.Errorf("demo has %d buildings, want at least 3", len(buildings))
	}

	jobs, err := r.ListJobs(org, repo.JobFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) < 8 {
		t.Errorf("demo has %d jobs, want at least 8", len(jobs))
	}

	// Jobs must span several statuses, or every screen looks the same.
	statuses := map[string]bool{}
	for _, j := range jobs {
		statuses[j.Status] = true
	}
	if len(statuses) < 4 {
		t.Errorf("demo jobs span %d statuses, want at least 4: %v", len(statuses), statuses)
	}

	rep := report.New(r.DB())

	// The unit-key collapse must be visible in the demo: Riverside's "Flat 3A",
	// "3a" and "3 A" are one unit carrying three jobs.
	units, err := rep.ByUnit(org, "")
	if err != nil {
		t.Fatal(err)
	}
	var collapsed *report.UnitTotals
	for i := range units {
		if units[i].Key == "3a" {
			collapsed = &units[i]
		}
	}
	if collapsed == nil {
		t.Fatal("demo has no unit with key 3a — the collapse case is not demonstrated")
	}
	if collapsed.Jobs != 3 {
		t.Errorf("unit 3a has %d jobs, want 3 (Flat 3A, 3a and 3 A are one door)", collapsed.Jobs)
	}

	// The mixed-use building must NOT collapse Shop 2 and Flat 2.
	var shop, flat bool
	for _, u := range units {
		switch u.Key {
		case "shop2":
			shop = true
		case "flat2":
			flat = true
		}
	}
	if !shop || !flat {
		t.Error("demo does not demonstrate the mixed-use scheme (shop2 and flat2 must both exist)")
	}

	// The negative correction must be present and must net correctly.
	jobTotals, err := rep.ByJob(org, "")
	if err != nil {
		t.Fatal(err)
	}
	var sawCorrection bool
	for _, jt := range jobTotals {
		costs, err := r.ListCosts(org, jt.JobID)
		if err != nil {
			t.Fatal(err)
		}
		var sum domain.Money
		for _, c := range costs {
			sum += c.AmountMinor
			if c.AmountMinor < 0 {
				sawCorrection = true
			}
		}
		if sum != jt.CostMinor {
			t.Errorf("job %s: report says %d, ledger sums to %d", jt.JobID, jt.CostMinor, sum)
		}
	}
	if !sawCorrection {
		t.Error("demo contains no negative correction entry — §6 is not demonstrated")
	}

	// The ingoing/outgoing pair is the differentiator; it must be in the demo,
	// with a real deterioration between the two.
	ingoing, err := r.ListInspections(org, repo.InspectionFilter{Kind: domain.InspectionIngoing})
	if err != nil {
		t.Fatal(err)
	}
	outgoing, err := r.ListInspections(org, repo.InspectionFilter{Kind: domain.InspectionOutgoing})
	if err != nil {
		t.Fatal(err)
	}
	if len(ingoing) == 0 || len(outgoing) == 0 {
		t.Fatal("demo has no ingoing/outgoing inspection pair")
	}
	if ingoing[0].UnitID == "" || ingoing[0].UnitID != outgoing[0].UnitID {
		t.Fatal("the ingoing and outgoing inspections are not for the same unit — they cannot be compared")
	}

	before, err := r.ListFindings(org, ingoing[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := r.ListFindings(org, outgoing[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) == 0 || len(after) == 0 {
		t.Fatal("inspections have no findings")
	}

	byItem := map[string]string{}
	for _, f := range before {
		byItem[f.ItemID] = f.Condition
	}
	var deteriorations int
	for _, f := range after {
		if was, ok := byItem[f.ItemID]; ok {
			if worse, comparable := domain.Deteriorated(was, f.Condition); comparable && worse {
				deteriorations++
			}
		}
	}
	if deteriorations < 2 {
		t.Errorf("demo shows %d deteriorations between ingoing and outgoing, want at least 2", deteriorations)
	}

	// Tenant-visible and internal notes must both exist, or the visibility
	// split (§4.3) is invisible in the demo.
	var sawPublic, sawInternal bool
	for _, j := range jobs {
		events, err := r.ListEvents(org, j.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			switch e.Visibility {
			case domain.VisibilityPublic:
				sawPublic = true
			case domain.VisibilityInternal:
				sawInternal = true
			}
		}
	}
	if !sawPublic || !sawInternal {
		t.Error("demo does not show both tenant-visible and internal events")
	}
}

// Demo mode must never touch a real database file.
func TestDemoUsesInMemoryDatabase(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := seedDemo(repo.New(s)); err != nil {
		t.Fatalf("demo seeding must work against an in-memory database: %v", err)
	}
}
