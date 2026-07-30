package inspect

// Compare is pure, so these tests build domain values by hand rather than
// going through a database — the point of keeping this package free of *sql.DB
// (see the package comment).

import (
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
)

func mkItem(id, section, label string, sort int64) domain.TemplateItem {
	return domain.TemplateItem{ID: id, Section: section, Label: label, Sort: sort}
}

func mkFinding(id, itemID, label, condition string) domain.Finding {
	return domain.Finding{ID: id, ItemID: itemID, Label: label, Condition: condition}
}

func TestCompareRejectsWrongKinds(t *testing.T) {
	ing := domain.Inspection{ID: "i1", Kind: domain.InspectionIngoing, UnitID: "u1"}
	out := domain.Inspection{ID: "o1", Kind: domain.InspectionOutgoing, UnitID: "u1"}

	if _, err := Compare(out, out, nil, nil, nil, nil); err == nil {
		t.Error("Compare accepted a non-ingoing first argument")
	}
	if _, err := Compare(ing, ing, nil, nil, nil, nil); err == nil {
		t.Error("Compare accepted a non-outgoing second argument")
	}
}

func TestCompareRejectsDifferentUnits(t *testing.T) {
	ing := domain.Inspection{ID: "i1", Kind: domain.InspectionIngoing, UnitID: "u1"}
	out := domain.Inspection{ID: "o1", Kind: domain.InspectionOutgoing, UnitID: "u2"}
	if _, err := Compare(ing, out, nil, nil, nil, nil); err == nil {
		t.Error("Compare accepted inspections of two different units")
	}
}

// The basic case: same template both times, one item unchanged, one
// deteriorated, one improved.
func TestCompareSameTemplate(t *testing.T) {
	tmpl := []domain.TemplateItem{
		mkItem("carpet", "Bedroom", "Carpet", 1),
		mkItem("blind", "Bedroom", "Blind", 2),
		mkItem("door", "Bedroom", "Door", 3),
	}
	ing := domain.Inspection{ID: "in", Kind: domain.InspectionIngoing, UnitID: "u1"}
	out := domain.Inspection{ID: "out", Kind: domain.InspectionOutgoing, UnitID: "u1"}

	ingF := []domain.Finding{
		mkFinding("f1", "carpet", "", domain.ConditionOK),
		mkFinding("f2", "blind", "", domain.ConditionDamage),
		mkFinding("f3", "door", "", domain.ConditionOK),
	}
	outF := []domain.Finding{
		mkFinding("f4", "carpet", "", domain.ConditionOK),   // unchanged
		mkFinding("f5", "blind", "", domain.ConditionOK),    // improved (tenant fixed it)
		mkFinding("f6", "door", "", domain.ConditionDamage), // deteriorated
	}

	cmp, err := Compare(ing, out, tmpl, tmpl, ingF, outF)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Items) != 3 {
		t.Fatalf("got %d items, want 3", len(cmp.Items))
	}
	outcomes := map[string]Outcome{}
	for _, it := range cmp.Items {
		outcomes[it.Label] = it.Outcome
	}
	if outcomes["Carpet"] != OutcomeUnchanged {
		t.Errorf("carpet = %v, want unchanged", outcomes["Carpet"])
	}
	if outcomes["Blind"] != OutcomeImproved {
		t.Errorf("blind = %v, want improved", outcomes["Blind"])
	}
	if outcomes["Door"] != OutcomeDeteriorated {
		t.Errorf("door = %v, want deteriorated", outcomes["Door"])
	}
	if len(cmp.DeterioratedItem) != 1 || cmp.DeterioratedItem[0].Label != "Door" {
		t.Errorf("deteriorated items = %+v, want just Door", cmp.DeterioratedItem)
	}
	if cmp.Counts[OutcomeDeteriorated] != 1 {
		t.Errorf("deteriorated count = %d, want 1", cmp.Counts[OutcomeDeteriorated])
	}
}

// The honest cases: a missing baseline must render as "not captured", never
// as deterioration — a comparison that guesses here is worse than no tool.
func TestCompareMissingCaptures(t *testing.T) {
	ing := domain.Inspection{ID: "in", Kind: domain.InspectionIngoing, UnitID: "u1"}
	out := domain.Inspection{ID: "out", Kind: domain.InspectionOutgoing, UnitID: "u1"}
	tmpl := []domain.TemplateItem{
		mkItem("counter", "Kitchen", "Counter", 1),
		mkItem("tap", "Kitchen", "Tap", 2),
	}
	// Counter: captured outgoing only — no ingoing baseline.
	// Tap: captured ingoing only — the move-out walk skipped it.
	ingF := []domain.Finding{mkFinding("f1", "tap", "", domain.ConditionOK)}
	outF := []domain.Finding{mkFinding("f2", "counter", "", domain.ConditionDamage)}

	cmp, err := Compare(ing, out, tmpl, tmpl, ingF, outF)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]Outcome{}
	for _, it := range cmp.Items {
		outcomes[it.Label] = it.Outcome
	}
	if outcomes["Counter"] != OutcomeNotCapturedIngoing {
		t.Errorf("counter = %v, want not_captured_ingoing (must not read as deterioration)", outcomes["Counter"])
	}
	if outcomes["Tap"] != OutcomeNotCapturedOutgoing {
		t.Errorf("tap = %v, want not_captured_outgoing", outcomes["Tap"])
	}
	if len(cmp.DeterioratedItem) != 0 {
		t.Errorf("a missing baseline must never appear in the deteriorated list, got %+v", cmp.DeterioratedItem)
	}
}

// An item nobody captured on either side is checklist noise, not a result.
func TestCompareOmitsItemsCapturedNowhere(t *testing.T) {
	ing := domain.Inspection{ID: "in", Kind: domain.InspectionIngoing, UnitID: "u1"}
	out := domain.Inspection{ID: "out", Kind: domain.InspectionOutgoing, UnitID: "u1"}
	tmpl := []domain.TemplateItem{mkItem("pool", "Exterior", "Pool", 1)}

	cmp, err := Compare(ing, out, tmpl, tmpl, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Items) != 0 {
		t.Errorf("got %d items, want 0 — nothing was captured on either side", len(cmp.Items))
	}
}

// The template-drift case: the outgoing walk ran against an edited template
// (an item removed, one added, one renamed). This must not crash, and it must
// not mispair "Bathroom mirror" with "Bathroom cabinet" just because they
// happen to be the same list position. The pairing rule is normalized
// (section, label) text — see the comment on key() in compare.go.
func TestCompareHandlesTemplateDrift(t *testing.T) {
	ing := domain.Inspection{ID: "in", Kind: domain.InspectionIngoing, UnitID: "u1", TemplateID: "t1"}
	out := domain.Inspection{ID: "out", Kind: domain.InspectionOutgoing, UnitID: "u1", TemplateID: "t2"}

	ingItems := []domain.TemplateItem{
		mkItem("a1", "Bathroom", "Mirror", 1),
		mkItem("a2", "Bathroom", "Cabinet", 2), // removed from t2
	}
	outItems := []domain.TemplateItem{
		mkItem("b1", "Bathroom", "Mirror", 1),        // same text, different id: must still pair
		mkItem("b2", "Bathroom", "Shower screen", 2), // new in t2: no ingoing baseline
	}
	ingF := []domain.Finding{
		mkFinding("f1", "a1", "", domain.ConditionOK),
		mkFinding("f2", "a2", "", domain.ConditionOK),
	}
	outF := []domain.Finding{
		mkFinding("f3", "b1", "", domain.ConditionDamage),
		mkFinding("f4", "b2", "", domain.ConditionOK),
	}

	cmp, err := Compare(ing, out, ingItems, outItems, ingF, outF)
	if err != nil {
		t.Fatalf("Compare crashed/errored on template drift: %v", err)
	}
	outcomes := map[string]Outcome{}
	for _, it := range cmp.Items {
		outcomes[it.Label] = it.Outcome
	}
	if outcomes["Mirror"] != OutcomeDeteriorated {
		t.Errorf("mirror (paired across template ids by label) = %v, want deteriorated", outcomes["Mirror"])
	}
	if outcomes["Cabinet"] != OutcomeNotCapturedOutgoing {
		t.Errorf("cabinet (dropped from the new template) = %v, want not_captured_outgoing", outcomes["Cabinet"])
	}
	if outcomes["Shower screen"] != OutcomeNotCapturedIngoing {
		t.Errorf("shower screen (new in the new template) = %v, want not_captured_ingoing", outcomes["Shower screen"])
	}
	if len(cmp.Items) != 3 {
		t.Errorf("got %d items, want 3 (Mirror, Cabinet, Shower screen) — items must not be mispaired by position", len(cmp.Items))
	}
}

// NA is unranked: an item that goes from "not applicable" to any other value,
// or vice versa, has no computable direction and must not be guessed at.
func TestCompareTreatsNotApplicableAsUnchanged(t *testing.T) {
	ing := domain.Inspection{ID: "in", Kind: domain.InspectionIngoing, UnitID: "u1"}
	out := domain.Inspection{ID: "out", Kind: domain.InspectionOutgoing, UnitID: "u1"}
	tmpl := []domain.TemplateItem{mkItem("pool", "Exterior", "Pool fence", 1)}
	ingF := []domain.Finding{mkFinding("f1", "pool", "", domain.ConditionNA)}
	outF := []domain.Finding{mkFinding("f2", "pool", "", domain.ConditionDamage)}

	cmp, err := Compare(ing, out, tmpl, tmpl, ingF, outF)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Items[0].Outcome != OutcomeUnchanged {
		t.Errorf("NA-involved pair = %v, want unchanged (no guessed direction)", cmp.Items[0].Outcome)
	}
}

// A freeform finding (no template item) is paired by its own label, so an
// ad-hoc note made on both the ingoing and outgoing walk still compares.
func TestCompareFreeformFindingsPairByLabel(t *testing.T) {
	ing := domain.Inspection{ID: "in", Kind: domain.InspectionIngoing, UnitID: "u1"}
	out := domain.Inspection{ID: "out", Kind: domain.InspectionOutgoing, UnitID: "u1"}
	ingF := []domain.Finding{mkFinding("f1", "", "Garage remote", domain.ConditionOK)}
	outF := []domain.Finding{mkFinding("f2", "", "Garage remote", domain.ConditionMissing)}

	cmp, err := Compare(ing, out, nil, nil, ingF, outF)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Items) != 1 || cmp.Items[0].Outcome != OutcomeDeteriorated {
		t.Errorf("freeform pairing = %+v, want one deteriorated item", cmp.Items)
	}
}
