// Package report computes Pango's aggregates: spend and labour per building,
// per unit and per job, open/closed counts, and created-versus-closed over
// time.
//
// Every number in this package is a SUM over the append-only entry tables at
// read time (§6). Not one of them reads a stored total, and adding a cached
// total column to make a query faster would be a correctness regression, not an
// optimisation:
//
// Two people record spend on the same job while partitioned. Union merge makes
// the entries add, and a SUM over them is right. A stored total would have kept
// whichever write landed last and thrown the other away — with no error, no
// conflict and no way to notice until a landlord queried an invoice against a
// contractor's own records.
//
// The queries below use scalar subqueries rather than joining cost_entry and
// time_entry to job in one statement. Joining both would multiply the rows
// (three cost entries and two time entries produce six rows) and every total
// would silently come out several times too large — an error that looks
// plausible on a small dataset and is catastrophic on a real one.
package report

import (
	"database/sql"
	"fmt"

	"github.com/vul-os/pango/backend/internal/domain"
)

// Reporter computes aggregates. It is read-only by construction: it holds a
// database handle and issues SELECTs.
type Reporter struct {
	db *sql.DB
}

// New builds a Reporter over a database handle.
func New(db *sql.DB) *Reporter { return &Reporter{db: db} }

// BuildingTotals is spend and labour for one building.
type BuildingTotals struct {
	BuildingID string       `json:"building_id"`
	Name       string       `json:"name"`
	Jobs       int64        `json:"jobs"`
	OpenJobs   int64        `json:"open_jobs"`
	CostMinor  domain.Money `json:"cost_minor"`
	Minutes    int64        `json:"minutes"`
}

// ByBuilding returns totals per building for one organisation.
func (r *Reporter) ByBuilding(orgID string) ([]BuildingTotals, error) {
	rows, err := r.db.Query(`
		SELECT b.id, b.name,
		  (SELECT COUNT(*) FROM job j
		     WHERE j.building_id = b.id AND j.org_id = ? AND j.deleted = 0),
		  (SELECT COUNT(*) FROM job j
		     WHERE j.building_id = b.id AND j.org_id = ? AND j.deleted = 0
		       AND j.status NOT IN ('closed', 'cancelled')),
		  (SELECT COALESCE(SUM(c.amount_minor), 0) FROM cost_entry c
		     JOIN job j ON j.id = c.job_id
		     WHERE j.building_id = b.id AND j.org_id = ? AND j.deleted = 0),
		  (SELECT COALESCE(SUM(t.minutes), 0) FROM time_entry t
		     JOIN job j ON j.id = t.job_id
		     WHERE j.building_id = b.id AND j.org_id = ? AND j.deleted = 0)
		FROM building b
		WHERE b.org_id = ? AND b.deleted = 0
		ORDER BY b.name`,
		orgID, orgID, orgID, orgID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BuildingTotals{}
	for rows.Next() {
		var t BuildingTotals
		var cost int64
		if err := rows.Scan(&t.BuildingID, &t.Name, &t.Jobs, &t.OpenJobs, &cost, &t.Minutes); err != nil {
			return nil, fmt.Errorf("scan building totals: %w", err)
		}
		t.CostMinor = domain.Money(cost)
		out = append(out, t)
	}
	return out, rows.Err()
}

// UnitTotals is spend and labour for one unit.
//
// This report is the reason unit is a real table (§4.1). In the legacy system
// it grouped by free text, so the same physical flat appeared as several rows —
// each understating its true spend, and none of them the number anyone wanted.
type UnitTotals struct {
	UnitID     string       `json:"unit_id"`
	BuildingID string       `json:"building_id"`
	Key        string       `json:"key"`
	Label      string       `json:"label"`
	Jobs       int64        `json:"jobs"`
	OpenJobs   int64        `json:"open_jobs"`
	CostMinor  domain.Money `json:"cost_minor"`
	Minutes    int64        `json:"minutes"`
}

// ByUnit returns totals per unit. buildingID, when non-empty, narrows to one
// building.
func (r *Reporter) ByUnit(orgID, buildingID string) ([]UnitTotals, error) {
	q := `
		SELECT u.id, u.building_id, u.key, u.label,
		  (SELECT COUNT(*) FROM job j
		     WHERE j.unit_id = u.id AND j.org_id = ? AND j.deleted = 0),
		  (SELECT COUNT(*) FROM job j
		     WHERE j.unit_id = u.id AND j.org_id = ? AND j.deleted = 0
		       AND j.status NOT IN ('closed', 'cancelled')),
		  (SELECT COALESCE(SUM(c.amount_minor), 0) FROM cost_entry c
		     JOIN job j ON j.id = c.job_id
		     WHERE j.unit_id = u.id AND j.org_id = ? AND j.deleted = 0),
		  (SELECT COALESCE(SUM(t.minutes), 0) FROM time_entry t
		     JOIN job j ON j.id = t.job_id
		     WHERE j.unit_id = u.id AND j.org_id = ? AND j.deleted = 0)
		FROM unit u
		WHERE u.org_id = ? AND u.deleted = 0`
	args := []any{orgID, orgID, orgID, orgID, orgID}
	if buildingID != "" {
		q += " AND u.building_id = ?"
		args = append(args, buildingID)
	}
	q += " ORDER BY u.building_id, u.key"

	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UnitTotals{}
	for rows.Next() {
		var t UnitTotals
		var cost int64
		if err := rows.Scan(&t.UnitID, &t.BuildingID, &t.Key, &t.Label,
			&t.Jobs, &t.OpenJobs, &cost, &t.Minutes); err != nil {
			return nil, fmt.Errorf("scan unit totals: %w", err)
		}
		t.CostMinor = domain.Money(cost)
		out = append(out, t)
	}
	return out, rows.Err()
}

// JobTotals is spend and labour for one job.
type JobTotals struct {
	JobID      string       `json:"job_id"`
	BuildingID string       `json:"building_id"`
	UnitID     string       `json:"unit_id"`
	Number     int64        `json:"number"`
	Title      string       `json:"title"`
	Status     string       `json:"status"`
	CostMinor  domain.Money `json:"cost_minor"`
	Minutes    int64        `json:"minutes"`
	Entries    int64        `json:"entries"`
}

// ByJob returns totals per job. jobID, when non-empty, narrows to one job.
func (r *Reporter) ByJob(orgID, jobID string) ([]JobTotals, error) {
	q := `
		SELECT j.id, j.building_id, COALESCE(j.unit_id, ''), j.number, j.title, j.status,
		  (SELECT COALESCE(SUM(c.amount_minor), 0) FROM cost_entry c WHERE c.job_id = j.id),
		  (SELECT COALESCE(SUM(t.minutes), 0) FROM time_entry t WHERE t.job_id = j.id),
		  (SELECT COUNT(*) FROM cost_entry c WHERE c.job_id = j.id)
		FROM job j
		WHERE j.org_id = ? AND j.deleted = 0`
	args := []any{orgID}
	if jobID != "" {
		q += " AND j.id = ?"
		args = append(args, jobID)
	}
	q += " ORDER BY j.building_id, j.number"

	rows, err := r.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobTotals{}
	for rows.Next() {
		var t JobTotals
		var cost int64
		if err := rows.Scan(&t.JobID, &t.BuildingID, &t.UnitID, &t.Number, &t.Title,
			&t.Status, &cost, &t.Minutes, &t.Entries); err != nil {
			return nil, fmt.Errorf("scan job totals: %w", err)
		}
		t.CostMinor = domain.Money(cost)
		out = append(out, t)
	}
	return out, rows.Err()
}

// StatusSummary counts jobs by status, plus the open/closed split.
type StatusSummary struct {
	ByStatus map[string]int64 `json:"by_status"`
	Open     int64            `json:"open"`
	Closed   int64            `json:"closed"`
	Total    int64            `json:"total"`
}

// Status returns the open/closed picture for an organisation.
func (r *Reporter) Status(orgID string) (StatusSummary, error) {
	rows, err := r.db.Query(
		`SELECT status, COUNT(*) FROM job WHERE org_id = ? AND deleted = 0 GROUP BY status`, orgID)
	if err != nil {
		return StatusSummary{}, err
	}
	defer rows.Close()

	sum := StatusSummary{ByStatus: map[string]int64{}}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return StatusSummary{}, err
		}
		sum.ByStatus[status] = n
		sum.Total += n
		// Open/closed is derived from the domain's own definition rather than
		// restated in SQL, so there is exactly one answer to "is this job
		// open" in the codebase.
		if domain.IsOpen(status) {
			sum.Open += n
		} else {
			sum.Closed += n
		}
	}
	return sum, rows.Err()
}

// DayPoint is one day of the created-versus-closed series.
type DayPoint struct {
	Day     string `json:"day"` // YYYY-MM-DD
	Created int64  `json:"created"`
	Closed  int64  `json:"closed"`
}

// Timeline returns jobs created and closed per day, oldest first.
//
// Both series come from the job's own timestamps rather than from a counter
// maintained on write, so a job that syncs in from a tablet three days late
// lands on the day the work was actually reported — which is the day the
// backlog it belongs to formed.
func (r *Reporter) Timeline(orgID string) ([]DayPoint, error) {
	created := map[string]int64{}
	closed := map[string]int64{}

	rows, err := r.db.Query(
		`SELECT substr(created_at, 1, 10), COUNT(*) FROM job
		 WHERE org_id = ? AND deleted = 0 GROUP BY 1`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day string
		var n int64
		if err := rows.Scan(&day, &n); err != nil {
			rows.Close()
			return nil, err
		}
		created[day] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.db.Query(
		`SELECT substr(closed_at, 1, 10), COUNT(*) FROM job
		 WHERE org_id = ? AND deleted = 0 AND closed_at <> '' GROUP BY 1`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day string
		var n int64
		if err := rows.Scan(&day, &n); err != nil {
			rows.Close()
			return nil, err
		}
		closed[day] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	days := map[string]bool{}
	for d := range created {
		days[d] = true
	}
	for d := range closed {
		days[d] = true
	}
	ordered := make([]string, 0, len(days))
	for d := range days {
		ordered = append(ordered, d)
	}
	sortStrings(ordered)

	out := make([]DayPoint, 0, len(ordered))
	for _, d := range ordered {
		out = append(out, DayPoint{Day: d, Created: created[d], Closed: closed[d]})
	}
	return out, nil
}

// sortStrings is an insertion sort over the small day list. Using it rather
// than pulling in sort keeps this package's imports to the two it genuinely
// needs; the series is bounded by days of history, not by rows.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
