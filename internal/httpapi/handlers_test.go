package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	clicks []link.ClickEvent
	nextID int64
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

func (m *memRepo) RecordClick(_ context.Context, c string, click link.ClickEvent) (link.Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.byCode[c]
	if !ok {
		return link.Link{}, link.ErrNotFound
	}
	l.HitCount++
	m.byCode[c] = l
	m.nextID++
	click.ID = m.nextID
	click.Code = c
	m.clicks = append(m.clicks, click)
	return l, nil
}

func (m *memRepo) ListClicks(_ context.Context, c string, limit int) ([]link.ClickEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]link.ClickEvent, 0)
	for i := len(m.clicks) - 1; i >= 0; i-- {
		if m.clicks[i].Code != c {
			continue
		}
		out = append(out, m.clicks[i])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
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

func createLink(t *testing.T, h http.Handler, alias, url string) (token string) {
	t.Helper()
	body := []byte(`{"url":"` + url + `","alias":"` + alias + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rr.Code, rr.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	token, _ = created["owner_token"].(string)
	if token == "" {
		t.Fatal("missing owner_token")
	}
	return token
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
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

func TestCreateValidationErrors(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)

	t.Run("bad json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader([]byte(`{`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", rr.Code)
		}
	})

	t.Run("bad url", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader([]byte(`{"url":"javascript:alert(1)"}`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("bad alias", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader([]byte(`{"url":"https://example.com","alias":"ab"}`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/links", bytes.NewReader([]byte(`{"url":"https://example.com","nope":1}`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestDeactivateWithoutTokenReturns401(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	_ = createLink(t, h, "auth-me", "https://example.com")

	patch := httptest.NewRequest(http.MethodPatch, "/api/v1/links/auth-me", bytes.NewReader([]byte(`{"active":false}`)))
	patch.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, patch)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestDeactivateWrongTokenAndBadBody(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	_ = createLink(t, h, "tok-me", "https://example.com")

	wrong := httptest.NewRequest(http.MethodPatch, "/api/v1/links/tok-me", bytes.NewReader([]byte(`{"active":false}`)))
	wrong.Header.Set("Content-Type", "application/json")
	wrong.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, wrong)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", rr.Code)
	}

	badBody := httptest.NewRequest(http.MethodPatch, "/api/v1/links/tok-me", bytes.NewReader([]byte(`{"active":true}`)))
	badBody.Header.Set("Content-Type", "application/json")
	badBody.Header.Set("Authorization", "Bearer anything")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, badBody)
	if rr2.Code != http.StatusBadRequest {
		t.Fatalf("active true status = %d body=%s", rr2.Code, rr2.Body.String())
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

func TestMetadata(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	_ = createLink(t, h, "meta-1", "https://example.com/meta")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/links/meta-1", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["original_url"] != "https://example.com/meta" {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["owner_token"]; ok {
		t.Fatal("metadata must not include owner_token")
	}
}

func TestCreateAndRedirectHappyPath(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	token := createLink(t, h, "ok-link", "https://example.com/x")

	redir := httptest.NewRequest(http.MethodGet, "/ok-link", nil)
	redir.Header.Set("Referer", "https://ref.example")
	redir.Header.Set("User-Agent", "TestAgent/1")
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

func TestAnalyticsRequiresBearerAndReturnsClicks(t *testing.T) {
	t.Parallel()
	h := newTestServer(t)
	token := createLink(t, h, "an-link", "https://example.com/a")

	unauth := httptest.NewRequest(http.MethodGet, "/api/v1/links/an-link/analytics", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, unauth)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr2.Code)
	}

	wrong := httptest.NewRequest(http.MethodGet, "/api/v1/links/an-link/analytics", nil)
	wrong.Header.Set("Authorization", "Bearer nope")
	rrWrong := httptest.NewRecorder()
	h.ServeHTTP(rrWrong, wrong)
	if rrWrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", rrWrong.Code)
	}

	redir := httptest.NewRequest(http.MethodGet, "/an-link", nil)
	redir.Header.Set("Referer", "https://ref.example")
	redir.Header.Set("User-Agent", "TestAgent/1.0")
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, redir)
	if rr3.Code != http.StatusFound {
		t.Fatalf("redirect = %d", rr3.Code)
	}

	stats := httptest.NewRequest(http.MethodGet, "/api/v1/links/an-link/analytics", nil)
	stats.Header.Set("Authorization", "Bearer "+token)
	rr4 := httptest.NewRecorder()
	h.ServeHTTP(rr4, stats)
	if rr4.Code != http.StatusOK {
		t.Fatalf("analytics = %d %s", rr4.Code, rr4.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rr4.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	clicks, _ := body["clicks"].([]any)
	if len(clicks) != 1 {
		t.Fatalf("clicks = %#v", body["clicks"])
	}
	first, _ := clicks[0].(map[string]any)
	if first["referrer"] != "https://ref.example" || first["user_agent"] != "TestAgent/1.0" {
		t.Fatalf("click = %#v", first)
	}
}
