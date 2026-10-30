package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"
)

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return err
	}
	if err = migrate(ctx, pool); err != nil {
		return err
	}
	if err = bootstrapAdmin(ctx, pool, cfg); err != nil {
		return err
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte(randomID()), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s := &Server{db: pool, cfg: cfg, dummyHash: dummy}
	srv := &http.Server{Addr: cfg.Addr, Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", cfg.Addr, "timezone", cfg.Timezone.String())
		errs <- srv.ListenAndServe()
	}()
	select {
	case err = <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-shutdownCtx.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err = srv.Shutdown(ctx); err != nil {
			_ = srv.Close()
			return err
		}
		return nil
	}
}
func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
