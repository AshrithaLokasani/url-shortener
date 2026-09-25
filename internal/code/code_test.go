package code_test

import (
	"strings"
	"testing"

	"github.com/url-shortener/url-shortener/internal/code"
)

func TestValidateAlias(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		alias   string
		wantErr bool
	}{
		{name: "valid", alias: "my-link", wantErr: false},
		{name: "underscores", alias: "my_link_1", wantErr: false},
		{name: "too short", alias: "ab", wantErr: true},
		{name: "bad chars", alias: "my link!", wantErr: true},
		{name: "reserved", alias: "api", wantErr: true},
		{name: "empty", alias: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := code.ValidateAlias(tt.alias)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "https ok", raw: "https://example.com/path", wantErr: false},
		{name: "http ok", raw: "http://example.com", wantErr: false},
		{name: "javascript rejected", raw: "javascript:alert(1)", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
		{name: "no host", raw: "https://", wantErr: true},
		{name: "ftp rejected", raw: "ftp://files.example.com/a", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := code.ValidateURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRandomGeneratorAlphabetAndLength(t *testing.T) {
	t.Parallel()
	g := code.NewRandomGenerator()
	got, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 7 {
		t.Fatalf("length = %d, want 7", len(got))
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for _, c := range got {
		if !strings.ContainsRune(alphabet, c) {
			t.Fatalf("unexpected rune %q in %q", c, got)
		}
	}
}
