package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/url-shortener/url-shortener/internal/link"
)

type createRequest struct {
	URL   string `json:"url"`
	Alias string `json:"alias,omitempty"`
}

type linkResponse struct {
	Code        string     `json:"code"`
	ShortURL    string     `json:"short_url,omitempty"`
	OriginalURL string     `json:"original_url"`
	OwnerToken  string     `json:"owner_token,omitempty"`
	HitCount    int64      `json:"hit_count,omitempty"`
	Active      bool       `json:"active"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type deactivateRequest struct {
	Active *bool `json:"active"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	result, err := s.svc.Create(r.Context(), link.CreateInput{URL: req.URL, Alias: req.Alias})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, linkResponse{
		Code:        result.Link.Code,
		ShortURL:    result.ShortURL,
		OriginalURL: result.Link.OriginalURL,
		OwnerToken:  result.OwnerToken,
		Active:      result.Link.Active,
		CreatedAt:   result.Link.CreatedAt,
		ExpiresAt:   result.Link.ExpiresAt,
	})
}

func (s *Server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" || strings.Contains(code, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	l, err := s.svc.ResolveRedirect(r.Context(), code, link.ClickInput{
		Referrer:  r.Referer(),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	http.Redirect(w, r, l.OriginalURL, http.StatusFound)
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	l, err := s.svc.Metadata(r.Context(), code)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, linkResponse{
		Code:        l.Code,
		OriginalURL: l.OriginalURL,
		HitCount:    l.HitCount,
		Active:      l.Active,
		CreatedAt:   l.CreatedAt,
		ExpiresAt:   l.ExpiresAt,
	})
}

func (s *Server) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid Authorization Bearer token")
		return
	}
	events, err := s.svc.Analytics(r.Context(), code, token, 100)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	out := make([]clickEventResponse, 0, len(events))
	for _, e := range events {
		out = append(out, clickEventResponse{
			ID:        e.ID,
			Code:      e.Code,
			ClickedAt: e.ClickedAt,
			Referrer:  e.Referrer,
			UserAgent: e.UserAgent,
		})
	}
	writeJSON(w, http.StatusOK, analyticsResponse{
		Code:   code,
		Clicks: out,
	})
}

type clickEventResponse struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	ClickedAt time.Time `json:"clicked_at"`
	Referrer  string    `json:"referrer"`
	UserAgent string    `json:"user_agent"`
}

type analyticsResponse struct {
	Code   string               `json:"code"`
	Clicks []clickEventResponse `json:"clicks"`
}

func (s *Server) handleDeactivate(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing or invalid Authorization Bearer token")
		return
	}
	var req deactivateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Active == nil || *req.Active {
		writeError(w, http.StatusBadRequest, "active must be false to deactivate")
		return
	}
	l, err := s.svc.Deactivate(r.Context(), code, token)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, linkResponse{
		Code:        l.Code,
		OriginalURL: l.OriginalURL,
		HitCount:    l.HitCount,
		Active:      l.Active,
		CreatedAt:   l.CreatedAt,
		ExpiresAt:   l.ExpiresAt,
	})
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, link.ErrInvalidURL), errors.Is(err, link.ErrInvalidAlias), errors.Is(err, link.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, link.ErrConflict):
		writeError(w, http.StatusConflict, "alias already exists")
	case errors.Is(err, link.ErrNotFound):
		writeError(w, http.StatusNotFound, "link not found")
	case errors.Is(err, link.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, link.ErrGone):
		writeError(w, http.StatusGone, "link is inactive or expired")
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
