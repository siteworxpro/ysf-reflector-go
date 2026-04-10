VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo "dev")
IMAGE   := siteworxpro/ysf-reflector-go

.PHONY: build docker-build

build:
	bash build.sh

docker-build:
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
