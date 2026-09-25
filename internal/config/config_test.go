package config_test

import (
	"os"
	"testing"

	"github.com/url-shortener/url-shortener/internal/config"
)

func TestLoadSuccess(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("BASE_URL", "http://localhost:8080")
	t.Setenv("HTTP_ADDR", ":9090")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL == "" || cfg.BaseURL != "http://localhost:8080" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr = %q", cfg.HTTPAddr)
	}
}

func TestLoadDefaultHTTPAddr(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("BASE_URL", "http://localhost:8080")
	os.Unsetenv("HTTP_ADDR")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want default :8080", cfg.HTTPAddr)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("BASE_URL")
	t.Setenv("HTTP_ADDR", ":8080")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when required env vars are missing")
	}
}
