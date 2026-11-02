.DEFAULT_GOAL := help

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/titiabdelghafour8-lab/certwatch/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)
TEST_DB ?= postgres://certwatch:certwatch@localhost:5432/certwatch_test?sslmode=disable

.PHONY: help build test test-integration lint fmt tidy run up down logs docker release-snapshot clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## Build the CLI and server into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/certwatch ./cmd/certwatch
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/certwatch-server ./cmd/certwatch-server

test: ## Run unit tests
	go test -race ./...

test-integration: ## Run all tests including PostgreSQL integration tests (needs `make up` or TEST_DB)
	docker compose exec -T postgres psql -U certwatch -tc "SELECT 1 FROM pg_database WHERE datname='certwatch_test'" | grep -q 1 || \
		docker compose exec -T postgres createdb -U certwatch certwatch_test
	CERTWATCH_TEST_DATABASE_URL="$(TEST_DB)" go test -race -p 1 -count=1 ./...

lint: ## Run linters
	golangci-lint run

fmt: ## Format code
	gofmt -w .

tidy: ## Tidy go.mod
	go mod tidy

run: ## Run the server locally against the compose database
	CERTWATCH_DATABASE_URL=postgres://certwatch:certwatch@localhost:5432/certwatch?sslmode=disable \
	CERTWATCH_LOG_FORMAT=text CERTWATCH_ALLOW_PRIVATE_TARGETS=true go run ./cmd/certwatch-server all

up: ## Start the local stack (postgres, mailpit, server)
	docker compose up -d --build

down: ## Stop the local stack
	docker compose down

logs: ## Tail server logs
	docker compose logs -f certwatch

docker: ## Build the server image
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE) -t certwatch:$(VERSION) .

release-snapshot: ## Build release artifacts locally without publishing
	goreleaser release --snapshot --clean

clean: ## Remove build output
	rm -rf bin dist coverage.out
