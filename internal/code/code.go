package code

import (
	"crypto/rand"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	alphabet       = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	defaultLength  = 7
	minAliasLength = 3
	maxAliasLength = 32
)

var (
	aliasPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	reserved     = map[string]struct{}{
		"api":         {},
		"healthz":     {},
		"readyz":      {},
		"metrics":     {},
		"favicon.ico": {},
	}
)

// Generator produces unique short codes.
type Generator interface {
	Generate() (string, error)
}

// RandomGenerator creates cryptographically random base62 codes.
type RandomGenerator struct {
	Length int
}

// NewRandomGenerator returns a generator with the default code length.
func NewRandomGenerator() *RandomGenerator {
	return &RandomGenerator{Length: defaultLength}
}

// Generate returns a random base62 string of the configured length.
func (g *RandomGenerator) Generate() (string, error) {
	n := g.Length
	if n <= 0 {
		n = defaultLength
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(out), nil
}

// ValidateAlias checks a user-supplied short code.
func ValidateAlias(alias string) error {
	if alias == "" {
		return fmt.Errorf("alias is required")
	}
	if len(alias) < minAliasLength || len(alias) > maxAliasLength {
		return fmt.Errorf("alias must be between %d and %d characters", minAliasLength, maxAliasLength)
	}
	if !aliasPattern.MatchString(alias) {
		return fmt.Errorf("alias may only contain letters, digits, underscores, and hyphens")
	}
	if _, ok := reserved[strings.ToLower(alias)]; ok {
		return fmt.Errorf("alias %q is reserved", alias)
	}
	return nil
}

// ValidateURL ensures the URL is an absolute http(s) URL with a host.
func ValidateURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("url is required")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return fmt.Errorf("url is malformed")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("url must include a host")
	}
	return nil
}
