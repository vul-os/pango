// Package inspect holds the ingoing/outgoing comparison engine — the
// differentiator described in docs/INSPECTIONS.md §1 and §5.
//
// It is deliberately pure: every function here takes already-fetched domain
// values and returns a result, with no *sql.DB anywhere in the package. The
// repo layer (§9 of ARCHITECTURE.md) owns persistence and org scoping; this
// package owns only the comparison rule, so that rule can be unit tested
// without a database and reused unchanged if the API ever needs to compare
// two inspections it fetched some other way (a report export, a WRAP
// attestation).
package inspect

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/vul-os/pango/backend/internal/domain"
)

// ErrMismatch is returned when the two inspections handed to Compare are not a
// valid ingoing/outgoing pair.
var ErrMismatch = errors.New("inspections are not a comparable ingoing/outgoing pair")

// Outcome is the per-item comparison result. There are five, deliberately —
// see docs/INSPECTIONS.md §5. Collapsing "not captured ingoing" into
// "deteriorated" (by treating a missing baseline as if it were a pristine one,
// or vice versa) would launder a missing record into evidence of damage, which
// is exactly the failure this feature exists to prevent.
type Outcome string

const (
	OutcomeUnchanged           Outcome = "unchanged"
	OutcomeDeteriorated        Outcome = "deteriorated"
	OutcomeImproved            Outcome = "improved"
	OutcomeNotCapturedIngoing  Outcome = "not_captured_ingoing"
	OutcomeNotCapturedOutgoing Outcome = "not_captured_outgoing"
)

// ItemComparison is one line of the comparison: an item, its condition at
// move-in, its condition at move-out, and the delta between them.
type ItemComparison struct {
	Section string `json:"section"`
	Label   string `json:"label"`

	IngoingCaptured  bool   `json:"ingoing_captured"`
	IngoingCondition string `json:"ingoing_condition,omitempty"`
	IngoingComment   string `json:"ingoing_comment,omitempty"`
	IngoingPhotoRefs string `json:"ingoing_photo_refs,omitempty"`
	IngoingFindingID string `json:"ingoing_finding_id,omitempty"`

	OutgoingCaptured  bool   `json:"outgoing_captured"`
	OutgoingCondition string `json:"outgoing_condition,omitempty"`
	OutgoingComment   string `json:"outgoing_comment,omitempty"`
	OutgoingPhotoRefs string `json:"outgoing_photo_refs,omitempty"`
	OutgoingFindingID string `json:"outgoing_finding_id,omitempty"`

	Outcome Outcome `json:"outcome"`
}

// Comparison is the full result for a unit's ingoing/outgoing pair.
type Comparison struct {
	UnitID           string           `json:"unit_id"`
	IngoingID        string           `json:"ingoing_inspection_id"`
	OutgoingID       string           `json:"outgoing_inspection_id"`
	Items            []ItemComparison `json:"items"`
	DeterioratedItem []ItemComparison `json:"deteriorated_items"`
	Counts           map[Outcome]int  `json:"counts"`
}

// key identifies "the same physical thing" across two inspections that may
// have run against different templates.
//
// ── Template drift rule ──────────────────────────────────────────────────
// Items are paired by their normalized (section, label) text, never by
// template-item id. Ids are stable only within one template row; if the
// template was edited or replaced between the ingoing and the outgoing walk
// (INSPECTIONS.md §3 flags this as unversioned and open), the item ids on the
// two sides belong to two different rows and comparing by id would either
// crash (id absent on one side) or — worse — silently pair two unrelated rows
// that happen to reuse a numeric offset. Text is the actual claim being
// compared ("this is the kitchen counter, both times"), and pairing on it
// degrades gracefully: an item whose label changed shows up as not captured
// on one side rather than being matched to the wrong item. A finding with no
// template item (freeform) has no other stable identity, so it uses the same
// key derived straight from its own label.
func key(section, label string) string {
	return strings.ToLower(strings.TrimSpace(section)) + "\x1f" + strings.ToLower(strings.TrimSpace(label))
}

// resolve turns a finding into (section, label, key) using the item lookup
// when the finding names a template item, falling back to the finding's own
// label when it does not (or when the item it named has since been removed
// from the fetched template — a tombstoned item is not in the lookup).
func resolve(f domain.Finding, items map[string]domain.TemplateItem) (section, label string) {
	if f.ItemID != "" {
		if it, ok := items[f.ItemID]; ok {
			return it.Section, it.Label
		}
	}
	return "", f.Label
}

// Compare pairs the latest finding per item across an ingoing and an outgoing
// inspection of the same unit and classifies each pair.
//
// findings must already be latest-per-item (repo.LatestFindings) — Compare
// does not itself collapse revisions, so that its behaviour on the append-only
// question stays visibly the caller's responsibility rather than a hidden
// assumption inside the algorithm.
func Compare(
	ingoing, outgoing domain.Inspection,
	ingoingItems, outgoingItems []domain.TemplateItem,
	ingoingFindings, outgoingFindings []domain.Finding,
) (Comparison, error) {
	if ingoing.Kind != domain.InspectionIngoing {
		return Comparison{}, fmt.Errorf("%w: first inspection is not ingoing", ErrMismatch)
	}
	if outgoing.Kind != domain.InspectionOutgoing {
		return Comparison{}, fmt.Errorf("%w: second inspection is not outgoing", ErrMismatch)
	}
	if ingoing.UnitID == "" || ingoing.UnitID != outgoing.UnitID {
		return Comparison{}, fmt.Errorf("%w: inspections are not of the same unit", ErrMismatch)
	}

	items := make(map[string]domain.TemplateItem, len(ingoingItems)+len(outgoingItems))
	for _, it := range ingoingItems {
		items[it.ID] = it
	}
	for _, it := range outgoingItems {
		items[it.ID] = it
	}

	type sortKey struct {
		section string
		sort    int64
		label   string
	}
	byKey := map[string]*ItemComparison{}
	order := map[string]sortKey{}

	place := func(section, label string, sortHint int64) *ItemComparison {
		k := key(section, label)
		ic, ok := byKey[k]
		if !ok {
			ic = &ItemComparison{Section: section, Label: label}
			byKey[k] = ic
			order[k] = sortKey{section: strings.ToLower(section), sort: sortHint, label: strings.ToLower(label)}
		}
		return ic
	}

	// Seed sort order from the template items, in template order, so the
	// output lines up the way the checklist does (INSPECTIONS.md §3 — items
	// are grouped by area precisely so a comparison can rely on that order).
	for _, it := range ingoingItems {
		place(it.Section, it.Label, it.Sort)
	}
	for _, it := range outgoingItems {
		place(it.Section, it.Label, it.Sort)
	}

	for _, f := range ingoingFindings {
		section, label := resolve(f, items)
		ic := place(section, label, 0)
		ic.IngoingCaptured = true
		ic.IngoingCondition = f.Condition
		ic.IngoingComment = f.Comment
		ic.IngoingPhotoRefs = f.PhotoRefs
		ic.IngoingFindingID = f.ID
	}
	for _, f := range outgoingFindings {
		section, label := resolve(f, items)
		ic := place(section, label, 0)
		ic.OutgoingCaptured = true
		ic.OutgoingCondition = f.Condition
		ic.OutgoingComment = f.Comment
		ic.OutgoingPhotoRefs = f.PhotoRefs
		ic.OutgoingFindingID = f.ID
	}

	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := order[keys[i]], order[keys[j]]
		if a.section != b.section {
			return a.section < b.section
		}
		if a.sort != b.sort {
			return a.sort < b.sort
		}
		return a.label < b.label
	})

	counts := map[Outcome]int{}
	out := make([]ItemComparison, 0, len(keys))
	var deteriorated []ItemComparison
	for _, k := range keys {
		ic := *byKey[k]
		// An item nobody captured on either side is not a comparison result —
		// it is an unused checklist line, and surfacing it would bury the
		// findings that actually matter under empty rows.
		if !ic.IngoingCaptured && !ic.OutgoingCaptured {
			continue
		}
		ic.Outcome = classify(ic)
		counts[ic.Outcome]++
		out = append(out, ic)
		if ic.Outcome == OutcomeDeteriorated {
			deteriorated = append(deteriorated, ic)
		}
	}

	return Comparison{
		UnitID:           ingoing.UnitID,
		IngoingID:        ingoing.ID,
		OutgoingID:       outgoing.ID,
		Items:            out,
		DeterioratedItem: deteriorated,
		Counts:           counts,
	}, nil
}

// classify decides one item's outcome. See the type comment on Outcome for
// why the missing-baseline cases are distinct outcomes rather than folded
// into "deteriorated" or "unchanged".
func classify(ic ItemComparison) Outcome {
	switch {
	case ic.IngoingCaptured && !ic.OutgoingCaptured:
		return OutcomeNotCapturedOutgoing
	case !ic.IngoingCaptured && ic.OutgoingCaptured:
		return OutcomeNotCapturedIngoing
	}
	// Both sides captured.
	if ic.IngoingCondition == ic.OutgoingCondition {
		return OutcomeUnchanged
	}
	worse, comparable := domain.Deteriorated(ic.IngoingCondition, ic.OutgoingCondition)
	if !comparable {
		// One side is "not applicable" or an unrecognised value and the other
		// is not: there is no direction to report. Guessing a direction here
		// would be exactly the overconfident claim §5 of the docs warns
		// against, so the conservative read — no asserted change — wins.
		return OutcomeUnchanged
	}
	if worse {
		return OutcomeDeteriorated
	}
	return OutcomeImproved
}
