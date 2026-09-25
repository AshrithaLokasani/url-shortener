.PHONY: help up down env run stop build test test-race test-integration tidy fmt docker-build smoke clean

# Load committed .env.dev, then optional local .env overrides (gitignored).
include .env.dev
-include .env
export

BIN := bin/server

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-14s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

env: ## Show effective env (from .env.dev + optional .env)
	@echo "DATABASE_URL=$(DATABASE_URL)"
	@echo "BASE_URL=$(BASE_URL)"
	@echo "HTTP_ADDR=$(HTTP_ADDR)"

up: ## Start Postgres (Docker Compose if usable, else local postgres service)
	@if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then \
		if docker compose up -d; then \
			echo "Waiting for Compose Postgres..."; \
			until docker compose exec -T postgres pg_isready -U shortener -d shortener >/dev/null 2>&1; do sleep 1; done; \
			echo "Postgres is ready (docker compose)."; \
			exit 0; \
		fi; \
		echo "warning: docker compose failed; falling back to local Postgres."; \
	else \
		echo "warning: docker unavailable; using local Postgres service."; \
	fi
	@command -v pg_isready >/dev/null 2>&1 || { echo "error: neither docker compose nor local Postgres tools found"; exit 1; }
	@sudo service postgresql start >/dev/null 2>&1 || true
	@until pg_isready -h 127.0.0.1 -p 5432 >/dev/null 2>&1; do sleep 1; done
	@echo "Postgres is ready (local)."
	@echo "Ensure role/db exist (one-time): see README Troubleshooting if login fails."

down: ## Stop docker compose services (no-op if not using Compose)
	@command -v docker >/dev/null 2>&1 && docker compose down || echo "docker compose not running"

run: ## Run the API with .env.dev (+ .env overrides)
	go run ./cmd/server

stop: ## Stop process listening on HTTP_ADDR (default :8080)
	@port=$${HTTP_ADDR#:}; port=$${port:-8080}; \
	pids=$$(ss -ltnp 2>/dev/null | sed -n "s/.*:$${port} .*pid=\\([0-9]*\\).*/\\1/p" | sort -u); \
	if [ -z "$$pids" ]; then echo "nothing listening on :$${port}"; exit 0; fi; \
	echo "stopping PIDs on :$${port}: $$pids"; kill $$pids 2>/dev/null || true

build: ## Build server binary to bin/server
	mkdir -p bin
	go build -o $(BIN) ./cmd/server

test: ## Run unit tests (no DB / env file required)
	go test ./...

test-race: ## Run unit tests with the race detector
	go test -race ./...

test-integration: ## Run DB integration tests (requires Postgres; loads .env.test)
	@test -f .env.test || { echo "missing .env.test"; exit 1; }
	set -a && . ./.env.test && set +a && go test ./internal/storage/postgres -tags=integration -count=1 -v

tidy: ## Sync go.mod / go.sum
	go mod tidy

fmt: ## Format Go sources
	go fmt ./...

docker-build: ## Build the production Docker image
	docker build -t url-shortener:local .

smoke: ## Curl happy-path against a running server on BASE_URL
	@echo "Creating link..."
	@RESP=$$(curl -sf -X POST "$(BASE_URL)/api/v1/links" \
		-H 'Content-Type: application/json' \
		-d '{"url":"https://example.com/smoke","alias":"smoke-demo"}'); \
	echo "$$RESP"; \
	TOKEN=$$(printf '%s' "$$RESP" | sed -n 's/.*"owner_token":"\([^"]*\)".*/\1/p'); \
	echo "Redirect:"; curl -sI "$(BASE_URL)/smoke-demo" | head -n 5; \
	echo "Metadata:"; curl -sf "$(BASE_URL)/api/v1/links/smoke-demo"; echo; \
	echo "Deactivate:"; curl -sf -X PATCH "$(BASE_URL)/api/v1/links/smoke-demo" \
		-H 'Content-Type: application/json' \
		-H "Authorization: Bearer $$TOKEN" \
		-d '{"active":false}'; echo; \
	echo "Redirect after deactivate (expect 410):"; \
	curl -s -o /dev/null -w "HTTP %{http_code}\n" "$(BASE_URL)/smoke-demo"

clean: ## Remove build artifacts
	rm -rf bin
