# URL Shortener

Production-minded Go REST service that shortens URLs, redirects, exposes metadata, and lets owners deactivate links via opaque Bearer tokens.

## Features

- **Create** a short link (auto code or custom alias)
- **Redirect** `GET /{code}` → original URL (`302`)
- **Metadata** original URL, created time, hit count, active flag
- **Deactivate** (owner-only) without deleting the row
- Validation, structured errors, unit tests, GitHub Actions CI

## Quick start

### Prerequisites

- Go 1.22+
- Docker / Docker Compose
- Make (optional but recommended)

### Run with Make

```bash
make up      # Postgres
make run     # API using .env.dev (and optional .env overrides)
make test    # unit tests (no DB required)
make smoke   # curl happy path (server must be running)
make help    # list all targets
```

### Run without Make

```bash
docker compose up -d
set -a && source .env.dev && set +a
go run ./cmd/server
```

### Example calls

Assume the server is on `http://localhost:8080` (`make run`).

```bash
# 1) Health
curl -s http://localhost:8080/healthz
# → 200 {"status":"ok"}

# 2) Create (auto-generated code)
curl -s -X POST http://localhost:8080/api/v1/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/auto"}'
# → 201 { code, short_url, original_url, owner_token, active, created_at }

# 3) Create (custom alias) — save owner_token
curl -s -X POST http://localhost:8080/api/v1/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/docs","alias":"my-link"}'
# → 201 ... copy owner_token into OWNER_TOKEN

export OWNER_TOKEN='usk_...'   # from create response

# 4) Duplicate alias (failure)
curl -s -i -X POST http://localhost:8080/api/v1/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/other","alias":"my-link"}'
# → 409 {"error":"alias already exists"}

# 5) Invalid URL (failure)
curl -s -i -X POST http://localhost:8080/api/v1/links \
  -H 'Content-Type: application/json' \
  -d '{"url":"javascript:alert(1)"}'
# → 400 {"error":"invalid url: ..."}

# 6) Redirect
curl -s -i http://localhost:8080/my-link
# → 302 Location: https://example.com/docs

# 7) Metadata
curl -s http://localhost:8080/api/v1/links/my-link
# → 200 { code, original_url, hit_count, active, created_at }

# 7b) Analytics (owner only) — after at least one redirect
curl -s http://localhost:8080/api/v1/links/my-link/analytics \
  -H "Authorization: Bearer $OWNER_TOKEN"
# → 200 { code, clicks: [{ id, code, clicked_at, referrer, user_agent }, ...] }

# 8) Deactivate without token (failure)
curl -s -i -X PATCH http://localhost:8080/api/v1/links/my-link \
  -H 'Content-Type: application/json' \
  -d '{"active":false}'
# → 401 {"error":"missing or invalid Authorization Bearer token"}

# 9) Deactivate (owner)
curl -s -X PATCH http://localhost:8080/api/v1/links/my-link \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $OWNER_TOKEN" \
  -d '{"active":false}'
# → 200 { ..., "active": false }

# 10) Redirect after deactivate
curl -s -i http://localhost:8080/my-link
# → 410 {"error":"link is inactive or expired"}

# 11) Unknown code
curl -s -i http://localhost:8080/api/v1/links/does-not-exist
# → 404 {"error":"link not found"}
```

## Tests

```bash
make test                 # unit tests (fakes, no DB)
make test-integration     # Postgres-backed tests (.env.test + running DB)
```

Unit tests never read env files. Integration tests load `.env.test` via Make (or CI `env:`).

## Configuration

| File | Committed? | Purpose |
|---|---|---|
| [`.env.dev`](.env.dev) | yes | Local API defaults (`make run`) |
| [`.env.test`](.env.test) | yes | Integration-test DB defaults (`make test-integration`) |
| `.env` | no (gitignored) | Optional machine-local overrides |

`make` loads `.env.dev`, then overlays `.env` if present.

| Env var | Required at runtime | Description |
|---|---|---|
| `DATABASE_URL` | yes (server + integration tests) | Postgres connection string |
| `BASE_URL` | yes (server) | Public base used to build `short_url` |
| `HTTP_ADDR` | no | Listen address (default `:8080`) |
| `MIGRATIONS_DIR` | no | Path to SQL migrations (default `migrations`) |

## Troubleshooting

### `make up` / `docker: No such file or directory`

Install Docker (Desktop / OrbStack / `docker.io`). Confirm `docker version` works in this terminal.

In some nested/devcontainer environments the Docker **daemon cannot run containers** (overlay mount permissions). In that case `make up` falls back to a **local Postgres** service if installed. One-time role/DB setup:

```bash
sudo bash -c 'runuser -u postgres -- psql -c "CREATE ROLE shortener LOGIN PASSWORD '\''shortener'\'';" || true'
sudo bash -c 'runuser -u postgres -- createdb -O shortener shortener || true'
```

Then `make run` with `.env.dev` pointing at `localhost:5432`.

## API summary

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/links` | public | Create (`url`, optional `alias`) |
| `GET` | `/{code}` | public | Redirect |
| `GET` | `/api/v1/links/{code}` | public | Metadata |
| `GET` | `/api/v1/links/{code}/analytics` | Bearer owner token | Per-click analytics |
| `PATCH` | `/api/v1/links/{code}` | Bearer owner token | Set `{"active":false}` |
| `GET` | `/healthz` | public | Liveness |

## Docs

- [ARCHITECTURE.md](ARCHITECTURE.md) — layers and request flow
- [DEPENDENCIES.md](DEPENDENCIES.md) — module choices, alternatives, and rejections

## Next steps

- Optional TTL (`expires_at`) on create
- Deploy to DigitalOcean App Platform using the included Dockerfile
