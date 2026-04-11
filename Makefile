VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
IMAGE   := siteworxpro/ysf-reflector-go

.PHONY: build docker-build test test-coverage lint help %

.DEFAULT_GOAL := help

help: ## Show this help message
	@echo "Usage: make <target>"
	@echo ""
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  %-15s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Build the binary via build.sh
	bash build.sh

test: ## Run all tests
	go test ./...

test-coverage: ## Run tests with coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint: ## Run golangci-lint
	golangci-lint run ./...

%:
	@echo "Unknown target '$@'. Run 'make help' for available targets."
	@exit 1

docker-build: ## Build and push multi-arch Docker images
	docker buildx build \
		--tag $(IMAGE):$(VERSION) \
		--push \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--provenance=true \
		--sbom=true \
		.

	docker buildx build \
		--tag $(IMAGE):latest \
		--push \
		--platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) \
		--provenance=true \
		--sbom=true \
		.
