package httpapi

import (
	"net/http"
	"strings"
)

// bearerToken extracts an opaque Bearer token from Authorization.
// It does not interpret JWT structure — the value is treated as a random secret.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
