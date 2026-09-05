# ═══════════════════════════════════════════════════════════════
# Arbitrage Platform — Makefile
# ═══════════════════════════════════════════════════════════════

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ═══════════════════════════════════════════════════════════════
# Git Hooks
# ═══════════════════════════════════════════════════════════════

.PHONY: hooks-install hooks-uninstall

hooks-install: ## Install git hooks (pre-commit, pre-push)
	@echo "Installing git hooks..."
	@mkdir -p .git/hooks
	@echo '#!/bin/sh' > .git/hooks/pre-commit
	@echo 'echo "Running pre-commit checks..."' >> .git/hooks/pre-commit
	@echo 'make check' >> .git/hooks/pre-commit
	@echo 'exit $$?' >> .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit
	@echo '#!/bin/sh' > .git/hooks/pre-push
	@echo 'echo "Running pre-push checks..."' >> .git/hooks/pre-push
	@echo 'make test-unit' >> .git/hooks/pre-push
	@echo 'exit $$?' >> .git/hooks/pre-push
	@chmod +x .git/hooks/pre-push
	@echo "✓ Git hooks installed"

hooks-uninstall: ## Remove git hooks
	@rm -f .git/hooks/pre-commit .git/hooks/pre-push
	@echo "✓ Git hooks removed"

# ═══════════════════════════════════════════════════════════════
# Code Quality
# ═══════════════════════════════════════════════════════════════

.PHONY: fmt lint vet check

fmt: ## Format code with gofmt
	@echo "Formatting code..."
	@gofmt -s -w .
	@echo "✓ Formatted"

lint: ## Run golangci-lint
	@echo "Running linter..."
	@golangci-lint run ./...
	@echo "✓ Lint passed"

vet: ## Run go vet
	@echo "Running go vet..."
	@go vet ./...
	@echo "✓ Vet passed"

check: fmt vet lint ## Run all checks (fmt + vet + lint)
	@echo "✓ All checks passed"

# ═══════════════════════════════════════════════════════════════
# Testing
# ═══════════════════════════════════════════════════════════════

.PHONY: test test-unit test-integration test-e2e test-race test-cover test-bench

test: test-unit ## Run all tests
	@echo "✓ All tests passed"

test-unit: ## Run unit tests
	@echo "Running unit tests..."
	@go test -short -race -count=1 ./...
	@echo "✓ Unit tests passed"

test-integration: ## Run integration tests (requires PostgreSQL)
	@echo "Running integration tests..."
	@go test -tags=integration -race -count=1 ./...
	@echo "✓ Integration tests passed"

test-e2e: ## Run E2E tests
	@echo "Running E2E tests..."
	@go test -tags=e2e -race -count=1 ./...
	@echo "✓ E2E tests passed"

test-race: ## Run tests with race detector
	@echo "Running race detector..."
	@go test -race -count=1 ./...
	@echo "✓ Race tests passed"

test-cover: ## Generate coverage report
	@echo "Generating coverage..."
	@go test -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report: coverage.html"

test-bench: ## Run benchmarks
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...
	@echo "✓ Benchmarks completed"

# ═══════════════════════════════════════════════════════════════
# Build & Run
# ═══════════════════════════════════════════════════════════════

.PHONY: build run dev clean

build: ## Build binary
	@echo "Building binary..."
	@go build -o bin/arbitrage-be ./cmd/server
	@echo "✓ Built: bin/arbitrage-be"

run: ## Run the application
	@go run ./cmd/server

dev: ## Run with hot-reload (air)
	@which air > /dev/null 2>&1 || go install github.com/air-verse/air@latest
	@air

clean: ## Remove build artifacts
	@rm -rf bin/ coverage.out coverage.html
	@echo "✓ Cleaned"

# ═══════════════════════════════════════════════════════════════
# Database
# ═══════════════════════════════════════════════════════════════

.PHONY: db-migrate-up db-migrate-down db-migrate-create

db-migrate-up: ## Run migrations up
	@go run ./cmd/server migrate up

db-migrate-down: ## Rollback migrations
	@go run ./cmd/server migrate down

db-migrate-create: ## Create new migration (usage: make db-migrate-create name=add_users)
	@read -p "Migration name: " name; \
	migrate create -ext sql -dir migrations -seq $$name

# ═══════════════════════════════════════════════════════════════
# Docker
# ═══════════════════════════════════════════════════════════════

.PHONY: docker-build docker-up docker-down docker-test

docker-build: ## Build Docker image
	@docker build -t arbitrage-be .

docker-up: ## Start services with docker compose
	@docker compose up -d

docker-down: ## Stop services
	@docker compose down

docker-test: ## Start test database
	@docker compose -f docker-compose.test.yml up -d
	@echo "✓ Test database started on port 5433"

# ═══════════════════════════════════════════════════════════════
# CI
# ═══════════════════════════════════════════════════════════════

.PHONY: ci

ci: fmt vet lint test-unit build ## Run full CI pipeline
	@echo "✓ CI passed"
