package main

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strconv"
	"time"
)

type Metrics struct {
	Active       int64 `json:"active"`
	Starts       int64 `json:"starts"`
	Sessions     int64 `json:"sessions"`
	PageViews    int64 `json:"page_views"`
	NewVisitors  int64 `json:"new_visitors"`
	CustomEvents int64 `json:"custom_events"`
}

func (m *Metrics) add(v Metrics) {
	m.Active += v.Active
	m.Starts += v.Starts
	m.Sessions += v.Sessions
	m.PageViews += v.PageViews
	m.NewVisitors += v.NewVisitors
	m.CustomEvents += v.CustomEvents
}

type Daily struct {
	Date string `json:"date"`
	Metrics
}
type Version struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Active   int64  `json:"active"`
	Starts   int64  `json:"starts"`
}
type Page struct {
	Path      string `json:"path"`
	PageViews int64  `json:"page_views"`
}

// CustomEvent aggregates one custom event name over the requested range.
type CustomEvent struct {
	Name     string `json:"name"`
	Count    int64  `json:"count"`
	Visitors int64  `json:"visitors"`
}
type AppMetrics struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Type                  string     `json:"type"`
	Enabled               bool       `json:"enabled"`
	AllowedPathPrefixes   []string   `json:"allowed_path_prefixes"`
	NormalizeNumericPaths bool       `json:"normalize_numeric_paths"`
	LastEventAt           *time.Time `json:"last_event_at"`
	Metrics
}

func (s *Server) dateRange(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	now := time.Now().In(s.cfg.Timezone)
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if to == "" {
		to = now.Format("2006-01-02")
	}
	if from == "" {
		from = now.AddDate(0, 0, -29).Format("2006-01-02")
	}
	f, e1 := time.Parse("2006-01-02", from)
	t, e2 := time.Parse("2006-01-02", to)
	if e1 != nil || e2 != nil || f.After(t) || t.Sub(f) > 365*24*time.Hour {
		fail(w, 400, "invalid_date_range", "Use YYYY-MM-DD with from <= to and at most 366 days")
		return "", "", false
	}
	return from, to, true
}

// appID="" selects all apps. Group before summing so anonymous IDs shared across apps remain separate visitors.
func statsMetrics(ctx context.Context, tx pgx.Tx, appID, from, to string) (Metrics, []Daily, map[string]Metrics, error) {
	totals := Metrics{}
	perApp := map[string]Metrics{}
	daily := []Daily{}
	rows, err := tx.Query(ctx, `WITH a AS (SELECT app_id,count(DISTINCT anonymous_id) n FROM daily_visitors WHERE day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY app_id),
 m AS (SELECT app_id,sum(starts) starts,sum(page_views) pv,sum(custom_events) custom FROM daily_metrics WHERE day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY app_id),
 v AS (SELECT app_id,count(*) n FROM visitors WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY app_id),
 ss AS (SELECT app_id,count(*) n FROM app_sessions WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY app_id)
 SELECT apps.id,coalesce(a.n,0),coalesce(m.starts,0)::bigint,coalesce(ss.n,0),coalesce(m.pv,0)::bigint,coalesce(v.n,0),coalesce(m.custom,0)::bigint FROM apps LEFT JOIN a ON a.app_id=apps.id LEFT JOIN m ON m.app_id=apps.id LEFT JOIN v ON v.app_id=apps.id LEFT JOIN ss ON ss.app_id=apps.id WHERE ($1='' OR apps.id=$1)`, appID, from, to)
	if err != nil {
		return totals, daily, perApp, err
	}
	for rows.Next() {
		var id string
		var m Metrics
		if err = rows.Scan(&id, &m.Active, &m.Starts, &m.Sessions, &m.PageViews, &m.NewVisitors, &m.CustomEvents); err != nil {
			rows.Close()
			return totals, daily, perApp, err
		}
		perApp[id] = m
		totals.add(m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return totals, daily, perApp, err
	}
	rows, err = tx.Query(ctx, `WITH a AS (SELECT day,count(*) n FROM daily_visitors WHERE day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY day),
 m AS (SELECT day,sum(starts) starts,sum(page_views) pv,sum(custom_events) custom FROM daily_metrics WHERE day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY day),
 v AS (SELECT first_day AS day,count(*) n FROM visitors WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY first_day),
 ss AS (SELECT first_day AS day,count(*) n FROM app_sessions WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY first_day)
 SELECT d.day::date::text,coalesce(a.n,0),coalesce(m.starts,0)::bigint,coalesce(ss.n,0),coalesce(m.pv,0)::bigint,coalesce(v.n,0),coalesce(m.custom,0)::bigint FROM generate_series($2::text::date,$3::text::date,'1 day'::interval) d(day) LEFT JOIN a ON a.day=d.day::date LEFT JOIN m ON m.day=d.day::date LEFT JOIN v ON v.day=d.day::date LEFT JOIN ss ON ss.day=d.day::date ORDER BY d.day`, appID, from, to)
	if err != nil {
		return totals, daily, perApp, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Daily
		if err = rows.Scan(&d.Date, &d.Active, &d.Starts, &d.Sessions, &d.PageViews, &d.NewVisitors, &d.CustomEvents); err != nil {
			return totals, daily, perApp, err
		}
		daily = append(daily, d)
	}
	return totals, daily, perApp, rows.Err()
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.dateRange(w, r)
	if !ok {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	totals, daily, perApp, err := statsMetrics(r.Context(), tx, "", from, to)
	if err != nil {
		s.internal(w, err)
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT "+appColumns+" FROM apps ORDER BY created_at,id")
	if err != nil {
		s.internal(w, err)
		return
	}
	apps := []AppMetrics{}
	for rows.Next() {
		a, e := scanApp(rows)
		if e != nil {
			rows.Close()
			s.internal(w, e)
			return
		}
		apps = append(apps, AppMetrics{
			ID: a.ID, Name: a.Name, Type: a.Type, Enabled: a.Enabled,
			AllowedPathPrefixes: a.AllowedPathPrefixes, NormalizeNumericPaths: a.NormalizeNumericPaths,
			LastEventAt: a.LastEventAt, Metrics: perApp[a.ID],
		})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"timezone": s.cfg.Timezone.String(), "from": from, "to": to, "totals": totals, "apps": apps, "daily": daily})
}
func (s *Server) appStats(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.dateRange(w, r)
	if !ok {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err := scanApp(tx.QueryRow(r.Context(), "SELECT "+appColumns+" FROM apps WHERE id=$1", r.PathValue("id")))
	if err != nil {
		s.appError(w, err)
		return
	}
	totals, daily, _, err := statsMetrics(r.Context(), tx, a.ID, from, to)
	if err != nil {
		s.internal(w, err)
		return
	}
	versions := []Version{}
	rows, err := tx.Query(r.Context(), "SELECT app_version,platform,count(DISTINCT anonymous_id),count(*) FILTER(WHERE event_type='app_start') FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date GROUP BY app_version,platform ORDER BY count(DISTINCT anonymous_id) DESC,app_version,platform", a.ID, from, to)
	if err != nil {
		s.internal(w, err)
		return
	}
	for rows.Next() {
		var v Version
		if err = rows.Scan(&v.Version, &v.Platform, &v.Active, &v.Starts); err != nil {
			rows.Close()
			s.internal(w, err)
			return
		}
		versions = append(versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.internal(w, err)
		return
	}
	pages := []Page{}
	rows, err = tx.Query(r.Context(), "SELECT path,count(*) FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date AND event_type='page_view' GROUP BY path ORDER BY count(*) DESC,path", a.ID, from, to)
	if err != nil {
		s.internal(w, err)
		return
	}
	for rows.Next() {
		var p Page
		if err = rows.Scan(&p.Path, &p.PageViews); err != nil {
			rows.Close()
			s.internal(w, err)
			return
		}
		pages = append(pages, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.internal(w, err)
		return
	}
	customEvents := []CustomEvent{}
	rows, err = tx.Query(r.Context(), "SELECT event_name,count(*),count(DISTINCT anonymous_id) FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date AND event_type='custom' GROUP BY event_name ORDER BY count(*) DESC,event_name LIMIT 20", a.ID, from, to)
	if err != nil {
		s.internal(w, err)
		return
	}
	for rows.Next() {
		var c CustomEvent
		if err = rows.Scan(&c.Name, &c.Count, &c.Visitors); err != nil {
			rows.Close()
			s.internal(w, err)
			return
		}
		customEvents = append(customEvents, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"timezone": s.cfg.Timezone.String(), "from": from, "to": to, "totals": totals, "daily": daily, "versions": versions, "pages": pages, "custom_events": customEvents, "last_event_at": a.LastEventAt})
}

// OperationTotals counts collect requests per day in operation_metrics.
type OperationTotals struct {
	Requests    int64 `json:"requests"`
	Accepted    int64 `json:"accepted"`
	Duplicates  int64 `json:"duplicates"`
	Rejected    int64 `json:"rejected"`
	RateLimited int64 `json:"rate_limited"`
}

func (t *OperationTotals) add(v OperationTotals) {
	t.Requests += v.Requests
	t.Accepted += v.Accepted
	t.Duplicates += v.Duplicates
	t.Rejected += v.Rejected
	t.RateLimited += v.RateLimited
}

type OperationApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	OperationTotals
}

type OperationDaily struct {
	Date string `json:"date"`
	OperationTotals
}

func (s *Server) operationMetrics(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.dateRange(w, r)
	if !ok {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	apps := []OperationApp{}
	rows, err := tx.Query(r.Context(), `SELECT a.id,a.name,coalesce(om.requests,0),coalesce(om.accepted,0),coalesce(om.duplicates,0),coalesce(om.rejected,0),coalesce(om.rate_limited,0)
FROM apps a LEFT JOIN (
SELECT app_id,sum(requests) requests,sum(accepted) accepted,sum(duplicates) duplicates,sum(rejected) rejected,sum(rate_limited) rate_limited
FROM operation_metrics WHERE day BETWEEN $1::text::date AND $2::text::date GROUP BY app_id
) om ON om.app_id=a.id ORDER BY a.created_at,a.id`, from, to)
	if err != nil {
		s.internal(w, err)
		return
	}
	totals := OperationTotals{}
	for rows.Next() {
		var app OperationApp
		if err = rows.Scan(&app.ID, &app.Name, &app.Requests, &app.Accepted, &app.Duplicates, &app.Rejected, &app.RateLimited); err != nil {
			rows.Close()
			s.internal(w, err)
			return
		}
		totals.add(app.OperationTotals)
		apps = append(apps, app)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		s.internal(w, err)
		return
	}
	rows.Close()
	daily := []OperationDaily{}
	rows, err = tx.Query(r.Context(), `SELECT d.day::date::text,coalesce(sum(om.requests),0),coalesce(sum(om.accepted),0),coalesce(sum(om.duplicates),0),coalesce(sum(om.rejected),0),coalesce(sum(om.rate_limited),0)
FROM generate_series($1::text::date,$2::text::date,'1 day'::interval) d(day)
LEFT JOIN operation_metrics om ON om.day=d.day::date GROUP BY d.day ORDER BY d.day`, from, to)
	if err != nil {
		s.internal(w, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var d OperationDaily
		if err = rows.Scan(&d.Date, &d.Requests, &d.Accepted, &d.Duplicates, &d.Rejected, &d.RateLimited); err != nil {
			s.internal(w, err)
			return
		}
		daily = append(daily, d)
	}
	if err = rows.Err(); err != nil {
		s.internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"timezone": s.cfg.Timezone.String(), "from": from, "to": to, "totals": totals, "apps": apps, "daily": daily})
}

type RetentionCohort struct {
	Cohort   string  `json:"cohort"`
	Size     int64   `json:"size"`
	Retained []int64 `json:"retained"`
}

func (s *Server) retention(w http.ResponseWriter, r *http.Request) {
	weeks := 8
	if v := r.URL.Query().Get("weeks"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 12 {
			fail(w, 400, "invalid_weeks", "weeks must be an integer between 1 and 12")
			return
		}
		weeks = n
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err := scanApp(tx.QueryRow(r.Context(), "SELECT "+appColumns+" FROM apps WHERE id=$1", r.PathValue("id")))
	if err != nil {
		s.appError(w, err)
		return
	}
	rows, err := tx.Query(r.Context(), "SELECT date_trunc('week',first_day::timestamp)::date::text,count(*) FROM visitors WHERE app_id=$1 GROUP BY 1 ORDER BY 1", a.ID)
	if err != nil {
		s.internal(w, err)
		return
	}
	sizes := map[string]int64{}
	var order []string
	for rows.Next() {
		var cohort string
		var size int64
		if err = rows.Scan(&cohort, &size); err != nil {
			rows.Close()
			s.internal(w, err)
			return
		}
		sizes[cohort] = size
		order = append(order, cohort)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		s.internal(w, err)
		return
	}
	rows.Close()
	rows, err = tx.Query(r.Context(), `WITH v AS (SELECT anonymous_id,date_trunc('week',first_day::timestamp)::date AS cohort FROM visitors WHERE app_id=$1)
SELECT v.cohort::text,(dv.day-v.cohort)/7,count(DISTINCT v.anonymous_id)
FROM v JOIN daily_visitors dv ON dv.app_id=$1 AND dv.anonymous_id=v.anonymous_id
WHERE (dv.day-v.cohort) BETWEEN 0 AND $2::int*7-1
GROUP BY v.cohort,(dv.day-v.cohort)/7`, a.ID, weeks)
	if err != nil {
		s.internal(w, err)
		return
	}
	hits := map[string]map[int]int64{}
	for rows.Next() {
		var cohort string
		var week int
		var n int64
		if err = rows.Scan(&cohort, &week, &n); err != nil {
			rows.Close()
			s.internal(w, err)
			return
		}
		if hits[cohort] == nil {
			hits[cohort] = map[int]int64{}
		}
		hits[cohort][week] = n
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		s.internal(w, err)
		return
	}
	rows.Close()
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	now := time.Now().In(s.cfg.Timezone)
	currentMonday := now.AddDate(0, 0, -(int(now.Weekday())+6)%7).Format("2006-01-02")
	cohorts := []RetentionCohort{}
	for _, cohort := range order {
		if cohort > currentMonday {
			continue
		}
		retained := make([]int64, weeks)
		for week, n := range hits[cohort] {
			if week >= 0 && week < weeks {
				retained[week] = n
			}
		}
		cohorts = append(cohorts, RetentionCohort{Cohort: cohort, Size: sizes[cohort], Retained: retained})
	}
	jsonResponse(w, 200, map[string]any{"weeks": weeks, "cohorts": cohorts, "timezone": s.cfg.Timezone.String(), "today": now.Format("2006-01-02")})
}
