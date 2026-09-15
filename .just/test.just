set working-directory := '..'

mod spec 'spec.just'
mod regression 'regression.just'

# Run ordinary unit, integration, and quick corpus tests.
[default]
quick corpus=env('CORPUS', 'quick'):
    just test unit
    just test corpus '{{ corpus }}'

# Build and run unit and integration tests without external spec suites.
unit:
    go build ./...
    go build -tags wago_runtime ./cli/...
    go test -count=1 ./...
    go test -count=1 -tags wago_runtime ./cli/...
    tests/scripts/build-release-assets.sh
    tests/scripts/release-qualification.sh

# Run every correctness suite.
all:
    just test unit
    just test corpus all
    just test spec
    just test fuzz

# Run the deterministic runtime concurrency harness.
concurrency:
    go test -count=1 -run '^TestRuntimeConcurrency' ./tests/integration/runtimeconcurrency

# Compare Starshine state hashes in Node and Railshot.
engine-state *args:
    scripts/fuzz-engine-state.sh {{ args }}

# Compile, link, and instantiate a MoonBit Starshine wasm-gc artifact.
starshine wasm:
    WAGO_STARSHINE_SMOKE_WASM='{{ wasm }}' go test ./src/wago -run '^TestMoonBitStarshineWasmGCSmoke(Compile|Instantiate)$' -count=1 -v

# Run public-API and corpus guard-page tests.
guard corpus=env('CORPUS', 'quick'):
    go test -count=1 -tags wago_guardpage ./src/wago/
    cd bench && go test -count=1 -tags wago_guardpage -run '^(TestCorpus|TestJsonAsGuardCorrect)$' ./suite -args -wago.corpus='{{ corpus }}'

# Run the checks formerly assigned to the native macOS/arm64 CI lane.
native-arm64:
    go test ./src/core/encoder/arm64 ./src/core/compiler/backend/railshot/arm64 ./src/core/runtime ./src/wago -count=1
    go test -tags wago_guardpage ./src/core/runtime ./src/wago -count=1 -v
    WAGO_CORPUS_TIMEOUT=20s just test corpus

# Run the corpus pipeline and exact-oracle execution.
corpus profile=env('CORPUS', 'quick'):
    cd bench && go test -count=1 -run '^(TestCorpus|TestCorpusSemanticExec|TestApplicationCorpusRuns)$' ./suite -args -wago.corpus='{{ profile }}'

# Run the semantic corpus against exact oracles.
semantic-corpus:
    cd bench && go test -count=1 ./internal/semanticcorpus

# Run bounded fuzzing gates.
fuzz duration=env('FUZZTIME', '5s'):
    go test ./tests/support/regressioncorpus -run '^$' -fuzz '^FuzzValidateRelativePathAndRustScannerDoNotPanic$' -fuzztime='{{ duration }}'
    go test ./tests/tools/regression-corpus -run '^$' -fuzz '^FuzzNormalizeWABTJSONDoesNotPanic$' -fuzztime='{{ duration }}'
    go test ./src/wago -run '^$' -fuzz '^FuzzSpecTrapMatchingDoesNotPanic$' -fuzztime='{{ duration }}'
    go test ./src/core/compiler/wasm -run '^$' -fuzz '^FuzzDecodeValidateByteBackedDifferentialGenerated$' -fuzztime='{{ duration }}'

# Run the runtime and public API suites under TinyGo.
tinygo compiler=env('TINYGO', 'tinygo') scheduler=env('TINYGO_SCHEDULER', 'tasks'):
    {{ compiler }} test -v -scheduler='{{ scheduler }}' ./src/core/runtime/ ./src/wago/
