//go:build integration

package postgres_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/url-shortener/url-shortener/internal/link"
	"github.com/url-shortener/url-shortener/internal/storage/postgres"
)

func TestStoreCreateGetConflictAndDeactivate(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Fatal("DATABASE_URL is required for integration tests")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	migrations := os.Getenv("MIGRATIONS_DIR")
	if migrations == "" {
		migrations = findMigrations(t)
	} else if !filepath.IsAbs(migrations) {
		// go test runs with cwd = package dir; resolve relative paths from module root.
		if _, err := os.Stat(migrations); err != nil {
			migrations = filepath.Clean(filepath.Join("..", "..", "..", migrations))
		}
	}
	if err := postgres.Migrate(pool, migrations); err != nil {
		t.Fatalf("migrate (%s): %v", migrations, err)
	}

	store := postgres.NewStore(pool)
	code := "itest-" + time.Now().UTC().Format("150405.000")

	created := link.Link{
		Code:           code,
		OriginalURL:    "https://example.com/integration",
		OwnerTokenHash: []byte("0123456789abcdef0123456789abcdef"),
		Active:         true,
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.Create(ctx, created); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := store.GetByCode(ctx, code)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OriginalURL != created.OriginalURL || !got.Active {
		t.Fatalf("unexpected row: %+v", got)
	}

	if err := store.Create(ctx, created); err != link.ErrConflict {
		t.Fatalf("expected conflict, got %v", err)
	}

	updated, err := store.RecordClick(ctx, code, link.ClickEvent{
		Referrer:  "https://ref.example",
		UserAgent: "integration-test",
		ClickedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("record click: %v", err)
	}
	if updated.HitCount != 1 {
		t.Fatalf("hit_count = %d, want 1", updated.HitCount)
	}

	clicks, err := store.ListClicks(ctx, code, 10)
	if err != nil {
		t.Fatalf("list clicks: %v", err)
	}
	if len(clicks) != 1 || clicks[0].Referrer != "https://ref.example" {
		t.Fatalf("unexpected clicks: %+v", clicks)
	}

	inactive, err := store.SetActive(ctx, code, false)
	if err != nil {
		t.Fatalf("set active: %v", err)
	}
	if inactive.Active {
		t.Fatal("expected inactive")
	}
}

func findMigrations(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"migrations",
		filepath.Join("..", "..", "..", "migrations"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	t.Fatal("migrations directory not found; set MIGRATIONS_DIR")
	return ""
}
