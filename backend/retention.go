package main

import (
	"context"
	"log/slog"
	"time"
)

const retentionBatchSize = 5000

// purgeLoop checks once at startup, then every six hours until shutdown.
func (s *Server) purgeLoop(ctx context.Context) {
	if s.cfg.RetentionDays == 0 {
		return
	}
	s.purgeExpiredEvents(ctx)
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.purgeExpiredEvents(ctx)
		}
	}
}

// Only raw events expire; aggregate and visitor/session dedup tables remain intact.
func (s *Server) purgeExpiredEvents(ctx context.Context) {
	if s.cfg.RetentionDays == 0 {
		return
	}
	cutoff := time.Now().In(s.cfg.Timezone).AddDate(0, 0, -s.cfg.RetentionDays).Format("2006-01-02")
	var total int64
	defer func() {
		slog.Info("retention purge finished", "deleted_events", total, "cutoff", cutoff)
	}()
	for {
		batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		deleted, err := s.purgeEventBatch(batchCtx, cutoff)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("retention purge failed", "error", err, "cutoff", cutoff)
			}
			return
		}
		total += deleted
		if deleted < retentionBatchSize {
			return
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Server) purgeEventBatch(ctx context.Context, cutoff string) (int64, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// Rebuild holds a shared lock on this row, so its raw inputs cannot expire mid-transaction.
	if _, err = tx.Exec(ctx, "UPDATE retention_state SET cutoff=GREATEST(cutoff,$1::text::date) WHERE id=1", cutoff); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, "DELETE FROM events WHERE ctid IN (SELECT ctid FROM events WHERE day < $1::text::date ORDER BY day LIMIT $2)", cutoff, retentionBatchSize)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
