package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validEvent(now time.Time) Event {
	return Event{SchemaVersion: 1, EventID: "event-1", EventType: "page_view", OccurredAt: now, AnonymousID: "anon-1", SessionID: "session-1", Path: "/docs?q=private#fragment"}
}
func TestValidateEventAndPrivacy(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	event := validEvent(now)
	if code, _ := validateEvent(&event, "web", now); code != "" {
		t.Fatal(code)
	}
	if event.Path != "/docs" {
		t.Fatalf("stored path %q", event.Path)
	}
	event = validEvent(now)
	event.EventType = "active"
	if code, _ := validateEvent(&event, "web", now); code != "" || event.Path != "" {
		t.Fatalf("active event retained path: %+v", event)
	}
}
func TestValidationBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(*Event)
		kind   string
		code   string
	}{
		{"schema", func(e *Event) { e.SchemaVersion = 2 }, "web", "invalid_schema"},
		{"missing id", func(e *Event) { e.EventID = "" }, "web", "invalid_identifier"},
		{"missing session", func(e *Event) { e.SessionID = "" }, "web", "invalid_identifier"},
		{"old", func(e *Event) { e.OccurredAt = now.Add(-7*24*time.Hour - time.Nanosecond) }, "web", "invalid_time"},
		{"future", func(e *Event) { e.OccurredAt = now.Add(5*time.Minute + time.Nanosecond) }, "web", "invalid_time"},
		{"old boundary", func(e *Event) { e.OccurredAt = now.Add(-7 * 24 * time.Hour) }, "web", ""},
		{"future boundary", func(e *Event) { e.OccurredAt = now.Add(5 * time.Minute) }, "web", ""},
		{"desktop page", func(e *Event) {}, "desktop", "invalid_event_type"},
		{"desktop start", func(e *Event) { e.EventType = "app_start" }, "desktop", ""},
		{"metadata length", func(e *Event) { e.AppVersion = strings.Repeat("v", 129) }, "web", "invalid_metadata"},
		{"absolute URL", func(e *Event) { e.Path = "https://example.com/private?token=x" }, "web", "invalid_path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := validEvent(now)
			tc.mutate(&e)
			if code, _ := validateEvent(&e, tc.kind, now); code != tc.code {
				t.Fatalf("got %q want %q", code, tc.code)
			}
		})
	}
}
func TestOrigins(t *testing.T) {
	for _, origin := range []string{"http://localhost:8080", "https://example.com"} {
		if !validOrigin(origin) {
			t.Fatal(origin)
		}
	}
	for _, origin := range []string{"*", "null", "https://example.com/", "https://example.com?x=y"} {
		if validOrigin(origin) {
			t.Fatal(origin)
		}
	}
	web := App{Type: "web", AllowedOrigins: []string{"https://example.com"}}
	if !originAllowed(web, "https://example.com") || originAllowed(web, "") {
		t.Fatal("web origin policy")
	}
	if !originAllowed(App{Type: "desktop"}, "") {
		t.Fatal("desktop transport must allow missing Origin")
	}
	r := httptest.NewRequest("POST", "http://localhost:8080/api/apps", nil)
	r.Header.Set("Origin", "http://localhost:8080")
	if !sameOrigin(r) {
		t.Fatal("same origin rejected")
	}
	r.Header.Set("Origin", "https://stats.example.com")
	if !originMatches(r, "https://stats.example.com") {
		t.Fatal("public origin rejected")
	}
}
func TestConfigEnvironmentPriority(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "StrongPass123!")
	t.Setenv("PORT", "18080")
	t.Setenv("ADDR", ":9000")
	t.Setenv("SESSION_SECURE", "false")
	t.Setenv("COOKIE_SECURE", "true")
	t.Setenv("PUBLIC_ORIGIN", "")
	t.Setenv("STATISTICS_TIMEZONE", "Asia/Shanghai")
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":18080" || c.SecureCookie {
		t.Fatalf("priority: %+v", c)
	}
	t.Setenv("SESSION_SECURE", "invalid")
	if _, err := loadConfig(); err == nil {
		t.Fatal("invalid boolean accepted")
	}
	t.Setenv("SESSION_SECURE", "true")
	t.Setenv("PORT", "70000")
	if _, err := loadConfig(); err == nil {
		t.Fatal("invalid port accepted")
	}
}
func TestStrongPassword(t *testing.T) {
	for _, password := range []string{"", "password", "LongPassword123", "longpassword!123", strings.Repeat("A", 73) + "a1!"} {
		if strongPassword(password) {
			t.Fatal("accepted weak or oversized password")
		}
	}
	if !strongPassword("StrongPass123!") {
		t.Fatal("strong password rejected")
	}
}
func TestDateRangeAndTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if got := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC).In(loc).Format("2006-01-02"); got != "2026-10-30" {
		t.Fatal(got)
	}
	s := Server{cfg: Config{Timezone: loc}}
	for _, query := range []string{"from=2026-10-02&to=2026-10-01", "from=bad&to=2026-10-01", "from=2024-01-01&to=2026-10-01"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/stats/overview?"+query, nil)
		if _, _, ok := s.dateRange(w, r); ok || w.Code != 400 {
			t.Fatal(query)
		}
	}
}
func TestStaticFallbackAndSDK(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>SPA</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sdk.js"), []byte("window.sdk = true;"), 0600); err != nil {
		t.Fatal(err)
	}
	s := Server{cfg: Config{StaticDir: dir}}
	for _, tc := range []struct {
		path     string
		status   int
		contains string
	}{{"/dashboard", 200, "SPA"}, {"/sdk.js", 200, "window.sdk"}, {"/api/not-a-route", 404, "not_found"}, {"/missing.js", 404, "not_found"}} {
		t.Run(tc.path, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			s.handler().ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
func TestBoundedLimiter(t *testing.T) {
	l := limiter{}
	now := time.Now()
	for i := 0; i < 60; i++ {
		if !l.allow("app", now) {
			t.Fatal(i)
		}
	}
	if l.allow("app", now) {
		t.Fatal("burst exceeded")
	}
	if !l.allow("app", now.Add(time.Second)) {
		t.Fatal("tokens did not refill")
	}
	for i := 0; i < 4096; i++ {
		l.entries[string(rune(i))] = bucket{at: now}
	}
	if l.allow("another", now) {
		t.Fatal("map cap exceeded")
	}
	if !l.allow("another", now.Add(11*time.Minute)) {
		t.Fatal("stale keys not evicted")
	}
}
