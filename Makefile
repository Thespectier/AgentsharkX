SHELL := /bin/bash

GO_VERSION := 1.26.5
GO_IMAGE := golang:$(GO_VERSION)-alpine
COMPOSE := docker compose --env-file deploy/versions.env --env-file deploy/example.env -f deploy/compose.yaml

.PHONY: verify format-check test repository-check openapi-validate compose-validate upstream-smoke

verify: format-check test repository-check openapi-validate compose-validate

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

repository-check:
	@./scripts/verify-repository.sh

openapi-validate:
	@./scripts/verify-openapi.sh

compose-validate:
	@$(COMPOSE) config --quiet

upstream-smoke:
	@./scripts/upstream-smoke.sh
