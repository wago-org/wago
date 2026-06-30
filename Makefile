# wago task runner. Single source of truth for the dev/CI chores: the GitHub
# Actions workflow (.github/workflows/ci.yml) calls these same targets, so
# `make lint` / `make test` reproduce CI exactly. Run `make` to list targets.
#
#   make lint   gofmt + go generate sync + go vet + staticcheck   (host, no act)
#   make test   go build + go test                                (host, no act)
#   make ci     replay the whole workflow in Docker via act       (scripts/ci-local.sh)
#   make bench  run the benchmark suite (BENCH=<regex> to filter) (host)
#
# The bench-* targets run on a stable local machine, never CI: shared runners
# make benchmark numbers noisy.

.DEFAULT_GOAL := help

# Files written by `go generate ./...` (the genfacade output). The staleness
# check diffs only these, so `make lint` is usable with unrelated uncommitted
# work in the tree (CI starts clean, so it behaves identically there).
GENERATED := wago.go

# Suite knobs and where `make bench` caches its run.
BENCHTIME ?= 1s
COUNT     ?= 6
BENCH_RUN ?= bench/.bench-run.txt
# WARP harness for chart engine-comparison (empty skips it): WARP=auto or a path.
WARP      ?=
# Where `make cover` writes the coverage profile, and where `make card` collects
# section fragments / writes the assembled PR card.
COVERPROFILE ?= coverage.out
CARD_DIR  ?= ci-card
CARD_FILE ?= card.md

# Current commit. `make bench` stamps it into the capture's first line so
# bench-publish can refuse a capture taken at a different commit (unless FORCE=1).
# Committed HEAD only — working-tree dirt (notably the always-dirty warp
# submodule) is intentionally ignored.
HEAD_HASH := $(shell git rev-parse HEAD 2>/dev/null)

# Default goal: a bare `make` sets up a fresh clone by installing the git hooks
# (only if not already installed) before printing the target list.
.PHONY: help
help: hooks-ensure ## List available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  make %-13s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

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

TINYGO ?= tinygo
# wago runs native code on a dedicated foreign stack. TinyGo's conservative
# collector with a threaded scheduler can stop a thread mid-run and scan that
# switched stack, so wago under TinyGo wants the cooperative scheduler. See
# docs/tinygo.md.
TINYGO_SCHEDULER ?= tasks

.PHONY: tinygo-build
tinygo-build: ## Build the CLI with TinyGo (no cgo) -> ./wago-tinygo  (see docs/tinygo.md)
	$(TINYGO) build -scheduler=$(TINYGO_SCHEDULER) -o wago-tinygo ./cli/wago

.PHONY: tinygo-test
tinygo-test: ## Run the runtime + public-API suites under TinyGo
	$(TINYGO) test -scheduler=$(TINYGO_SCHEDULER) ./src/core/runtime/ ./src/wago/

.PHONY: cover
cover: ## Run tests with cross-package coverage + per-package report (COVERPROFILE=path)
	COVERPROFILE=$(COVERPROFILE) scripts/coverage.sh

.PHONY: card
card: ## Build the PR CI info card -> card.md (coverage + tests filled)
	@mkdir -p $(CARD_DIR)
	COVER_REPORT=$(CARD_DIR)/coverage.md scripts/coverage.sh >/dev/null
	TESTS_REPORT=$(CARD_DIR)/tests.md scripts/tests-card.sh >/dev/null
	CARD_DIR=$(CARD_DIR) CARD_FILE=$(CARD_FILE) scripts/pr-card.sh
	@cat $(CARD_FILE)

.PHONY: ci
ci: ## Replay the full CI workflow locally in Docker (act)
	scripts/ci-local.sh

# Run the full suite and write the capture file, stamped with the current commit
# (so bench-publish can tell whether it is current). Always runs.
.PHONY: bench
bench: ## Run the full suite and write the capture (bench/.bench-run.txt)
	{ echo "# git $(HEAD_HASH)"; (cd bench && go test -run '^$$' -bench . -benchmem -count $(COUNT) -benchtime $(BENCHTIME) -timeout 0 .); } | tee $(BENCH_RUN)

# Build charts from the last capture into bench/out — no re-run, no publish.
# Uses whatever capture exists. WARP is skipped unless WARP=<harness> is given.
.PHONY: bench-chart
bench-chart: ## Build charts from the last capture into bench/out
	@if [ ! -f "$(BENCH_RUN)" ]; then echo "make: no capture at $(BENCH_RUN); run 'make bench'" >&2; exit 1; fi
	cd bench && go run ./cmd/benchpub -in $(notdir $(BENCH_RUN)) -warp "$(WARP)" -out out
	@echo "make: charts written to bench/out/charts/*.svg"

# Publish the captured run to wago-org/docs: publish-bench.sh re-renders the
# charts from the capture, appends history, and pushes. Rejects a capture whose
# git stamp differs from HEAD unless FORCE=1.
.PHONY: bench-publish
bench-publish: ## Publish the capture to wago-org/docs (stale git hash rejected unless FORCE=1)
	@if [ ! -f "$(BENCH_RUN)" ]; then echo "make: no capture at $(BENCH_RUN); run 'make bench'" >&2; exit 1; fi
	@cached="$$(sed -n 's/^\# git //p' $(BENCH_RUN) | head -1)"; \
	if [ "$$cached" != "$(HEAD_HASH)" ] && [ -z "$(FORCE)" ]; then \
		echo "make: capture is stale (captured at $${cached:-none}, HEAD is $(HEAD_HASH)); run 'make bench' or FORCE=1" >&2; exit 1; \
	fi
	WAGO_BENCH_IN=$(BENCH_RUN) scripts/publish-bench.sh

.PHONY: bench-charts
bench-charts: ## Regenerate + publish benchmark charts to wago-org/docs
	scripts/publish-charts.sh

.PHONY: bench-warp
bench-warp: ## Build the WARP comparison harness (vb_bench)
	scripts/build-warp-bench.sh

.PHONY: hooks
hooks: ## Install the repo git hooks (.githooks)
	scripts/install-hooks.sh
