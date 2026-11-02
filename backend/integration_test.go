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
	"slices"
	"strings"
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
	c := Config{Username: "admin", Password: "StrongPass123!", Timezone: loc, RateLimitPerSecond: 2, RateLimitBurst: 60}
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
func loginAdmin(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	w := requestJSON(t, h, "POST", "/api/auth/login", map[string]string{"username": "admin", "password": "StrongPass123!"}, "http://example.com", nil)
	if w.Code != 200 {
		t.Fatalf("login: %s", w.Body.String())
	}
	return w.Result().Cookies()[0]
}
func countRows(t *testing.T, s *Server, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
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

func TestPostgresCustomEventsPathRulesAndOperationMetrics(t *testing.T) {
	s := testServer(t)
	h := s.handler()
	ctx := context.Background()
	origin := "http://example.com"
	cookie := loginAdmin(t, h)
	w := requestJSON(t, h, "POST", "/api/apps", map[string]any{"name": "Rules", "type": "web", "allowed_origins": []string{origin}, "allowed_path_prefixes": []string{"/docs/", "/users/", "/users/"}, "normalize_numeric_paths": true}, origin, cookie)
	if w.Code != 201 {
		t.Fatalf("create: %s", w.Body.String())
	}
	var created struct {
		App App `json:"app"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	appID := created.App.ID
	if !created.App.NormalizeNumericPaths || len(created.App.AllowedPathPrefixes) != 2 {
		t.Fatalf("path rules not stored: %+v", created.App)
	}
	w = requestJSON(t, h, "PATCH", "/api/apps/"+appID, map[string]any{"allowed_path_prefixes": []string{"docs", "//bad"}}, origin, cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_path_rules") {
		t.Fatalf("invalid rules: %d %s", w.Code, w.Body.String())
	}
	w = requestJSON(t, h, "GET", "/api/apps/"+appID, nil, "", cookie)
	var current struct {
		App App `json:"app"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if len(current.App.AllowedPathPrefixes) != 2 {
		t.Fatalf("rules changed after rejection: %+v", current.App)
	}
	now := time.Now().Add(-time.Minute)
	pv1 := validEvent(now)
	pv1.Path = "/users/123/profile"
	pv2 := validEvent(now)
	pv2.EventID = "pv-2"
	pv2.AnonymousID = "anon-2"
	pv2.SessionID = "session-2"
	pv2.Path = "/docs/guide"
	blocked := validEvent(now)
	blocked.EventID = "blocked"
	blocked.AnonymousID = "anon-3"
	blocked.SessionID = "session-3"
	blocked.Path = "/admin/secret"
	custom1 := validEvent(now)
	custom1.EventID = "custom-1"
	custom1.EventType = "custom"
	custom1.EventName = "signup"
	custom1.Path = "/should-not-be-stored"
	custom2 := custom1
	custom2.EventID = "custom-2"
	custom2.AnonymousID = "anon-3"
	custom2.SessionID = "session-3"
	custom2.EventName = "支付完成"
	custom2.Path = ""
	invalidName := custom1
	invalidName.EventID = "invalid-name"
	invalidName.EventName = "   "
	payload := map[string]any{"app_id": appID, "events": []Event{pv1, pv2, blocked, custom1, custom2, invalidName}}
	w = requestJSON(t, h, "POST", "/api/collect", payload, origin, nil)
	if w.Code != 200 {
		t.Fatalf("collect: %d %s", w.Code, w.Body.String())
	}
	var result CollectResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Accepted != 4 || result.Duplicates != 0 || len(result.Rejected) != 2 {
		t.Fatalf("counts: %+v", result)
	}
	codes := []string{result.Rejected[0].Code, result.Rejected[1].Code}
	if !slices.Equal(codes, []string{"invalid_path", "invalid_event_name"}) && !slices.Equal(codes, []string{"invalid_event_name", "invalid_path"}) {
		t.Fatalf("codes: %+v", result.Rejected)
	}
	var path, eventName string
	if err := s.db.QueryRow(ctx, "SELECT path,event_name FROM events WHERE app_id=$1 AND event_id='custom-1'", appID).Scan(&path, &eventName); err != nil || path != "" || eventName != "signup" {
		t.Fatalf("custom row: %q %q %v", path, eventName, err)
	}
	if err := s.db.QueryRow(ctx, "SELECT path,event_name FROM events WHERE app_id=$1 AND event_id=$2", appID, pv1.EventID).Scan(&path, &eventName); err != nil || path != "/users/:id/profile" || eventName != "" {
		t.Fatalf("normalized row: %q %q %v", path, eventName, err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM daily_visitors WHERE app_id=$1", appID); n != 3 {
		t.Fatalf("custom must count as active: %d daily visitor rows", n)
	}
	var starts, pageViews, customEvents int64
	if err := s.db.QueryRow(ctx, "SELECT starts,page_views,custom_events FROM daily_metrics WHERE app_id=$1", appID).Scan(&starts, &pageViews, &customEvents); err != nil || starts != 0 || pageViews != 2 || customEvents != 2 {
		t.Fatalf("daily metrics: %d %d %d %v", starts, pageViews, customEvents, err)
	}
	var requests, accepted, duplicates, rejected, rateLimited int64
	if err := s.db.QueryRow(ctx, "SELECT requests,accepted,duplicates,rejected,rate_limited FROM operation_metrics WHERE app_id=$1", appID).Scan(&requests, &accepted, &duplicates, &rejected, &rateLimited); err != nil || requests != 1 || accepted != 4 || duplicates != 0 || rejected != 2 || rateLimited != 0 {
		t.Fatalf("operation metrics: %d %d %d %d %d %v", requests, accepted, duplicates, rejected, rateLimited, err)
	}
	// Custom events are valid for desktop applications too.
	w = requestJSON(t, h, "POST", "/api/apps", map[string]any{"name": "Desk", "type": "desktop"}, origin, cookie)
	if w.Code != 201 {
		t.Fatalf("desktop create: %s", w.Body.String())
	}
	var desk struct {
		App App `json:"app"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &desk); err != nil {
		t.Fatal(err)
	}
	desktop := validEvent(now)
	desktop.EventType = "custom"
	desktop.EventName = "export"
	w = requestJSON(t, h, "POST", "/api/collect", map[string]any{"app_id": desk.App.ID, "events": []Event{desktop}}, "", nil)
	if w.Code != 200 {
		t.Fatalf("desktop collect: %s", w.Body.String())
	}
	// Statistics expose custom_events in totals/daily and a per-name breakdown.
	day := now.In(s.cfg.Timezone).Format("2006-01-02")
	w = requestJSON(t, h, "GET", "/api/apps/"+appID+"/stats?from="+day+"&to="+day, nil, "", cookie)
	if w.Code != 200 {
		t.Fatalf("stats: %d %s", w.Code, w.Body.String())
	}
	var stats struct {
		Totals       Metrics       `json:"totals"`
		Daily        []Daily       `json:"daily"`
		CustomEvents []CustomEvent `json:"custom_events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.Totals.PageViews != 2 || stats.Totals.CustomEvents != 2 || len(stats.Daily) != 1 || stats.Daily[0].CustomEvents != 2 {
		t.Fatalf("stats exposure: %+v %+v", stats.Totals, stats.Daily)
	}
	if len(stats.CustomEvents) != 2 {
		t.Fatalf("breakdown: %+v", stats.CustomEvents)
	}
	byName := map[string]CustomEvent{}
	for _, c := range stats.CustomEvents {
		byName[c.Name] = c
	}
	if byName["signup"] != (CustomEvent{Name: "signup", Count: 1, Visitors: 1}) || byName["支付完成"] != (CustomEvent{Name: "支付完成", Count: 1, Visitors: 1}) {
		t.Fatalf("breakdown entries: %+v", byName)
	}
	w = requestJSON(t, h, "GET", "/api/stats/overview?from="+day+"&to="+day, nil, "", cookie)
	var overview struct {
		Totals Metrics `json:"totals"`
		Daily  []Daily `json:"daily"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if overview.Totals.CustomEvents != 3 || len(overview.Daily) != 1 || overview.Daily[0].CustomEvents != 3 {
		t.Fatalf("overview exposure: %+v %+v", overview.Totals, overview.Daily)
	}
}

func TestPostgresDeleteAppCascade(t *testing.T) {
	s := testServer(t)
	h := s.handler()
	ctx := context.Background()
	origin := "http://example.com"
	appID := "web-del"
	if _, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type,allowed_origins) VALUES($1,'Del','web',ARRAY['http://example.com'])", appID); err != nil {
		t.Fatal(err)
	}
	e := validEvent(time.Now().Add(-time.Minute))
	w := requestJSON(t, h, "POST", "/api/collect", map[string]any{"app_id": appID, "events": []Event{e}}, origin, nil)
	if w.Code != 200 {
		t.Fatalf("collect: %s", w.Body.String())
	}
	for _, table := range []string{"events", "visitors", "daily_visitors", "app_sessions", "daily_metrics", "operation_metrics"} {
		if n := countRows(t, s, "SELECT count(*) FROM "+table+" WHERE app_id=$1", appID); n != 1 {
			t.Fatalf("%s: %d rows before delete", table, n)
		}
	}
	cookie := loginAdmin(t, h)
	w = requestJSON(t, h, "DELETE", "/api/apps/"+appID, nil, origin, cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	for _, table := range []string{"events", "visitors", "daily_visitors", "app_sessions", "daily_metrics", "operation_metrics"} {
		if n := countRows(t, s, "SELECT count(*) FROM "+table+" WHERE app_id=$1", appID); n != 0 {
			t.Fatalf("%s retained rows after delete", table)
		}
	}
	if n := countRows(t, s, "SELECT count(*) FROM apps WHERE id=$1", appID); n != 0 {
		t.Fatal("apps row survived delete")
	}
	for _, tc := range []struct{ method, path string }{
		{"DELETE", "/api/apps/" + appID},
		{"POST", "/api/apps/" + appID + "/rebuild"},
		{"GET", "/api/apps/" + appID + "/retention"},
		{"GET", "/api/apps/" + appID},
		{"GET", "/api/apps/" + appID + "/stats"},
	} {
		w = requestJSON(t, h, tc.method, tc.path, nil, origin, cookie)
		if w.Code != 404 || !strings.Contains(w.Body.String(), "app_not_found") {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	w = requestJSON(t, h, "POST", "/api/collect", map[string]any{"app_id": appID, "events": []Event{e}}, origin, nil)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "app_not_found") {
		t.Fatalf("collect after delete: %d %s", w.Code, w.Body.String())
	}
}

func TestPostgresRebuildAggregates(t *testing.T) {
	s := testServer(t)
	h := s.handler()
	ctx := context.Background()
	appID := "web-rb"
	if _, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type,allowed_origins) VALUES($1,'RB','web',ARRAY['http://example.com'])", appID); err != nil {
		t.Fatal(err)
	}
	loc := s.cfg.Timezone
	now := time.Now().In(loc)
	yesterdayNoon := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, loc).AddDate(0, 0, -1)
	e1 := validEvent(yesterdayNoon)
	e2 := e1
	e2.EventID = "event-2"
	e2.AnonymousID = "anon-2"
	e2.SessionID = "session-2"
	custom := validEvent(time.Now())
	custom.EventID = "custom-1"
	custom.EventType = "custom"
	custom.EventName = "signup"
	custom.Path = ""
	w := requestJSON(t, h, "POST", "/api/collect", map[string]any{"app_id": appID, "events": []Event{e1, e2, custom}}, "http://example.com", nil)
	if w.Code != 200 {
		t.Fatalf("collect: %s", w.Body.String())
	}
	today := now.Format("2006-01-02")
	yesterday := yesterdayNoon.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	// Corrupt every derived table, then rebuild from raw events.
	for _, stmt := range []string{
		"UPDATE daily_metrics SET starts=99 WHERE app_id=$1",
		"INSERT INTO daily_metrics(app_id,day,starts) VALUES($1,$2::text::date,7)",
		"DELETE FROM daily_visitors WHERE app_id=$1 AND anonymous_id='anon-1'",
		"UPDATE visitors SET first_day=$2::text::date WHERE app_id=$1 AND anonymous_id='anon-1'",
		"UPDATE visitors SET first_day=($2::text::date - 10) WHERE app_id=$1 AND anonymous_id='anon-2'",
		"INSERT INTO app_sessions(app_id,session_id,first_day) VALUES($1,'ghost',$2::text::date)",
	} {
		args := []any{appID}
		if strings.Contains(stmt, "$2") {
			args = append(args, tomorrow)
		}
		if _, err := s.db.Exec(ctx, stmt, args...); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	cookie := loginAdmin(t, h)
	w = requestJSON(t, h, "POST", "/api/apps/"+appID+"/rebuild?from="+yesterday+"&to="+tomorrow, nil, "http://example.com", cookie)
	if w.Code != 200 {
		t.Fatalf("rebuild: %d %s", w.Code, w.Body.String())
	}
	var report struct {
		RebuiltDays int64 `json:"rebuilt_days"`
		Events      int64 `json:"events"`
		Visitors    int64 `json:"visitors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.RebuiltDays != 3 || report.Events != 3 || report.Visitors != 2 {
		t.Fatalf("report: %+v", report)
	}
	var starts, pageViews, customEvents int64
	if err := s.db.QueryRow(ctx, "SELECT starts,page_views,custom_events FROM daily_metrics WHERE app_id=$1 AND day=$2::text::date", appID, yesterday).Scan(&starts, &pageViews, &customEvents); err != nil || starts != 0 || pageViews != 2 || customEvents != 0 {
		t.Fatalf("yesterday metrics: %d %d %d %v", starts, pageViews, customEvents, err)
	}
	if err := s.db.QueryRow(ctx, "SELECT starts,page_views,custom_events FROM daily_metrics WHERE app_id=$1 AND day=$2::text::date", appID, today).Scan(&starts, &pageViews, &customEvents); err != nil || starts != 0 || pageViews != 0 || customEvents != 1 {
		t.Fatalf("today metrics: %d %d %d %v", starts, pageViews, customEvents, err)
	}
	if n := countRows(t, s, "SELECT count(*) FROM daily_metrics WHERE app_id=$1 AND day=$2::text::date", appID, tomorrow); n != 0 {
		t.Fatal("bogus future metrics row survived rebuild")
	}
	if n := countRows(t, s, "SELECT count(*) FROM daily_visitors WHERE app_id=$1 AND anonymous_id='anon-1'", appID); n != 2 {
		t.Fatalf("daily visitors not restored: %d", n)
	}
	if n := countRows(t, s, "SELECT count(*) FROM app_sessions WHERE app_id=$1", appID); n != 2 {
		t.Fatalf("ghost session survived: %d", n)
	}
	var day string
	if err := s.db.QueryRow(ctx, "SELECT first_day::text FROM app_sessions WHERE app_id=$1 AND session_id='session-1'", appID).Scan(&day); err != nil || day != yesterday {
		t.Fatalf("session first day: %q %v", day, err)
	}
	if err := s.db.QueryRow(ctx, "SELECT first_day::text FROM visitors WHERE app_id=$1 AND anonymous_id='anon-1'", appID).Scan(&day); err != nil || day != yesterday {
		t.Fatalf("visitor first day restored: %q %v", day, err)
	}
	if err := s.db.QueryRow(ctx, "SELECT first_day::text FROM visitors WHERE app_id=$1 AND anonymous_id='anon-2'", appID).Scan(&day); err != nil {
		t.Fatal(err)
	}
	earlier := now.AddDate(0, 0, -9).Format("2006-01-02") // tampered to tomorrow-10; must stay
	if day != earlier {
		t.Fatalf("earlier first_day must not roll forward: %q want %q", day, earlier)
	}
	w = requestJSON(t, h, "POST", "/api/apps/"+appID+"/rebuild?from=2026-10-02&to=2026-10-01", nil, "http://example.com", cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_date_range") {
		t.Fatalf("invalid range: %d %s", w.Code, w.Body.String())
	}
}

func TestPostgresRetentionCohorts(t *testing.T) {
	s := testServer(t)
	h := s.handler()
	ctx := context.Background()
	appID := "web-ret"
	if _, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type) VALUES($1,'Ret','web')", appID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().In(s.cfg.Timezone)
	thisMonday := now.AddDate(0, 0, -(int(now.Weekday())+6)%7)
	lastWeek := thisMonday.AddDate(0, 0, -7).Format("2006-01-02")
	thisWeek := thisMonday.Format("2006-01-02")
	thursday := thisMonday.AddDate(0, 0, -4).Format("2006-01-02")
	nextTuesday := thisMonday.AddDate(0, 0, 1).Format("2006-01-02")
	for _, v := range []struct{ anon, day string }{{"u1", lastWeek}, {"u2", lastWeek}, {"u3", thisWeek}} {
		if _, err := s.db.Exec(ctx, "INSERT INTO visitors(app_id,anonymous_id,first_day) VALUES($1,$2,$3::text::date)", appID, v.anon, v.day); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []struct{ anon, day string }{{"u1", lastWeek}, {"u1", thursday}, {"u2", nextTuesday}, {"u3", thisWeek}} {
		if _, err := s.db.Exec(ctx, "INSERT INTO daily_visitors(app_id,day,anonymous_id) VALUES($1,$2::text::date,$3)", appID, d.day, d.anon); err != nil {
			t.Fatal(err)
		}
	}
	cookie := loginAdmin(t, h)
	w := requestJSON(t, h, "GET", "/api/apps/"+appID+"/retention?weeks=3", nil, "", cookie)
	if w.Code != 200 {
		t.Fatalf("retention: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Weeks   int               `json:"weeks"`
		Cohorts []RetentionCohort `json:"cohorts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Weeks != 3 || len(got.Cohorts) != 2 {
		t.Fatalf("shape: %+v", got)
	}
	first, second := got.Cohorts[0], got.Cohorts[1]
	if first.Cohort != lastWeek || first.Size != 2 || !slices.Equal(first.Retained, []int64{1, 1, 0}) {
		t.Fatalf("first cohort: %+v", first)
	}
	if second.Cohort != thisWeek || second.Size != 1 || !slices.Equal(second.Retained, []int64{1, 0, 0}) {
		t.Fatalf("second cohort: %+v", second)
	}
	w = requestJSON(t, h, "GET", "/api/apps/"+appID+"/retention", nil, "", cookie)
	var def struct {
		Weeks   int `json:"weeks"`
		Cohorts []struct {
			Retained []int64 `json:"retained"`
		} `json:"cohorts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}
	if def.Weeks != 8 || len(def.Cohorts) == 0 || len(def.Cohorts[0].Retained) != 8 {
		t.Fatalf("default weeks: %+v", def)
	}
	for _, query := range []string{"?weeks=0", "?weeks=13", "?weeks=abc"} {
		w = requestJSON(t, h, "GET", "/api/apps/"+appID+"/retention"+query, nil, "", cookie)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_weeks") {
			t.Fatalf("%s: %d %s", query, w.Code, w.Body.String())
		}
	}
	w = requestJSON(t, h, "GET", "/api/apps/missing/retention", nil, "", cookie)
	if w.Code != 404 {
		t.Fatalf("unknown app: %d", w.Code)
	}
}

func TestPostgresMetricsSummaryAndSettings(t *testing.T) {
	s := testServer(t)
	h := s.handler()
	ctx := context.Background()
	for _, id := range []string{"web-op", "desktop-other"} {
		if _, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type,allowed_origins) VALUES($1,$1,$2,ARRAY['http://example.com'])", id, map[string]string{"web-op": "web", "desktop-other": "desktop"}[id]); err != nil {
			t.Fatal(err)
		}
	}
	e := validEvent(time.Now().Add(-time.Minute))
	second := e
	second.EventID = "event-2"
	payload := map[string]any{"app_id": "web-op", "events": []Event{e, second}}
	if w := requestJSON(t, h, "POST", "/api/collect", payload, "http://example.com", nil); w.Code != 200 {
		t.Fatalf("collect: %s", w.Body.String())
	}
	if w := requestJSON(t, h, "POST", "/api/collect", payload, "http://example.com", nil); w.Code != 200 {
		t.Fatalf("duplicate collect: %s", w.Body.String())
	}
	bad := e
	bad.SchemaVersion = 2
	if w := requestJSON(t, h, "POST", "/api/collect", map[string]any{"app_id": "web-op", "events": []Event{bad}}, "http://example.com", nil); w.Code != 200 {
		t.Fatalf("rejected collect: %s", w.Body.String())
	}
	if w := requestJSON(t, h, "POST", "/api/collect", map[string]any{"app_id": "ghost", "events": []Event{e}}, "", nil); w.Code != 404 {
		t.Fatalf("unknown app: %d", w.Code)
	}
	day := time.Now().In(s.cfg.Timezone).Format("2006-01-02")
	cookie := loginAdmin(t, h)
	w := requestJSON(t, h, "GET", "/api/metrics?from="+day+"&to="+day, nil, "", cookie)
	if w.Code != 200 {
		t.Fatalf("metrics: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Timezone string           `json:"timezone"`
		From     string           `json:"from"`
		To       string           `json:"to"`
		Totals   OperationTotals  `json:"totals"`
		Apps     []OperationApp   `json:"apps"`
		Daily    []OperationDaily `json:"daily"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := OperationTotals{Requests: 3, Accepted: 2, Duplicates: 2, Rejected: 1}
	if got.Timezone != "Asia/Shanghai" || got.From != day || got.To != day || got.Totals != want {
		t.Fatalf("totals: %+v", got)
	}
	if len(got.Apps) != 2 || len(got.Daily) != 1 || got.Daily[0].Date != day || got.Daily[0].OperationTotals != want {
		t.Fatalf("daily: %+v", got.Daily)
	}
	byID := map[string]OperationTotals{}
	for _, app := range got.Apps {
		byID[app.ID] = app.OperationTotals
	}
	if byID["web-op"] != want || byID["desktop-other"] != (OperationTotals{}) {
		t.Fatalf("apps: %+v", byID)
	}
	w = requestJSON(t, h, "GET", "/api/settings", nil, "", cookie)
	if w.Code != 200 {
		t.Fatalf("settings: %d %s", w.Code, w.Body.String())
	}
	var settings struct {
		Version            string `json:"version"`
		StatisticsTimezone string `json:"statistics_timezone"`
		RetentionDays      int    `json:"retention_days"`
		RateLimitPerSecond int    `json:"rate_limit_per_second"`
		RateLimitBurst     int    `json:"rate_limit_burst"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings != (struct {
		Version            string `json:"version"`
		StatisticsTimezone string `json:"statistics_timezone"`
		RetentionDays      int    `json:"retention_days"`
		RateLimitPerSecond int    `json:"rate_limit_per_second"`
		RateLimitBurst     int    `json:"rate_limit_burst"`
	}{ServerVersion, "Asia/Shanghai", 0, 2, 60}) {
		t.Fatalf("settings: %+v", settings)
	}
	w = requestJSON(t, h, "GET", "/api/settings", nil, "", nil)
	if w.Code != 401 {
		t.Fatalf("unauthenticated settings: %d", w.Code)
	}
	w = requestJSON(t, h, "GET", "/api/status", nil, "", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"version":"`+ServerVersion+`"`) {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
}
