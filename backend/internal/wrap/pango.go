package wrap

// The trades/v0 binding: mapping a Pango job onto a WRAP WorkOrder and
// back (docs/WRAP.md §2, §4), and the one authorship rule this package
// enforces outside of Decode — an Assignment MUST be authored by the
// WorkOrder's own issuer (WRAP 02-objects.md §3.6, 04-signing.md §5.5).
//
// docs/WRAP.md's role table:
//
//	Pango              WRAP
//	Managing agent       Issuer
//	In-house staff       Performer via direct offer (mode = 0)
//	External contractor  Performer, possibly via a pool
//	Tenant                Beneficiary — no key required
//	Job                   WorkOrder (profile: "trades/v0")
//	Quote                 Bid carrying a Quote
//	Job events             Progress
//	Sign-off               Attestation

import (
	"bytes"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
)

// ProfileTrades is the WRAP profile identifier Pango work orders carry
// (WRAP 11-profiles.md §12.3).
const ProfileTrades = "trades/v0"

// trades/v0 body field keys (WRAP 11-profiles.md §12.3).
const (
	bodyTrade     uint64 = 32
	bodyLicence   uint64 = 33
	bodyVisit     uint64 = 34
	bodyMaterials uint64 = 35
	bodyAccess    uint64 = 36
)

// Visit reasons (WRAP 11-profiles.md §12.3): a callout is not one event.
const (
	VisitQuotation uint64 = 0
	VisitWork      uint64 = 1
	VisitFollowUp  uint64 = 2
)

// JobToWorkOrderOptions carries the fields Pango's domain.Job has no place
// to keep (WRAP requires an expiry with no default; Pango jobs do not
// track a bidding/scheduling window or a licence requirement) so a caller
// supplies them explicitly rather than this package inventing a silent
// default that would make every Pango work order expire at the same
// arbitrary instant.
type JobToWorkOrderOptions struct {
	TS      string // HLC stamp, MUST be set — WRAP 02-objects.md §3.2 key 5.
	Expires uint64 // unix seconds, MUST be set — WRAP 02-objects.md §3.3
	Window  *Window
	Licence string
	Visit   uint64 // defaults to VisitWork if left zero
	Access  string
}

// JobToWorkOrder maps a Pango job, its building and (optionally) its unit
// onto a `trades/v0` WorkOrder (docs/WRAP.md §4). The job's own id and
// building id travel in `refs` so WorkOrderToJob can recover them, and so an
// issuer can correlate an incoming Bid or Progress back to its own record —
// WRAP objects are communication, never app-state replication (docs/WRAP.md
// §6), so this is metadata, not a way to smuggle the oplog across the trust
// boundary.
func JobToWorkOrder(job domain.Job, building domain.Building, unitLabel string, issuer [32]byte, opts JobToWorkOrderOptions) *Object {
	place := Place{
		Role:   "site",
		Lat:    building.Lat,
		Lon:    building.Lon,
		Label:  building.Address,
		Detail: unitLabel,
	}
	visit := opts.Visit
	if visit == 0 && opts.Visit != VisitQuotation {
		visit = VisitWork
	}
	body := M{bodyVisit: visit}
	if job.Category != "" {
		body[bodyTrade] = job.Category
	}
	if opts.Licence != "" {
		body[bodyLicence] = opts.Licence
	}
	if opts.Access != "" {
		body[bodyAccess] = opts.Access
	}

	wo := WorkOrder{
		Profile: ProfileTrades,
		Title:   job.Title,
		Detail:  job.Description,
		Places:  []Place{place},
		Window:  opts.Window,
		Expires: opts.Expires,
		Refs: map[string]string{
			"pango_job_id":      job.ID,
			"pango_building_id": job.BuildingID,
			"pango_org_id":      job.OrgID,
		},
		Body: body,
	}
	if wo.Window == nil {
		wo.Window = &Window{Kind: WindowScheduled}
	}
	return wo.ToObject(issuer, opts.TS)
}

// JobFromWorkOrder is the inverse: given a signed, verified WorkOrder object
// (typically one a performer's own node received as an Offer), it builds the
// domain.Job fields that WorkOrder actually carries. BuildingID, UnitID,
// OrgID and Number are deliberately NOT populated — those are the
// performer's own local concerns (a job number is allocated by whichever
// building owns it, ARCHITECTURE.md §5, and a work order from another
// organisation names no building the performer necessarily has a row for).
// The originating job id (if the WorkOrder was minted by JobToWorkOrder)
// survives in Refs for correlation.
func JobFromWorkOrder(o *Object) (domain.Job, WorkOrder, error) {
	if o.Kind != KindWorkOrder {
		return domain.Job{}, WorkOrder{}, fmt.Errorf("wrap: JobFromWorkOrder: object is kind %s, not WorkOrder", o.Kind)
	}
	wo, err := WorkOrderFrom(o)
	if err != nil {
		return domain.Job{}, WorkOrder{}, err
	}
	j := domain.Job{
		Title:       wo.Title,
		Description: wo.Detail,
		Status:      domain.StatusReported,
		Priority:    domain.PriorityNormal,
	}
	if trade, ok := wo.Body[bodyTrade]; ok {
		if s, ok := trade.(string); ok {
			j.Category = s
		}
	}
	if wo.Refs != nil {
		if id := wo.Refs["pango_job_id"]; id != "" {
			j.ID = id
		}
	}
	return j, wo, nil
}

// Refs keys JobToWorkOrder writes, exported so a caller can look them up
// without hard-coding the strings.
const (
	RefJobID      = "pango_job_id"
	RefBuildingID = "pango_building_id"
	RefOrgID      = "pango_org_id"
)

// VerifyAssignmentAuthor enforces WRAP's one hard authorship rule (§3.6,
// §5.5): an Assignment is valid only when its author is the WorkOrder's own
// author. assignment and workOrder must already be signature-verified
// (i.e. the product of Decode, not raw input) — this function checks
// authorization, not authenticity.
//
// This is intentionally a rejection, not a ranking: an Assignment that fails
// this check MUST NOT enter local state at all (§5.5), because otherwise a
// performer could assign work to themselves.
func VerifyAssignmentAuthor(assignment, workOrder *Object) error {
	if assignment.Kind != KindAssignment {
		return fmt.Errorf("wrap: VerifyAssignmentAuthor: object is kind %s, not Assignment", assignment.Kind)
	}
	if workOrder.Kind != KindWorkOrder {
		return fmt.Errorf("wrap: VerifyAssignmentAuthor: referent is kind %s, not WorkOrder", workOrder.Kind)
	}
	if !bytes.Equal(assignment.Author[:], workOrder.Author[:]) {
		return ErrNotIssuer
	}
	return nil
}

// VerifyAssignmentOrder additionally checks the Assignment actually refers to
// this WorkOrder (its `order` field equals the WorkOrder's id) — a
// prerequisite for VerifyAssignmentAuthor to mean anything, since otherwise
// an attacker could pair a validly-self-authored Assignment for WorkOrder X
// with an unrelated WorkOrder Y that happens to share an issuer.
func VerifyAssignmentOrder(assignment *Object, workOrder *Object) error {
	a, err := AssignmentFrom(assignment)
	if err != nil {
		return err
	}
	if !bytes.Equal(a.Order, workOrder.ID) {
		return fmt.Errorf("wrap: assignment refers to a different work order")
	}
	return nil
}
