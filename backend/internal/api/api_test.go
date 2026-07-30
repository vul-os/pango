package api

// API tests, focused on the boundary this package exists to hold: a request
// authenticated as org A must not be able to read or write org B's rows by any
// route, including by supplying org B's ids as parameters.
//
// This is the legacy breach restated as a test. That system took an
// organization_id filter from the frontend, so the isolation was whatever the
// client chose to send. Every case below sends org B's ids from an org A
// session on purpose.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vul-os/pango/backend/internal/domain"
	"github.com/vul-os/pango/backend/internal/repo"
	"github.com/vul-os/pango/backend/internal/store"
)

type tenant struct {
	org      string
	token    string
	building string
	job      string
	unit     string
}

func testServer(t *testing.T) (*Server, http.Handler, *repo.Repo) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	r := repo.New(s)
	srv := New(r, "test")
	return srv, srv.Handler(), r
}

// newTenant creates an organisation with a user, a session, a building, a unit
// and a job — enough for every cross-tenant probe below.
func newTenant(t *testing.T, r *repo.Repo, name, email string) tenant {
	t.Helper()
	org, err := r.CreateOrg(name)
	if err != nil {
		t.Fatal(err)
	}
	user, err := r.CreateUser(org.ID, email, "correct-horse-battery", name, "owner")
	if err != nil {
		t.Fatal(err)
	}
	token, err := r.CreateSession(user)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.CreateBuilding(org.ID, domain.Building{Name: name + " Court"})
	if err != nil {
		t.Fatal(err)
	}
	j, err := r.CreateJob(org.ID, domain.Job{BuildingID: b.ID, Title: name + " secret job"}, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.AddCost(org.ID, domain.CostEntry{JobID: j.ID, AmountMinor: 12345}); err != nil {
		t.Fatal(err)
	}
	u, err := r.EnsureUnit(org.ID, b.ID, "Flat 3A")
	if err != nil {
		t.Fatal(err)
	}
	return tenant{org: org.ID, token: token, building: b.ID, job: j.ID, unit: u.ID}
}

func do(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthNeedsNoAuth(t *testing.T) {
	srv, h, _ := testServer(t)

	rec := do(t, h, "GET", "/api/health", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["version"] != "test" {
		t.Errorf("version = %v, want test", body["version"])
	}
	// Health is public, so it must carry no tenant data.
	for _, leak := range []string{"org", "email", "building", "job"} {
		if _, present := body[leak]; present {
			t.Errorf("health response exposes %q", leak)
		}
	}
	_ = srv
}

func TestScopedRoutesRequireAuth(t *testing.T) {
	_, h, r := testServer(t)
	a := newTenant(t, r, "Meridian", "a@example.com")

	paths := []string{
		"/api/auth/me", "/api/buildings", "/api/jobs", "/api/parties", "/api/peers",
		"/api/templates", "/api/inspections",
		"/api/reports/buildings", "/api/reports/units", "/api/reports/jobs",
		"/api/reports/status", "/api/reports/timeline",
		"/api/buildings/" + a.building, "/api/jobs/" + a.job,
	}
	for _, p := range paths {
		if rec := do(t, h, "GET", p, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a token = %d, want 401", p, rec.Code)
		}
		if rec := do(t, h, "GET", p, "not-a-real-token", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s with a bogus token = %d, want 401", p, rec.Code)
		}
	}
}

// The core case: an org A session probing org B's ids sees nothing.
func TestOrgIsolationOverHTTP(t *testing.T) {
	_, h, r := testServer(t)
	a := newTenant(t, r, "Meridian", "a@example.com")
	b := newTenant(t, r, "Cornerstone", "b@example.com")

	// Direct reads of B's rows, using A's session.
	for _, p := range []string{
		"/api/buildings/" + b.building,
		"/api/buildings/" + b.building + "/units",
		"/api/jobs/" + b.job,
		"/api/jobs/" + b.job + "/events",
		"/api/jobs/" + b.job + "/costs",
		"/api/jobs/" + b.job + "/time",
	} {
		rec := do(t, h, "GET", p, a.token, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s as org A = %d, want 404 (got body %s)", p, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret job") {
			t.Fatalf("GET %s leaked org B data: %s", p, rec.Body.String())
		}
	}

	// Listings contain only A's own rows.
	rec := do(t, h, "GET", "/api/buildings", a.token, nil)
	var buildings []domain.Building
	if err := json.Unmarshal(rec.Body.Bytes(), &buildings); err != nil {
		t.Fatal(err)
	}
	if len(buildings) != 1 || buildings[0].ID != a.building {
		t.Fatalf("org A sees %d buildings, want only its own", len(buildings))
	}

	rec = do(t, h, "GET", "/api/jobs", a.token, nil)
	if strings.Contains(rec.Body.String(), "Cornerstone secret job") {
		t.Fatal("job listing leaked org B's job")
	}

	// A client-supplied filter naming B's building returns nothing, rather
	// than B's jobs — this is the exact shape of the legacy breach.
	rec = do(t, h, "GET", "/api/jobs?building_id="+b.building, a.token, nil)
	var jobs []domain.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("filtering by org B's building returned %d jobs to org A", len(jobs))
	}

	// Reports must not aggregate across the boundary.
	for _, p := range []string{
		"/api/reports/buildings",
		"/api/reports/units?building_id=" + b.building,
		"/api/reports/jobs?job_id=" + b.job,
	} {
		rec := do(t, h, "GET", p, a.token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", p, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "Cornerstone") {
			t.Errorf("report %s leaked org B: %s", p, rec.Body.String())
		}
	}

	// Writes against B's rows, using A's session.
	writes := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/jobs", jobReq{BuildingID: b.building, Title: "injected"}},
		{"POST", "/api/jobs/" + b.job + "/costs", costReq{AmountMinor: 5000}},
		{"POST", "/api/jobs/" + b.job + "/time", timeReq{Minutes: 30}},
		{"POST", "/api/jobs/" + b.job + "/events", eventReq{Kind: "note", Body: "x"}},
		{"POST", "/api/jobs/" + b.job + "/status", statusReq{Status: domain.StatusClosed}},
		{"POST", "/api/jobs/" + b.job + "/assign", assignReq{}},
		{"POST", "/api/buildings/" + b.building + "/units", unitReq{Label: "Flat 9"}},
		{"PATCH", "/api/buildings/" + b.building, buildingReq{Name: "hijacked"}},
		{"DELETE", "/api/buildings/" + b.building, nil},
	}
	for _, wr := range writes {
		rec := do(t, h, wr.method, wr.path, a.token, wr.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s as org A = %d, want 404", wr.method, wr.path, rec.Code)
		}
	}

	// Org B is intact after all of it.
	rec = do(t, h, "GET", "/api/buildings/"+b.building, b.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("org B's building = %d after cross-tenant writes", rec.Code)
	}
	var bldg domain.Building
	if err := json.Unmarshal(rec.Body.Bytes(), &bldg); err != nil {
		t.Fatal(err)
	}
	if bldg.Name == "hijacked" {
		t.Fatal("org A renamed org B's building")
	}
	rec = do(t, h, "GET", "/api/jobs/"+b.job, b.token, nil)
	if strings.Contains(rec.Body.String(), `"status":"closed"`) {
		t.Fatal("org A closed org B's job")
	}
}

// A body that carries an org_id must not be honoured. Rejecting it outright is
// better than ignoring it: a silent success leaves the sender believing the
// scoping worked.
func TestClientSuppliedOrgIDIsRejected(t *testing.T) {
	_, h, r := testServer(t)
	a := newTenant(t, r, "Meridian", "a@example.com")
	b := newTenant(t, r, "Cornerstone", "b@example.com")

	body := map[string]any{"name": "Injected Court", "org_id": b.org}
	rec := do(t, h, "POST", "/api/buildings", a.token, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST with an org_id field = %d, want 400", rec.Code)
	}

	// And nothing was created in either organisation.
	for _, tn := range []tenant{a, b} {
		rec := do(t, h, "GET", "/api/buildings", tn.token, nil)
		if strings.Contains(rec.Body.String(), "Injected Court") {
			t.Fatal("a building was created despite the rejected request")
		}
	}
}

func TestLoginFlow(t *testing.T) {
	_, h, r := testServer(t)

	// First run: registration is open.
	rec := do(t, h, "POST", "/api/auth/register", "", registerReq{
		Organisation: "Meridian", Email: "manager@meridian.example",
		Password: "correct-horse-battery", Name: "Manager",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", rec.Code, rec.Body.String())
	}
	var reg authResp
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
		t.Fatal(err)
	}
	if reg.Token == "" {
		t.Fatal("register returned no token")
	}
	// The session cookie must be HttpOnly, or an XSS could exfiltrate it.
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			found = true
			if !c.HttpOnly {
				t.Error("session cookie is not HttpOnly")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Error("session cookie is not SameSite=Lax")
			}
		}
	}
	if !found {
		t.Error("no session cookie set")
	}

	// Second registration is refused: a node belongs to one organisation.
	rec = do(t, h, "POST", "/api/auth/register", "", registerReq{
		Organisation: "Interloper", Email: "x@y.example", Password: "correct-horse-battery",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("second register = %d, want 403", rec.Code)
	}

	// Login works, and the wrong password does not.
	rec = do(t, h, "POST", "/api/auth/login", "", loginReq{
		Email: "manager@meridian.example", Password: "correct-horse-battery",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d", rec.Code)
	}
	var login authResp
	if err := json.Unmarshal(rec.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}

	rec = do(t, h, "POST", "/api/auth/login", "", loginReq{
		Email: "manager@meridian.example", Password: "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad password = %d, want 401", rec.Code)
	}

	// me works with the token; logout kills it.
	if rec := do(t, h, "GET", "/api/auth/me", login.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("me = %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/auth/logout", login.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("logout = %d", rec.Code)
	}
	if rec := do(t, h, "GET", "/api/auth/me", login.Token, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout = %d, want 401", rec.Code)
	}

	_ = r
}

// No response may ever carry a password hash or the node's private seed.
func TestNoSecretsInResponses(t *testing.T) {
	srv, h, r := testServer(t)
	a := newTenant(t, r, "Meridian", "a@example.com")

	seed := srv.Repo.Store().PrivateSeedHexForTest()
	if seed == "" {
		t.Fatal("no identity to check against")
	}

	for _, p := range []string{
		"/api/health", "/api/auth/me", "/api/buildings", "/api/jobs", "/api/peers", "/api/parties",
	} {
		rec := do(t, h, "GET", p, a.token, nil)
		body := rec.Body.String()
		if strings.Contains(body, seed) {
			t.Errorf("%s leaked the node private seed", p)
		}
		for _, marker := range []string{"password_hash", "$2a$", "node_privkey"} {
			if strings.Contains(body, marker) {
				t.Errorf("%s leaked %q", p, marker)
			}
		}
	}
}

// Unit creation over HTTP is create-or-return, so a tablet posting "Flat 3A"
// and an office posting "3a" converge on one unit rather than two.
func TestEnsureUnitOverHTTPIsIdempotent(t *testing.T) {
	_, h, r := testServer(t)
	a := newTenant(t, r, "Meridian", "a@example.com")

	var first domain.Unit
	for i, label := range []string{"Flat 3A", "3a", "3 A", "FLAT-3A"} {
		rec := do(t, h, "POST", "/api/buildings/"+a.building+"/units", a.token, unitReq{Label: label})
		if rec.Code != http.StatusOK {
			t.Fatalf("POST unit %q = %d: %s", label, rec.Code, rec.Body.String())
		}
		var u domain.Unit
		if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = u
			continue
		}
		if u.ID != first.ID {
			t.Fatalf("label %q created a second unit (%s vs %s)", label, u.ID, first.ID)
		}
	}

	rec := do(t, h, "GET", "/api/buildings/"+a.building+"/units", a.token, nil)
	var units []domain.Unit
	if err := json.Unmarshal(rec.Body.Bytes(), &units); err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("building has %d units, want 1", len(units))
	}
}

// A full job lifecycle over HTTP, ending in totals computed from the ledgers.
func TestJobLifecycleOverHTTP(t *testing.T) {
	_, h, r := testServer(t)
	a := newTenant(t, r, "Meridian", "a@example.com")

	rec := do(t, h, "POST", "/api/jobs", a.token, jobReq{
		BuildingID: a.building, UnitLabel: "Flat 12", Title: "No hot water",
		Priority: domain.PriorityHigh, Category: "plumbing",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create job = %d: %s", rec.Code, rec.Body.String())
	}
	var job domain.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Number == 0 {
		t.Error("job was not allocated a number")
	}

	for _, amount := range []int64{45000, 90000, -45000} {
		rec := do(t, h, "POST", "/api/jobs/"+job.ID+"/costs", a.token, costReq{
			Kind: domain.CostLabour, AmountMinor: amount,
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("add cost %d = %d: %s", amount, rec.Code, rec.Body.String())
		}
	}
	if rec := do(t, h, "POST", "/api/jobs/"+job.ID+"/time", a.token, timeReq{Minutes: 90}); rec.Code != http.StatusCreated {
		t.Fatalf("add time = %d", rec.Code)
	}

	// A zero-amount entry is refused.
	if rec := do(t, h, "POST", "/api/jobs/"+job.ID+"/costs", a.token, costReq{AmountMinor: 0}); rec.Code != http.StatusBadRequest {
		t.Errorf("zero cost = %d, want 400", rec.Code)
	}

	rec = do(t, h, "GET", "/api/jobs/"+job.ID, a.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get job = %d", rec.Code)
	}
	var got struct {
		Job    domain.Job `json:"job"`
		Totals struct {
			CostMinor int64 `json:"cost_minor"`
			Minutes   int64 `json:"minutes"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Totals.CostMinor != 90000 {
		t.Errorf("cost total = %d, want 90000 (45000 + 90000 - 45000)", got.Totals.CostMinor)
	}
	if got.Totals.Minutes != 90 {
		t.Errorf("minutes = %d, want 90", got.Totals.Minutes)
	}

	// There is no route that could edit or delete a ledger entry (§6).
	for _, method := range []string{"PATCH", "DELETE", "PUT"} {
		rec := do(t, h, method, "/api/jobs/"+job.ID+"/costs", a.token, costReq{AmountMinor: 1})
		if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
			t.Errorf("%s on the cost ledger succeeded — entries must be immutable", method)
		}
	}
}
