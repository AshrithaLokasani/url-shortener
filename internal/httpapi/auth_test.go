package httpapi

import (
	"net/http"
	"testing"
)

func TestBearerToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		header    string
		wantToken string
		wantOK    bool
	}{
		{name: "missing", header: "", wantOK: false},
		{name: "basic rejected", header: "Basic abc", wantOK: false},
		{name: "bearer empty", header: "Bearer ", wantOK: false},
		{name: "bearer spaces", header: "Bearer    ", wantOK: false},
		{name: "valid", header: "Bearer usk_abc", wantToken: "usk_abc", wantOK: true},
		{name: "valid with trailing space trimmed", header: "Bearer usk_abc  ", wantToken: "usk_abc", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &http.Request{Header: make(http.Header)}
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			got, ok := bearerToken(r)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantToken {
				t.Fatalf("token = %q, want %q", got, tt.wantToken)
			}
		})
	}
}
