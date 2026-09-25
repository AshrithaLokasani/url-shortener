package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/url-shortener/url-shortener/internal/code"
	"github.com/url-shortener/url-shortener/internal/httpapi"
	"github.com/url-shortener/url-shortener/internal/link"
)

type memRepo struct {
	mu     sync.Mutex
	byCode map[string]link.Link
}

func newMemRepo() *memRepo {
	return &memRepo{byCode: map[string]link.Link{}}
}

func (m *memRepo) Create(_ context.Context, l link.Link) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byCode[l.Code]; ok {
		return link.ErrConflict
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}
	m.byCode[l.Code] = l
	return nil
}

func (m *memRepo) GetByCode(_ context.Context, c string) (link.Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	return l, nil
}

func (m *memRepo) IncrementHits(_ context.Context, c string) (link.Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	l.HitCount++
	m.byCode[c] = l
	return l, nil
}

func (m *memRepo) SetActive(_ context.Context, c string, active bool) (link.Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	l.Active = active
	m.byCode[c] = l
	return l, nil
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	svc := link.NewService(newMemRepo(), code.NewRandomGenerator(), "http://localhost:8080")
	return httpapi.New(svc, slog.New(slog.NewTextHandler(io.Discard, nil)), ":0").Handler()
}

func TestCreateDuplicateAliasReturns409(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	body := []byte(`{"url":"https://example.com","alias":"dup"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first create status = %d body=%s", rr.Code, rr.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestDeactivateWithoutTokenReturns401(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	createBody := []byte(`{"url":"https://example.com","alias":"auth-me"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rr.Code)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/v1/links/auth-me", bytes.NewReader([]byte(`{"active":false}`)))
	patch.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, patch)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestRedirectMissingCodeReturns404(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestCreateAndRedirectHappyPath(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader([]byte(`{"url":"https://example.com/x","alias":"ok-link"}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	token, _ := created["owner_token"].(string)

	redir := httptest.NewRequest(http.MethodGet, "/ok-link", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, redir)
	if rr2.Code != http.StatusFound {
		t.Fatalf("redirect = %d", rr2.Code)
	}
	if loc := rr2.Header().Get("Location"); loc != "https://example.com/x" {
		t.Fatalf("location = %q", loc)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/v1/links/ok-link", bytes.NewReader([]byte(`{"active":false}`)))
	patch.Header.Set("Content-Type", "application/json")
	patch.Header.Set("Authorization", "Bearer "+token)
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, patch)
	if rr3.Code != http.StatusOK {
		t.Fatalf("deactivate = %d %s", rr3.Code, rr3.Body.String())
	}

	redir2 := httptest.NewRequest(http.MethodGet, "/ok-link", nil)
	rr4 := httptest.NewRecorder()
	h.ServeHTTP(rr4, redir2)
	if rr4.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", rr4.Code)
	}
}
