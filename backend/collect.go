package main

import (
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
)

type Event struct {
	SchemaVersion int       `json:"schema_version"`
	EventID       string    `json:"event_id"`
	EventType     string    `json:"event_type"`
	OccurredAt    time.Time `json:"occurred_at"`
	AnonymousID   string    `json:"anonymous_id"`
	SessionID     string    `json:"session_id"`
	AppVersion    string    `json:"app_version"`
	Platform      string    `json:"platform"`
	SDKVersion    string    `json:"sdk_version"`
	Path          string    `json:"path"`
}
type Rejection struct {
	EventID string `json:"event_id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type CollectResult struct {
	Accepted   int         `json:"accepted"`
	Duplicates int         `json:"duplicates"`
	Rejected   []Rejection `json:"rejected"`
}

func validText(s string, max int, required bool) bool {
	if len(s) > max || (required && strings.TrimSpace(s) == "") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func cleanPath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "", false
	}
	return u.EscapedPath(), true
}
func validateEvent(e *Event, kind string, now time.Time) (string, string) {
	if e.SchemaVersion != 1 {
		return "invalid_schema", "schema_version must be 1"
	}
	if !validText(e.EventID, 128, true) || !validText(e.AnonymousID, 128, true) || !validText(e.SessionID, 128, true) {
		return "invalid_identifier", "event_id, anonymous_id and session_id must be 1–128 bytes without control characters"
	}
	if e.OccurredAt.Before(now.Add(-7*24*time.Hour)) || e.OccurredAt.After(now.Add(5*time.Minute)) {
		return "invalid_time", "occurred_at must be within the past 7 days and at most 5 minutes ahead"
	}
	if !validText(e.AppVersion, 128, false) || !validText(e.Platform, 64, false) || !validText(e.SDKVersion, 64, false) {
		return "invalid_metadata", "Invalid version or platform"
	}
	switch e.EventType {
	case "active":
	case "app_start":
		if kind != "desktop" {
			return "invalid_event_type", "app_start is only valid for desktop applications"
		}
	case "page_view":
		if kind != "web" {
			return "invalid_event_type", "page_view is only valid for web applications"
		}
	default:
		return "invalid_event_type", "Unknown event_type"
	}
	if len(e.Path) > 2048 {
		return "invalid_path", "path exceeds 2048 bytes"
	}
	if e.EventType == "page_view" {
		path, ok := cleanPath(e.Path)
		if !ok {
			return "invalid_path", "page_view requires an absolute path without a host"
		}
		e.Path = path
	} else {
		e.Path = ""
	}
	return "", ""
}
func originAllowed(a App, origin string) bool {
	if origin == "" {
		return a.Type == "desktop"
	}
	for _, o := range a.AllowedOrigins {
		if origin == o {
			return true
		}
	}
	return false
}
func cors(w http.ResponseWriter, origin string) {
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Add("Vary", "Origin")
	}
}
func (s *Server) collectOptions(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if !validOrigin(origin) {
		fail(w, 403, "origin_forbidden", "Origin is not allowed")
		return
	}
	var ok bool
	err := s.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM apps WHERE enabled AND $1=ANY(allowed_origins))", origin).Scan(&ok)
	if err != nil {
		s.internal(w, err)
		return
	}
	if !ok {
		fail(w, 403, "origin_forbidden", "Origin is not allowed")
		return
	}
	cors(w, origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(204)
}
func (s *Server) collect(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AppID  string            `json:"app_id"`
		Events []json.RawMessage `json:"events"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.AppID == "" || len(in.AppID) > 128 || len(in.Events) == 0 || len(in.Events) > 500 {
		fail(w, 400, "invalid_batch", "app_id and 1–500 events are required")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	a, err := scanApp(tx.QueryRow(r.Context(), "SELECT "+appColumns+" FROM apps WHERE id=$1 FOR UPDATE", in.AppID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fail(w, 404, "app_not_found", "Application not found")
		} else {
			s.internal(w, err)
		}
		return
	}
	origin := r.Header.Get("Origin")
	if !originAllowed(a, origin) {
		fail(w, 403, "origin_forbidden", "Origin is not allowed for this application")
		return
	}
	cors(w, origin)
	if !a.Enabled {
		fail(w, 403, "app_disabled", "Application is disabled")
		return
	}
	if !s.limit.allow("collect:"+a.ID, time.Now()) {
		fail(w, 429, "rate_limited", "Too many requests")
		return
	}
	result := CollectResult{Rejected: []Rejection{}}
	now := time.Now()
	for _, raw := range in.Events {
		var event Event
		var identity struct {
			EventID string `json:"event_id"`
		}
		_ = json.Unmarshal(raw, &identity)
		if err = json.Unmarshal(raw, &event); err != nil {
			result.Rejected = append(result.Rejected, Rejection{identity.EventID, "invalid_event", "Invalid event field type or timestamp"})
			continue
		}
		code, message := validateEvent(&event, a.Type, now)
		if code != "" {
			result.Rejected = append(result.Rejected, Rejection{event.EventID, code, message})
			continue
		}
		day := event.OccurredAt.In(s.cfg.Timezone).Format("2006-01-02")
		tag, e := tx.Exec(r.Context(), "INSERT INTO events(app_id,event_id,event_type,occurred_at,day,anonymous_id,session_id,app_version,platform,sdk_version,path) VALUES($1,$2,$3,$4,$5::text::date,$6,$7,$8,$9,$10,$11) ON CONFLICT(app_id,event_id) DO NOTHING", a.ID, event.EventID, event.EventType, event.OccurredAt, day, event.AnonymousID, event.SessionID, event.AppVersion, event.Platform, event.SDKVersion, event.Path)
		if e != nil {
			s.internal(w, e)
			return
		}
		if tag.RowsAffected() == 0 {
			result.Duplicates++
			continue
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO visitors(app_id,anonymous_id,first_day) VALUES($1,$2,$3::text::date) ON CONFLICT(app_id,anonymous_id) DO UPDATE SET first_day=LEAST(visitors.first_day,EXCLUDED.first_day)", a.ID, event.AnonymousID, day); err != nil {
			s.internal(w, err)
			return
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO daily_visitors(app_id,day,anonymous_id) VALUES($1,$2::text::date,$3) ON CONFLICT DO NOTHING", a.ID, day, event.AnonymousID); err != nil {
			s.internal(w, err)
			return
		}
		if a.Type == "web" {
			if _, err = tx.Exec(r.Context(), "INSERT INTO app_sessions(app_id,session_id,first_day) VALUES($1,$2,$3::text::date) ON CONFLICT(app_id,session_id) DO UPDATE SET first_day=LEAST(app_sessions.first_day,EXCLUDED.first_day)", a.ID, event.SessionID, day); err != nil {
				s.internal(w, err)
				return
			}
		}
		starts, pv := 0, 0
		if event.EventType == "app_start" {
			starts = 1
		}
		if event.EventType == "page_view" {
			pv = 1
		}
		if _, err = tx.Exec(r.Context(), "INSERT INTO daily_metrics(app_id,day,starts,page_views) VALUES($1,$2::text::date,$3,$4) ON CONFLICT(app_id,day) DO UPDATE SET starts=daily_metrics.starts+EXCLUDED.starts,page_views=daily_metrics.page_views+EXCLUDED.page_views", a.ID, day, starts, pv); err != nil {
			s.internal(w, err)
			return
		}
		if _, err = tx.Exec(r.Context(), "UPDATE apps SET last_event_at=GREATEST(last_event_at,$2) WHERE id=$1", a.ID, event.OccurredAt); err != nil {
			s.internal(w, err)
			return
		}
		result.Accepted++
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 200, result)
}
