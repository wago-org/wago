# Analyze checkpoint verification

Environment: Linux AMD64, Go 1.27.1. These are correctness/compilation outputs, not benchmark timings. Trailing whitespace in captured output is normalized.

| Output | Command |
| --- | --- |
| tests-default.txt | `go test ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/optimization ./cli/internal/settings ./cli/runtime/commands/run -count=1` |
| tests-focused-stats.txt | `go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestGCLifetime' -count=1` |
| tests-race.txt | `go test -race -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestGCLifetime' -count=1` |
| tests-runtime.txt | `go test ./src/wago -run 'Test(ConfigOptimizationSelectionIsImmutableAndValidated\|DeadGC\|GCConstructor\|GCStructReduction)' -count=1` (shell regex uses ordinary `|`, table escapes are formatting only) |
| tests-stats.txt | `go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 ./src/core/compiler/optimization -count=1` — fails; see baseline comparison |
| baseline-fixture.txt | In isolated `git archive 9713878ab` export with only `gc_lifetime_bench_test.go` copied in: `go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -count=1` |
| baseline-stats.txt | Same export: `go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -count=1` — fails |

All 24 top-level failing tests in tests-stats.txt also fail on the base. The base additionally fails TestSharedEstablishedPathPairs; this is not evidence of a fix here. No engine fixes made. The passing race run includes the complete new test family. Full tagged suite outputs predate only later diagnostic offset reporting/additional focused tests, with no emitted-code changes.

Temporary baseline exports were created with Python TemporaryDirectory and extracted via `git archive`; they were removed on exit. To avoid mise trust/toolchain mismatches outside this checkout, that check used `/home/dev/.local/share/mise/installs/go/1.27.1/bin/go` with `GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1` and `GOTOOLCHAIN=local`. No benchmarks ran. See the plan for the portable benchmark entrypoint, baseline setup and matched measurement requirements.
