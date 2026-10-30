package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TEST_DATABASE_URL must point to a database where the test user can create schemas.
// All objects are isolated in a random schema and dropped on cleanup.
func testServer(t *testing.T) *Server {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + randomID()[:16]
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); admin.Close() })
	if err = migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = migrate(ctx, pool); err != nil {
		t.Fatal("migration replay:", err)
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	c := Config{Username: "admin", Password: "StrongPass123!", Timezone: loc}
	if err = bootstrapAdmin(ctx, pool, c); err != nil {
		t.Fatal(err)
	}
	dummy, _ := bcrypt.GenerateFromPassword([]byte("dummy"), bcrypt.MinCost)
	return &Server{db: pool, cfg: c, dummyHash: dummy}
}
func requestJSON(t *testing.T, h http.Handler, method, path string, body any, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "http://example.com"+path, bytes.NewReader(data))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestPostgresCollectionAndStatistics(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	h := s.handler()
	appID := "web-test"
	_, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type,allowed_origins) VALUES($1,'Website','web',ARRAY['http://example.com'])", appID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Minute)
	yesterday := now.Add(-24 * time.Hour)
	e := validEvent(now)
	e.Path = "/guide?secret=discard#fragment"
	e.AppVersion = "1.0"
	e.Platform = "web"
	older := e
	older.EventID = "older"
	older.OccurredAt = yesterday
	active := e
	active.EventID = "active"
	active.EventType = "active"
	invalid := e
	invalid.EventID = "bad"
	invalid.SchemaVersion = 2
	payload := map[string]any{"app_id": appID, "events": []Event{e, e, older, active, invalid}}
	w := requestJSON(t, h, "POST", "/api/collect", payload, "http://example.com", nil)
	if w.Code != 200 {
		t.Fatalf("collect: %d %s", w.Code, w.Body.String())
	}
	var result CollectResult
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 3 || result.Duplicates != 1 || len(result.Rejected) != 1 {
		t.Fatalf("counts: %+v", result)
	}
	w = requestJSON(t, h, "POST", "/api/collect", payload, "http://example.com", nil)
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 0 || result.Duplicates != 4 || len(result.Rejected) != 1 {
		t.Fatalf("retry: %+v", result)
	}
	var path string
	if err = s.db.QueryRow(ctx, "SELECT path FROM events WHERE event_id='event-1'").Scan(&path); err != nil || path != "/guide" {
		t.Fatalf("privacy path: %q %v", path, err)
	}
	from, to := yesterday.In(s.cfg.Timezone).Format("2006-01-02"), now.In(s.cfg.Timezone).Format("2006-01-02")
	tx, err := s.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	totals, daily, _, err := statsMetrics(ctx, tx, appID, from, to)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if totals != (Metrics{Active: 1, Sessions: 1, PageViews: 2, NewVisitors: 1}) {
		t.Fatalf("totals: %+v", totals)
	}
	if len(daily) != 2 || daily[0].NewVisitors != 1 || daily[0].Sessions != 1 || daily[1].NewVisitors != 0 || daily[1].Sessions != 0 || daily[0].Active != 1 || daily[1].Active != 1 {
		t.Fatalf("daily/backfill: %+v", daily)
	}
	w = requestJSON(t, h, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "StrongPass123!"}, "http://example.com", nil)
	if w.Code != 200 {
		t.Fatalf("login: %s", w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie: %+v", cookies)
	}
	cookie := cookies[0]
	w = requestJSON(t, h, "GET", fmt.Sprintf("/api/apps/%s/stats?from=%s&to=%s", appID, from, to), nil, "", cookie)
	if w.Code != 200 {
		t.Fatalf("stats: %s", w.Body.String())
	}
	var stats struct {
		Versions []Version `json:"versions"`
		Pages    []Page    `json:"pages"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if len(stats.Versions) != 1 || stats.Versions[0].Active != 1 || len(stats.Pages) != 1 || stats.Pages[0].PageViews != 2 {
		t.Fatalf("breakdowns: %+v", stats)
	}
	w = requestJSON(t, h, "POST", "/api/auth/logout", nil, "http://example.com", cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = requestJSON(t, h, "GET", "/api/auth/me", nil, "", cookie)
	if w.Code != 401 {
		t.Fatalf("logout session remained valid: %d", w.Code)
	}
}
func TestPostgresAppLifecycleAndOverview(t *testing.T) {
	s := testServer(t)
	h := s.handler()
	origin := "http://example.com"
	w := requestJSON(t, h, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "StrongPass123!"}, origin, nil)
	if w.Code != 200 {
		t.Fatalf("login: %s", w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	w = requestJSON(t, h, "POST", "/api/apps", map[string]any{"name": "Demo website", "type": "web", "allowed_origins": []string{origin}}, origin, cookie)
	if w.Code != 201 {
		t.Fatalf("create: %s", w.Body.String())
	}
	var created struct {
		App App `json:"app"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id := created.App.ID
	for _, path := range []string{"/api/apps", "/api/apps/" + id, "/api/auth/me"} {
		w = requestJSON(t, h, "GET", path, nil, "", cookie)
		if w.Code != 200 {
			t.Fatalf("read %s: %s", path, w.Body.String())
		}
	}
	e := validEvent(time.Now().Add(-time.Minute))
	payload := map[string]any{"app_id": id, "events": []Event{e}}
	w = requestJSON(t, h, "POST", "/api/collect", payload, origin, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	day := e.OccurredAt.In(s.cfg.Timezone).Format("2006-01-02")
	w = requestJSON(t, h, "GET", "/api/stats/overview?from="+day+"&to="+day, nil, "", cookie)
	if w.Code != 200 {
		t.Fatalf("overview: %s", w.Body.String())
	}
	var overview struct {
		Timezone string       `json:"timezone"`
		Totals   Metrics      `json:"totals"`
		Apps     []AppMetrics `json:"apps"`
		Daily    []Daily      `json:"daily"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if overview.Timezone != "Asia/Shanghai" || overview.Totals.Active != 1 || overview.Totals.PageViews != 1 || len(overview.Apps) != 1 || len(overview.Daily) != 1 || overview.Apps[0].LastEventAt == nil {
		t.Fatalf("overview: %+v", overview)
	}
	for _, enabled := range []bool{false, true} {
		w = requestJSON(t, h, "PATCH", "/api/apps/"+id, map[string]any{"enabled": enabled, "name": "Renamed website", "allowed_origins": []string{origin}}, origin, cookie)
		if w.Code != 200 {
			t.Fatalf("patch: %s", w.Body.String())
		}
		var updated struct {
			App App `json:"app"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
			t.Fatal(err)
		}
		if updated.App.Enabled != enabled || updated.App.Name != "Renamed website" {
			t.Fatalf("patch result: %+v", updated.App)
		}
		if enabled {
			e.EventID = "after-enable"
			payload["events"] = []Event{e}
			w = requestJSON(t, h, "POST", "/api/collect", payload, origin, nil)
			if w.Code != 200 {
				t.Fatalf("re-enabled collect: %s", w.Body.String())
			}
		}
	}
	for _, path := range []string{"/healthz", "/readyz", "/api/status"} {
		w = requestJSON(t, h, "GET", path, nil, "", nil)
		if w.Code != 200 {
			t.Fatalf("health %s: %s", path, w.Body.String())
		}
	}
}

func TestPostgresDesktopStartsAndAppSum(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	for _, id := range []string{"desktop-a", "desktop-b"} {
		if _, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type) VALUES($1,$1,'desktop')", id); err != nil {
			t.Fatal(err)
		}
		e := validEvent(time.Now().Add(-time.Minute))
		e.EventType = "app_start"
		e.AppVersion = "1.0"
		e.Platform = "windows"
		e2 := e
		e2.EventID = "event-2"
		w := requestJSON(t, s.handler(), "POST", "/api/collect", map[string]any{"app_id": id, "events": []Event{e, e2}}, "", nil)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	day := time.Now().Add(-time.Minute).In(s.cfg.Timezone).Format("2006-01-02")
	tx, err := s.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	totals, _, perApp, err := statsMetrics(ctx, tx, "", day, day)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if totals != (Metrics{Active: 2, Starts: 4, NewVisitors: 2}) || perApp["desktop-a"].Sessions != 0 {
		t.Fatalf("desktop/application sum: %+v %+v", totals, perApp)
	}
}
