package postgres

import (
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrate applies SQL migrations from dir against the pool's database.
func Migrate(pool *pgxpool.Pool, dir string) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	return runGoose(db, dir)
}

func runGoose(db *sql.DB, dir string) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}
