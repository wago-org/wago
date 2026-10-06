# Tests

Package-local Go tests stay beside the implementation they exercise. This
directory contains repository-level conformance, integration, fuzz, corpus,
fixture, and support code:

```text
tests/
  conformance/   pinned WebAssembly spec suites and Wasmtime Core 3 ports
  corpus/        regression artifacts with provenance ledgers
  fuzz/          reusable fuzz workers and oracles
  integration/   cross-package and CI-policy tests
  fixtures/      small purpose-built test modules
  support/       shared Go test helpers
  scripts/       shell-level integration checks
  tools/         corpus and documentation maintenance commands
```

Use `just` as the public entry point. Run `just --list test` or
`just --list test spec` to explore:

```sh
just test                         # unit/integration tests + every benchmark corpus workload
CORPUS=quick just test            # faster representative local gate
just test corpus algorithms
just test corpus tag:polybench
just test corpus all              # every curated executable workload
just test spec v1                 # one pinned spec version
just test spec                    # all pinned spec versions
just test fuzz 30s                # bounded fuzzing gates
just test all                     # all of the above
```

The runtime benchmark corpus is repository-level data in `corpus/`; it is not
duplicated under tests. Regression artifacts remain under `tests/corpus`
because they are narrow bug reproductions, not performance workloads.

## AMD64 loop-boundary regression checks

`src/wago/loop_boundary_*test.go` keeps the #817 experiment's useful
checks: arithmetic and trap oracles, live stack/register pressure, native
producer placement, and compile/prepared-call benchmarks. It tests zero, one,
and many iterations, conditional entry, aliases, constants, subnormal values,
signed zeros, infinities and NaNs. No additional eager-flush optimization was
retained: the measured candidate emitted identical native code in all nine
fixtures and had no significant timing improvement on the tested AMD CPU.

The native check reads instruction addresses and direct backedge targets in
GNU objdump output, for both SSE2 and modern encodings. Its deliberately
inside-loop producer has the same multiply count as the pre-loop producer;
the observer must distinguish their placement. This is a bounded single-loop
check, not a general native control-flow analyzer. With `wago_codegenstats`, it
also verifies the established compiler path and actual allocator spills in the
register-pressure fixture.

```sh
go test -tags=wago_regalloccheck,wago_codegenstats ./src/wago -run '^TestLoopBoundary'
go test ./src/wago -run '^$' -bench '^BenchmarkLoopBoundary' -benchmem
```

These tests use the existing WABT helper and skip when `wat2wasm` is unavailable;
native placement additionally requires GNU `objdump` or `gobjdump` and skips
when only LLVM objdump is available. WAT assembly and guest
execution are excluded from the compile benchmark; setup and compilation are
excluded from the prepared-call benchmark. No wall-clock threshold is asserted.
Native ARM64 and admitted memory-region/shared-compiler loops remain outside
this coverage.

## Bytecode summary agreement

`TestBytecodeSummary*` in `src/core/compiler/wasm` and `src/wago` checks the
bytecode scanners against explicit expectations, full decoding, and validation.
Run the bounded matrix with:

```sh
go test ./src/core/compiler/wasm ./src/wago -run '^TestBytecodeSummary' -count=1
```

`tests/support/summaryfixtures/fixtures.go` records the WAT and fixed binary
encoding of 19 small modules. Eighteen encodings were checked with WABT 1.0.41
(`wat2wasm --enable-all`). The GC encoding was checked by hand because that
WABT does not parse the selected GC syntax. Tests need no external tools.
Each test decodes its own binary slice. The largest module is 113 bytes; tests
reject fixtures above 512 bytes. Branch vectors have at most eight explicit
labels, and structured bodies nest at most two levels.

The 48 immediate cases cover scalar LEB/fixed values, block signatures,
branch/call indexes, typed select, indexed memory32/memory64 offsets, bulk
segments, SIMD constants/shuffles/lanes/memory, atomics, references, GC index
and cast forms, and EH catch vectors. Existing walker, module-facts, and feature
fusion tests remain in place. The added matrix checks 2,304 immediate-storage
predecessor pairs and 361 module-analysis pairs. Another 76 transitions check
recovery after validation errors. Wrong-skip and disabled-observer controls
must fail the same gates used by the positive cases.

Malformed fixtures stay in decoding and validation tests. Error categories,
positions, partial feature summaries, and conservative module facts follow each
API's contract. A byte-at-a-time decoder and a bulk skip can report different
positions for the same truncated vector. SIMD constant/shuffle classification
uses the existing prefix/subopcode fields, without requiring a populated kind.

This is bounded coverage, not an exhaustive opcode or proposal test. Full GC
type graphs, descriptor/string proposals, every SIMD/atomic and EH form,
imported mixed-width memories, and maximum-size stress cases remain outside
this matrix. Decoder and scanner helpers are shared; agreement between them
alone is not independent proof. Hand expectations and pinned WABT cases provide
separate checks. Validation and product admission remain separate operations.

`BenchmarkBytecodeSummary*` reports scan, full-decode, module-summary, and
complete-compile costs. Cached body scans must allocate nothing. Complete
module-summary calls allocate their result facts and can allocate classifier
setup. Do not compare those two allocation contracts as if they were identical.

The final #827 experiment tried to omit a second requirements scan for
`ref.null func` and `ref.null extern`. Seven alternating 100 ms sample pairs on
Linux/amd64 (Go 1.27.1, Ryzen 7 8845HS, one Go worker, CPU 4) showed isolated
summary reductions of 14.2%, 92.7%, and 99.5% for 1, 128, and 2,048 nulls.
There was no significant full-compile improvement in those cases or in cjson,
coremark, zstd, and wren. Allocation counts were unchanged. Those four corpus
modules already needed zero detailed requirements scans. The optimization was
not retained; no production speed gain is claimed.

## AMD64 worker reset

`TestWorkerScratchMatchesFresh` compares each function with a fresh worker after
14 fixed predecessor sequences. Its generated Go fixtures need no WABT or
external files. The matrix covers shared scalar and fallback lowering, large
stacks and locals, branch tables, calls, traps, memory32/memory64, and both
native code-size policies. Exact code and metadata checks run before native
execution. Modern-CPU code runs only when the host supports its features.

Run the focused checks with:

```sh
go test -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'TestWorkerScratch|TestWorkerModuleStatsComparison'
go test -tags=wago_profile,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run TestWorkerScratch
python3 tests/scripts/check-worker-reset-control.py
```

The control script uses a temporary Go overlay to omit the CPU-feature reset.
It requires the comparison to reject stale metadata before the first native
call. It changes no checkout file. The profile build checks source and unwind
metadata on fallback lowering; it does not qualify the shared scalar path.
GC/EH roots and native ARM64 are outside this matrix.

`TestWorkerScratchMatchesFreshAfterError` checks reuse after a controlled backend
error. Failed code is never executed. `BenchmarkWorkerScratchReuse` compares
existing reuse with a fresh worker per function and reports allocations and
tracked retained scalar/node/control scratch. These counters do not include
all worker heap storage. They measure existing behavior, not a new speed gain.

The serial/parallel statistics comparison excludes scalar admission timing and
worker-lifetime scratch counters, just as it excludes node/control counters.
Code, source metadata, frame data, and compiler path remain part of the check.
