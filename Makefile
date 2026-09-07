# wago task runner. Single source of truth for the dev/CI chores: the GitHub
# Actions workflow (.github/workflows/ci.yml) calls these same targets, so
# `make lint` / `make test` reproduce CI exactly. Run `make` to list targets.
#
#   make lint        gofmt + generate sync + vet + staticcheck (host, no act)
#   make docs-check  local Markdown paths and anchors           (host, no act)
#   make test        go build + go test                         (host, no act)
#   make test-concurrency  seeded runtime concurrency harness   (host, no act)
#   make ci          replay the whole workflow in Docker via act
#   make bench       benchmark suite (BENCH=<regex> to filter)  (host)
#
# The bench-* targets run on a stable local machine, never CI: shared runners
# make benchmark numbers noisy.

.DEFAULT_GOAL := help

# Files written by `go generate ./...` (the public facade and schema enums). The staleness
# check diffs only these, so `make lint` is usable with unrelated uncommitted
# work in the tree (CI starts clean, so it behaves identically there).
GENERATED := wago.go schema.json

# Suite knobs and where `make bench` caches its run.
BENCHTIME ?= 1s
COUNT     ?= 1
BENCH_RUN ?= bench/.bench-run.txt
BENCH_ISA ?= 0
STARSHINE_WASM ?=
# Per-engine -bench filters. wago = the stage suite + the _wago comparisons;
# wazero = every benchmark carrying "azero" (BenchmarkWazero* and *_wazero).
WAGO_BENCH_RE   ?= ^Benchmark(Decode|Validate|Compile|CompileFull|Instantiate|Exec)$$|_wago$$
WAZERO_BENCH_RE ?= [Ww]azero
BENCH_ISA_GO_FLAG     := $(if $(filter 1 true yes,$(BENCH_ISA)),-wago.bench.isa,)
BENCH_ISA_BENCHPUB_FLAG := $(if $(filter 1 true yes,$(BENCH_ISA)),-isa,)
# Where `make cover` writes the coverage profile, and where `make card` collects
# section fragments / writes the assembled PR card.
COVERPROFILE ?= coverage.out
CARD_DIR  ?= ci-card
CARD_FILE ?= card.md

# Current commit. `make bench` stamps it into the capture's first line so
# bench-publish can refuse a capture taken at a different commit (unless FORCE=1).
# Committed HEAD only — unrelated working-tree dirt is intentionally ignored.
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
lint: lint-fmt lint-generate lint-vet lint-staticcheck lint-website-generator ## Run all lint checks (host)

.PHONY: lint-fmt
lint-fmt:
	@unformatted="$$(git ls-files -z -- '*.go' | xargs -0 gofmt -l)"; \
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
	go vet -tags wago_runtime ./cli/...

# staticcheck is enforced in CI (installed before `make lint`); locally it is
# optional — skip with a hint rather than fail when it is not installed.
.PHONY: lint-staticcheck
lint-staticcheck:
	@if command -v staticcheck >/dev/null 2>&1; then \
		staticcheck ./...; \
		staticcheck -tags wago_runtime ./cli/...; \
	else \
		echo "make: staticcheck not found, skipping (go install honnef.co/go/tools/cmd/staticcheck@2024.1.1)"; \
	fi

.PHONY: lint-website-generator
lint-website-generator:
	node --test scripts/update-website-bench.test.mjs
	node --test scripts/engine-state-oracle.test.mjs
	node --test scripts/fuzz-engine-state.test.mjs

.PHONY: docs-check
docs-check: ## Validate local paths and anchors in tracked Markdown files
	go run ./tests/tools/docs-check

.PHONY: test
test: ## Build and run the test suite (host)
	go build ./...
	go build -tags wago_runtime ./cli/...
	go test -count=1 ./...
	go test -count=1 -tags wago_runtime ./cli/...
	tests/scripts/build-release-assets.sh
	tests/scripts/release-qualification.sh

.PHONY: test-concurrency
test-concurrency: ## Run the deterministic runtime concurrency harness (WAGO_CONCURRENCY_SEED=...)
	go test -count=1 -run '^TestRuntimeConcurrency' ./tests/runtimeconcurrency

ENGINE_FUZZ_ARGS ?=

.PHONY: fuzz-engine-state
fuzz-engine-state: ## Compare Starshine state hashes in Node and Railshot (ENGINE_FUZZ_ARGS="...")
	scripts/fuzz-engine-state.sh $(ENGINE_FUZZ_ARGS)

.PHONY: install-local
install-local: ## Run the installer from this checkout
	@go run ./cli/installer install

.PHONY: install-local-runtime
install-local-runtime: ## Build this checkout and atomically replace the global manager and canary standard runtime
	@if [ -n "$(WAGO_HOME)" ]; then root="$(WAGO_HOME)"; runtime_root="$$root/data"; else root="$(HOME)/.wago"; runtime_root="$$root"; fi; \
		runtime_dir="$$runtime_root/versions/canary/standard/normal"; \
		runtime="$$runtime_dir/wago-runtime$$(go env GOEXE)"; \
		manager_dir="$$root/bin"; \
		manager="$$manager_dir/wago$$(go env GOEXE)"; \
		build="canary-local-$$(git rev-parse --short HEAD)"; \
		mkdir -p "$$runtime_dir" "$$manager_dir"; \
		staged_runtime="$$runtime_dir/.wago-runtime-local-$$$$"; \
		staged_manager="$$manager_dir/.wago-local-$$$$"; \
		CGO_ENABLED=0 go build -tags wago_runtime -ldflags "-s -w -X main.version=canary" -o "$$staged_runtime" ./cli/wago && \
		CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=canary" -o "$$staged_manager" ./cli/wago && \
		chmod 755 "$$staged_runtime" "$$staged_manager" && \
		mv -f "$$staged_runtime" "$$runtime" && \
		mv -f "$$staged_manager" "$$manager"; \
		echo "installed $$build at $$manager and $$runtime"

.PHONY: test-starshine
test-starshine: ## Compile/link/instantiate a MoonBit Starshine wasm-gc artifact (STARSHINE_WASM=/path/cmd.wasm)
	@test -n "$(STARSHINE_WASM)" || { echo "set STARSHINE_WASM=/path/to/cmd.wasm"; exit 1; }
	WAGO_STARSHINE_SMOKE_WASM="$(STARSHINE_WASM)" go test ./src/wago -run '^TestMoonBitStarshineWasmGCSmoke(Compile|Instantiate)$$' -count=1 -v

.PHONY: bench-starshine
bench-starshine: ## Benchmark Starshine compile, cold link/JIT, compile+link, and instantiate (STARSHINE_WASM=/path/cmd.wasm)
	@test -n "$(STARSHINE_WASM)" || { echo "set STARSHINE_WASM=/path/to/cmd.wasm"; exit 1; }
	WAGO_STARSHINE_SMOKE_WASM="$(STARSHINE_WASM)" go test ./src/wago -run '^$$' -bench '^BenchmarkMoonBitStarshineWasmGC' -benchmem -count $(COUNT) -benchtime $(BENCHTIME)

.PHONY: test-guard
test-guard: ## Guard-page (signals-based) tests: full public-API suite (incl. the SIGSEGV fault->trap path) + in-bounds differential
	go test -count=1 -tags wago_guardpage ./src/wago/
	cd bench && go test -count=1 -tags wago_guardpage -run 'TestCorpusDifferential|TestJsonAsGuardCorrect' .

.PHONY: test-native-arm64
test-native-arm64: ## Native arm64 gate (run locally on your Mac): the checks CI used to run on the macOS/arm64 runner
	go test ./src/core/encoder/arm64 ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -count=1
	go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1 -v
	WAGO_CORPUS_TIMEOUT=20s $(MAKE) test-corpus

.PHONY: test-corpus
test-corpus: ## Corpus pipeline + differential execution in parent/child processes (WAGO_CORPUS_TIMEOUT=15s)
	cd bench && go test -count=1 -run '^TestCorpus$$' .
	cd bench && go test -count=1 -tags wago_guardpage -run '^TestCorpus$$' .

.PHONY: test-semantic-corpus
test-semantic-corpus: ## Semantic corpus: real programs checked against exact oracles (tests/corpora)
	go test -count=1 ./tests/semanticcorpus

REGRESSION_UPSTREAM ?= $(CURDIR)/.tmp/regression-corpus-upstream
WAST2JSON ?= wast2json

.PHONY: regression-corpus-check
regression-corpus-check: ## Verify pinned upstream sources and exact WABT artifacts
	go run ./tests/tools/regression-corpus -repo $(CURDIR) -upstream $(REGRESSION_UPSTREAM) -wast2json $(WAST2JSON)

.PHONY: regression-corpus-sync
regression-corpus-sync: ## Fetch the pinned upstream revision and refresh regression artifacts
	go run ./tests/tools/regression-corpus -repo $(CURDIR) -upstream $(REGRESSION_UPSTREAM) -wast2json $(WAST2JSON) -fetch -write

.PHONY: regression-stress
regression-stress: ## Repeat lifecycle tests, optimizer and guard modes, and fuzz targets
	tests/scripts/regression-stress.sh

# Run the WebAssembly spec suites as native execution oracles for the x64
# backend. The preserved MVP baseline is WebAssembly/testsuite at tests/spec;
# Release 2.0 and Release 3.0 are independently pinned from WebAssembly/spec at
# tests/spec-v2 and tests/spec-v3; both official core corpora live under
# test/core. Release 3 bootstraps the checksum-pinned WABT tool below; older
# suites retain their existing PATH behavior. Env paths are absolute because
# `go test` runs in the package directory.
SPEC1_DIR = $(CURDIR)/tests/spec
SPEC2_DIR = $(CURDIR)/tests/spec-v2
SPEC3_DIR = $(CURDIR)/tests/spec-v3
define run-spec
	@command -v wast2json >/dev/null 2>&1 || { echo "wast2json (wabt) not on PATH; install wabt (e.g. apt-get install wabt)"; exit 1; }
	@test -f $(2)/$(3) || git submodule update --init $(4)
	WAGO_SPECTEST_DIR=$(2) WAGO_SPEC_VERSION=$(1) go test -count=1 -run TestSpecSuiteExec -v ./src/wago/
endef

.PHONY: spec1
spec1: ## Run the WebAssembly 1.0 (MVP core) spec suite against x64 (needs wast2json)
	$(call run-spec,1.0,$(SPEC1_DIR),i32.wast,tests/spec)

.PHONY: spec2
spec2: ## Run the pinned official WebAssembly 2.0 core suite against x64 (needs wast2json)
	@command -v wast2json >/dev/null 2>&1 || { echo "wast2json (wabt) not on PATH; install wabt (e.g. apt-get install wabt)"; exit 1; }
	@test -f $(SPEC2_DIR)/test/core/i32.wast || git submodule update --init tests/spec-v2
	go test -count=1 -run '^TestCoreV2Validation$$' -v ./src/core/compiler/wasm/
	go test -count=1 -run '^TestCoreV2SpecExecution$$' -v ./src/wago/

.PHONY: wabt
wabt: ## Bootstrap and verify the checksum-pinned WABT used by Release 3
	@scripts/bootstrap-wabt.sh --verify

.PHONY: spec-interpreter
spec-interpreter: ## Bootstrap and verify the official Release 3 reference interpreter
	@scripts/bootstrap-spec-interpreter.sh --verify

.PHONY: spec3
spec3: wabt spec-interpreter ## Run the pinned official WebAssembly 3.0 core suite on the current supported host
	@wast2json="$$(scripts/bootstrap-wabt.sh --print-path)"; \
		interpreter="$$(scripts/bootstrap-spec-interpreter.sh --print-path)"; \
		interpreter_revision="$$(scripts/bootstrap-spec-interpreter.sh --print-revision)"; \
		test -f $(SPEC3_DIR)/test/core/i32.wast || git submodule update --init tests/spec-v3; \
		WAGO_WAST2JSON="$$wast2json" WAGO_WABT_VERSION=1.0.41 \
		WAGO_SPEC_INTERPRETER="$$interpreter" WAGO_SPEC_INTERPRETER_REVISION="$$interpreter_revision" \
		WAGO_SPECTEST_DIR=$(SPEC3_DIR) WAGO_SPEC_VERSION=3.0 \
		go test -count=1 -run TestSpecSuiteExec -v ./src/wago/

.PHONY: spec3-signals
spec3-signals: wabt spec-interpreter ## Run zero-gap Core 3 with linux/amd64 signal-backed bounds
	@wast2json="$$(scripts/bootstrap-wabt.sh --print-path)"; \
		interpreter="$$(scripts/bootstrap-spec-interpreter.sh --print-path)"; \
		interpreter_revision="$$(scripts/bootstrap-spec-interpreter.sh --print-revision)"; \
		test -f $(SPEC3_DIR)/test/core/i32.wast || git submodule update --init tests/spec-v3; \
		WAGO_BOUNDS=signals WAGO_WAST2JSON="$$wast2json" WAGO_WABT_VERSION=1.0.41 \
		WAGO_SPEC_INTERPRETER="$$interpreter" WAGO_SPEC_INTERPRETER_REVISION="$$interpreter_revision" \
		WAGO_SPECTEST_DIR=$(SPEC3_DIR) WAGO_SPEC_VERSION=3.0 \
		go test -tags wago_guardpage -count=1 -run TestSpecSuiteExec -v ./src/wago/

.PHONY: spec3-baseline
spec3-baseline: ## Refresh tests/spec-v3-baseline.json and return the spec3 status
	@scripts/spec3-baseline.sh

.PHONY: simd
simd: ## Run the official SIMD proposal execution suite (needs wast2json)
	$(call run-spec,simd,$(SPEC1_DIR),proposals/simd/simd_address.wast,tests/spec)

.PHONY: spec
spec: spec1 spec2 spec3 ## Run the WebAssembly spec suite for all versions

TINYGO ?= tinygo
# wago runs native code on a dedicated foreign stack. TinyGo's conservative
# collector with a threaded scheduler can stop a thread mid-run and scan that
# switched stack, so wago under TinyGo uses the cooperative scheduler.
TINYGO_SCHEDULER ?= tasks
# Stamped into the manager and runners via -ldflags -X. Release workflows pass
# the git tag; 0.0.0 is the pre-release default until the first tag.
WAGO_VERSION ?= 0.0.0

.PHONY: build
build: build-manager ## Build the standard-Go manager -> ./wago

.PHONY: build-manager
build-manager: ## Build the runtime-independent manager with standard Go -> ./wago
	CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$(WAGO_VERSION)" -o wago ./cli/wago

.PHONY: build-runtime-standard
build-runtime-standard: ## Build the everything runtime with standard Go
	CGO_ENABLED=0 go build -tags wago_runtime -ldflags "-s -w -X main.version=$(WAGO_VERSION)" -o wago-runtime-standard-normal ./cli/wago

.PHONY: build-runtime-minimal
build-runtime-minimal: ## Build the run-only runtime with standard Go
	CGO_ENABLED=0 go build -tags wago_runtime,wago_minimal -ldflags "-s -w -X main.version=$(WAGO_VERSION)" -o wago-runtime-minimal-normal ./cli/wago

.PHONY: build-runtime-standard-tinygo
build-runtime-standard-tinygo: ## Build the everything runtime with TinyGo
	$(TINYGO) build -scheduler=$(TINYGO_SCHEDULER) -no-debug -opt=z -gc=conservative \
		-tags wago_runtime \
		-ldflags "-X main.version=$(WAGO_VERSION)" -o wago-runtime-standard-tiny ./cli/wago

.PHONY: build-runtime-minimal-tinygo
build-runtime-minimal-tinygo: ## Build the run-only runtime with TinyGo
	$(TINYGO) build -scheduler=$(TINYGO_SCHEDULER) -no-debug -opt=z -gc=conservative \
		-tags wago_runtime,wago_lean,wago_minimal \
		-ldflags "-X main.version=$(WAGO_VERSION)" -o wago-runtime-minimal-tiny ./cli/wago
	@echo "wago minimal/tiny $(WAGO_VERSION): $$(du -h wago-runtime-minimal-tiny | cut -f1)"

.PHONY: build-release
build-release: ## Build the host CLI plus all supported runtime profiles/builds
	GOOS="$$(go env GOOS)" GOARCH="$$(go env GOARCH)" WAGO_VERSION="$(WAGO_VERSION)" scripts/build-release-assets.sh

.PHONY: build-engine
build-engine: ## Diagnostic run-only Minimal/Tiny runtime -> ./wago-engine
	$(TINYGO) build -scheduler=$(TINYGO_SCHEDULER) -no-debug -opt=z -gc=conservative \
		-tags wago_runtime,wago_lean,wago_minimal \
		-ldflags "-X main.version=$(WAGO_VERSION)" -o wago-engine ./cli/wago
	@if [ "$$(go env GOOS)" = linux ]; then \
		strip -s --strip-section-headers --remove-section=.eh_frame \
			--remove-section=.eh_frame_hdr --remove-section=.comment wago-engine; \
	fi
	@echo "wago-engine $(WAGO_VERSION): $$(du -h wago-engine | cut -f1)"

.PHONY: tinygo-build
tinygo-build: ## Build the Minimal runtime with TinyGo (no cgo, debug) -> ./wago-tinygo
	$(TINYGO) build -scheduler=$(TINYGO_SCHEDULER) -tags wago_runtime,wago_lean,wago_minimal -o wago-tinygo ./cli/wago

.PHONY: tinygo-test
tinygo-test: ## Run the runtime + public-API suites under TinyGo
	$(TINYGO) test -v -scheduler=$(TINYGO_SCHEDULER) ./src/core/runtime/ ./src/wago/

.PHONY: cover
cover: ## Run all five public gates with merged cross-package coverage
	COVERPROFILE=$(COVERPROFILE) scripts/coverage.sh

.PHONY: verify-public
verify-public: ## Run/count SIMD, spec1, spec2, normal, and guard-page gates, then merge coverage
	scripts/verification.sh

# card-fragments produces the go-only section fragments (coverage/tests/spec).
# The build-size fragment is produced separately (scripts/size-card.sh) since it
# needs TinyGo — in CI it runs as its own parallel job. `make card` does all of it
# locally for a full preview.
.PHONY: card-fragments
card-fragments:
	@mkdir -p $(CARD_DIR)
	COVER_REPORT=$(CARD_DIR)/coverage.md scripts/coverage.sh >/dev/null
	TESTS_REPORT=$(CARD_DIR)/tests.md scripts/tests-card.sh >/dev/null
	SPEC_REPORT=$(CARD_DIR)/spec.md scripts/spec-card.sh >/dev/null

.PHONY: card
card: card-fragments ## Build the full PR CI info card -> card.md (incl. build size)
	SIZE_REPORT=$(CARD_DIR)/size.md scripts/size-card.sh >/dev/null
	CARD_DIR=$(CARD_DIR) CARD_FILE=$(CARD_FILE) scripts/pr-card.sh
	@cat $(CARD_FILE)

.PHONY: ci
ci: ## Replay the full CI workflow locally in Docker (act)
	scripts/ci-local.sh

# Run the full suite and write the capture file, stamped with the current commit
# (so bench-publish can tell whether it is current). Default to guard-page bounds
# (-tags wago_guardpage + WAGO_BOUNDS=signals) — the faster, production-relevant
# mode; use bench-noguard for explicit-bounds numbers.
.PHONY: bench
bench: ## Run all engine benches (wago + wazero) under guard-page bounds and write the capture (bench/.bench-run.txt)
	{ echo "# git $(HEAD_HASH)"; (cd bench && WAGO_BOUNDS=signals go test -run '^$$' -tags wago_guardpage -bench . -benchmem -count $(COUNT) -benchtime $(BENCHTIME) -timeout 0 $(BENCH_ISA_GO_FLAG) .); } | tee $(BENCH_RUN)

.PHONY: bench-noguard
bench-noguard: ## Run the full suite under explicit bounds and write the capture
	{ echo "# git $(HEAD_HASH)"; (cd bench && go test -run '^$$' -bench . -benchmem -count $(COUNT) -benchtime $(BENCHTIME) -timeout 0 $(BENCH_ISA_GO_FLAG) .); } | tee $(BENCH_RUN)

.PHONY: bench-wago
bench-wago: ## Run only the wago benchmarks
	cd bench && go test -run '^$$' -bench '$(WAGO_BENCH_RE)' -benchmem -count $(COUNT) -benchtime $(BENCHTIME) -timeout 0 $(BENCH_ISA_GO_FLAG) .

.PHONY: bench-jit
bench-jit: ## Benchmark railshot JIT edge cases and corpus raw/end-to-end compilation
	go test ./src/core/compiler/backend/railshot/amd64 -run '^$$' -bench '^BenchmarkRailshotCompile' -benchmem -count $(COUNT) -benchtime $(BENCHTIME)
	cd bench && go test -run '^$$' -bench '^BenchmarkCompile(Full)?$$' -benchmem -count $(COUNT) -benchtime $(BENCHTIME) -timeout 0 .

.PHONY: bench-wazero
bench-wazero: ## Run only the wazero benchmarks
	cd bench && go test -run '^$$' -bench '$(WAZERO_BENCH_RE)' -benchmem -count $(COUNT) -benchtime $(BENCHTIME) -timeout 0 $(BENCH_ISA_GO_FLAG) .

# Build charts from the last capture into bench/out — no re-run, no publish.
# Uses whatever capture exists.
.PHONY: bench-chart
bench-chart: ## Build charts from the last capture into bench/out
	@if [ ! -f "$(BENCH_RUN)" ]; then echo "make: no capture at $(BENCH_RUN); run 'make bench'" >&2; exit 1; fi
	cd bench && go run ./cmd/benchpub -in $(notdir $(BENCH_RUN)) $(BENCH_ISA_BENCHPUB_FLAG) -out out
	@echo "make: charts written to bench/out/charts/*.svg"

.PHONY: bench-website
bench-website: ## Update ../website performance numbers from the last benchmark capture
	@if [ ! -f "$(BENCH_RUN)" ]; then echo "make: no capture at $(BENCH_RUN); run 'make bench'" >&2; exit 1; fi
	WAGO_BENCH_IN=$(BENCH_RUN) scripts/update-website-bench.mjs

# Cross-runtime startup-latency sweep (full process, exec→exit) over the
# committed work-twins in bench/startup/twins, across every runtime found on the
# machine → bench/out/startup.json. See bench/startup/runtimes.json for the
# runtime list and *_BIN env overrides; a missing runtime is skipped.
.PHONY: bench-startup
bench-startup: ## Run the cross-runtime startup-latency sweep and write bench/startup/startup.json
	node bench/startup/run.mjs

# Website checkout (sibling by default); override for a worktree:
#   make site WEBSITE_DIR=/abs/path/to/website
WEBSITE_DIR ?= ../website

.PHONY: startup-website
startup-website: ## Update the website startup-latency numbers from bench/startup/startup.json
	WAGO_WEBSITE_DIR=$(WEBSITE_DIR) scripts/update-website-startup.mjs

# One command to rebuild the whole website from committed data — startup +
# performance sections and the stats sync — then build once. No benchmarking:
# refresh the data first with `make bench bench-chart` (performance) and
# `make bench-startup` (startup) when you want new numbers.
.PHONY: site
site: ## Regenerate all of the website from committed data (startup + perf + stats) and build
	@if [ ! -f "$(WEBSITE_DIR)/package.json" ]; then echo "make: $(WEBSITE_DIR) not found (set WEBSITE_DIR)" >&2; exit 1; fi
	WAGO_SITE_NOBUILD=1 WAGO_WEBSITE_DIR=$(WEBSITE_DIR) scripts/update-website-startup.mjs
	@if [ -f bench/out/bench.json ] || [ -f $(BENCH_RUN) ]; then \
		WAGO_SITE_NOBUILD=1 WAGO_WEBSITE_DIR=$(WEBSITE_DIR) scripts/update-website-bench.mjs; \
	else \
		echo "make: no local bench data (bench/out/bench.json or $(BENCH_RUN)); leaving performance section as-is — run 'make bench bench-chart' to refresh it"; \
	fi
	cd $(WEBSITE_DIR) && npm run sync && npm run build
	@echo "make: website regenerated from data (startup + performance + stats)"

# Publish the captured run to wago-org/docs: publish-bench.sh re-renders the
# charts from the capture, appends history, and pushes. Best-effort: a capture
# whose git stamp differs from HEAD is published anyway with a warning (benchpub
# stamps the numbers with the capture's origin commit and warns too).
.PHONY: bench-publish
bench-publish: ## Publish the capture to wago-org/docs (warns, doesn't fail, if the capture is stale)
	@if [ ! -f "$(BENCH_RUN)" ]; then echo "make: no capture at $(BENCH_RUN); run 'make bench'" >&2; exit 1; fi
	@cached="$$(sed -n 's/^\# git //p' $(BENCH_RUN) | head -1)"; \
	if [ "$$cached" != "$(HEAD_HASH)" ]; then \
		echo "make: WARNING capture is stale (captured at $${cached:-none}, HEAD is $(HEAD_HASH)); publishing anyway — run 'make bench' to refresh" >&2; \
	fi
	WAGO_BENCH_IN=$(BENCH_RUN) scripts/publish-bench.sh
	@if [ -f "../website/package.json" ]; then \
		WAGO_BENCH_IN=$(BENCH_RUN) scripts/update-website-bench.mjs; \
	else \
		echo "make: ../website not found; skipping website benchmark update"; \
	fi

.PHONY: bench-charts
bench-charts: ## Regenerate + publish benchmark charts to wago-org/docs
	scripts/publish-charts.sh

.PHONY: hooks
hooks: ## Install the repo git hooks (.githooks)
	scripts/install-hooks.sh
