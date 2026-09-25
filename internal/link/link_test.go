package link_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/url-shortener/url-shortener/internal/code"
	"github.com/url-shortener/url-shortener/internal/link"
)

type fakeRepo struct {
	mu    sync.Mutex
	byCode map[string]link.Link
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

func (f *fakeRepo) IncrementHits(_ context.Context, c string) (link.Link, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	l.HitCount++
	f.byCode[c] = l
	return l, nil
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

type fixedGenerator struct{ code string }

func (f fixedGenerator) Generate() (string, error) { return f.code, nil }

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
	svc := link.NewService(repo, fixedGenerator{code: "AbC1234"}, "http://localhost:8080")
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

	_, err = svc.Deactivate(ctx, "secure", "wrong-token")
	if !errors.Is(err, link.ErrUnauthorized) {
		t.Fatalf("expected unauthorized, got %v", err)
	}

	l, err := svc.Deactivate(ctx, "secure", res.OwnerToken)
	if err != nil {
		t.Fatal(err)
	}
	if l.Active {
		t.Fatal("expected inactive")
	}

	_, err = svc.ResolveRedirect(ctx, "secure")
	if !errors.Is(err, link.ErrGone) {
		t.Fatalf("expected gone, got %v", err)
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
