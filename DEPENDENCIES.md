# Dependency choices

Each external (and key standard-library) module we use, why we chose it, alternatives considered, and why those were rejected.

## Language & API surface

### Go 1.22+
- **Why**: Strong standard library, simple deployment (single binary), excellent concurrency and testing story, common in security-minded backend teams.
- **Alternatives**: Node/TypeScript, Python/FastAPI, Rust.
- **Why not**: Node/Python are fine for speed of writing but weaker “production binary + typed boundaries” signal for this take-home; Rust would burn the timebox on ownership fights.

### REST over `net/http` (not gRPC)
- **Module**: standard library [`net/http`](https://pkg.go.dev/net/http)
- **Why**: Short links are consumed by browsers and arbitrary HTTP clients. Redirection needs a real HTTP `302` + `Location`. Go 1.22 method/path routing removes the need for a router framework.
- **Alternatives**: gRPC, GraphQL, Gin/Echo/Chi on top of HTTP.
- **Why not**:
  - **gRPC**: Excellent for internal service meshes; poor fit for public redirects and zero-friction curl/browser access.
  - **GraphQL**: Overkill for four endpoints.
  - **Gin/Echo/Chi**: Extra dependency and framework idioms without buying much once Go 1.22 routing exists. Chi is the closest “acceptable” alternative if middleware composition becomes complex later.

## Configuration

### `github.com/caarlos0/env/v11`
- **Why**: Typed env structs, fail-fast on missing required vars, tiny API.
- **Alternatives**: `spf13/viper`, manual `os.Getenv`, `kelseyhightower/envconfig`.
- **Why not**: Viper is heavy (files, remote, watchers) for a service that only needs env; manual getenv scatters validation; envconfig is fine but caarlos0/env is actively maintained and ergonomic.

## Database

### Postgres 16
- **Why**: Production-grade relational store, unique constraints for aliases, easy App Platform / managed offering later.
- **Alternatives**: SQLite, MySQL, in-memory map, Redis-only.
- **Why not**: SQLite is great for demos but weaker “production-ready” signal when the brief mentions deployability; Redis-only loses durable relational semantics; in-memory loses restarts.

### `github.com/jackc/pgx/v5` + `pgxpool`
- **Why**: Best-in-class Postgres driver for Go; native protocol; pooled connections; clear error types (`pgconn.PgError`) for SQLSTATE mapping (`23505` → conflict).
- **Alternatives**: `database/sql` + `lib/pq`, GORM, ent, sqlc.
- **Why not**:
  - **lib/pq**: Maintenance mode; pgx is the modern default.
  - **GORM**: Hides SQL, encourages fat models, awkward for a small schema and interview clarity.
  - **sqlc**: Excellent at scale; generators add setup time we don’t need for one table.
  - **ent**: Same ceremony concern for this size.

### `github.com/pressly/goose/v3`
- **Why**: Boring, SQL-first migrations; runs on startup; easy to reason about in reviews.
- **Alternatives**: golang-migrate, Atlas, GORM AutoMigrate, hand-rolled.
- **Why not**: AutoMigrate is risky in production narratives; Atlas is powerful but heavier; golang-migrate is a close second — goose won for embed/CLI simplicity with pgx stdlib bridge.

## Application structure (stdlib packages)

### `context`
- **Why**: First-class cancellation through handlers → service → DB; required for graceful shutdown and request-scoped timeouts.
- **Alternative**: Ignoring context / using globals.
- **Why not**: Globals and orphaned queries are non-idiomatic and fail under load/shutdown.

### `log/slog`
- **Why**: Structured logging in the standard library (Go 1.21+); JSON handler for production.
- **Alternatives**: zap, zerolog, logrus.
- **Why not**: Third-party loggers are great at huge scale; slog is enough and removes a dependency.

### `encoding/json`
- **Why**: Sufficient for small DTOs; `DisallowUnknownFields` for strict input.
- **Alternatives**: easyjson, go-json, protobuf.
- **Why not**: Micro-optimizations not justified; protobuf belongs with gRPC.

### `crypto/rand`, `crypto/sha256`, `crypto/subtle`
- **Why**: Owner tokens need cryptographic randomness; SHA-256 is appropriate for **high-entropy** secrets at rest; constant-time compare avoids timing leaks on token checks.
- **Alternatives**: UUID-as-token stored plaintext; bcrypt/argon2; JWT HMAC.
- **Why not**:
  - **Plaintext UUID in DB**: Fine for demos; weaker under backup/DB leak narratives.
  - **bcrypt/argon2**: Designed for low-entropy passwords; unnecessary latency for 256-bit random tokens.
  - **JWT**: Stateless claims fight revocation; no federated identity need here (see security notes in ARCHITECTURE.md).

### `net/url` + `regexp` (validation)
- **Why**: URL parsing and alias charset checks without a validation framework.
- **Alternatives**: `go-playground/validator`, ozzo-validation.
- **Why not**: Two fields don’t justify struct-tag frameworks.

### `testing` + `net/http/httptest`
- **Why**: Idiomatic table-driven unit tests and handler tests with fakes; no DB required for core logic.
- **Alternatives**: testify suites, testcontainers-go everywhere, Ginkgo.
- **Why not**: testify is optional sugar (we stayed on stdlib asserts for fewer deps); full testcontainers for every run slows CI — fake `Repository` covers SOLID/unit paths; Postgres is still used for local/manual smoke.

## Explicitly not used (and why)

| Rejected | Reason |
|---|---|
| Gin / Echo / Fiber | Framework lock-in; stdlib routing is enough |
| gRPC / protobuf | Wrong public redirect UX |
| GORM / ent | Too much magic for one table |
| Redis cache | Premature; add when redirect latency/metrics demand it |
| OpenAPI codegen | Timebox; hand-documented API in README |
| OAuth / user accounts | No identity model; capability token is the right authz |
| JWT access tokens | Revocation + key mgmt without multi-service benefit |
| Viper | Over-configured for env-only |

## Versioning note

Dependencies are pinned via `go.mod` / `go.sum` after `go mod tidy`. Prefer minimal direct requires: `env`, `pgx`, `goose`.
