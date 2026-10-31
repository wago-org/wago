# Bulk-memory execution qualification

The ARM64 candidate reaches Wasmtime parity on the five steady-state bulk-memory
contracts currently used by wasm.fyi. This is a local execution comparison of the
exact Wasm artifacts, not a published wasm.fyi result or a compilation, RSS or
host-call-latency claim. Baseline is Wago
`90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`; candidate source is the tree containing
this report on `jairus/tweak-bulk-memory`.

## ARM64, 2026-10-31

Apple M4 Max, macOS/Darwin arm64, Go 1.27.1, Rust 1.98.1, Wasmtime crate 50.0.0
with Cranelift. The two small protocol adapters use Wago's public
`WasmFunc.Invoke` and Wasmtime's Rust `Func.call`. Wago uses one compiler worker.
Both capture every call's result inside the timing window and verify it afterward.
They are local core embeddings, not complete copies of the wasm.fyi adapters.

The [raw samples and identities](arm64.json) retain eight independent launches
per engine/workload, seven measured samples per launch, and a minimum 75 ms
per sample. Each workload alternates runtime order between launches in fresh
processes, with GOMAXPROCS=1, foreground task policy and the existing wasm.fyi
measurement lease. There is no concurrent build. Other desktop applications were
running; timing variation is retained rather than filtered away.

Times below are medians of launch medians in microseconds per exported call.
Each call contains 64 in-Wasm repetitions. Deltas are medians of paired launch
ratios: `(candidate / comparison - 1) * 100`; negative is faster.

| Contract | Wago baseline us | Wago candidate us | Wasmtime us | Delta vs baseline | Delta vs Wasmtime |
| --- | ---: | ---: | ---: | ---: | ---: |
| fill, 65536x64 | 39.062 | 34.660 | 35.883 | -10.2% | -4.4% |
| copy-overlap, 65536x64 | 98.900 | 69.659 | 75.344 | -30.0% | -8.5% |
| record-pack, 64x64 | 28.562 | 4.779 | 19.653 | -83.2% | -76.1% |
| sliding-window-compaction, 64x64 | 8.446 | 8.558 | 8.871 | +3.5% | -3.3% |
| doubling-pattern-expansion, 64x64 | 25.300 | 3.717 | 18.753 | -85.3% | -80.3% |

Fill is close enough that the claim is parity, not a reliable small win: its
paired ratios span 0.903–1.026 and Wasmtime sample CV reaches 0.125. The other
four workloads beat Wasmtime in every pair. Compaction regresses against the
Wago baseline in this run; the improvement claim does not hide that regression.

The first full campaign used 512-byte fill groups. After choosing 1024-byte
fill groups, fill and copy-overlap were requalified for another eight launches.
The other three contracts have byte-identical native code between the two
candidates; the JSON records those hashes. The three earlier measurements are
retained on that basis, rather than implying a second full campaign occurred.
All 840 measured samples pass the independent exact-return oracles.

## Inputs and reproduction limits

The JSON records every workload's artifact SHA-256, every raw timed sample,
launch order, duration qualification and verification result. The fixture
manifest is the bulk-memory subset of wasm.fyi's
`.wasmbench/feature-performance-current/manifest.json`; all five artifact hashes
match its live ARM64 contracts at measurement time. Expected returns are
896, 1408, 4234330112, 4342784 and 4085170176, respectively. The separate
init/drop lifecycle probe is not part of these five stateless steady workloads.

Local driver, adapter sources, Cargo.lock, binaries, plans, original campaigns
and logs remain in the ignored `.validation/bulk-memory/` directory of the
`wago-bulk-memory` worktree. They reference the local wasm.fyi checkout and are
not a portable benchmark distribution. The checked-in samples document this
historical run; reproducing it requires those adapters and exact fixtures.
Exploratory screens, including rejected copy/fill strategies, are excluded.

## Correctness

- Full `go test -tags=wago_regalloccheck ./src/... -count=1` passes.
- Targeted ordinary, codegen-stat, profiling and guard-page checked tests pass.
- Whole-memory differential tests cover overlap, all small-copy lengths,
  alignment, dirty upper halves of memory32 operands, exact bounds and traps
  before writes. Filled-source forwarding tests also check a completed fill
  remains visible when the following copy traps, cross-memory copies, and
  rejection after a mutation or changed source/count.
- Byte reductions check both final locals against independent modular arithmetic,
  signed-boundary weights, static offsets, empty ranges and bounds fallback.
  Selection tests reject shared/memory64, interruption and source-recording paths.

AMD64 results and remaining parity gaps are recorded below. Hosted CI has not
been run for this branch.

## AMD64, 2026-10-31

Native Hub: Ryzen 7 7800X3D, Linux 7.0.0-38-generic x86_64, Go 1.27.1,
Rust 1.98.1 and Wasmtime 50.0.0 with Cranelift. The same five exact artifacts
and independent oracles are used. Each fresh adapter process is pinned to CPU 2
with one worker; runtime order alternates on each launch. Hub's shared benchmark
lock excludes concurrent builds and tests during measurement. Other host
applications were not stopped. Hardware counters were unavailable under the
host's existing perf policy.

The candidate combines exact short-copy chunks, declared-minimum address
proofs, adjacent filled-source forwarding, guarded byte reductions and retained
registers on proved call-free loop exits. Large fills use 512-byte groups with
AVX-512, 128-byte groups with AVX2, and the existing byte string-store path on
baseline SSE2 CPUs. Rejected qword-only fill, narrower-copy and memory-allocation
alignment experiments are excluded.

All source files changed by the AMD64 implementation match between the local
worktree and Hub by SHA-256. The raw results record those hashes and the measured
adapter binary hashes. Wago baseline remains
`90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`; its Go 1.27.1 Linux/amd64 adapter
was cross-built and run natively. The candidate adapter was built natively on
Hub with the same Go version, CGO disabled, trimpath and no build tags. Source
qualification and measurement remain separate from a hosted wasm.fyi result.

The final ARM64 checked suite also passes after the shared option/test changes.
All five ARM64 native-code hashes match the previously qualified candidate.
The settled AMD64 source passes the full checked `./src/...` suite, including
its pinned Release 3 fixtures and reference interpreter. Targeted SSE2,
guard-page and source-profiling builds with register checks pass; selection
checks additionally use `wago_codegenstats`. The native encoder suite and
`go generate ./...` pass. These are local checks, not hosted CI results.

The [raw AMD64 samples and identities](amd64.json) retain eight launches per
engine/workload, seven samples per launch and a minimum 75 ms per sample. All
840 measured samples pass their independent exact-return oracles. Values and
paired deltas follow the same definitions as the ARM64 table.

| Contract | Wago baseline us | Wago candidate us | Wasmtime us | Delta vs baseline | Delta vs Wasmtime |
| --- | ---: | ---: | ---: | ---: | ---: |
| fill | 30.589 | 30.069 | 29.096 | -1.6% | +3.4% |
| copy-overlap | 73.633 | 59.993 | 61.770 | -18.6% | -2.8% |
| record-pack | 34.362 | 7.673 | 30.761 | -77.7% | -75.1% |
| sliding-window-compaction | 20.215 | 19.867 | 18.601 | -1.5% | +6.7% |
| doubling-pattern-expansion | 37.825 | 6.712 | 29.650 | -82.2% | -77.3% |

The geometric mean of the five paired candidate/Wasmtime ratios corresponds to
1.75x faster execution overall. This aggregate is not an individual-workload
parity claim: fill remains 3.4% slower and compaction 6.7% slower than Wasmtime.
Neither remaining gap is hidden by the much larger reduction-loop wins. Maximum
sample CV is 0.020. The goal of meeting or beating Wasmtime on every individual
AMD64 workload remains unmet. All five improve against the Wago baseline in this
campaign. No runtime allocation-layout change or workload-identity dispatch is
included.

## AMD64 copy-and-append qualification, 2026-10-31

A further `bulk-copy-vector` improvement combines short left-shift copies and
adjacent byte appends after proving the complete unshared span. The native
sequence loads original vectors at the same addresses as the previous iteration's
stores, then aligns and inserts bytes in registers. It replaces partial stores
that made the next overlapping read stall. No fill, allocation-layout, adapter
or workload-identity change is included.

The [complete fresh campaign](amd64-copy-append.json) uses the same native Hub,
Go/Rust versions, Wasmtime 50.0.0, exact artifacts, independent oracles, lock,
affinity and alternating fresh-process protocol. All five workloads were
requalified for eight launches with seven samples of at least 75 ms each; all
840 measured samples pass. Fill and overlap-copy native code match the initial
candidate byte for byte. The three curated kernels share one module, so the
changed compaction lowering also changes native layout in the other two modules;
those results were measured again rather than carried forward.

| Contract | Wago baseline us | Wago candidate us | Wasmtime us | Delta vs baseline | Delta vs Wasmtime |
| --- | ---: | ---: | ---: | ---: | ---: |
| fill | 30.193 | 30.018 | 28.933 | -1.1% | +3.4% |
| copy-overlap | 73.517 | 60.209 | 61.578 | -18.0% | -2.5% |
| record-pack | 34.453 | 7.691 | 30.627 | -77.8% | -75.0% |
| sliding-window-compaction | 20.173 | 11.236 | 18.565 | -44.3% | -39.4% |
| doubling-pattern-expansion | 37.658 | 6.701 | 29.497 | -82.2% | -77.3% |

Compaction now beats Wasmtime in every paired launch. Four of the five AMD64
contracts meet or beat it; fill remains 3.4% slower by the median paired ratio.
The five-workload geometric mean is 1.96x faster, but individual-workload parity
is still unfinished. Maximum sample CV is 0.020. ARM64 source and runtime layout
are unchanged from the already qualified implementation.

The final source passes the full checked `./src/...` suite with pinned WABT and
Release 3 interpreter tools, plus targeted checked/codegen-stat SSE2, guard-page
and source-profile builds. New whole-memory tests cover lengths 0 through 65,
unaligned and boundary spans, nonmatching sources, trapping appends after visible
copies, earlier load/division traps, repeated calls, modular append expressions,
dirty upper argument bits and up to twenty live vectors. Admission tests cover
inlined locals and rejection of shared memory, missing CPU features, disabled
minimum proofs, effects, memory reads, and source profiling. `go generate ./...`
and diff checks pass. No hosted CI or push has been performed for this update.

## Final two-host qualification, 2026-10-31

The last AMD64 fill gap came from scheduler transitions around an otherwise
competitive native kernel. A same-code diagnostic that selected bounded entry
removed the gap. Production now proves bounded work from fresh Wasm source and
checks any dynamic count on every invocation. The measured candidate uses the
ordinary reference adapter, without the diagnostic override.

The proof supports closed acyclic local calls, restricted i32 operations and
exact non-wrapping constant-count loops. It caps a complete invocation at 4096
admitted source operations and 4 MiB of logical bulk traffic; copies charge both
reads and writes. Up to four i32 parameters are supported, with at most one
invariant parameter contributing dynamic bulk work. Unproved code and larger
inputs retain ordinary scheduler transitions. Existing isolated ownership and
trap handling remain mandatory. Imports, tables, globals, tags, shared/memory64,
GC helpers, custom instructions, interruptible compilation, source maps and
profiles are excluded. The existing `prepared-bounded-entry` option disables the
admission. This policy is Linux/AMD64 only; ARM64 entry selection is unchanged.
Logical source budgets are not elapsed-time guarantees, particularly with
first-touch page faults.

The private immutable sidecar is omitted from serialization and shared by
execution snapshots. It adds 24 bytes to `Compiled` and, only when there is an
admitted function, an eight-byte plan per local function (at most 4096 functions).
No RSS or compilation-speed claim is made. The copy-and-append reader also keeps
its scratch-reuse contract: restricted expression lowering avoids callback
escape paths, and lookahead advances only the reader position. The ordinary
zero-allocation dynamic-copy lowering test passes. All five generated guest-code
hashes on each host match the preceding qualified implementation byte for byte.

Both hosts completed a fresh eight-launch campaign with seven samples of at
least 75 ms per engine/workload. Alternating fresh processes compare baseline
`90f2b57dafa7f8c3cc7e3aca38b91148f15e1d1e`, the candidate and Wasmtime 50.0.0
using the frozen matching wasm.fyi artifacts, exact independent oracles, one
worker and `GOMAXPROCS=1`. Measurements exclude concurrent builds/tests on their
host and use the existing host measurement locks; Hub additionally pins CPU 2.
All 840 measured samples on each host pass, 1680 in total. This is steady
exported-call execution evidence for the five contracts below, separate from
initialization/drop, compilation, memory metrics and hosted wasm.fyi results.

The source is commit `3f8c512dc` plus implementation patch SHA-256
`9ae84a35814840549c0182073897e416f3d9677141e21f8daa645e7c0751d8f0`.
The patch excludes this report and raw results. The raw files record source,
adapter, binary, input and native-code identities. The same normal Go adapter
source is used on both hosts; production builds use Go 1.27.1 with CGO disabled,
trimpath and no build tags. Rust 1.98.1 builds the Wasmtime adapter.

The tables report median launch medians in microseconds. Deltas are medians of
paired per-launch ratios, `(candidate/reference - 1) * 100%`; negative is better.

### ARM64 final refresh

[Raw ARM64 results and identities](arm64-final.json), Apple M4 Max:

| Contract | Wago baseline us | Wago candidate us | Wasmtime us | Delta vs baseline | Delta vs Wasmtime |
| --- | ---: | ---: | ---: | ---: | ---: |
| fill | 38.710 | 34.691 | 36.226 | -11.1% | -3.9% |
| copy-overlap | 99.181 | 71.555 | 75.156 | -28.0% | -4.2% |
| record-pack | 27.108 | 4.790 | 20.236 | -82.4% | -76.2% |
| sliding-window-compaction | 8.702 | 8.784 | 9.194 | +1.5% | -3.5% |
| doubling-pattern-expansion | 24.837 | 3.758 | 19.385 | -84.9% | -80.6% |

All five median paired ratios meet or beat Wasmtime. Fill spans 0.887–1.041
across paired launches and is conservatively treated as parity given timing
noise. The other four beat Wasmtime in every paired launch. Maximum sample CV is
0.310 for baseline fill, 0.082 for the candidate and 0.063 for Wasmtime; these
outliers remain in the raw data. Compaction is 1.5% slower than the Wago baseline
in this refresh, while still beating Wasmtime. The earlier ARM64 qualification
above remains separate; the final refresh does not erase its measurements.

### AMD64 final result

[Raw AMD64 results and identities](amd64-final.json), native Ryzen 7 7800X3D Hub:

| Contract | Wago baseline us | Wago candidate us | Wasmtime us | Delta vs baseline | Delta vs Wasmtime |
| --- | ---: | ---: | ---: | ---: | ---: |
| fill | 30.277 | 28.015 | 29.055 | -7.5% | -3.6% |
| copy-overlap | 73.916 | 60.065 | 61.860 | -18.3% | -2.9% |
| record-pack | 34.537 | 7.711 | 30.817 | -77.8% | -75.0% |
| sliding-window-compaction | 20.356 | 11.348 | 18.674 | -44.0% | -39.3% |
| doubling-pattern-expansion | 37.757 | 6.741 | 29.641 | -82.1% | -77.2% |

All five contracts beat Wasmtime in every paired launch. Fill paired ratios
range from 0.945 to 0.972 and overlap-copy from 0.965 to 0.988. The five-workload
geometric mean is 1.99x faster; every individual workload also passes the requested
gate. Maximum sample CV is 0.025 overall, 0.018 for the candidate and 0.009 for
Wasmtime. Every workload improves against the Wago baseline.

### Final validation

Both hosts pass `go test ./...`, the full root suite with
`-tags=wago_regalloccheck`, and the checked bench module. Hub also passes focused
race, SSE2/codegen-stat, guard-page and source-profile builds. New tests exercise
parameter selection and dirty upper bits, maximum and above-budget work, static
work, nested loops, loop-carried count rejection, non-wrapping induction,
call substitution and depth independent of declaration order, metadata omission,
serial/parallel compilation, exact memory state, repeated traps, ownership
revocation and single-P GC progress. Parser fuzzing completed 20765 executions
without a failure. `go generate ./...` and diff checks pass.

The full runs exposed environment and integration-test issues separately: Hub
Git tag signing was disabled only through test-process configuration, and a
local wrapper gives TinyGo Go 1.26.8 while production/tests use Go 1.27.1. Hub's
plugin integration test transiently reported `text file busy`; the ordinary
full command passed on retry. ARM64's checked CLI fake-command startup timed out
under parallel package load; the full command passed with `-p=1`. No test skip
or production workaround was added for those failures. Structure footprint
assertions include the new sidecar, including the RV64 assertion; native RV64
execution was not part of this two-host qualification. These are local and Hub
checks; no hosted CI, publication or push is claimed.
