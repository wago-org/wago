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

## Exact SIMD source pairs

`simd_rule_pairs_test.go` tests the S14/S15/S16 supplement of #809. It compares
widened unsigned rounded byte averages, fixed high-byte extraction, and signed
constant dot/sub expressions with their shorter core-SIMD forms. An additional
dot/sub form extracts the two signed halves with arithmetic shifts and subtracts
them. This avoids the fused-dot form's SSE2 regression in the measured fixture. Each original
and simplified module must match an independent full-vector lane oracle. No
production rewrite is added. Signed i16 dot coefficients are exactly 1/0/-1;
these tests do not generalize to relaxed dot, saturation, or arbitrary constants.

The byte-average lane model exhausts all 65,536 pairs. The high-byte model
enumerates every high byte and selected low-bit patterns; the shift identity
follows because the low 24 bits cannot contribute to a shift by 30. The dot
identity is the exact linear equation `a*1+b*0-(a*0+b*1)=a-b`; its signed-i16
range fits i32. The test sweeps every first lane with five second-lane controls.
Seven near misses reject wrapping/rounding changes, swapped halves, wrong byte
mapping, sign extension, changed shift counts, and different vector producers.

Native execution uses 64 distinct records from seed 809, with signed extremes
and byte sentinels. Each record contains two inputs and one complete 128-bit
output. Tests check unchanged input bytes, all output bytes, zero-count behavior,
and completed work. The same observer rejects skipped work and changed output.
AMD64 runs SSE2 and the enabled AVX2 profile. A diagnostic build reports the
established compiler, spills, and literal bytes. GNU objdump checks the selected
`pavgb`, immediate-30 `psrld`, `pmaddwd`, and the two immediate-16 `psrad`
instructions; substituting each valid
original artifact must fail that selection gate. Loaded and Wasm hashes are logged.

`BenchmarkSIMDSourcePairs` calls the low-level native engine on 256 records per
iteration, with correctness checks before and after timing. Its result is a
source-form comparison, not a public invocation or compiler-fix speedup.
`BenchmarkSIMDSourcePairCompile` includes decode, validation, and code generation
and excludes WAT assembly, executable mapping, and execution. Both report
allocations; execution also reports native bytes. The fixtures need the existing
WABT helper. Native ARM64 runs are supported but were not executed locally.
Rule-directed versus generic-generator yield, other control-flow/effect contexts,
and the rest of #809 remain outside this bounded slice.


## Semantic result and execution identity controls

`TestSemanticProfilesAndExecutionIdentity` uses the existing spec command runner
and isolated regression process. Twelve deterministic cases define two bounded
profiles: `core-exact-and-arithmetic-nan-v1` and `relaxed-swizzle-v1`. They cover
integer widths, signed zero, exact signaling-NaN copies, full-vector lane order,
operation-specific arithmetic/canonical NaN classes, an unreachable trap, and
the pinned Core 3 relaxed-swizzle case with indices 16–31. The latter accepts
the permitted TBL/PSHUFB alternatives and separately checks Wago's target choice.
This does not generalize relaxed allowances to ordinary SIMD or NaN copies.

The optional observer hashes the Wasm bytes passed to compilation and the actual
executable mapping used by the instance. It records the test-binary SHA-256,
Go/OS/architecture, selected/required CPU mask, explicit bounds mode, public
invocation API, and result-profile versions. Diagnostic builds recompile with
the same options, require exact loaded-code agreement, and report each actual
shared/established function path. Ordinary builds leave path evidence absent;
an absent report is inconclusive, not proof of shared admission.

A schedule prepared before execution requires one terminal record per case.
Counts distinguish requested, executed, excluded, and missing cases. Return and
trap records retain raw slots before comparison and before the next invocation
expires them. Rejection, unsupported feature, limit, timeout, host failure, and
mismatch statuses cannot count as successful semantic coverage. Unit controls
check status classification; a compile-rejection wrapper checks real rejection
accounting. The supported positive lane must execute every scheduled case.

Two independent wrapper controls silently omit a supported action or load a
valid module with an extra custom section. The substitute returns the same
values and can have identical native code. The scheduled-case/source-identity
gate must still reject both controls for the intended reason. Wrong signed-zero,
width, lane-order, NaN-copy, NaN-class, and relaxed-result controls exercise the
existing comparison predicates. Deliberately faulty native code is never run.

The observer is optional and confined to `_test.go` files. Retained records are
bounded to 257, with at most two raw slots per record and bounded error text.
`BenchmarkSemanticResultObservation` compares the existing action path with and
without raw-result retention; compile, load, hashing, and gate evaluation are
outside that timing. There is no production performance or allocation change.
Native AMD64 and emulated ARM64 passed locally; native ARM64 is required in CI.
The profiles cover these named operations, not every relaxed-SIMD operation or
all corpus engine adapters. Wider adoption remains part of #819.

## Shared versus established compiler paths

`TestSharedEstablishedPathPairs` uses the same 22 fixture modules on AMD64 and
ARM64. A test-only adapter selects established compilation or ordinary shared
admission with the same options. Tests execute all 71 defined functions in both
modes, including the void fixture and every function in the many-function row.
They compare exact semantic result bits, the unreachable trap, ordered deferred
host events, and visible memory with fixed oracles. An optional independent Node/V8 run checks the same module bytes and
all exported functions; its absence is a separate skip, not successful coverage.

The matrix includes small and empty functions, 256/257 locals, 32/33 nested
controls, and both sides of the 16,384-instruction admission budget. Explicit
fallbacks cover indexed block/loop parameters, compatible branch-table labels,
unreachable typed blocks, discarded multi-results, FP, SIMD, and memory effects. A mixed module compares the ordered event log
from an established caller that invokes a shared-eligible callee. An i64 leaf
returns an exact value above binary64 integer precision.
Seven typed-control/result variants each violate one type premise and must stop
at authoritative validation. They are never sent to the native compiler.
The catch-free typed and unreachable `try_table` shapes reuse the #739
regression, with actual fallback evidence added here. A GC i31 construction
and extraction row checks reference fallback without collector allocation.
Existing EH/GC suites retain broader combinations; this matrix does not claim
an independent transport or collector-root proof.

Diagnostic builds require actual per-function shared/established evidence,
record frame sizes, and reject a valid established artifact labeled as shared.
The log's source-premise text explains the fixture; it is not a new production
fallback-reason field. A control omits the zero-returning function while its
untouched result buffer still looks correct; the completed-call gate rejects it.
Profile builds separately require established fallback and real source ranges,
even when shared compilation is requested. Ordinary builds leave path evidence
absent. Locally, all 44 ordinary/diagnostic source and native hash pairs matched.

`BenchmarkSharedEstablishedCompile` includes decode, validation, and codegen
with one worker and no executable mapping. `BenchmarkSharedEstablishedExecute`
times the prepared low-level call to function 0, with checks outside timing;
trap execution is excluded. Both compare matched ordinary builds and report
allocations. Native bytes, target features, explicit bounds, API, source hashes,
and loaded-code hashes are recorded. Compile/execution numbers are current-path
comparisons, not gains from a new compiler change. Native AMD64 and emulated
ARM64 passed locally; native ARM64 CI is still required. All new state is in
shared test support or test files; production admission and codegen are unchanged.

## Rule-directed compiler recipes

`tests/support/ruleguide` supplies bounded typed recipes for three existing
rules: AMD64 `i32-bswap-tee`, portable `swar-mask-test`, and portable
`simd-shift-imm`. The architecture tests compile and execute 36 positive or
one-premise-broken recipes, plus 96 directed and 96 generic template samples
from seed 809. Each case uses 23 edge or random inputs and checks all semantic
result bits against a Go bit-vector model. Contexts cover a plain result, a
local update followed by a source overwrite, a branch result, and a result
kept live across a non-inlined guest call.

Checked diagnostic builds require the exact rule count and record actual
shared/established admission, native frame/spill/literal counts, source/native/
input hashes, Go version, target, feature masks, bounds mode, and completed
calls. Valid disabled scalar rules and a dynamic SIMD-count substitution must
fail the activation gate while retaining correct results. Ordinary builds
check numerical results and report selection as unavailable. ARM64 executes
the byte-swap sources but reports that rule's selection as unsupported.
AMD64 checks SSE2 and, when the host permits it, AVX2.

The yield benchmark gives both generators the same 96-candidate budget, typed
root templates, contexts, operand alphabet, and seed. The generic sampler can
produce every selected rule. It samples operand relations independently;
the directed sampler constrains the required relations. A useful case is a
**distinct source hash within that budget**, with actual rule activation and
two native calls checked against the model. The benchmark includes generation,
validation, compilation, mapping, execution, and duplicate removal. It reports
attempts, activations, distinct useful cases, and useful cases per second.
This is a bounded template comparison, not a yield claim for a general fuzzer.

```sh
go test -tags=wago_regalloccheck,wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^TestRuleDirectedRecipes$' -v
go test -tags=wago_codegenstats ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkRuleDirectedYield' -benchmem
go test ./src/core/compiler/backend/railshot/amd64 -run '^$' -bench '^BenchmarkRuleDirectedPairs' -benchmem
```

The paired benchmarks report decode/validate/compile cost separately from
prepared native calls. Scalar pairs use existing rule kill switches. SIMD pairs
compare a constant count with an equal dynamic count. Setup is outside native
call timing. No wall-clock threshold is asserted. The support code adds no
production instrumentation or retained production memory.
