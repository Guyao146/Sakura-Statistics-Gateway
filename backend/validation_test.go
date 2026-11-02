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
	if code, _ := validateEvent(&event, &App{Type: "web"}, now); code != "" {
		t.Fatal(code)
	}
	if event.Path != "/docs" {
		t.Fatalf("stored path %q", event.Path)
	}
	event = validEvent(now)
	event.EventType = "active"
	if code, _ := validateEvent(&event, &App{Type: "web"}, now); code != "" || event.Path != "" {
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
			if code, _ := validateEvent(&e, &App{Type: tc.kind}, now); code != tc.code {
				t.Fatalf("got %q want %q", code, tc.code)
			}
		})
	}
}
func TestCustomEventNameValidation(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	app := &App{Type: "desktop"}
	cases := []struct {
		name string
		code string
	}{
		{"", "invalid_event_name"},
		{"   ", "invalid_event_name"},
		{"\t\n", "invalid_event_name"},
		{strings.Repeat("x", 65), "invalid_event_name"},
		{"bad\nname", "invalid_event_name"},
		{strings.Repeat("x", 64), ""},
		{"signup", ""},
		{"支付完成", ""},
	}
	for _, tc := range cases {
		e := validEvent(now)
		e.EventType = "custom"
		e.Path = "/ignored"
		e.EventName = tc.name
		if code, _ := validateEvent(&e, app, now); code != tc.code {
			t.Fatalf("name %q: got %q want %q", tc.name, code, tc.code)
		}
		if tc.code == "" && (e.Path != "" || e.EventName != tc.name) {
			t.Fatalf("custom must drop path and keep name: %+v", e)
		}
	}
	// Non-custom events never store an event_name.
	e := validEvent(now)
	e.EventName = "ignored"
	if code, _ := validateEvent(&e, &App{Type: "web"}, now); code != "" || e.EventName != "" {
		t.Fatalf("page_view retained event_name: %+v", e)
	}
}
func TestPathNormalizationAndPrefixes(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	app := &App{Type: "web", NormalizeNumericPaths: true, AllowedPathPrefixes: []string{"/docs/", "/users/"}}
	e := validEvent(now)
	e.Path = "/users/123/orders/0042?x=1#f"
	if code, _ := validateEvent(&e, app, now); code != "" || e.Path != "/users/:id/orders/:id" {
		t.Fatalf("normalize: %q %q", code, e.Path)
	}
	e.Path = "/docs/v2026/release"
	if code, _ := validateEvent(&e, app, now); code != "" || e.Path != "/docs/v2026/release" {
		t.Fatalf("mixed segment must stay: %q %q", code, e.Path)
	}
	e.Path = "/other/page"
	if code, _ := validateEvent(&e, app, now); code != "invalid_path" {
		t.Fatalf("prefix miss: %q", code)
	}
	app.NormalizeNumericPaths = false
	e.Path = "/docs/123"
	if code, _ := validateEvent(&e, app, now); code != "" || e.Path != "/docs/123" {
		t.Fatalf("no normalization: %q %q", code, e.Path)
	}
	if got := normalizeNumericPaths("/a//007/b"); got != "/a//:id/b" {
		t.Fatal(got)
	}
}
func TestPathPrefixRules(t *testing.T) {
	out, ok := normalizePathPrefixes([]string{"/b", "/a", "/b", "/a/"})
	if !ok || len(out) != 3 || out[0] != "/b" || out[1] != "/a" || out[2] != "/a/" {
		t.Fatalf("dedupe: %q %v", out, ok)
	}
	empty, ok := normalizePathPrefixes(nil)
	if !ok || empty == nil || len(empty) != 0 {
		t.Fatalf("empty: %q %v", empty, ok)
	}
	bad := [][]string{{"no-slash"}, {"//double"}, {""}, {"/" + strings.Repeat("a", 256)}, {"/ctrl\n"}}
	for _, prefixes := range bad {
		if _, ok := normalizePathPrefixes(prefixes); ok {
			t.Fatalf("accepted invalid prefixes: %q", prefixes)
		}
	}
	many := make([]string, 51)
	for i := range many {
		many[i] = "/" + strings.Repeat("x", i+1)
	}
	if _, ok := normalizePathPrefixes(many); ok {
		t.Fatal("accepted 51 unique prefixes")
	}
	if _, ok := normalizePathPrefixes(many[:50]); !ok {
		t.Fatal("rejected 50 unique prefixes")
	}
	if out, ok := normalizePathPrefixes([]string{"/" + strings.Repeat("a", 255)}); !ok || len(out[0]) != 256 {
		t.Fatalf("256-byte boundary: %v %v", out, ok)
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
func TestConfigV02Validation(t *testing.T) {
	base := func() {
		t.Setenv("DATABASE_URL", "postgres://localhost/test")
		t.Setenv("ADMIN_USERNAME", "admin")
		t.Setenv("ADMIN_PASSWORD", "StrongPass123!")
		t.Setenv("RETENTION_DAYS", "")
		t.Setenv("RATE_LIMIT_PER_SECOND", "")
		t.Setenv("RATE_LIMIT_BURST", "")
	}
	base()
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.RetentionDays != 0 || c.RateLimitPerSecond != 2 || c.RateLimitBurst != 60 {
		t.Fatalf("defaults: %+v", c)
	}
	for _, value := range []string{"6", "-1", "abc", "3.5"} {
		t.Setenv("RETENTION_DAYS", value)
		if _, err := loadConfig(); err == nil {
			t.Fatalf("RETENTION_DAYS=%s accepted", value)
		}
	}
	t.Setenv("RETENTION_DAYS", "7")
	if c, err = loadConfig(); err != nil || c.RetentionDays != 7 {
		t.Fatalf("RETENTION_DAYS=7: %+v %v", c, err)
	}
	for name, invalid := range map[string][]string{
		"RATE_LIMIT_PER_SECOND": {"0", "1001", "abc", "-2"},
		"RATE_LIMIT_BURST":      {"0", "10001", "abc", "-2"},
	} {
		for _, value := range invalid {
			t.Setenv(name, value)
			if _, err := loadConfig(); err == nil {
				t.Fatalf("%s=%s accepted", name, value)
			}
		}
		t.Setenv(name, "")
	}
	t.Setenv("RATE_LIMIT_PER_SECOND", "1000")
	t.Setenv("RATE_LIMIT_BURST", "10000")
	if c, err = loadConfig(); err != nil || c.RateLimitPerSecond != 1000 || c.RateLimitBurst != 10000 {
		t.Fatalf("upper bounds: %+v %v", c, err)
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
		if !l.allow("app", now, 2, 60) {
			t.Fatal(i)
		}
	}
	if l.allow("app", now, 2, 60) {
		t.Fatal("burst exceeded")
	}
	if !l.allow("app", now.Add(time.Second), 2, 60) {
		t.Fatal("tokens did not refill")
	}
	for i := 0; i < 4096; i++ {
		l.entries[string(rune(i))] = bucket{at: now}
	}
	if l.allow("another", now, 2, 60) {
		t.Fatal("map cap exceeded")
	}
	if !l.allow("another", now.Add(11*time.Minute), 2, 60) {
		t.Fatal("stale keys not evicted")
	}
	custom := limiter{}
	if !custom.allow("k", now, 1, 1) || custom.allow("k", now, 1, 1) {
		t.Fatal("custom budget: one request per second expected")
	}
	if !custom.allow("k", now.Add(time.Second), 1, 1) {
		t.Fatal("custom budget did not refill")
	}
}
