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

// RecordClick increments hit_count and inserts a click_events row in one transaction.
func (s *Store) RecordClick(ctx context.Context, code string, click link.ClickEvent) (link.Link, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return link.Link{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	const updateQ = `
		UPDATE links
		SET hit_count = hit_count + 1
		WHERE code = $1
		RETURNING code, original_url, owner_token_hash, hit_count, active, created_at, expires_at
	`
	l, err := scanOneTx(ctx, tx, updateQ, code)
	if err != nil {
		return link.Link{}, err
	}

	clickedAt := click.ClickedAt
	if clickedAt.IsZero() {
		clickedAt = time.Now().UTC()
	}
	const insertQ = `
		INSERT INTO click_events (code, clicked_at, referrer, user_agent)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := tx.Exec(ctx, insertQ, code, clickedAt, click.Referrer, click.UserAgent); err != nil {
		return link.Link{}, fmt.Errorf("insert click: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return link.Link{}, fmt.Errorf("commit: %w", err)
	}
	return l, nil
}

// ListClicks returns the most recent click events for a code.
func (s *Store) ListClicks(ctx context.Context, code string, limit int) ([]link.ClickEvent, error) {
	const q = `
		SELECT id, code, clicked_at, referrer, user_agent
		FROM click_events
		WHERE code = $1
		ORDER BY clicked_at DESC, id DESC
		LIMIT $2
	`
	rows, err := s.pool.Query(ctx, q, code, limit)
	if err != nil {
		return nil, fmt.Errorf("list clicks: %w", err)
	}
	defer rows.Close()

	out := make([]link.ClickEvent, 0)
	for rows.Next() {
		var e link.ClickEvent
		if err := rows.Scan(&e.ID, &e.Code, &e.ClickedAt, &e.Referrer, &e.UserAgent); err != nil {
			return nil, fmt.Errorf("scan click: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clicks: %w", err)
	}
	return out, nil
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
	return scanOne(ctx, s.pool, q, args...)
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func scanOne(ctx context.Context, q querier, sql string, args ...any) (link.Link, error) {
	var (
		l         link.Link
		expiresAt *time.Time
	)
	err := q.QueryRow(ctx, sql, args...).Scan(
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

func scanOneTx(ctx context.Context, tx pgx.Tx, sql string, args ...any) (link.Link, error) {
	return scanOne(ctx, tx, sql, args...)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
