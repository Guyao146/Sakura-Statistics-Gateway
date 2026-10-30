package main

import (
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
	"time"
)

type App struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	Enabled        bool       `json:"enabled"`
	AllowedOrigins []string   `json:"allowed_origins"`
	CreatedAt      time.Time  `json:"created_at"`
	LastEventAt    *time.Time `json:"last_event_at"`
}

const appColumns = "id,name,type,enabled,allowed_origins,created_at,last_event_at"

func scanApp(row pgx.Row) (App, error) {
	var a App
	err := row.Scan(&a.ID, &a.Name, &a.Type, &a.Enabled, &a.AllowedOrigins, &a.CreatedAt, &a.LastEventAt)
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
func (s *Server) createApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name           string   `json:"name"`
		Type           string   `json:"type"`
		AllowedOrigins []string `json:"allowed_origins"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validApp(in.Name, in.Type, in.AllowedOrigins) {
		fail(w, 400, "invalid_app", "Expected name (1–120 bytes), type web/desktop, and exact http(s) origins")
		return
	}
	if in.AllowedOrigins == nil {
		in.AllowedOrigins = []string{}
	}
	a, err := scanApp(s.db.QueryRow(r.Context(), "INSERT INTO apps(id,name,type,allowed_origins) VALUES($1,$2,$3,$4) RETURNING "+appColumns, randomID()[:24], in.Name, in.Type, in.AllowedOrigins))
	if err != nil {
		s.internal(w, err)
		return
	}
	jsonResponse(w, 201, map[string]any{"app": a})
}
func (s *Server) patchApp(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled        *bool     `json:"enabled"`
		Name           *string   `json:"name"`
		AllowedOrigins *[]string `json:"allowed_origins"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, err := s.findApp(r)
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
	if !validApp(a.Name, a.Type, a.AllowedOrigins) {
		fail(w, 400, "invalid_app", "Invalid application name or allowed_origins")
		return
	}
	a, err = scanApp(s.db.QueryRow(r.Context(), "UPDATE apps SET name=$2,enabled=$3,allowed_origins=$4 WHERE id=$1 RETURNING "+appColumns, a.ID, a.Name, a.Enabled, a.AllowedOrigins))
	if err != nil {
		s.appError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"app": a})
}
