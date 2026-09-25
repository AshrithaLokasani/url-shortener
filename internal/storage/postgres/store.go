package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/url-shortener/url-shortener/internal/link"
)

// Store implements link.Repository using Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps a connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create inserts a new link row.
func (s *Store) Create(ctx context.Context, l link.Link) error {
	const q = `
		INSERT INTO links (code, original_url, owner_token_hash, hit_count, active, created_at, expires_at)
		VALUES ($1, $2, $3, 0, $4, $5, $6)
	`
	createdAt := l.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, q,
		l.Code,
		l.OriginalURL,
		l.OwnerTokenHash,
		l.Active,
		createdAt,
		l.ExpiresAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return link.ErrConflict
		}
		return fmt.Errorf("insert link: %w", err)
	}
	return nil
}

// GetByCode loads a link by short code.
func (s *Store) GetByCode(ctx context.Context, code string) (link.Link, error) {
	const q = `
		SELECT code, original_url, owner_token_hash, hit_count, active, created_at, expires_at
		FROM links
		WHERE code = $1
	`
	return s.scanOne(ctx, q, code)
}

// IncrementHits atomically bumps hit_count and returns the updated row.
func (s *Store) IncrementHits(ctx context.Context, code string) (link.Link, error) {
	const q = `
		UPDATE links
		SET hit_count = hit_count + 1
		WHERE code = $1
		RETURNING code, original_url, owner_token_hash, hit_count, active, created_at, expires_at
	`
	l, err := s.scanOne(ctx, q, code)
	if err != nil {
		return link.Link{}, err
	}
	return l, nil
}

// SetActive updates the active flag.
func (s *Store) SetActive(ctx context.Context, code string, active bool) (link.Link, error) {
	const q = `
		UPDATE links
		SET active = $2
		WHERE code = $1
		RETURNING code, original_url, owner_token_hash, hit_count, active, created_at, expires_at
	`
	l, err := s.scanOne(ctx, q, code, active)
	if err != nil {
		return link.Link{}, err
	}
	return l, nil
}

func (s *Store) scanOne(ctx context.Context, q string, args ...any) (link.Link, error) {
	var (
		l         link.Link
		expiresAt *time.Time
	)
	err := s.pool.QueryRow(ctx, q, args...).Scan(
		&l.Code,
		&l.OriginalURL,
		&l.OwnerTokenHash,
		&l.HitCount,
		&l.Active,
		&l.CreatedAt,
		&expiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return link.Link{}, link.ErrNotFound
		}
		return link.Link{}, fmt.Errorf("query link: %w", err)
	}
	l.ExpiresAt = expiresAt
	return l, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
