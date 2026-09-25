package link_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/url-shortener/url-shortener/internal/code"
	"github.com/url-shortener/url-shortener/internal/link"
)

type fakeRepo struct {
	mu     sync.Mutex
	byCode map[string]link.Link
	clicks []link.ClickEvent
	nextID int64
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byCode: make(map[string]link.Link)}
}

func (f *fakeRepo) Create(_ context.Context, l link.Link) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byCode[l.Code]; ok {
		return link.ErrConflict
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}
	f.byCode[l.Code] = l
	return nil
}

func (f *fakeRepo) GetByCode(_ context.Context, c string) (link.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	return l, nil
}

func (f *fakeRepo) RecordClick(_ context.Context, c string, click link.ClickEvent) (link.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	l.HitCount++
	f.byCode[c] = l
	f.nextID++
	click.ID = f.nextID
	click.Code = c
	if click.ClickedAt.IsZero() {
		click.ClickedAt = time.Now().UTC()
	}
	f.clicks = append(f.clicks, click)
	return l, nil
}

func (f *fakeRepo) ListClicks(_ context.Context, c string, limit int) ([]link.ClickEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]link.ClickEvent, 0)
	for i := len(f.clicks) - 1; i >= 0; i-- {
		if f.clicks[i].Code != c {
			continue
		}
		out = append(out, f.clicks[i])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeRepo) SetActive(_ context.Context, c string, active bool) (link.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	l.Active = active
	f.byCode[c] = l
	return l, nil
}

func (f *fakeRepo) mutate(code string, fn func(*link.Link)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l := f.byCode[code]
	fn(&l)
	f.byCode[code] = l
}

type fixedGenerator struct{ code string }

func (f fixedGenerator) Generate() (string, error) { return f.code, nil }

type sequenceGenerator struct {
	codes []string
	i     int
}

func (s *sequenceGenerator) Generate() (string, error) {
	if s.i >= len(s.codes) {
		return "", errors.New("no more codes")
	}
	c := s.codes[s.i]
	s.i++
	return c, nil
}

func TestIsAvailable(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name string
		link link.Link
		want bool
	}{
		{name: "active no expiry", link: link.Link{Active: true}, want: true},
		{name: "inactive", link: link.Link{Active: false}, want: false},
		{name: "expired", link: link.Link{Active: true, ExpiresAt: &past}, want: false},
		{name: "expires exactly now", link: link.Link{Active: true, ExpiresAt: &now}, want: false},
		{name: "not yet expired", link: link.Link{Active: true, ExpiresAt: &future}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.link.IsAvailable(now); got != tt.want {
				t.Fatalf("IsAvailable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateWithAliasAndDuplicateConflict(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc := link.NewService(repo, code.NewRandomGenerator(), "http://localhost:8080")
	ctx := context.Background()

	_, err := svc.Create(ctx, link.CreateInput{URL: "https://example.com", Alias: "my-link"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = svc.Create(ctx, link.CreateInput{URL: "https://example.com/other", Alias: "my-link"})
	if !errors.Is(err, link.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCreateWithoutAlias(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc := link.NewService(repo, fixedGenerator{code: "AbC1234"}, "http://localhost:8080/")
	res, err := svc.Create(context.Background(), link.CreateInput{URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Link.Code != "AbC1234" {
		t.Fatalf("code = %q", res.Link.Code)
	}
	if res.OwnerToken == "" || res.ShortURL != "http://localhost:8080/AbC1234" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !strings.HasPrefix(res.OwnerToken, "usk_") {
		t.Fatalf("owner token prefix: %q", res.OwnerToken)
	}
}

func TestCreateRetriesOnGeneratedCollision(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	ctx := context.Background()
	// Seed a conflicting code.
	_ = repo.Create(ctx, link.Link{
		Code:           "taken01",
		OriginalURL:    "https://example.com/old",
		OwnerTokenHash: []byte("hashhashhashhashhashhashhashhash"),
		Active:         true,
	})

	gen := &sequenceGenerator{codes: []string{"taken01", "fresh01"}}
	svc := link.NewService(repo, gen, "http://localhost:8080")
	res, err := svc.Create(ctx, link.CreateInput{URL: "https://example.com/new"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Link.Code != "fresh01" {
		t.Fatalf("code = %q, want fresh01", res.Link.Code)
	}
}

func TestCreateRejectsInvalidAlias(t *testing.T) {
	t.Parallel()
	svc := link.NewService(newFakeRepo(), code.NewRandomGenerator(), "http://localhost:8080")
	_, err := svc.Create(context.Background(), link.CreateInput{
		URL:   "https://example.com",
		Alias: "ab",
	})
	if !errors.Is(err, link.ErrInvalidAlias) {
		t.Fatalf("expected invalid alias, got %v", err)
	}
}

func TestCreateRejectsBadURL(t *testing.T) {
	t.Parallel()
	svc := link.NewService(newFakeRepo(), code.NewRandomGenerator(), "http://localhost:8080")
	_, err := svc.Create(context.Background(), link.CreateInput{URL: "javascript:alert(1)"})
	if !errors.Is(err, link.ErrInvalidURL) {
		t.Fatalf("expected invalid url, got %v", err)
	}
}

func TestDeactivateRequiresValidToken(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc := link.NewService(repo, code.NewRandomGenerator(), "http://localhost:8080")
	ctx := context.Background()

	res, err := svc.Create(ctx, link.CreateInput{URL: "https://example.com", Alias: "secure"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Deactivate(ctx, "secure", "")
	if !errors.Is(err, link.ErrUnauthorized) {
		t.Fatalf("expected unauthorized for empty token, got %v", err)
	}

	_, err = svc.Deactivate(ctx, "secure", "wrong-token")
	if !errors.Is(err, link.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}

	_, err = svc.Deactivate(ctx, "missing", res.OwnerToken)
	if !errors.Is(err, link.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	l, err := svc.Deactivate(ctx, "secure", res.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if l.Active {
		t.Fatal("expected inactive")
	}

	_, err = svc.ResolveRedirect(ctx, "secure", link.ClickInput{})
	if !errors.Is(err, link.ErrGone) {
		t.Fatalf("expected gone, got %v", err)
	}
}

func TestResolveRedirectNotFoundAndExpired(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc := link.NewService(repo, code.NewRandomGenerator(), "http://localhost:8080")
	ctx := context.Background()

	_, err := svc.ResolveRedirect(ctx, "nope", link.ClickInput{})
	if !errors.Is(err, link.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}

	_, err = svc.Create(ctx, link.CreateInput{URL: "https://example.com", Alias: "expiring"})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Minute)
	repo.mutate("expiring", func(l *link.Link) { l.ExpiresAt = &past })

	_, err = svc.ResolveRedirect(ctx, "expiring", link.ClickInput{})
	if !errors.Is(err, link.ErrGone) {
		t.Fatalf("expected gone for expired, got %v", err)
	}
}

func TestAnalyticsRequiresOwnerTokenAndRecordsClicks(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	svc := link.NewService(repo, code.NewRandomGenerator(), "http://localhost:8080")
	ctx := context.Background()

	res, err := svc.Create(ctx, link.CreateInput{URL: "https://example.com", Alias: "stats"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Analytics(ctx, "stats", "", 10)
	if !errors.Is(err, link.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}

	_, err = svc.Analytics(ctx, "stats", "bad", 10)
	if !errors.Is(err, link.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}

	longRef := strings.Repeat("r", 3000)
	longUA := strings.Repeat("u", 3000)
	if _, err := svc.ResolveRedirect(ctx, "stats", link.ClickInput{
		Referrer:  longRef,
		UserAgent: longUA,
	}); err != nil {
		t.Fatal(err)
	}

	events, err := svc.Analytics(ctx, "stats", res.OwnerToken, 0) // default limit
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("clicks = %d, want 1", len(events))
	}
	if len(events[0].Referrer) != 2048 || len(events[0].UserAgent) != 2048 {
		t.Fatalf("expected truncation to 2048, got ref=%d ua=%d", len(events[0].Referrer), len(events[0].UserAgent))
	}

	meta, err := svc.Metadata(ctx, "stats")
	if err != nil {
		t.Fatal(err)
	}
	if meta.HitCount != 1 {
		t.Fatalf("hit_count = %d", meta.HitCount)
	}
}
