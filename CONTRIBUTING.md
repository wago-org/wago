# Contributing

Thank you for helping with `wago`! Please keep each
change small, tested, and easy to review.

Run commands from the repository root unless a command says otherwise. Start
with [README.md](README.md), [ARCHITECTURE.md](ARCHITECTURE.md),
[FEATURES.md](FEATURES.md), and [ROADMAP.md](ROADMAP.md).

## Before You Start

`wago` supports Go **1.22+** and uses [just](https://just.systems/) **1.43+**
for developer commands. Its first-class targets are linux/amd64,
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
just test
just build
./wago version
just build runtime standard
./wago-runtime-standard-normal version
just install hooks
```

Run `just install` to install the manager built from the current checkout's
`HEAD`. It does not fetch a release or clone the repository. Set
`WAGO_MANAGER_PATH` to install an existing manager binary instead. Set
`WAGO_MANAGER_SOURCE` with it when the matching source is in another directory.

`wago` is the manager command. `wago-runtime-standard-normal` is the standard
runtime command. The optional hook formats staged Go files. Review and stage
its changes before you commit again.

`cli/wago-installer` is a nested Go module so its `@latest` version is not
selected from Wago runtime tags. Before a beta or stable release, set its Wago
requirement and the matching `go.work` replacement to the release version. The
release workflow checks both values and publishes
`cli/wago-installer/<version>` with the qualified Wago release tag.

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
| `cli/wago-installer` | Installable `wago-installer` command entry point. |
| `cli/installer` | Shared installer implementation. |
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

CI validates documentation for every change. Changes limited to Markdown,
`LICENSE`, or `docs/` paths skip the native code matrix; mixed or executable
changes run both documentation validation and the full matrix.

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

For TinyGo startup changes, test optimized release settings as well as the
default debug build: `-scheduler=tasks -no-debug -opt=z -gc=conservative`.
Repeat `--version` and a real-module call in fresh processes; a single successful
start does not clear an intermittent initialization failure. CI runs this check
for the Minimal/Tiny CLI on its supported native targets. Shared ASCII name
checks in `internal/namecheck` are checked against the prior exact regex grammar
under standard Go; callers must retain their own length limits.

### WebAssembly Conformance

[SPECTEST.md](SPECTEST.md) records results against the official WebAssembly
testsuite. The WebAssembly 1.0 suite is vendored as the `tests/conformance/spec-v1` submodule
at a pre-reference-types revision. `TestSpecExec` runs its `assert_return` and
`assert_trap` assertions in isolated subprocesses. It needs the checked-out
submodule and WABT's `wast2json` on `PATH`.

`TestSpecExec` runs on linux/amd64, linux/arm64, and darwin/arm64. Regenerate
the WebAssembly 1.0 report when conformance changes:

```bash
git submodule update --init tests/conformance/spec-v1
WAGO_SPECTEST_WRITE=SPECTEST.md go test . -run TestSpecExec
```

The report's `note` column gives the first blocker for each file. A missing
opcode can block a whole module. Commit the regenerated report with the
conformance change.

The pinned WebAssembly 2.0 wrappers need WABT and `tests/conformance/spec-v2`. Run
`just test spec v2` when you change decoding, validation, linking, or execution
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

For synchronous host-boundary work, run `BenchmarkInvokeHostFuncDirect` and
`BenchmarkHostRoundtripLoop` in `./src/wago`, including the independent-instance
parallel cases. Compare counts on the same host-capable export and subtract the
matched guest-loop slope. Record each stage separately. See the
[host-call measurement and safety rules](docs/host-roundtrip-performance.md).
Also compare `BenchmarkInvokeCallerHostFuncDirect`,
`BenchmarkHostRoundtripLoopCaller`, `BenchmarkCallerGCLoop`,
`BenchmarkCallerDomainLoop`, and `BenchmarkCallerArity` when changing concrete
callback dispatch. Record medians and ranges, not just the fastest sample. See
the [concrete caller invariants](docs/host-caller-performance.md). Run profiles
and benchmarks without concurrent builds. The full race suite starts many
subprocesses; `GORACE=atexit_sleep_ms=0` avoids the race runtime's fixed exit
delay while retaining race checks.

## Make a Commit

Keep each commit small, measurable, and easy to review. A commit should do one
of these things:

1. Add a test that shows a missing behavior or regression.
2. Make a focused test pass with the smallest change.
3. Do both for one small topic when separate commits would add noise.

Each commit must have one topic. Keep decoder, validator, backend, runtime,
CLI, documentation, and benchmark changes separate unless the behavior needs
them together. Name the test, benchmark, fixture, or observable behavior that
proves the change. Avoid drive-by cleanup, broad formatting, unrelated renames,
and speculative refactors.

When practical, use this sequence:

1. Add the smallest test or fixture that fails for the missing behavior.
2. Make the smallest implementation change that makes the test pass.
3. Make a separate cleanup commit only when it is needed.

You can combine the first two steps when a separate failing state would not
help. State why in the commit message or pull request description.

Update the relevant developer or agent documentation when a commit changes
workflow, test, benchmark, review, unsafe/runtime rule, or performance or
memory expectation. If no documentation update is needed, say why in the
commit message or pull request description.

Use a short, specific subject such as `wasm: reject passive data segments` or
`runtime: reduce host-call buffer allocation`. Add this body when it helps a
reviewer:

```text
Why:
- what behavior, regression, or measurement motivated this

What:
- the focused code, test, or documentation change

Proof:
- the test, fixture, benchmark, or measurement

Docs:
- relevant documentation updated
- or: unchanged, no developer or agent workflow impact
```

Before you commit, confirm that the diff has one topic, the focused tests or
benchmarks pass, and hot-path or memory-sensitive changes include measurements
when practical. Confirm that unsupported WebAssembly behavior is rejected
clearly and that no unrelated formatting, rename, or cleanup is included.

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
