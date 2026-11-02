package main

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
	"unicode"
)

type App struct {
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Type                  string     `json:"type"`
	Enabled               bool       `json:"enabled"`
	AllowedOrigins        []string   `json:"allowed_origins"`
	AllowedPathPrefixes   []string   `json:"allowed_path_prefixes"`
	NormalizeNumericPaths bool       `json:"normalize_numeric_paths"`
	CreatedAt             time.Time  `json:"created_at"`
	LastEventAt           *time.Time `json:"last_event_at"`
}

const appColumns = "id,name,type,enabled,allowed_origins,allowed_path_prefixes,normalize_numeric_paths,created_at,last_event_at"

func scanApp(row pgx.Row) (App, error) {
	var a App
	err := row.Scan(&a.ID, &a.Name, &a.Type, &a.Enabled, &a.AllowedOrigins, &a.AllowedPathPrefixes, &a.NormalizeNumericPaths, &a.CreatedAt, &a.LastEventAt)
	return a, err
}
func (s *Server) findApp(r *http.Request) (App, error) {
	return scanApp(s.db.QueryRow(r.Context(), "SELECT "+appColumns+" FROM apps WHERE id=$1", r.PathValue("id")))
}
func (s *Server) appError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, 404, "app_not_found", "Application not found")
	} else {
		s.internal(w, err)
	}
}
func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), "SELECT "+appColumns+" FROM apps ORDER BY created_at,id")
	if err != nil {
		s.internal(w, err)
		return
	}
	defer rows.Close()
	apps := []App{}
	for rows.Next() {
		a, e := scanApp(rows)
		if e != nil {
			s.internal(w, e)
			return
		}
		apps = append(apps, a)
	}
	if err = rows.Err(); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"apps": apps})
}
func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	a, err := s.findApp(r)
	if err != nil {
		s.appError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"app": a})
}
func validApp(name, kind string, origins []string) bool {
	if len(name) == 0 || len(name) > 120 || (kind != "web" && kind != "desktop") || len(origins) > 50 {
		return false
	}
	for _, origin := range origins {
		if len(origin) > 512 || !validOrigin(origin) {
			return false
		}
	}
	return true
}

// normalizePathPrefixes dedupes the whitelist in order and enforces the per-item rules.
func normalizePathPrefixes(prefixes []string) ([]string, bool) {
	seen := make(map[string]bool, len(prefixes))
	out := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		if len(prefix) == 0 || len(prefix) > 256 || !strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") {
			return nil, false
		}
		for _, r := range prefix {
			if unicode.IsControl(r) {
				return nil, false
			}
		}
		if !seen[prefix] {
			seen[prefix] = true
			out = append(out, prefix)
		}
	}
	if len(out) > 50 {
		return nil, false
	}
	return out, true
}
func (s *Server) createApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name                  string   `json:"name"`
		Type                  string   `json:"type"`
		AllowedOrigins        []string `json:"allowed_origins"`
		AllowedPathPrefixes   []string `json:"allowed_path_prefixes"`
		NormalizeNumericPaths bool     `json:"normalize_numeric_paths"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validApp(in.Name, in.Type, in.AllowedOrigins) {
		fail(w, 400, "invalid_app", "Expected name (1–120 bytes), type web/desktop, and exact http(s) origins")
		return
	}
	prefixes, ok := normalizePathPrefixes(in.AllowedPathPrefixes)
	if !ok {
		fail(w, 400, "invalid_path_rules", "allowed_path_prefixes must hold up to 50 unique /-prefixed items of 1–256 bytes without control characters")
		return
	}
	if in.AllowedOrigins == nil {
		in.AllowedOrigins = []string{}
	}
	a, err := scanApp(s.db.QueryRow(r.Context(), "INSERT INTO apps(id,name,type,allowed_origins,allowed_path_prefixes,normalize_numeric_paths) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+appColumns, randomID()[:24], in.Name, in.Type, in.AllowedOrigins, prefixes, in.NormalizeNumericPaths))
	if err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 201, map[string]any{"app": a})
}
func (s *Server) patchApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled               *bool     `json:"enabled"`
		Name                  *string   `json:"name"`
		AllowedOrigins        *[]string `json:"allowed_origins"`
		AllowedPathPrefixes   *[]string `json:"allowed_path_prefixes"`
		NormalizeNumericPaths *bool     `json:"normalize_numeric_paths"`
	}
	if !decode(w, r, &in) {
		return
	}
	var prefixes []string
	if in.AllowedPathPrefixes != nil {
		var ok bool
		if prefixes, ok = normalizePathPrefixes(*in.AllowedPathPrefixes); !ok {
			fail(w, 400, "invalid_path_rules", "allowed_path_prefixes must hold up to 50 unique /-prefixed items of 1–256 bytes without control characters")
			return
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err := scanApp(tx.QueryRow(r.Context(), "SELECT "+appColumns+" FROM apps WHERE id=$1 FOR UPDATE", r.PathValue("id")))
	if err != nil {
		s.appError(w, err)
		return
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	if in.Name != nil {
		a.Name = strings.TrimSpace(*in.Name)
	}
	if in.AllowedOrigins != nil {
		a.AllowedOrigins = *in.AllowedOrigins
	}
	if in.AllowedPathPrefixes != nil {
		a.AllowedPathPrefixes = prefixes
	}
	if in.NormalizeNumericPaths != nil {
		a.NormalizeNumericPaths = *in.NormalizeNumericPaths
	}
	if !validApp(a.Name, a.Type, a.AllowedOrigins) {
		fail(w, 400, "invalid_app", "Invalid application name or allowed_origins")
		return
	}
	a, err = scanApp(tx.QueryRow(r.Context(), "UPDATE apps SET name=$2,enabled=$3,allowed_origins=$4,allowed_path_prefixes=$5,normalize_numeric_paths=$6 WHERE id=$1 RETURNING "+appColumns, a.ID, a.Name, a.Enabled, a.AllowedOrigins, a.AllowedPathPrefixes, a.NormalizeNumericPaths))
	if err != nil {
		s.appError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"app": a})
}
func (s *Server) deleteApp(w http.ResponseWriter, r *http.Request) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	// Share the retention lock before locking the application, so purge and rebuild cannot race.
	var cutoff *string
	if err = tx.QueryRow(r.Context(), "SELECT cutoff::text FROM retention_state WHERE id=1 FOR SHARE").Scan(&cutoff); err != nil {
		s.internal(w, err)
		return
	}
	id := r.PathValue("id")
	var locked string
	if err = tx.QueryRow(r.Context(), "SELECT id FROM apps WHERE id=$1 FOR UPDATE", id).Scan(&locked); err != nil {
		s.appError(w, err)
		return
	}
	for _, stmt := range []string{
		"DELETE FROM events WHERE app_id=$1",
		"DELETE FROM visitors WHERE app_id=$1",
		"DELETE FROM daily_visitors WHERE app_id=$1",
		"DELETE FROM app_sessions WHERE app_id=$1",
		"DELETE FROM daily_metrics WHERE app_id=$1",
		"DELETE FROM operation_metrics WHERE app_id=$1",
		"DELETE FROM apps WHERE id=$1",
	} {
		if _, err = tx.Exec(r.Context(), stmt, id); err != nil {
			s.internal(w, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (s *Server) rebuildApp(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.dateRange(w, r)
	if !ok {
		return
	}
	f, _ := time.Parse("2006-01-02", from)
	t, _ := time.Parse("2006-01-02", to)
	rebuiltDays := int64(t.Sub(f).Hours()/24) + 1
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	var purgedBefore *string
	if err = tx.QueryRow(r.Context(), "SELECT cutoff::text FROM retention_state WHERE id=1 FOR SHARE").Scan(&purgedBefore); err != nil {
		s.internal(w, err)
		return
	}
	a, err := scanApp(tx.QueryRow(r.Context(), "SELECT "+appColumns+" FROM apps WHERE id=$1 FOR UPDATE", r.PathValue("id")))
	if err != nil {
		s.appError(w, err)
		return
	}
	if (purgedBefore != nil && from < *purgedBefore) || (s.cfg.RetentionDays > 0 && from < time.Now().In(s.cfg.Timezone).AddDate(0, 0, -s.cfg.RetentionDays).Format("2006-01-02")) {
		fail(w, 400, "rebuild_outside_retention", "Requested range includes unavailable raw events; historical aggregates were not changed")
		return
	}
	var events int64
	if err = tx.QueryRow(r.Context(), "SELECT count(*) FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date", a.ID, from, to).Scan(&events); err != nil {
		s.internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "DELETE FROM daily_metrics WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date", a.ID, from, to); err != nil {
		s.internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO daily_metrics(app_id,day,starts,page_views,custom_events)
SELECT app_id,day,count(*) FILTER (WHERE event_type='app_start'),count(*) FILTER (WHERE event_type='page_view'),count(*) FILTER (WHERE event_type='custom')
FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date GROUP BY app_id,day
ON CONFLICT (app_id,day) DO UPDATE SET starts=EXCLUDED.starts,page_views=EXCLUDED.page_views,custom_events=EXCLUDED.custom_events`, a.ID, from, to); err != nil {
		s.internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), "DELETE FROM daily_visitors WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date", a.ID, from, to); err != nil {
		s.internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO daily_visitors(app_id,day,anonymous_id)
SELECT DISTINCT app_id,day,anonymous_id FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date
ON CONFLICT DO NOTHING`, a.ID, from, to); err != nil {
		s.internal(w, err)
		return
	}
	if a.Type == "web" {
		if _, err = tx.Exec(r.Context(), "DELETE FROM app_sessions WHERE app_id=$1 AND first_day BETWEEN $2::text::date AND $3::text::date", a.ID, from, to); err != nil {
			s.internal(w, err)
			return
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO app_sessions(app_id,session_id,first_day)
SELECT e.app_id,e.session_id,min(e.day) FROM events e
WHERE e.app_id=$1 AND e.session_id IN (
    SELECT DISTINCT session_id FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date
)
GROUP BY e.app_id,e.session_id
ON CONFLICT (app_id,session_id) DO UPDATE SET first_day=LEAST(app_sessions.first_day,EXCLUDED.first_day)`, a.ID, from, to); err != nil {
			s.internal(w, err)
			return
		}
	}
	var visitors int64
	if err = tx.QueryRow(r.Context(), "SELECT count(DISTINCT anonymous_id) FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date", a.ID, from, to).Scan(&visitors); err != nil {
		s.internal(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO visitors(app_id,anonymous_id,first_day)
SELECT app_id,anonymous_id,min(day) FROM events WHERE app_id=$1 AND day BETWEEN $2::text::date AND $3::text::date GROUP BY app_id,anonymous_id
ON CONFLICT (app_id,anonymous_id) DO UPDATE SET first_day=LEAST(visitors.first_day,EXCLUDED.first_day)`, a.ID, from, to); err != nil {
		s.internal(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, map[string]int64{"rebuilt_days": rebuiltDays, "events": events, "visitors": visitors})
}
