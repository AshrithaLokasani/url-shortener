package link

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/url-shortener/url-shortener/internal/code"
)

// Sentinel errors mapped to HTTP status by the transport layer.
var (
	ErrNotFound     = errors.New("link not found")
	ErrConflict     = errors.New("alias already exists")
	ErrInvalidURL   = errors.New("invalid url")
	ErrInvalidAlias = errors.New("invalid alias")
	ErrUnauthorized = errors.New("unauthorized")
	ErrGone         = errors.New("link inactive or expired")
	ErrInvalidInput = errors.New("invalid input")
)

// Link is the domain model for a shortened URL.
type Link struct {
	Code           string
	OriginalURL    string
	OwnerTokenHash []byte
	HitCount       int64
	Active         bool
	CreatedAt      time.Time
	ExpiresAt      *time.Time
}

// IsAvailable reports whether the link may be redirected.
func (l Link) IsAvailable(now time.Time) bool {
	if !l.Active {
		return false
	}
	if l.ExpiresAt != nil && !l.ExpiresAt.After(now) {
		return false
	}
	return true
}

// ClickEvent is one recorded redirect/click for analytics.
type ClickEvent struct {
	ID        int64
	Code      string
	ClickedAt time.Time
	Referrer  string
	UserAgent string
}

// ClickInput is request metadata captured at redirect time.
type ClickInput struct {
	Referrer  string
	UserAgent string
}

// Repository is the persistence port for links.
type Repository interface {
	Create(ctx context.Context, l Link) error
	GetByCode(ctx context.Context, code string) (Link, error)
	RecordClick(ctx context.Context, code string, click ClickEvent) (Link, error)
	ListClicks(ctx context.Context, code string, limit int) ([]ClickEvent, error)
	SetActive(ctx context.Context, code string, active bool) (Link, error)
}

// CreateInput is the application input for creating a link.
type CreateInput struct {
	URL   string
	Alias string
}

// CreateResult is returned to the caller after a successful create.
type CreateResult struct {
	Link       Link
	ShortURL   string
	OwnerToken string
}

// Service implements link use cases.
type Service struct {
	repo    Repository
	codes   code.Generator
	baseURL string
	now     func() time.Time
}

// NewService constructs a Service.
func NewService(repo Repository, codes code.Generator, baseURL string) *Service {
	return &Service{
		repo:    repo,
		codes:   codes,
		baseURL: strings.TrimRight(baseURL, "/"),
		now:     time.Now,
	}
}

// Create shortens a URL, optionally using a custom alias.
func (s *Service) Create(ctx context.Context, in CreateInput) (CreateResult, error) {
	if err := code.ValidateURL(in.URL); err != nil {
		return CreateResult{}, fmt.Errorf("%w: %s", ErrInvalidURL, err.Error())
	}

	ownerToken, hash, err := newOwnerToken()
	if err != nil {
		return CreateResult{}, err
	}

	var shortCode string
	if strings.TrimSpace(in.Alias) != "" {
		if err := code.ValidateAlias(in.Alias); err != nil {
			return CreateResult{}, fmt.Errorf("%w: %s", ErrInvalidAlias, err.Error())
		}
		shortCode = in.Alias
		l := Link{
			Code:           shortCode,
			OriginalURL:    in.URL,
			OwnerTokenHash: hash,
			Active:         true,
			CreatedAt:      s.now().UTC(),
		}
		if err := s.repo.Create(ctx, l); err != nil {
			return CreateResult{}, err
		}
		return s.createResult(l, ownerToken), nil
	}

	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		generated, err := s.codes.Generate()
		if err != nil {
			return CreateResult{}, fmt.Errorf("generate code: %w", err)
		}
		l := Link{
			Code:           generated,
			OriginalURL:    in.URL,
			OwnerTokenHash: hash,
			Active:         true,
			CreatedAt:      s.now().UTC(),
		}
		err = s.repo.Create(ctx, l)
		if err == nil {
			return s.createResult(l, ownerToken), nil
		}
		if errors.Is(err, ErrConflict) {
			continue
		}
		return CreateResult{}, err
	}
	return CreateResult{}, fmt.Errorf("generate unique code: exceeded retries")
}

// ResolveRedirect loads a link for redirection, records analytics, and increments hits.
func (s *Service) ResolveRedirect(ctx context.Context, shortCode string, click ClickInput) (Link, error) {
	l, err := s.repo.GetByCode(ctx, shortCode)
	if err != nil {
		return Link{}, err
	}
	if !l.IsAvailable(s.now().UTC()) {
		return Link{}, ErrGone
	}
	event := ClickEvent{
		Code:      shortCode,
		ClickedAt: s.now().UTC(),
		Referrer:  truncate(click.Referrer, 2048),
		UserAgent: truncate(click.UserAgent, 2048),
	}
	return s.repo.RecordClick(ctx, shortCode, event)
}

// Metadata returns public metadata for a short code.
func (s *Service) Metadata(ctx context.Context, shortCode string) (Link, error) {
	return s.repo.GetByCode(ctx, shortCode)
}

// Analytics returns recent click events when the Bearer owner token matches.
func (s *Service) Analytics(ctx context.Context, shortCode, ownerToken string, limit int) ([]ClickEvent, error) {
	if strings.TrimSpace(ownerToken) == "" {
		return nil, ErrUnauthorized
	}
	l, err := s.repo.GetByCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}
	if !tokenMatches(ownerToken, l.OwnerTokenHash) {
		return nil, ErrUnauthorized
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return s.repo.ListClicks(ctx, shortCode, limit)
}

// Deactivate soft-disables a link when the Bearer owner token matches.
func (s *Service) Deactivate(ctx context.Context, shortCode, ownerToken string) (Link, error) {
	if strings.TrimSpace(ownerToken) == "" {
		return Link{}, ErrUnauthorized
	}
	l, err := s.repo.GetByCode(ctx, shortCode)
	if err != nil {
		return Link{}, err
	}
	if !tokenMatches(ownerToken, l.OwnerTokenHash) {
		return Link{}, ErrUnauthorized
	}
	return s.repo.SetActive(ctx, shortCode, false)
}

func (s *Service) createResult(l Link, ownerToken string) CreateResult {
	return CreateResult{
		Link:       l,
		ShortURL:   s.baseURL + "/" + l.Code,
		OwnerToken: ownerToken,
	}
}

func newOwnerToken() (plaintext string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate owner token: %w", err)
	}
	plaintext = "usk_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plaintext))
	return plaintext, sum[:], nil
}

func tokenMatches(plaintext string, hash []byte) bool {
	sum := sha256.Sum256([]byte(plaintext))
	if len(hash) != len(sum) {
		return false
	}
	return subtle.ConstantTimeCompare(hash, sum[:]) == 1
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
