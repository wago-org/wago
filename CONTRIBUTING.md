# Contributing

Thank you for helping with `wago`, a small Go-first WebAssembly JIT. Keep each
change small, tested, and easy to review.

Run commands from the repository root unless a command says otherwise. Start
with [README.md](README.md), [ARCHITECTURE.md](ARCHITECTURE.md),
[FEATURES.md](FEATURES.md), and [ROADMAP.md](ROADMAP.md).

## Before You Start

`wago` supports Go **1.22+**. Its first-class targets are linux/amd64,
linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, and windows/arm64. CI,
release assets, and conformance gates cover all six targets. See
[FEATURES.md](FEATURES.md) for feature-level platform support.

Read these files before you plan feature work:

- [FEATURES.md](FEATURES.md) lists implemented and unsupported features.
- [ROADMAP.md](ROADMAP.md) lists the current priorities.
- [ARCHITECTURE.md](ARCHITECTURE.md) explains the main compiler and runtime
  parts.

## Set Up a Checkout

```bash
git clone https://github.com/wago-org/wago
cd wago
make test
go build -o wago ./cli/wago
./wago version
go build -tags wago_runtime -o wago-runtime ./cli/wago
./scripts/install-hooks.sh
```

`wago` is the manager command. `wago-runtime` is the runtime command. The
optional hook formats staged Go files. Review and stage its changes before you
commit again.

The benchmark suite is a separate Go module:

```bash
cd bench
go test ./...
go test -bench .
```

## Find the Code

| Path | Purpose |
|---|---|
| `wago.go` | Generated public API facade. It re-exports `src/wago`. |
| `src/wago` | Public API implementation. |
| `internal/genfacade` | Generator for `wago.go`. |
| `cli/wago` | Build-tagged manager and runtime entry point. |
| `cli/manager` | Manager commands. |
| `cli/runtime` | Runtime commands. |
| `cli/internal` | Shared CLI code. |
| `src/core/compiler/wasm` | WebAssembly decoder and validator. |
| `src/core/compiler/backend/railshot` | Single-pass amd64 and arm64 code generator. |
| `src/core/runtime` | Memory maps, foreign stack, and trap code. |
| `tests` | Test harnesses, fixtures, corpora, and scripts. |
| `bench` | Runtime-comparison benchmarks. |

`wago.go` is generated. When you add or rename public API in `src/wago`, run
`go generate ./...` and commit the updated `wago.go`. CI rejects a stale facade.

## Make a Change

- Prefer the existing design over a new abstraction.
- Keep generated machine-code changes narrow and cover them with tests.
- Decode, validate, and compile a WebAssembly feature completely. Otherwise,
  reject it with a clear error.
- Return errors for bad public API input. Do not panic.
- Keep the no-cgo runtime boundary unless a design note explains the change.
- Use short comments. Add doc comments for exported API and for non-obvious
  compiler or runtime code.

For a new opcode or module feature:

1. Decode the feature.
2. Validate it against the WebAssembly type rules.
3. Compile it completely or reject it clearly.
4. Test successful execution and failure or trap behavior.
5. Update [FEATURES.md](FEATURES.md) and [ROADMAP.md](ROADMAP.md) when support
   status changes.

Take extra care in runtime code. It crosses into native execution.

- Check bounds before you write shared buffers.
- Keep memory-map permissions and cleanup paths easy to inspect.
- Return Go errors for traps and bad instantiate-time state.
- Add stress tests for stacks, memory, host calls, and traps.

## Test Your Change

Start with the smallest relevant test. Before you open a pull request, run:

```bash
go test ./...
(cd bench && go test ./...)
```

For CLI changes, also build and run these checks:

```bash
go build -o wago ./cli/wago
go build -tags wago_runtime -o wago-runtime ./cli/wago
./wago-runtime run tests/fixtures/wasm/fib.wasm 30
./wago-runtime run -e hypot tests/fixtures/wasm/fprog.wasm 3.0 4.0
./wago-runtime build -o /tmp/fib.wago tests/fixtures/wasm/fib.wasm
./wago-runtime run /tmp/fib.wago 30
./wago-runtime validate tests/fixtures/wasm/fib.wasm
```

Use the smallest fixture that proves new behavior. Prefer readable WAT in a
test or a small checked-in `.wasm` file under `tests/fixtures/wasm`. See
[tests/README.md](tests/README.md) for the complete test layout and fixture
provenance.

### WebAssembly Conformance

[SPECTEST.md](SPECTEST.md) records results against the official WebAssembly
testsuite. The WebAssembly 1.0 suite is vendored as the `tests/spec` submodule
at a pre-reference-types revision. `TestSpecExec` runs its `assert_return` and
`assert_trap` assertions in isolated subprocesses. It needs the checked-out
submodule and WABT's `wast2json` on `PATH`.

`TestSpecExec` runs on linux/amd64, linux/arm64, and darwin/arm64. Regenerate
the WebAssembly 1.0 report when conformance changes:

```bash
git submodule update --init tests/spec
WAGO_SPECTEST_WRITE=SPECTEST.md go test . -run TestSpecExec
```

The report's `note` column gives the first blocker for each file. A missing
opcode can block a whole module. Commit the regenerated report with the
conformance change.

The pinned WebAssembly 2.0 wrappers need WABT and `tests/spec-v2`. Run
`make spec2` when you change decoding, validation, linking, or execution
semantics.

## Measure Performance and Stress

`wago` is performance-sensitive. Run benchmarks when you change compiler,
runtime, call-boundary, or memory hot paths:

```bash
cd bench
go test -bench .
```

For changes to parsing, validation, code generation, instantiation, memory, or
host-call logging, test a larger or adversarial input as well as the small
fixture. For function-worker changes, include `BenchmarkValidateWorkers` or
`BenchmarkCompileFullWorkers` at a fixed `GOMAXPROCS`, with serial and parallel
allocation counts.

Use inputs with many functions, locals, parameters, results, blocks, or table
entries. Also cover large active data or element segments, deep or repeated
calls, long loops, repeated host imports, and memory access near bounds.

If a change can affect speed or memory use, include before-and-after numbers in
the pull request. If it affects only cold paths, say so. Do not accept an
unsupported WebAssembly feature just to improve an optimization result.

## Write Docs and Open a Pull Request

Keep README examples runnable from a fresh checkout. Keep their fixtures in the
repository. Describe only supported behavior, not planned behavior.

Every pull request needs:

- a short description of the behavior change;
- tests, or a clear reason tests are not useful;
- support-matrix updates when needed; and
- benchmark numbers for hot-path changes.

Small pull requests are easier to review.

## AI-Assisted Work

AI tools are allowed, but you own the patch. Add a short pull-request note that
states how you used them, such as drafting docs, generating tests, exploring an
approach, or reviewing code.

Before you submit AI-assisted work:

- Read every changed line and be able to explain it.
- Check edge cases, especially validation, traps, memory, and native code
  generation.
- Run the same tests you would run for handwritten code.
- Split broad generated output into small, reviewable commits or pull requests.
- Remove generated code that you do not fully understand.

For a large design change, open an issue first and describe the intended
behavior. A clear human explanation is more important than the first draft's
source.
