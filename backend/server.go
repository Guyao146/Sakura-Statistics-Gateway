package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	db        *pgxpool.Pool
	cfg       Config
	limit     limiter
	dummyHash []byte
}
type bucket struct {
	tokens float64
	at     time.Time
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]bucket
}

func (l *limiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]bucket{}
	}
	b, ok := l.entries[key]
	if !ok {
		if len(l.entries) >= 4096 {
			for k, v := range l.entries {
				if now.Sub(v.at) > 10*time.Minute {
					delete(l.entries, k)
				}
			}
			if len(l.entries) >= 4096 {
				return false
			}
		}
		b = bucket{tokens: 60, at: now}
	}
	b.tokens += now.Sub(b.at).Seconds() * 2
	if b.tokens > 60 {
		b.tokens = 60
	}
	b.at = now
	allowed := b.tokens >= 1
	if allowed {
		b.tokens--
	}
	l.entries[key] = b
	return allowed
}
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	jsonResponse(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid_json", "Invalid JSON or request exceeds 1 MiB")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "invalid_json", "Expected one JSON value")
		return false
	}
	return true
}
func (s *Server) internal(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	fail(w, 500, "internal_error", "Internal server error")
}
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { jsonResponse(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", s.status)
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.admin(s.logout))
	mux.HandleFunc("GET /api/auth/me", s.admin(s.me))
	mux.HandleFunc("GET /api/apps", s.admin(s.listApps))
	mux.HandleFunc("POST /api/apps", s.admin(s.createApp))
	mux.HandleFunc("GET /api/apps/{id}", s.admin(s.getApp))
	mux.HandleFunc("PATCH /api/apps/{id}", s.admin(s.patchApp))
	mux.HandleFunc("GET /api/stats/overview", s.admin(s.overview))
	mux.HandleFunc("GET /api/apps/{id}/stats", s.admin(s.appStats))
	mux.HandleFunc("POST /api/collect", s.collect)
	mux.HandleFunc("OPTIONS /api/collect", s.collectOptions)
	mux.HandleFunc("/", s.static)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	state, status := "ok", 200
	database := "ok"
	if err := s.db.Ping(ctx); err != nil {
		state = "unavailable"
		database = "unavailable"
		status = 503
	}
	jsonResponse(w, status, map[string]string{"status": state, "database": database, "time": time.Now().UTC().Format(time.RFC3339)})
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api" {
		fail(w, 404, "not_found", "API route not found")
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "method_not_allowed", "Method not allowed")
		return
	}
	path := filepath.Join(s.cfg.StaticDir, filepath.FromSlash(strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")))
	if st, e := os.Stat(path); e == nil && !st.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	if r.URL.Path == "/sdk.js" || filepath.Ext(r.URL.Path) != "" {
		fail(w, 404, "not_found", "File not found")
		return
	}
	index := filepath.Join(s.cfg.StaticDir, "index.html")
	if _, err := os.Stat(index); errors.Is(err, os.ErrNotExist) {
		fail(w, 404, "not_found", "Frontend is not built")
		return
	}
	http.ServeFile(w, r, index)
}
