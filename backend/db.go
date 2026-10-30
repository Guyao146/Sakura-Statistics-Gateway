package main

import (
	"context"
	"embed"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"sort"
)

//go:embed migrations/*.sql
var migrations embed.FS

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(734928501)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)", entry.Name()).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		b, e := migrations.ReadFile("migrations/" + entry.Name())
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(name) VALUES($1)", entry.Name()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func bootstrapAdmin(ctx context.Context, pool *pgxpool.Pool, c Config) error {
	var existing string
	err := pool.QueryRow(ctx, "SELECT password_hash FROM admins WHERE username=$1", c.Username).Scan(&existing)
	if err == nil && bcrypt.CompareHashAndPassword([]byte(existing), []byte(c.Password)) == nil {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "INSERT INTO admins(username,password_hash) VALUES($1,$2) ON CONFLICT(username) DO UPDATE SET password_hash=EXCLUDED.password_hash", c.Username, string(hash)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "DELETE FROM auth_sessions WHERE username=$1", c.Username); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
