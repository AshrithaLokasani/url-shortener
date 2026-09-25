package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/url-shortener/url-shortener/internal/code"
	"github.com/url-shortener/url-shortener/internal/config"
	"github.com/url-shortener/url-shortener/internal/httpapi"
	"github.com/url-shortener/url-shortener/internal/link"
	"github.com/url-shortener/url-shortener/internal/storage/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", slog.String("err", err.Error()))
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db connect", slog.String("err", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("db ping", slog.String("err", err.Error()))
		os.Exit(1)
	}

	migrationsDir := migrationsPath()
	if err := postgres.Migrate(pool, migrationsDir); err != nil {
		logger.Error("migrate", slog.String("err", err.Error()), slog.String("dir", migrationsDir))
		os.Exit(1)
	}

	store := postgres.NewStore(pool)
	svc := link.NewService(store, code.NewRandomGenerator(), cfg.BaseURL)
	srv := httpapi.New(svc, logger, cfg.HTTPAddr)

	go func() {
		logger.Info("listening", slog.String("addr", cfg.HTTPAddr), slog.String("base_url", cfg.BaseURL))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server", slog.String("err", err.Error()))
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", slog.String("err", err.Error()))
		os.Exit(1)
	}
	logger.Info("stopped")
}

func migrationsPath() string {
	if dir := os.Getenv("MIGRATIONS_DIR"); dir != "" {
		return dir
	}
	// Prefer ./migrations when run from repo root; fall back beside the binary.
	candidates := []string{
		"migrations",
		filepath.Join("..", "..", "migrations"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return "migrations"
}
