package api

// End-to-end tests for the inspections HTTP surface: the comparison endpoint
// (the product's differentiator, docs/INSPECTIONS.md §1) and the completion
// rule the legacy system shipped as an empty stub.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
)

// createInspection is a small helper around the JSON API so each test reads
// as the scenario it checks rather than request plumbing.
func createInspection(t *testing.T, h http.Handler, token, buildingID, unitLabel, templateID, kind string) map[string]any {
	t.Helper()
	rec := do(t, h, "POST", "/api/inspections", token, map[string]any{
		"building_id": buildingID, "unit_label": unitLabel, "template_id": templateID, "kind": kind,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create inspection: %d %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func addFinding(t *testing.T, h http.Handler, token, inspectionID, itemID, condition string) *httpRecorderCode {
	t.Helper()
	rec := do(t, h, "POST", "/api/inspections/"+inspectionID+"/findings", token, map[string]any{
		"item_id": itemID, "condition": condition,
	})
	return &httpRecorderCode{code: rec.Code, body: rec.Body.String()}
}

type httpRecorderCode struct {
	code int
	body string
}

func completeInspection(t *testing.T, h http.Handler, token, inspectionID string) int {
	t.Helper()
	rec := do(t, h, "POST", "/api/inspections/"+inspectionID+"/status", token, map[string]any{
		"status": domain.InspectionComplete,
	})
	return rec.Code
}

// TestComparisonEndpointFindsDeterioration walks a full ingoing/outgoing cycle
// through the HTTP API and checks the comparison endpoint reports the item
// that got worse, and nothing else.
func TestComparisonEndpointFindsDeterioration(t *testing.T) {
	_, h, r := testServer(t)
	tn := newTenant(t, r, "Meridian", "owner@meridian.test")

	tmplRec := do(t, h, "POST", "/api/templates", tn.token, map[string]any{
		"name": "Move-in", "items": []map[string]any{
			{"section": "Kitchen", "label": "Counter"},
			{"section": "Kitchen", "label": "Tap"},
		},
	})
	if tmplRec.Code != http.StatusCreated {
		t.Fatalf("create template: %d %s", tmplRec.Code, tmplRec.Body.String())
	}
	var tmpl struct {
		ID    string `json:"id"`
		Items []struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"items"`
	}
	if err := json.Unmarshal(tmplRec.Body.Bytes(), &tmpl); err != nil {
		t.Fatal(err)
	}
	var counterItem, tapItem string
	for _, it := range tmpl.Items {
		switch it.Label {
		case "Counter":
			counterItem = it.ID
		case "Tap":
			tapItem = it.ID
		}
	}

	ing := createInspection(t, h, tn.token, tn.building, "Flat 3A", tmpl.ID, domain.InspectionIngoing)
	ingID := ing["id"].(string)
	if rec := addFinding(t, h, tn.token, ingID, counterItem, domain.ConditionOK); rec.code != http.StatusCreated {
		t.Fatalf("ingoing counter finding: %d %s", rec.code, rec.body)
	}
	if rec := addFinding(t, h, tn.token, ingID, tapItem, domain.ConditionOK); rec.code != http.StatusCreated {
		t.Fatalf("ingoing tap finding: %d %s", rec.code, rec.body)
	}
	if code := completeInspection(t, h, tn.token, ingID); code != http.StatusOK {
		t.Fatalf("complete ingoing: %d", code)
	}

	out := createInspection(t, h, tn.token, tn.building, "Flat 3A", tmpl.ID, domain.InspectionOutgoing)
	outID := out["id"].(string)
	if rec := addFinding(t, h, tn.token, outID, counterItem, domain.ConditionDamage); rec.code != http.StatusCreated {
		t.Fatalf("outgoing counter finding: %d %s", rec.code, rec.body)
	}
	// Tap left uncaptured on the move-out walk on purpose.
	if code := completeInspection(t, h, tn.token, outID); code != http.StatusOK {
		t.Fatalf("complete outgoing: %d", code)
	}

	rec := do(t, h, "GET", "/api/inspections/"+outID+"/comparison", tn.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("comparison: %d %s", rec.Code, rec.Body.String())
	}
	var cmp struct {
		Items []struct {
			Label   string `json:"label"`
			Outcome string `json:"outcome"`
		} `json:"items"`
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cmp); err != nil {
		t.Fatal(err)
	}
	outcomes := map[string]string{}
	for _, it := range cmp.Items {
		outcomes[it.Label] = it.Outcome
	}
	if outcomes["Counter"] != "deteriorated" {
		t.Errorf("counter = %q, want deteriorated", outcomes["Counter"])
	}
	if outcomes["Tap"] != "not_captured_outgoing" {
		t.Errorf("tap = %q, want not_captured_outgoing", outcomes["Tap"])
	}
	if cmp.Counts["deteriorated"] != 1 {
		t.Errorf("deteriorated count = %d, want 1", cmp.Counts["deteriorated"])
	}
}

// A completed inspection is immutable end to end: the legacy system's
// handleCompletion() accepted findings after "completion" with no error.
func TestCompletedInspectionRejectsFindingsOverHTTP(t *testing.T) {
	_, h, r := testServer(t)
	tn := newTenant(t, r, "Meridian", "owner2@meridian.test")

	insp := createInspection(t, h, tn.token, tn.building, "Flat 5C", "", domain.InspectionRoutine)
	id := insp["id"].(string)
	if code := completeInspection(t, h, tn.token, id); code != http.StatusOK {
		t.Fatalf("complete: %d", code)
	}
	rec := do(t, h, "POST", "/api/inspections/"+id+"/findings", tn.token, map[string]any{
		"label": "Late note", "condition": domain.ConditionOK,
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("finding on completed inspection = %d, want 409", rec.Code)
	}
	// And no further status change is accepted either.
	if code := completeInspection(t, h, tn.token, id); code != http.StatusConflict {
		t.Errorf("re-completing = %d, want 409", code)
	}
}

// The comparison endpoint must not let one org fetch another org's inspection
// comparison by guessing an id — the same isolation rule as every other route
// (§11).
func TestComparisonEndpointRespectsOrgIsolation(t *testing.T) {
	_, h, r := testServer(t)
	tnA := newTenant(t, r, "Meridian", "a@meridian.test")
	tnB := newTenant(t, r, "Highgate", "b@highgate.test")

	ing := createInspection(t, h, tnA.token, tnA.building, "Flat 3A", "", domain.InspectionIngoing)
	if code := completeInspection(t, h, tnA.token, ing["id"].(string)); code != http.StatusOK {
		t.Fatalf("complete ingoing: %d", code)
	}
	out := createInspection(t, h, tnA.token, tnA.building, "Flat 3A", "", domain.InspectionOutgoing)
	outID := out["id"].(string)

	rec := do(t, h, "GET", "/api/inspections/"+outID+"/comparison", tnB.token, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("org B fetching org A's comparison = %d, want 404", rec.Code)
	}
}
