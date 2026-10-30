package main

import (
	"context"
	"github.com/jackc/pgx/v5"
	"net/http"
	"time"
)

type Metrics struct {
	Active      int64 `json:"active"`
	Starts      int64 `json:"starts"`
	Sessions    int64 `json:"sessions"`
	PageViews   int64 `json:"page_views"`
	NewVisitors int64 `json:"new_visitors"`
}

func (m *Metrics) add(v Metrics) {
	m.Active += v.Active
	m.Starts += v.Starts
	m.Sessions += v.Sessions
	m.PageViews += v.PageViews
	m.NewVisitors += v.NewVisitors
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
type AppMetrics struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	Enabled     bool       `json:"enabled"`
	LastEventAt *time.Time `json:"last_event_at"`
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
 m AS (SELECT app_id,sum(starts) starts,sum(page_views) pv FROM daily_metrics WHERE day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY app_id),
 v AS (SELECT app_id,count(*) n FROM visitors WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY app_id),
 ss AS (SELECT app_id,count(*) n FROM app_sessions WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY app_id)
 SELECT apps.id,coalesce(a.n,0),coalesce(m.starts,0)::bigint,coalesce(ss.n,0),coalesce(m.pv,0)::bigint,coalesce(v.n,0) FROM apps LEFT JOIN a ON a.app_id=apps.id LEFT JOIN m ON m.app_id=apps.id LEFT JOIN v ON v.app_id=apps.id LEFT JOIN ss ON ss.app_id=apps.id WHERE ($1='' OR apps.id=$1)`, appID, from, to)
	if err != nil {
		return totals, daily, perApp, err
	}
	for rows.Next() {
		var id string
		var m Metrics
		if err = rows.Scan(&id, &m.Active, &m.Starts, &m.Sessions, &m.PageViews, &m.NewVisitors); err != nil {
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
 m AS (SELECT day,sum(starts) starts,sum(page_views) pv FROM daily_metrics WHERE day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY day),
 v AS (SELECT first_day AS day,count(*) n FROM visitors WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY first_day),
 ss AS (SELECT first_day AS day,count(*) n FROM app_sessions WHERE first_day BETWEEN $2::text::date AND $3::text::date AND ($1='' OR app_id=$1) GROUP BY first_day)
 SELECT d.day::date::text,coalesce(a.n,0),coalesce(m.starts,0)::bigint,coalesce(ss.n,0),coalesce(m.pv,0)::bigint,coalesce(v.n,0) FROM generate_series($2::text::date,$3::text::date,'1 day'::interval) d(day) LEFT JOIN a ON a.day=d.day::date LEFT JOIN m ON m.day=d.day::date LEFT JOIN v ON v.day=d.day::date LEFT JOIN ss ON ss.day=d.day::date ORDER BY d.day`, appID, from, to)
	if err != nil {
		return totals, daily, perApp, err
	}
	defer rows.Close()
	for rows.Next() {
		var d Daily
		if err = rows.Scan(&d.Date, &d.Active, &d.Starts, &d.Sessions, &d.PageViews, &d.NewVisitors); err != nil {
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
		apps = append(apps, AppMetrics{ID: a.ID, Name: a.Name, Type: a.Type, Enabled: a.Enabled, LastEventAt: a.LastEventAt, Metrics: perApp[a.ID]})
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
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"timezone": s.cfg.Timezone.String(), "from": from, "to": to, "totals": totals, "daily": daily, "versions": versions, "pages": pages, "last_event_at": a.LastEventAt})
}
