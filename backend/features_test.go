package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPostgresMigrationPreservesLegacyData(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	// This random test schema is reduced to v0.1 before seeding a legacy installation.
	for _, sql := range []string{
		"DROP TABLE operation_metrics",
		"DROP TABLE retention_state",
		"DROP INDEX events_custom_app_day",
		"ALTER TABLE daily_metrics DROP COLUMN custom_events",
		"ALTER TABLE events DROP COLUMN event_name",
		"ALTER TABLE apps DROP COLUMN allowed_path_prefixes, DROP COLUMN normalize_numeric_paths",
		"DELETE FROM schema_migrations WHERE name='002_features.sql'",
	} {
		if _, err := s.db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	day := time.Now().In(s.cfg.Timezone).Format("2006-01-02")
	for _, sql := range []string{
		"INSERT INTO apps(id,name,type) VALUES('legacy','Legacy desktop','desktop')",
		"INSERT INTO events(app_id,event_id,event_type,occurred_at,day,anonymous_id,session_id) VALUES('legacy','legacy-start','app_start',now(),$1::text::date,'legacy-install','legacy-session')",
		"INSERT INTO visitors(app_id,anonymous_id,first_day) VALUES('legacy','legacy-install',$1::text::date)",
		"INSERT INTO daily_visitors(app_id,day,anonymous_id) VALUES('legacy',$1::text::date,'legacy-install')",
		"INSERT INTO daily_metrics(app_id,day,starts) VALUES('legacy',$1::text::date,1)",
	} {
		args := []any{}
		if strings.Contains(sql, "$1") {
			args = append(args, day)
		}
		if _, err := s.db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrate(ctx, s.db); err != nil {
		t.Fatal("upgrade:", err)
	}
	if err := migrate(ctx, s.db); err != nil {
		t.Fatal("migration replay:", err)
	}
	if countRows(t, s, "SELECT count(*) FROM events WHERE app_id='legacy'") != 1 || countRows(t, s, "SELECT count(*) FROM admins") != 1 {
		t.Fatal("legacy events or administrator changed during migration")
	}
	cookie := loginAdmin(t, s.handler())
	w := requestJSON(t, s.handler(), "GET", "/api/apps/legacy/stats?from="+day+"&to="+day, nil, "", cookie)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var result struct {
		Totals Metrics `json:"totals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Totals != (Metrics{Active: 1, Starts: 1, NewVisitors: 1}) {
		t.Fatalf("legacy totals: %+v", result.Totals)
	}
	var prefixes []string
	var normalize bool
	if err := s.db.QueryRow(ctx, "SELECT allowed_path_prefixes,normalize_numeric_paths FROM apps WHERE id='legacy'").Scan(&prefixes, &normalize); err != nil || len(prefixes) != 0 || normalize {
		t.Fatalf("new application defaults: %v %v %v", prefixes, normalize, err)
	}
}

func TestPostgresRetentionBatchAndPersistentRebuildProtection(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	now := time.Now().In(s.cfg.Timezone)
	oldDay := now.AddDate(0, 0, -8).Format("2006-01-02")
	today := now.Format("2006-01-02")
	cutoff := now.AddDate(0, 0, -7).Format("2006-01-02")
	if _, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type) VALUES('purge-test','Synthetic retention test','desktop')"); err != nil {
		t.Fatal(err)
	}
	// Old raw events simulate an installation with historical data, not incoming backdated requests.
	if _, err := s.db.Exec(ctx, `INSERT INTO events(app_id,event_id,event_type,occurred_at,day,anonymous_id,session_id)
SELECT 'purge-test','old-'||i::text,'app_start',now()-interval '8 days',$1::text::date,'visitor','old-session'
FROM generate_series(1,5001) i`, oldDay); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"INSERT INTO visitors(app_id,anonymous_id,first_day) VALUES('purge-test','visitor',$1::text::date)",
		"INSERT INTO daily_visitors(app_id,day,anonymous_id) VALUES('purge-test',$1::text::date,'visitor')",
		"INSERT INTO daily_metrics(app_id,day,starts) VALUES('purge-test',$1::text::date,5001)",
	} {
		if _, err := s.db.Exec(ctx, sql, oldDay); err != nil {
			t.Fatal(err)
		}
	}
	fresh := validEvent(time.Now().Add(-time.Second))
	fresh.EventType = "app_start"
	fresh.AnonymousID = "visitor"
	w := requestJSON(t, s.handler(), "POST", "/api/collect", map[string]any{"app_id": "purge-test", "events": []Event{fresh}}, "", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookie := loginAdmin(t, s.handler())
	s.cfg.RetentionDays = 7
	w = requestJSON(t, s.handler(), "POST", "/api/apps/purge-test/rebuild?from="+oldDay+"&to="+oldDay, nil, "http://example.com", cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "rebuild_outside_retention") {
		t.Fatalf("configured retention guard: %d %s", w.Code, w.Body.String())
	}
	first, err := s.purgeEventBatch(ctx, cutoff)
	if err != nil || first != retentionBatchSize {
		t.Fatalf("first batch: %d %v", first, err)
	}
	second, err := s.purgeEventBatch(ctx, cutoff)
	if err != nil || second != 1 {
		t.Fatalf("second batch: %d %v", second, err)
	}
	if countRows(t, s, "SELECT count(*) FROM events WHERE app_id='purge-test'") != 1 {
		t.Fatal("purge removed a recent event or retained expired raw events")
	}
	if countRows(t, s, "SELECT count(*) FROM daily_visitors WHERE app_id='purge-test'") != 2 || countRows(t, s, "SELECT count(*) FROM visitors WHERE app_id='purge-test'") != 1 {
		t.Fatal("purge removed preserved visitor aggregates")
	}
	var oldStarts int64
	if err := s.db.QueryRow(ctx, "SELECT starts FROM daily_metrics WHERE app_id='purge-test' AND day=$1::text::date", oldDay).Scan(&oldStarts); err != nil || oldStarts != 5001 {
		t.Fatalf("historical aggregate: %d %v", oldStarts, err)
	}
	if _, err := s.purgeEventBatch(ctx, now.AddDate(0, 0, -14).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := s.db.QueryRow(ctx, "SELECT cutoff::text FROM retention_state WHERE id=1").Scan(&stored); err != nil || stored != cutoff {
		t.Fatalf("monotonic cutoff: %q %v", stored, err)
	}
	// A restarted server with cleanup disabled still honors the persisted cutoff.
	protected := &Server{db: s.db, cfg: s.cfg, dummyHash: s.dummyHash}
	protected.cfg.RetentionDays = 0
	w = requestJSON(t, protected.handler(), "POST", "/api/apps/purge-test/rebuild?from="+oldDay+"&to="+oldDay, nil, "http://example.com", cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "rebuild_outside_retention") {
		t.Fatalf("persisted retention guard: %d %s", w.Code, w.Body.String())
	}
	if err := s.db.QueryRow(ctx, "SELECT starts FROM daily_metrics WHERE app_id='purge-test' AND day=$1::text::date", oldDay).Scan(&oldStarts); err != nil || oldStarts != 5001 {
		t.Fatal("rejected rebuild modified a preserved aggregate")
	}
	w = requestJSON(t, protected.handler(), "POST", "/api/apps/purge-test/rebuild?from="+today+"&to="+today, nil, "http://example.com", cookie)
	if w.Code != 200 {
		t.Fatalf("rebuild in retained range: %d %s", w.Code, w.Body.String())
	}
	var freshStarts int64
	if err := s.db.QueryRow(ctx, "SELECT starts FROM daily_metrics WHERE app_id='purge-test' AND day=$1::text::date", today).Scan(&freshStarts); err != nil || freshStarts != 1 {
		t.Fatalf("recent rebuilt aggregate: %d %v", freshStarts, err)
	}
	s.cfg.RetentionDays = 7
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	done := make(chan struct{})
	go func() { s.purgeLoop(cancelled); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retention loop did not honor cancellation")
	}
}

func TestPostgresRateLimitOperationalCounters(t *testing.T) {
	s := testServer(t)
	ctx := context.Background()
	if _, err := s.db.Exec(ctx, "INSERT INTO apps(id,name,type) VALUES('rate-test','Rate counters','desktop')"); err != nil {
		t.Fatal(err)
	}
	s.cfg.RateLimitPerSecond = 1
	s.cfg.RateLimitBurst = 1
	item := validEvent(time.Now().Add(-time.Second))
	item.EventType = "custom"
	item.EventName = "export"
	payload := map[string]any{"app_id": "rate-test", "events": []Event{item}}
	w := requestJSON(t, s.handler(), "POST", "/api/collect", payload, "", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	s.limit.mu.Lock()
	s.limit.entries["collect:rate-test"] = bucket{tokens: 0, at: time.Now()}
	s.limit.mu.Unlock()
	w = requestJSON(t, s.handler(), "POST", "/api/collect", payload, "", nil)
	if w.Code != 429 {
		t.Fatalf("configured rate limit: %d %s", w.Code, w.Body.String())
	}
	var requests, accepted, rateLimited int64
	if err := s.db.QueryRow(ctx, "SELECT requests,accepted,rate_limited FROM operation_metrics WHERE app_id='rate-test'").Scan(&requests, &accepted, &rateLimited); err != nil || requests != 2 || accepted != 1 || rateLimited != 1 {
		t.Fatalf("rate-limit counters: %d %d %d %v", requests, accepted, rateLimited, err)
	}
}
