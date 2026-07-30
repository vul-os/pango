// Package domain holds Pango's entities and the invariants that make them
// meaningful. It imports no SQL driver, no HTTP package and nothing from
// repo/, api/ or store/ (§9): dependencies point inward.
//
// That constraint is load-bearing rather than aesthetic. The rules in this
// package — a job cannot close with no work recorded against it, a cost entry
// cannot be zero, an outgoing inspection needs an ingoing one to compare
// against — have to hold identically whether a write arrives from the HTTP API,
// from a sync round, or from a WRAP work order. Keeping them out of the
// handlers is what stops the second and third of those paths from growing their
// own slightly different version of the rules.
package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ── organisation, users, parties ────────────────────────────────────────────

// Organisation is the tenancy boundary. Every replicated row carries its id
// (§4.2), and that id is written from the authenticated session, never from a
// request body (§11).
type Organisation struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HLC       string `json:"hlc"`
	Deleted   bool   `json:"deleted"`
	CreatedAt string `json:"created_at"`
}

// User is a local operator account. It is not replicated: see the comment on
// app_user in migration 1_core.sql.
type User struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	Email     string `json:"email"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
	// PasswordHash is deliberately absent from this struct. The type that
	// crosses the API boundary cannot leak a hash it does not carry.
}

// Party kinds.
const (
	PartyStaff      = "staff"
	PartyContractor = "contractor"
	PartyTenant     = "tenant"
)

// Party is a person: staff, contractor or tenant. One table for all three,
// because the same human is often two of them and splitting them fragments job
// history.
//
// A tenant is a participant, not an account (§4.3): no key, no login, no
// install. They report a leak and are told it is fixed, and that is the whole
// interaction the product asks of them.
type Party struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	PubKey    string `json:"pubkey"` // optional Ed25519 key, hex
	HLC       string `json:"hlc"`
	Deleted   bool   `json:"deleted"`
	CreatedAt string `json:"created_at"`
}

// Validate checks a party is usable.
func (p Party) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("party name is required")
	}
	switch p.Kind {
	case PartyStaff, PartyContractor, PartyTenant:
	default:
		return fmt.Errorf("unknown party kind %q", p.Kind)
	}
	return nil
}

// Peer is an enrolled sync peer. Discovery is manual (§7).
type Peer struct {
	ID         string `json:"id"`
	OrgID      string `json:"org_id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	PubKey     string `json:"pubkey"`
	Enabled    bool   `json:"enabled"`
	LastSyncAt string `json:"last_sync_at"`
	LastStatus string `json:"last_status"`
	CreatedAt  string `json:"created_at"`
}

// ── property ────────────────────────────────────────────────────────────────

// Building is the unit of authority (§5). Its owning organisation is the single
// writer for its jobs, its job number sequence and its inspection scheduling —
// which is precisely why this system has no consensus protocol, no leader
// election and no distributed lock.
type Building struct {
	ID         string   `json:"id"`
	OrgID      string   `json:"org_id"`
	Name       string   `json:"name"`
	Address    string   `json:"address"`
	Lat        *float64 `json:"lat"`
	Lon        *float64 `json:"lon"`
	UnitScheme string   `json:"unit_scheme"`
	HLC        string   `json:"hlc"`
	Deleted    bool     `json:"deleted"`
	CreatedAt  string   `json:"created_at"`
}

// Validate checks a building is usable.
//
// Lat/lon are pointers so "not surveyed" is distinguishable from the Gulf of
// Guinea. A zero-value coordinate would sort a building with no location to the
// top of every proximity ranking, which is the most visible place for a missing
// value to hide.
func (b Building) Validate() error {
	if strings.TrimSpace(b.Name) == "" {
		return errors.New("building name is required")
	}
	if b.Lat != nil && (*b.Lat < -90 || *b.Lat > 90) {
		return errors.New("latitude out of range")
	}
	if b.Lon != nil && (*b.Lon < -180 || *b.Lon > 180) {
		return errors.New("longitude out of range")
	}
	switch b.UnitScheme {
	case SchemeDefault, SchemeMixedUse, SchemeVerbatim:
	default:
		return fmt.Errorf("unknown unit scheme %q", b.UnitScheme)
	}
	return nil
}

// ── jobs ────────────────────────────────────────────────────────────────────

// Job statuses.
const (
	StatusReported   = "reported"
	StatusTriaged    = "triaged"
	StatusAssigned   = "assigned"
	StatusInProgress = "in_progress"
	StatusOnHold     = "on_hold"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
	StatusCancelled  = "cancelled"
)

// Job priorities.
const (
	PriorityLow       = "low"
	PriorityNormal    = "normal"
	PriorityHigh      = "high"
	PriorityEmergency = "emergency"
)

// jobTransitions is the allowed status graph.
//
// It is permissive on purpose. Maintenance work does not proceed in a straight
// line — a job goes back on hold because a part is out of stock, gets reopened
// because the leak came back, is reassigned when a contractor does not turn up.
// A strict workflow would be worked around by staff picking whatever status the
// software would accept, which destroys the reporting that the status field
// exists to feed. What is forbidden is only what is meaningless: leaving a
// terminal state by any route other than an explicit reopen.
var jobTransitions = map[string][]string{
	StatusReported:   {StatusTriaged, StatusAssigned, StatusInProgress, StatusOnHold, StatusCancelled},
	StatusTriaged:    {StatusAssigned, StatusInProgress, StatusOnHold, StatusCancelled},
	StatusAssigned:   {StatusInProgress, StatusOnHold, StatusTriaged, StatusResolved, StatusCancelled},
	StatusInProgress: {StatusOnHold, StatusResolved, StatusAssigned, StatusCancelled},
	StatusOnHold:     {StatusInProgress, StatusAssigned, StatusTriaged, StatusCancelled},
	StatusResolved:   {StatusClosed, StatusInProgress}, // reopen if it comes back
	StatusClosed:     {StatusInProgress},               // explicit reopen
	StatusCancelled:  {StatusReported},                 // explicit reopen
}

// ValidStatus reports whether s is a known job status.
func ValidStatus(s string) bool {
	_, ok := jobTransitions[s]
	return ok
}

// IsOpen reports whether a status counts as open work. Closed and cancelled are
// the only terminal states; everything else is somebody's problem today.
func IsOpen(status string) bool {
	return status != StatusClosed && status != StatusCancelled
}

// CanTransition reports whether from → to is allowed. A no-op transition is
// allowed so an idempotent retry (a tablet resending a queued write after a
// reconnect) is not an error.
func CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	for _, allowed := range jobTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// Job is the work. It is owned by its building (§5).
//
// There is no Cost or Minutes field, and adding one would be a bug however
// convenient it looked. Totals are SUM() over the append-only ledgers at read
// time (§6).
type Job struct {
	ID          string `json:"id"`
	OrgID       string `json:"org_id"`
	BuildingID  string `json:"building_id"`
	UnitID      string `json:"unit_id"`
	Number      int64  `json:"number"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Category    string `json:"category"`
	AssigneeID  string `json:"assignee_party_id"`
	ReporterID  string `json:"reporter_party_id"`
	OpenedAt    string `json:"opened_at"`
	ClosedAt    string `json:"closed_at"`
	HLC         string `json:"hlc"`
	Deleted     bool   `json:"deleted"`
	CreatedAt   string `json:"created_at"`
}

// Validate checks a job is usable.
func (j Job) Validate() error {
	if strings.TrimSpace(j.Title) == "" {
		return errors.New("job title is required")
	}
	if j.BuildingID == "" {
		return errors.New("job must belong to a building")
	}
	if !ValidStatus(j.Status) {
		return fmt.Errorf("unknown job status %q", j.Status)
	}
	switch j.Priority {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityEmergency:
	default:
		return fmt.Errorf("unknown priority %q", j.Priority)
	}
	return nil
}

// Event visibility. One thread serves internal notes and tenant communication,
// gated by this flag (§4.3).
const (
	VisibilityInternal = "internal"
	VisibilityPublic   = "public"
)

// JobEvent is append-only.
type JobEvent struct {
	ID         string `json:"id"`
	OrgID      string `json:"org_id"`
	JobID      string `json:"job_id"`
	Kind       string `json:"kind"`
	Body       string `json:"body"`
	ActorID    string `json:"actor_party_id"`
	Visibility string `json:"visibility"`
	HLC        string `json:"hlc"`
	CreatedAt  string `json:"created_at"`
}

// Validate checks an event is usable.
//
// An unknown visibility is rejected rather than defaulted, because the two
// possible defaults are both wrong: defaulting to public leaks internal notes
// to a tenant, and defaulting to internal silently hides an update somebody
// meant to send.
func (e JobEvent) Validate() error {
	if e.JobID == "" {
		return errors.New("event must belong to a job")
	}
	if strings.TrimSpace(e.Kind) == "" {
		return errors.New("event kind is required")
	}
	switch e.Visibility {
	case VisibilityInternal, VisibilityPublic:
	default:
		return fmt.Errorf("unknown visibility %q", e.Visibility)
	}
	return nil
}

// Cost kinds.
const (
	CostLabour     = "labour"
	CostMaterial   = "material"
	CostCallout    = "callout"
	CostContractor = "contractor"
	CostOther      = "other"
)

// CostEntry is immutable and insert-only (§6). A correction is a new entry with
// a negative amount.
type CostEntry struct {
	ID          string `json:"id"`
	OrgID       string `json:"org_id"`
	JobID       string `json:"job_id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	AmountMinor Money  `json:"amount_minor"`
	Currency    string `json:"currency"`
	PartyID     string `json:"party_id"`
	HLC         string `json:"hlc"`
	CreatedAt   string `json:"created_at"`
}

// Validate checks a cost entry is usable.
//
// Zero is rejected. A zero-amount entry is either a mistake or an attempt to
// use the ledger as a comment field, and both make the audit trail harder to
// read for no gain — job_event is the comment field.
func (c CostEntry) Validate() error {
	if c.JobID == "" {
		return errors.New("cost entry must belong to a job")
	}
	if c.AmountMinor == 0 {
		return errors.New("cost amount cannot be zero")
	}
	switch c.Kind {
	case CostLabour, CostMaterial, CostCallout, CostContractor, CostOther:
	default:
		return fmt.Errorf("unknown cost kind %q", c.Kind)
	}
	if len(c.Currency) != 3 {
		return errors.New("currency must be a 3-letter code")
	}
	return nil
}

// TimeEntry is immutable and insert-only (§6). Minutes, not hours: hours invite
// a float, and 1.75 hours has the same representation problem as R17.50.
type TimeEntry struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	JobID     string `json:"job_id"`
	Minutes   int64  `json:"minutes"`
	Note      string `json:"note"`
	PartyID   string `json:"party_id"`
	HLC       string `json:"hlc"`
	CreatedAt string `json:"created_at"`
}

// Validate checks a time entry is usable. Negative minutes are legal for the
// same reason negative amounts are: a correction is an entry, not an edit.
func (t TimeEntry) Validate() error {
	if t.JobID == "" {
		return errors.New("time entry must belong to a job")
	}
	if t.Minutes == 0 {
		return errors.New("time minutes cannot be zero")
	}
	return nil
}

// ── inspections ─────────────────────────────────────────────────────────────

// Inspection kinds. The ingoing/outgoing pair is the differentiator (§1): it is
// what turns a move-out damage argument into an evidence comparison.
const (
	InspectionIngoing  = "ingoing"
	InspectionOutgoing = "outgoing"
	InspectionRoutine  = "routine"
	InspectionSnag     = "snag"
	// InspectionPeriodic is a scheduled condition check outside a tenancy
	// change — a body corporate's quarterly walk, a landlord's annual visit.
	// It compares like a routine inspection: no counterpart it must be paired
	// against, so no unit requirement, though the API resolves one when a
	// caller supplies a unit label.
	InspectionPeriodic = "periodic"
)

// Inspection statuses.
const (
	InspectionScheduled = "scheduled"
	InspectionActive    = "in_progress"
	InspectionComplete  = "complete"
)

// Inspection is linked to a building and, for unit-level inspections, a unit
// (§4.2).
type Inspection struct {
	ID         string `json:"id"`
	OrgID      string `json:"org_id"`
	BuildingID string `json:"building_id"`
	UnitID     string `json:"unit_id"`
	TemplateID string `json:"template_id"`
	// JobID optionally links an inspection to the job it verifies or the job
	// it was raised from — e.g. an inspection scheduled to confirm a repair,
	// or a job raised against an item a walk found deteriorated (§6 of
	// INSPECTIONS.md). Empty for a standalone inspection, which is the common
	// case.
	JobID        string `json:"job_id"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	ScheduledFor string `json:"scheduled_for"`
	PerformedAt  string `json:"performed_at"`
	InspectorID  string `json:"inspector_party_id"`
	Notes        string `json:"notes"`
	HLC          string `json:"hlc"`
	Deleted      bool   `json:"deleted"`
	CreatedAt    string `json:"created_at"`
}

// Validate checks an inspection is usable.
//
// An ingoing or outgoing inspection MUST name a unit. Those two kinds exist
// only to be compared against each other for a specific tenancy; one recorded
// against the building as a whole can never be paired, so it is evidence of
// nothing and would surface as a mysteriously empty comparison months later,
// when the tenancy is already in dispute.
func (i Inspection) Validate() error {
	if i.BuildingID == "" {
		return errors.New("inspection must belong to a building")
	}
	switch i.Kind {
	case InspectionIngoing, InspectionOutgoing:
		if i.UnitID == "" {
			return fmt.Errorf("%s inspection must name a unit", i.Kind)
		}
	case InspectionRoutine, InspectionSnag, InspectionPeriodic:
	default:
		return fmt.Errorf("unknown inspection kind %q", i.Kind)
	}
	switch i.Status {
	case InspectionScheduled, InspectionActive, InspectionComplete:
	default:
		return fmt.Errorf("unknown inspection status %q", i.Status)
	}
	return nil
}

// InspectionTemplate is a reusable checklist.
type InspectionTemplate struct {
	ID        string         `json:"id"`
	OrgID     string         `json:"org_id"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Items     []TemplateItem `json:"items,omitempty"`
	HLC       string         `json:"hlc"`
	Deleted   bool           `json:"deleted"`
	CreatedAt string         `json:"created_at"`
}

// TemplateItem is one line of a checklist.
type TemplateItem struct {
	ID         string `json:"id"`
	OrgID      string `json:"org_id"`
	TemplateID string `json:"template_id"`
	Section    string `json:"section"`
	Label      string `json:"label"`
	Sort       int64  `json:"sort"`
	HLC        string `json:"hlc"`
	Deleted    bool   `json:"deleted"`
	CreatedAt  string `json:"created_at"`
}

// Finding conditions, ordered from best to worst so a comparison can say
// whether a unit got worse during a tenancy.
const (
	ConditionOK      = "ok"
	ConditionWear    = "wear"
	ConditionDamage  = "damage"
	ConditionMissing = "missing"
	ConditionNA      = "na"
)

// conditionRank orders conditions for deterioration comparison. NA is
// unranked: "not applicable" is not a point on the scale, and treating it as
// one would report a fitting that was removed between inspections as either
// pristine or destroyed depending on which end of the scale it was pinned to.
var conditionRank = map[string]int{
	ConditionOK:      0,
	ConditionWear:    1,
	ConditionDamage:  2,
	ConditionMissing: 3,
}

// Deteriorated reports whether to is a worse condition than from, and whether
// the pair is comparable at all.
func Deteriorated(from, to string) (worse bool, comparable bool) {
	a, okA := conditionRank[from]
	b, okB := conditionRank[to]
	if !okA || !okB {
		return false, false
	}
	return b > a, true
}

// Finding is append-only: it is the evidence in a deposit dispute, and evidence
// that can be edited after the fact is worth nothing.
type Finding struct {
	ID           string `json:"id"`
	OrgID        string `json:"org_id"`
	InspectionID string `json:"inspection_id"`
	ItemID       string `json:"item_id"`
	Label        string `json:"label"`
	Condition    string `json:"condition"`
	Comment      string `json:"comment"`
	PhotoRefs    string `json:"photo_refs"`
	HLC          string `json:"hlc"`
	CreatedAt    string `json:"created_at"`
}

// Validate checks a finding is usable.
func (f Finding) Validate() error {
	if f.InspectionID == "" {
		return errors.New("finding must belong to an inspection")
	}
	switch f.Condition {
	case ConditionOK, ConditionWear, ConditionDamage, ConditionMissing, ConditionNA:
	default:
		return fmt.Errorf("unknown condition %q", f.Condition)
	}
	if strings.TrimSpace(f.Label) == "" && f.ItemID == "" {
		return errors.New("finding needs a template item or a label")
	}
	return nil
}

// Attachment is a content-addressed blob reference.
type Attachment struct {
	ID        string `json:"id"`
	OrgID     string `json:"org_id"`
	SHA256    string `json:"sha256"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
	HLC       string `json:"hlc"`
	CreatedAt string `json:"created_at"`
}
