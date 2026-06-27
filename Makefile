# wago task runner. Single source of truth for the dev/CI chores: the GitHub
# Actions workflow (.github/workflows/ci.yml) calls these same targets, so
# `make lint` / `make test` reproduce CI exactly. Run `make` to list targets.
#
#   make lint   gofmt + go generate sync + go vet + staticcheck   (host, no act)
#   make test   go build + go test                                (host, no act)
#   make ci     replay the whole workflow in Docker via act       (scripts/ci-local.sh)

.DEFAULT_GOAL := help

# Files written by `go generate ./...` (the genfacade output). The staleness
# check diffs only these, so `make lint` is usable with unrelated uncommitted
# work in the tree (CI starts clean, so it behaves identically there).
GENERATED := wago.go

# Default goal: a bare `make` sets up a fresh clone by installing the git hooks
# (only if not already installed) before printing the target list.
.PHONY: help
help: hooks-ensure ## List available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  make %-8s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Install the hooks unless core.hooksPath already points at .githooks. Silent
# no-op when set up; the explicit `make hooks` always (re)installs.
.PHONY: hooks-ensure
hooks-ensure:
	@[ "$$(git config --get core.hooksPath)" = ".githooks" ] || scripts/install-hooks.sh

.PHONY: lint
lint: lint-fmt lint-generate lint-vet lint-staticcheck ## Run all lint checks (host)

.PHONY: lint-fmt
lint-fmt:
	@unformatted="$$(gofmt -l . | grep -vE '^(warp|tests/spec)/' || true)"; \
	if [ -n "$$unformatted" ]; then \
		echo "::error::These files are not gofmt-ed:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: lint-generate
lint-generate:
	@go generate ./...
	@if ! git diff --exit-code -- $(GENERATED); then \
		echo "::error::Generated files are stale. Run 'go generate ./...' and commit the result."; \
		exit 1; \
	fi

.PHONY: lint-vet
lint-vet:
	go vet ./...

# staticcheck is enforced in CI (installed before `make lint`); locally it is
# optional — skip with a hint rather than fail when it is not installed.
.PHONY: lint-staticcheck
lint-staticcheck:
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
	else \
		echo "make: staticcheck not found, skipping (go install honnef.co/go/tools/cmd/staticcheck@2024.1.1)"; \
	fi

.PHONY: test
test: ## Build and run the test suite (host)
	go build ./...
	go test -count=1 ./...

.PHONY: ci
ci: ## Replay the full CI workflow locally in Docker (act)
	scripts/ci-local.sh

.PHONY: hooks
hooks: ## Install the repo git hooks (.githooks)
	scripts/install-hooks.sh
