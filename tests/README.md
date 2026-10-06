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

## AMD64 straight-line bounds proofs

The backend's `bounds_cert_model_test.go` checks source identity, proof extent,
changed sources, empty slots, replacement, and replacement-cursor wrap. Its
independent map records valid proofs without copying the cache's placement
rules. A deliberately stale proof must fail this gate before native execution.
`bounds_research_test.go` checks actual bounds-check counts at the table's
capacity, the established compiler path, explicit/guard modes, disabled facts,
and trap/memory agreement after an address changes.

Run the diagnostic tests from the repository root:

```sh
go test -tags=wago_codegenstats,wago_regalloccheck ./src/core/compiler/backend/railshot/amd64 -run 'TestBounds(Certificates|Facts)'
```

The ordinary-build `BenchmarkBoundsCertificates`, `BenchmarkBoundsFactsCompile`,
and `BenchmarkBoundsFactsExecute` separate table operations, complete backend
compilation, and native execution. Compile inputs include 1, 2, 8, 9, and 32
independent addresses and the cjson, coremark, zstd, and wren corpus modules.
Each generated execution body reads the addresses 32 times. Guard and explicit
checks are separate rows. These tests do not qualify memory64, imported-memory
aliasing, or native ARM64.

Research issue #818 compared three alternatives against main `640548d8` on
Linux AMD64 (Ryzen 7 8845HS, Go 1.27.1, CPU 4, GOMAXPROCS=1). Seven samples used
rotating order and 100 ms per row, followed by seven 150 ms samples for the last
two alternatives. Production changes were rejected:

- A single-pass update slowed table operations by 3.7–12.9% without a clear
  full-compile gain.
- An unrolled lookup improved larger table operations but slowed the one-source
  case by 17.4%. Its initial wren compile gain did not repeat.
- A 16-entry table improved the nine-address explicit-mode fixture by 83.5% in
  compilation and 47.9% in execution. It added 96 bytes to the proof table and
  128 allocated bytes per compile in the measured fixtures. Two guard-mode
  compile controls slowed by 4.6% and 6.8%; the four corpus compile comparisons
  showed no significant explicit-mode gain. Across 121 corpus modules, it
  removed 225 of 5,653,784 explicit checks and 3,232 code bytes. All guard-mode
  code and 108 explicit-mode modules were unchanged.

These results use unadjusted pairwise comparisons on one host. They support
keeping the tests and benchmarks, not a general workload speed claim. The
production table remains eight entries; these additions use no production
memory or instrumentation.

## Footprint report omission controls

`TestNativeSizeRejectsOmittedAlignment` compiles small exported functions alone
and together on each native backend. The standalone images establish physical
function spans. The combined image establishes the intervening padding span.
The positive report must reconcile with those bytes. Omitting nonzero padding
from a report copy must fail the same gate. Reassigning padding to another
category must fail the boundary check even when the total remains correct.
Run it with `-tags=wago_codegenstats` on the target architecture.

`TestArtifactSizeRejectsOmittedMetadata` reads section lengths and the entry
vector span from serialized bytes. It rejects report copies with omitted
metadata, entries, or framing, plus an entry-to-import reassignment. It never
changes executable or serialized artifacts. `BenchmarkArtifactFootprintCount`
measures the existing count-only path with 0-byte, 64-KiB, and 8-MiB passive
payloads; these controls add no production state or allocation.

These are byte-attribution checks after compilation/serialization, not release
or leak tests. Executable payload, mapped capacity, retained compiler storage,
allocation volume, peak memory, and RSS remain separate quantities. Existing
ownership tests own release semantics. Native execution is reported separately
from cross-build success; these checks do not address deferred issue #801.

## Synchronous and deferred host-event boundaries

`host_event_boundary_test.go` uses a host-owned imported scalar global to
observe the public callback contract. The guest writes phase 1, emits event 11,
reads the global, writes phase 2, emits event 22, and writes phase 3 before
return. Synchronous callbacks observe phases 1 and 2; their mutation is visible
to the next guest instruction. Deferred callbacks both observe phase 3 and do
not change that guest result. The callbacks use public numeric global accessors
and retain no borrowed guest storage.

The tests also check that a guest trap discards pending events, a replayed
callback failure stops later events after guest work finishes, and a repeated
prepared invocation has no leftover events. The same observer rejects a local
adapter that delivers the first event early, reversed events, and a dropped
event. Existing `host_event_test.go` tests own buffer overflow, exact signatures,
mixed-mode rejection, and cross-instance restrictions.

Verbose output records Wasm and loaded-code hashes, Go/OS/architecture, explicit
bounds mode, required AMD64 features, callback mode, and retained event-log
bytes. With `wago_codegenstats`, an independent diagnostic compile must reproduce
the loaded bytes and report the established compiler path. These tests support
ordinary Go on native AMD64/ARM64; TinyGo and precompiled-only builds are
excluded. Cross-compilation does not count as executed delivery coverage.

`BenchmarkHostEventBoundary` measures repeated prepared calls with 1, 16, and
1,024 events, checks exact counts and sums, and reports allocations and retained
log bytes. It omits the phase observer's global reads. Synchronous latency and
deferred throughput are different contracts, not interchangeable optimizations.
No production state, API, callback buffer, or instrumentation is added.
