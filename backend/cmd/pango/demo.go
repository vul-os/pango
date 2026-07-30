package main

// Demo seeding (§12).
//
// `pango --demo` has to produce a browsable product with no database, no
// configuration and no signup, because it is what the screenshotter runs
// against and the first thing a new contributor sees. A demo that shows three
// empty tables teaches nobody what the product is.
//
// So the dataset is deliberately shaped like real work rather than like test
// fixtures: jobs spread across every status, spend that includes a negative
// correction entry (§6), units entered with inconsistent spellings that collapse
// onto one key (§4.1), and an ingoing/outgoing inspection pair with a
// deterioration between them — which is the differentiator (§1) and is
// impossible to appreciate from an empty screen.

import (
	"fmt"
	"time"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/repo"
)

type demoCreds struct {
	Email    string
	Password string
}

// backdateJob rewrites one demo job's timestamps so the seeded portfolio spans
// about a month rather than a single afternoon. The reports timeline groups on
// `created_at` and `closed_at` (internal/report), so without this every job
// lands in one column and the chart reads as broken rather than as a quiet month.
//
// It writes to the database directly instead of going through the repo, which is
// precisely what production code must never do: it bypasses the oplog, so the
// change is invisible to sync and would diverge a replica. That is safe **here
// and only here** — `--demo` forces an in-memory database (see main.go), nothing
// is persisted, and a demo instance never enrols a peer. The alternative would be
// a timestamp override on the real write path, which is a much worse thing to own
// for the sake of a screenshot.
func backdateJob(r *repo.Repo, orgID, jobID string, daysAgo, closedAfter int) error {
	if daysAgo <= 0 {
		return nil
	}
	now := time.Now().UTC()
	opened := now.AddDate(0, 0, -daysAgo)
	openedStr := opened.Format(time.RFC3339)

	if _, err := r.DB().Exec(
		`UPDATE job SET created_at = ?, opened_at = ? WHERE id = ? AND org_id = ?`,
		openedStr, openedStr, jobID, orgID,
	); err != nil {
		return err
	}

	// The ledger and the thread belong to the same span as the job; leaving them
	// stamped "now" would show a month-old closed job accruing costs today.
	during := opened.AddDate(0, 0, 1)
	if during.After(now) {
		during = now
	}
	duringStr := during.Format(time.RFC3339)
	for _, tbl := range []string{"cost_entry", "time_entry", "job_event"} {
		if _, err := r.DB().Exec(
			`UPDATE `+tbl+` SET created_at = ? WHERE job_id = ? AND org_id = ?`,
			duringStr, jobID, orgID,
		); err != nil {
			return err
		}
	}

	// Only a job that actually reached a terminal status carries a closed_at,
	// hence the `<> ''` guard — and it can never close before it opened.
	closed := opened.AddDate(0, 0, max(closedAfter, 1))
	if closed.After(now) {
		closed = now
	}
	_, err := r.DB().Exec(
		`UPDATE job SET closed_at = ? WHERE id = ? AND org_id = ? AND closed_at <> ''`,
		closed.Format(time.RFC3339), jobID, orgID,
	)
	return err
}

func seedDemo(r *repo.Repo) (demoCreds, error) {
	creds := demoCreds{Email: "demo@pango.local", Password: "demopassword"}

	org, err := r.CreateOrg("Meridian Property Management")
	if err != nil {
		return creds, err
	}
	if _, err := r.CreateUser(org.ID, creds.Email, creds.Password, "Demo Manager", "owner"); err != nil {
		return creds, err
	}
	o := org.ID

	// People.
	staff, err := r.CreateParty(o, domain.Party{
		Kind: domain.PartyStaff, Name: "Thabo Nkosi", Email: "thabo@meridian.example", Phone: "+27 82 555 0134",
	})
	if err != nil {
		return creds, err
	}
	plumber, err := r.CreateParty(o, domain.Party{
		Kind: domain.PartyContractor, Name: "Rapid Plumbing CC", Email: "jobs@rapidplumbing.example",
	})
	if err != nil {
		return creds, err
	}
	electrician, err := r.CreateParty(o, domain.Party{
		Kind: domain.PartyContractor, Name: "Voltec Electrical", Email: "ops@voltec.example",
	})
	if err != nil {
		return creds, err
	}
	tenant, err := r.CreateParty(o, domain.Party{
		Kind: domain.PartyTenant, Name: "N. Adams", Email: "n.adams@example.com",
	})
	if err != nil {
		return creds, err
	}

	// Buildings. Coordinates are real Johannesburg/Cape Town points so the map
	// surface has something to render.
	lat1, lon1 := -26.1076, 28.0567
	riverside, err := r.CreateBuilding(o, domain.Building{
		Name: "Riverside Court", Address: "14 Riverside Road, Rosebank, Johannesburg",
		Lat: &lat1, Lon: &lon1,
	})
	if err != nil {
		return creds, err
	}
	lat2, lon2 := -33.9249, 18.4241
	harbour, err := r.CreateBuilding(o, domain.Building{
		Name: "Harbour View", Address: "3 Dock Road, Cape Town",
		Lat: &lat2, Lon: &lon2,
	})
	if err != nil {
		return creds, err
	}
	// A mixed-use block, so the demo shows why unit_scheme exists: here
	// "Shop 2" and "Flat 2" must stay two units.
	oakmead, err := r.CreateBuilding(o, domain.Building{
		Name: "Oakmead Mews", Address: "22 Oak Avenue, Durban",
		UnitScheme: domain.SchemeMixedUse,
	})
	if err != nil {
		return creds, err
	}

	// Jobs. Note the unit labels: "Flat 3A", "3a" and "3 A" are three
	// spellings of one door, and all three jobs land on one unit.
	type jobSpec struct {
		building  string
		unitLabel string
		title     string
		desc      string
		priority  string
		category  string
		assignee  string
		status    []string // transitions applied in order
		costs     []domain.CostEntry
		minutes   []int64
		// daysAgo backdates the job after it is created (see backdateJob). A
		// real portfolio accumulates over weeks, and seeding every job at
		// "now" made the reports timeline a single column — which reads as a
		// broken chart rather than as a quiet month.
		daysAgo int
		// closedAfter is how many days after opening a closed job was closed.
		// Ignored unless the last status transition is a terminal one.
		closedAfter int
	}
	specs := []jobSpec{
		{
			building: riverside.ID, unitLabel: "Flat 3A",
			title:   "Kitchen mixer leaking under sink",
			daysAgo: 31, closedAfter: 4,
			desc:     "Tenant reports water pooling in the cupboard overnight.",
			priority: domain.PriorityHigh, category: "plumbing", assignee: plumber.ID,
			status: []string{domain.StatusTriaged, domain.StatusAssigned, domain.StatusInProgress, domain.StatusResolved, domain.StatusClosed},
			costs: []domain.CostEntry{
				{Kind: domain.CostCallout, Description: "Call-out fee", AmountMinor: 45000},
				{Kind: domain.CostMaterial, Description: "Mixer cartridge + seals", AmountMinor: 28550},
				{Kind: domain.CostLabour, Description: "2h labour", AmountMinor: 90000},
				// A correction, not an edit (§6). The call-out was waived
				// because the contractor was already on site.
				{Kind: domain.CostCallout, Description: "Call-out waived — already on site", AmountMinor: -45000},
			},
			minutes: []int64{45, 75},
		},
		{
			building: riverside.ID, unitLabel: "3a",
			title:    "Bathroom extractor fan noisy",
			daysAgo:  6,
			desc:     "Intermittent rattle, worse in the evenings.",
			priority: domain.PriorityLow, category: "electrical", assignee: electrician.ID,
			status:  []string{domain.StatusTriaged, domain.StatusAssigned},
			costs:   []domain.CostEntry{{Kind: domain.CostCallout, Description: "Assessment visit", AmountMinor: 35000}},
			minutes: []int64{30},
		},
		{
			building: riverside.ID, unitLabel: "3 A",
			title:    "Front door lock stiff",
			daysAgo:  12,
			priority: domain.PriorityNormal, category: "general", assignee: staff.ID,
			status:  []string{domain.StatusInProgress},
			minutes: []int64{20},
		},
		{
			building: riverside.ID, unitLabel: "Flat 12",
			title:    "No hot water",
			daysAgo:  2,
			desc:     "Geyser element suspected.",
			priority: domain.PriorityEmergency, category: "plumbing", assignee: plumber.ID,
			status: []string{domain.StatusAssigned, domain.StatusInProgress},
			costs: []domain.CostEntry{
				{Kind: domain.CostMaterial, Description: "Geyser element 4kW", AmountMinor: 68000},
			},
			minutes: []int64{90},
		},
		{
			building: harbour.ID, unitLabel: "Unit 7",
			title:    "Balcony door seal perished",
			daysAgo:  9,
			priority: domain.PriorityNormal, category: "general",
			status: []string{domain.StatusTriaged},
		},
		{
			building: harbour.ID, unitLabel: "7",
			title:   "Parking bay light out",
			daysAgo: 26, closedAfter: 3,
			priority: domain.PriorityLow, category: "electrical", assignee: electrician.ID,
			status: []string{domain.StatusAssigned, domain.StatusInProgress, domain.StatusResolved, domain.StatusClosed},
			costs: []domain.CostEntry{
				{Kind: domain.CostMaterial, Description: "LED fitting", AmountMinor: 42000},
				{Kind: domain.CostLabour, Description: "Replacement", AmountMinor: 30000},
			},
			minutes: []int64{40},
		},
		{
			building: harbour.ID, unitLabel: "Common",
			title:    "Lift service overdue",
			daysAgo:  19,
			desc:     "Annual service certificate expires end of month.",
			priority: domain.PriorityHigh, category: "compliance",
			status: []string{domain.StatusTriaged, domain.StatusOnHold},
		},
		{
			building: oakmead.ID, unitLabel: "Shop 2",
			title:   "Shopfront glass chipped",
			daysAgo: 23, closedAfter: 5,
			priority: domain.PriorityNormal, category: "general",
			status: []string{domain.StatusTriaged, domain.StatusCancelled},
		},
		{
			building: oakmead.ID, unitLabel: "Flat 2",
			title:    "Damp patch on bedroom ceiling",
			daysAgo:  15,
			desc:     "Below the shop's roof outlet — possible blocked downpipe.",
			priority: domain.PriorityHigh, category: "damp", assignee: staff.ID,
			status: []string{domain.StatusTriaged, domain.StatusAssigned, domain.StatusInProgress},
			costs: []domain.CostEntry{
				{Kind: domain.CostContractor, Description: "Damp survey", AmountMinor: 120000},
			},
			minutes: []int64{120, 60},
		},

		// Closed history. Without a tail of finished work the board shows only
		// a backlog, the "closed" series on the timeline is flat, and the
		// per-unit cost report has nothing to rank.
		{
			building: riverside.ID, unitLabel: "Flat 8",
			title:   "Intercom panel not buzzing",
			daysAgo: 29, closedAfter: 2,
			desc:     "Handset lifts but no release for the street door.",
			priority: domain.PriorityNormal, category: "electrical", assignee: electrician.ID,
			status: []string{domain.StatusTriaged, domain.StatusAssigned, domain.StatusInProgress, domain.StatusResolved, domain.StatusClosed},
			costs: []domain.CostEntry{
				{Kind: domain.CostMaterial, Description: "Door release solenoid", AmountMinor: 51000},
				{Kind: domain.CostLabour, Description: "Fit and test", AmountMinor: 60000},
			},
			minutes: []int64{55},
		},
		{
			building: harbour.ID, unitLabel: "Unit 3",
			title:   "Gutter overflowing at rear",
			daysAgo: 24, closedAfter: 6,
			desc:     "Water tracking down the back wall in heavy rain.",
			priority: domain.PriorityHigh, category: "damp", assignee: staff.ID,
			status: []string{domain.StatusTriaged, domain.StatusAssigned, domain.StatusInProgress, domain.StatusResolved, domain.StatusClosed},
			costs: []domain.CostEntry{
				{Kind: domain.CostContractor, Description: "Gutter clear and re-hang", AmountMinor: 145000},
			},
			minutes: []int64{180},
		},
		{
			building: oakmead.ID, unitLabel: "Flat 5",
			title:   "Kitchen window catch broken",
			daysAgo: 21, closedAfter: 1,
			priority: domain.PriorityLow, category: "general", assignee: staff.ID,
			status: []string{domain.StatusAssigned, domain.StatusResolved, domain.StatusClosed},
			costs: []domain.CostEntry{
				{Kind: domain.CostMaterial, Description: "Espagnolette handle", AmountMinor: 18500},
			},
			minutes: []int64{25},
		},
		{
			building: riverside.ID, unitLabel: "Common",
			title:   "Garage door remote range poor",
			daysAgo: 17, closedAfter: 4,
			priority: domain.PriorityNormal, category: "general", assignee: electrician.ID,
			status: []string{domain.StatusTriaged, domain.StatusAssigned, domain.StatusResolved, domain.StatusClosed},
			costs: []domain.CostEntry{
				{Kind: domain.CostMaterial, Description: "External aerial kit", AmountMinor: 39000},
			},
			minutes: []int64{50},
		},
		{
			building: riverside.ID, unitLabel: "Flat 12",
			title:   "Smoke alarm chirping",
			daysAgo: 13, closedAfter: 1,
			priority: domain.PriorityHigh, category: "compliance", assignee: staff.ID,
			status: []string{domain.StatusAssigned, domain.StatusResolved, domain.StatusClosed},
			costs: []domain.CostEntry{
				{Kind: domain.CostMaterial, Description: "10-year sealed alarm", AmountMinor: 22000},
			},
			minutes: []int64{20},
		},
		{
			building: harbour.ID, unitLabel: "Unit 7",
			title:    "Bathroom silicone mouldy",
			daysAgo:  8,
			desc:     "Tenant asked at the last inspection; not urgent.",
			priority: domain.PriorityLow, category: "damp",
			status: []string{domain.StatusTriaged},
		},
		{
			building: oakmead.ID, unitLabel: "Shop 2",
			title:    "Roller shutter jamming",
			daysAgo:  4,
			desc:     "Tenant can close it but it needs two people.",
			priority: domain.PriorityHigh, category: "general", assignee: staff.ID,
			status:  []string{domain.StatusTriaged, domain.StatusAssigned},
			minutes: []int64{35},
		},
	}

	for _, spec := range specs {
		j, err := r.CreateJob(o, domain.Job{
			BuildingID: spec.building, Title: spec.title, Description: spec.desc,
			Priority: spec.priority, Category: spec.category, ReporterID: tenant.ID,
		}, spec.unitLabel)
		if err != nil {
			return creds, fmt.Errorf("job %q: %w", spec.title, err)
		}
		if spec.assignee != "" {
			if _, err := r.AssignJob(o, j.ID, spec.assignee); err != nil {
				return creds, err
			}
		}
		for _, st := range spec.status {
			if _, err := r.SetJobStatus(o, j.ID, st, staff.ID, ""); err != nil {
				return creds, fmt.Errorf("job %q → %s: %w", spec.title, st, err)
			}
		}
		for _, c := range spec.costs {
			c.JobID = j.ID
			if _, err := r.AddCost(o, c); err != nil {
				return creds, err
			}
		}
		for _, m := range spec.minutes {
			if _, err := r.AddTime(o, domain.TimeEntry{
				JobID: j.ID, Minutes: m, Note: "on site", PartyID: spec.assignee,
			}); err != nil {
				return creds, err
			}
		}
		// One tenant-visible update per job with an assignee, so the
		// visibility split (§4.3) is visible in the demo rather than described.
		if spec.assignee != "" {
			if _, err := r.AddEvent(o, domain.JobEvent{
				JobID: j.ID, Kind: "note", ActorID: staff.ID,
				Visibility: domain.VisibilityPublic,
				Body:       "A contractor has been assigned and will contact you to arrange access.",
			}); err != nil {
				return creds, err
			}
			if _, err := r.AddEvent(o, domain.JobEvent{
				JobID: j.ID, Kind: "note", ActorID: staff.ID,
				Visibility: domain.VisibilityInternal,
				Body:       "Recharge to owner if the cause is tenant damage — confirm at sign-off.",
			}); err != nil {
				return creds, err
			}
		}
		if err := backdateJob(r, o, j.ID, spec.daysAgo, spec.closedAfter); err != nil {
			return creds, fmt.Errorf("backdate %q: %w", spec.title, err)
		}
	}

	// An inspection template, and the ingoing/outgoing pair that uses it.
	template, err := r.CreateTemplate(o, domain.InspectionTemplate{
		Name: "Residential move-in / move-out", Kind: "tenancy",
		Items: []domain.TemplateItem{
			{Section: "Kitchen", Label: "Worktops and cupboards", Sort: 1},
			{Section: "Kitchen", Label: "Sink and taps", Sort: 2},
			{Section: "Kitchen", Label: "Oven and hob", Sort: 3},
			{Section: "Bathroom", Label: "Bath / shower and screen", Sort: 4},
			{Section: "Bathroom", Label: "Toilet and cistern", Sort: 5},
			{Section: "Living", Label: "Walls and ceiling", Sort: 6},
			{Section: "Living", Label: "Flooring", Sort: 7},
			{Section: "Living", Label: "Windows and blinds", Sort: 8},
			{Section: "General", Label: "Keys returned", Sort: 9},
		},
	})
	if err != nil {
		return creds, err
	}

	unit3A, err := r.EnsureUnit(o, riverside.ID, "Flat 3A")
	if err != nil {
		return creds, err
	}

	ingoing, err := r.CreateInspection(o, domain.Inspection{
		BuildingID: riverside.ID, UnitID: unit3A.ID, TemplateID: template.ID,
		Kind: domain.InspectionIngoing, InspectorID: staff.ID,
		ScheduledFor: time.Now().AddDate(-1, 0, 0).UTC().Format(time.RFC3339),
		Notes:        "Move-in walkthrough with tenant present.",
	}, "")
	if err != nil {
		return creds, err
	}
	// Ingoing: everything sound apart from fair wear on the flooring.
	ingoingConditions := []string{
		domain.ConditionOK, domain.ConditionOK, domain.ConditionOK,
		domain.ConditionOK, domain.ConditionOK,
		domain.ConditionOK, domain.ConditionWear, domain.ConditionOK,
		domain.ConditionOK,
	}
	for i, item := range template.Items {
		if _, err := r.AddFinding(o, domain.Finding{
			InspectionID: ingoing.ID, ItemID: item.ID,
			Condition: ingoingConditions[i], Comment: "Recorded at move-in.",
		}); err != nil {
			return creds, err
		}
	}
	if _, err := r.SetInspectionStatus(o, ingoing.ID, domain.InspectionComplete); err != nil {
		return creds, err
	}

	outgoing, err := r.CreateInspection(o, domain.Inspection{
		BuildingID: riverside.ID, UnitID: unit3A.ID, TemplateID: template.ID,
		Kind: domain.InspectionOutgoing, InspectorID: staff.ID,
		ScheduledFor: time.Now().UTC().Format(time.RFC3339),
		Notes:        "Move-out walkthrough. Two items deteriorated against the ingoing record.",
	}, "")
	if err != nil {
		return creds, err
	}
	// Outgoing: the hob is damaged and a blind is missing — the two items a
	// deposit deduction would rest on, and the reason the pair is worth
	// keeping.
	outgoingConditions := []string{
		domain.ConditionOK, domain.ConditionWear, domain.ConditionDamage,
		domain.ConditionOK, domain.ConditionOK,
		domain.ConditionWear, domain.ConditionWear, domain.ConditionMissing,
		domain.ConditionOK,
	}
	outgoingComments := []string{
		"", "Limescale on the mixer.", "Cracked ceramic on the front-left plate.",
		"", "", "Scuffing behind the sofa.", "Unchanged since move-in.",
		"Bedroom blind missing.", "All keys returned.",
	}
	for i, item := range template.Items {
		if _, err := r.AddFinding(o, domain.Finding{
			InspectionID: outgoing.ID, ItemID: item.ID,
			Condition: outgoingConditions[i], Comment: outgoingComments[i],
		}); err != nil {
			return creds, err
		}
	}
	if _, err := r.SetInspectionStatus(o, outgoing.ID, domain.InspectionActive); err != nil {
		return creds, err
	}

	// A routine inspection still to be walked, so the demo has a scheduled one.
	if _, err := r.CreateInspection(o, domain.Inspection{
		BuildingID:   harbour.ID,
		Kind:         domain.InspectionRoutine,
		InspectorID:  staff.ID,
		ScheduledFor: time.Now().AddDate(0, 0, 9).UTC().Format(time.RFC3339),
		Notes:        "Quarterly common-property walk.",
	}, ""); err != nil {
		return creds, err
	}

	return creds, nil
}
