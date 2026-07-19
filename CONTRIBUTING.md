# Contributing

`wago` is a small Go-first WebAssembly JIT. Keep changes direct, tested, and
easy to audit.

Target today: **linux/amd64** with Go **1.22+**.

## Setup

```bash
git clone https://github.com/wago-org/wago
cd wago
go test ./...
go build -o wago ./cli/wago
./wago version
./scripts/install-hooks.sh
```

The benchmark module is separate:

```bash
cd bench
go test ./...
go test -bench .
```

## Project Shape

```text
wago.go                          public API facade, generated (re-exports src/wago)
src/wago                         public API implementation
internal/genfacade               generator for wago.go
cli/wago                         CLI
src/core/compiler/wasm           decoder + validator
src/core/compiler/backend/railshot  single-pass x86-64 codegen
src/core/runtime                 mmap, foreign stack, trap plumbing
tests/testdata                   small wasm fixtures
bench                            wazero comparison benchmarks
warp                             upstream C++ reference
```

The root `wago.go` is generated: it re-exports every exported symbol of
`src/wago` so callers keep the clean `github.com/wago-org/wago` import path. When
you add or rename public API in `src/wago`, run `go generate ./...` and commit
the regenerated `wago.go`. CI fails if it is stale.

Use [FEATURES.md](FEATURES.md) before adding feature work and
[ROADMAP.md](ROADMAP.md) before reshuffling priorities.

## Development

- Prefer the existing shape over new abstractions.
- Keep generated machine-code changes tightly scoped and covered by tests.
- Reject unsupported Wasm features explicitly instead of accepting modules that
  run with partial semantics.
- Keep public API behavior boring: return errors for bad inputs, do not panic.
- Preserve the pure-Go runtime and firmware boundary. Use target assembly and
  linker scripts where unavoidable, but do not introduce cgo, a C runtime,
  CMake, or mixed-language board shims without an explicitly approved design.
- Keep comments minimal. Add short doc comments for exported API and larger or
  less obvious compiler/runtime functions.

The optional git hook installed by `./scripts/install-hooks.sh` runs `gofmt` on
staged Go files before commit. If formatting changes anything, review and stage
those changes before committing again.

## AI-Assisted Contributions

AI assistance is fine, but the contributor owns the patch.

If you use AI while preparing a PR, include a short note describing how it was
used. Examples: drafting docs, generating test cases, exploring an approach, or
reviewing code. No long disclosure is needed.

Before submitting AI-assisted work:

- read every changed line and make sure you can explain it
- verify edge cases yourself, especially around validation, traps, memory, and
  native-code generation
- run the same tests you would run for hand-written code
- split broad generated output into small, reviewable commits or PRs
- remove AI-produced code you do not fully understand

For larger design changes, open an issue first and describe the intended
behavior. A clear human explanation matters more than where the first draft came
from.

## Tests

Run this before opening a PR:

```bash
go test ./...
(cd bench && go test ./...)
```

For CLI-facing changes, also build and exercise the examples:

```bash
go build -o wago ./cli/wago
./wago run tests/testdata/fib.wasm 30
./wago run -e hypot tests/testdata/fprog.wasm 3.0 4.0
./wago compile -o /tmp/fib.wago tests/testdata/fib.wasm
./wago run /tmp/fib.wago 30
./wago validate tests/testdata/fib.wasm
```

When adding behavior, add the smallest fixture that proves it. Prefer readable
WAT in the test or a tiny checked-in wasm fixture under `tests/testdata`.

### Spec conformance (wasm 1.0 / MVP)

[`SPECTEST.md`](SPECTEST.md) is a scoreboard of wago against the official
WebAssembly testsuite, vendored as a submodule at `tests/spec` (pinned to a
pre-reference-types commit so the file set is MVP). `TestSpecExec` (in
`spectest_exec_test.go`) runs each file's `assert_return`/`assert_trap`
assertions in an isolated subprocess and scores it; it skips unless the
submodule is checked out and `wast2json` (wabt) is on `PATH`.

Note: `TestSpecExec` is currently only built on linux/amd64 (the JIT backend’s supported platform).

```bash
git submodule update --init tests/spec        # one time
WAGO_SPECTEST_WRITE=SPECTEST.md go test . -run TestSpecExec   # regenerate the scoreboard
```

The `note` column points at the first blocker per file (a missing opcode often
blocks a whole module). Regenerate and commit `SPECTEST.md` when conformance
changes.

## Performance and Stress

`wago` is performance-sensitive, but not every PR needs a full benchmark report.
Use judgment based on the code path you touched.

Run the benchmark suite when changing hot compiler, runtime, call-boundary, or
memory paths:

```bash
cd bench
go test -bench .
```

For changes that affect parsing, validation, codegen, instantiation, memory, or
host-call logging, also test a larger or more adversarial input than the happy
path fixture. The goal is to catch behavior that is technically correct on small
modules but falls over with:

- many functions, locals, params, results, blocks, or table entries
- large active data or element segments
- deep or repeated calls
- long-running loops
- repeated host imports
- large linear-memory reads and writes near bounds

If a change is expected to affect speed or memory use, include before/after
numbers in the PR. If it only affects cold paths, say that instead. Avoid adding
optimizations that make unsupported Wasm features silently accepted; correctness
and explicit failure come first.

## Compiler Changes

Decoder and validator changes live in `src/core/compiler/wasm`. Backend changes
live in `src/core/compiler/backend/railshot`.

For new opcodes or module features:

1. Decode the feature.
2. Validate it against the Wasm type rules.
3. Either compile it completely or reject it with a clear error.
4. Add tests for success and failure/trap behavior.
5. Update [FEATURES.md](FEATURES.md) and [ROADMAP.md](ROADMAP.md) if support
   status changes.

## Runtime Changes

Runtime code crosses into native execution. Be conservative:

- check bounds before writing shared buffers
- keep mmap permissions and cleanup paths obvious
- return Go errors for traps and invalid instantiate-time state
- add stress tests for stack, memory, host-call, and trap behavior

## Docs

README examples should be copy-paste runnable from a fresh checkout. If a doc
example uses a fixture, keep that fixture checked in.

Keep docs concise and concrete. Avoid documenting planned features as supported.

## Pull Requests

Good PRs include:

- a short explanation of the behavior change
- tests or a clear reason tests are not useful
- any support-matrix updates
- benchmark numbers when changing hot code paths

Small PRs are easier to review than broad rewrites.
