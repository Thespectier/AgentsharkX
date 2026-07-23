SHELL := /bin/bash

GO_VERSION := 1.26.5
GO_IMAGE := golang:$(GO_VERSION)-alpine
GO_RACE_IMAGE := golang:$(GO_VERSION)
COMPOSE := docker compose --env-file deploy/versions.env --env-file deploy/example.env -f deploy/compose.yaml
PREVIEW_COMPOSE := ./scripts/preview-compose.sh

.PHONY: verify format-check test race-test web-check secret-boundary secret-scan repository-check openapi-validate compose-validate upstream-smoke gateway-config-write-smoke preview-bootstrap preview-up preview-down preview-status container-build release-e2e sbom security-scan release-gate

verify: format-check test race-test web-check secret-boundary repository-check openapi-validate compose-validate

format-check:
	@if command -v go >/dev/null 2>&1; then \
		files="$$(gofmt -l apps/server)"; \
	else \
		files="$$(docker run --rm -v "$(CURDIR):/src" -w /src $(GO_IMAGE) gofmt -l apps/server)"; \
	fi; \
	if [[ -n "$$files" ]]; then echo "Go files need formatting:"; echo "$$files"; exit 1; fi

test:
	@if command -v go >/dev/null 2>&1; then \
		cd apps/server && go test ./...; \
	else \
		docker run --rm -v "$(CURDIR):/src" -w /src/apps/server $(GO_IMAGE) go test ./...; \
	fi

race-test:
	@if command -v go >/dev/null 2>&1; then \
		cd apps/server && go test -race ./...; \
	else \
		docker run --rm -v "$(CURDIR):/src" -w /src/apps/server $(GO_RACE_IMAGE) go test -race ./...; \
	fi

web-check:
	@npm --prefix apps/web run check

secret-boundary:
	@VITE_ENABLE_MOCKS=false npm --prefix apps/web run build >/dev/null
	@./scripts/verify-secret-boundary.sh

repository-check:
	@./scripts/verify-repository.sh

openapi-validate:
	@./scripts/verify-openapi.sh

compose-validate:
	@$(COMPOSE) config --quiet

upstream-smoke:
	@./scripts/upstream-smoke.sh

gateway-config-write-smoke:
	@./scripts/gateway-config-write-smoke.sh

preview-bootstrap:
	@./scripts/bootstrap-preview.sh

preview-up:
	@$(PREVIEW_COMPOSE) up --build -d

preview-down:
	@$(PREVIEW_COMPOSE) down

preview-status:
	@$(PREVIEW_COMPOSE) ps

container-build:
	@docker build -f deploy/Dockerfile \
		--build-arg AGENTSHARK_VERSION=0.7.0-preview \
		--build-arg AGENTSHARK_REVISION=$$(git rev-parse --short HEAD) \
		-t agentsharkx/preview:verification .

secret-scan:
	@VITE_ENABLE_MOCKS=false npm --prefix apps/web run build >/dev/null
	@./scripts/secret-scan.sh

release-e2e:
	@./scripts/release-e2e.sh

sbom:
	@node scripts/generate-release-artifacts.mjs

security-scan:
	@./scripts/security-scan.sh

release-gate: verify secret-scan sbom security-scan container-build release-e2e
