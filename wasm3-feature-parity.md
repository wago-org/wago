# WebAssembly feature implementation and performance parity

The full objective remains open: implement WebAssembly Core 3 and proposals in
phases 4 and 5, and reach feature-by-feature execution performance parity with
the SIMD-capable JairusSW/w2c2 fork, while protecting compile time and memory.

Starting Wago revision: `2286d676facdfa1cabe2d1c61072e505438ca7f2`.
Branch: `jairus/wasm3-feature-parity`.

The [official proposal tracker](https://github.com/WebAssembly/proposals), read
2026-10-12, lists phase 4 threads, compact imports, and wide arithmetic; phase 5
JS Promise Integration and Web Content Security Policy. Core 3 already has
executable implementation and conformance gates in this checkout; their recorded
support alone does not establish performance parity.

## Implementation checkpoints

- Compact imports now have an independent public bit and CLI discovery. Both
  grouped encodings run with Core 2 plus compact imports, and survive artifact
  reload. Their generated native code is byte-identical to ordinary imports.
  The previous multiple-memory admission route is retained for compatibility.
- Wide arithmetic now decodes, validates, classifies, encodes, and compiles all
  four proposal instructions. Native add/subtract use carry/borrow chains;
  multiplication uses hardware signed/unsigned widening multiply. Both result
  words use existing multi-value paths. Artifacts preserve the required feature
  bit. Earlier deferred traps materialize in source order.
- Threads now support instance-owned shared memory as well as imported shared
  memory. Separate backing and instance-control mappings preserve the existing
  atomic addressing path, with ownership retained while importers remain live.
  Memoryless `atomic.fence` and shared `memory.size` are admitted. Constant-zero shared waits now lower
  directly to native loads and comparisons on arm64 and amd64.
  Ordinary scalar and SIMD loads/stores now execute against shared backing.
  ARM64 atomics use CPU-gated LSE with an exclusive-loop fallback and persisted
  artifact requirements. Both backends prove aligned constant atomic bounds
  against declared memory minimums.
  Shared `memory.copy` and `memory.fill` now execute with bounds-before-write
  and overlap-safe copying, including small unrolled paths on both backends.
  Shared growth now publishes a single atomic extent after guarded pages are
  accessible; shared accesses and size queries see growth by other instances.
  Active data segments now initialize shared backing before start execution;
  passive `memory.init` and instance-local `data.drop` are admitted.
  Host function imports and native cross-instance function imports now execute
  with shared memory, including callback growth and blocking imported waits.
  Concurrent foreign callbacks retain their own scope and GC activation roots.
  Defined/imported tables, active/passive elements, and exception tags now
  execute with shared memory, including foreign dispatch and exception catches.
  Mutable-global imports now use the shared execution lease, including host
  access and concurrent importers. Shared memory64 now retains full-width atomic
  and helper addresses, growth results, and data initialization destinations.
  Unshared memory32/64 atomics use canonical backing. Defined and imported
  unshared secondary memories now execute beside a shared primary memory, with
  all numeric atomics and wait/notify selecting the encoded memory index.
  Imported unshared secondary backing retains the common execution lease.
  Defined/imported shared memories now execute at nonzero indices too, with
  live shared extents, selected growth helpers and per-backing wait queues.
  Wider proposal composition and performance parity remain open.

## Current validation

Apple M4 Max, Darwin/arm64, Go 1.27.1:

- Decoder/validator/frontend/ARM64 encoder and backend packages passed with the
  allocation checker enabled.
- Wide arithmetic: 500 input vectors per instruction and pressure variant,
  executed from both original and codec-loaded products. Edge vectors include
  zero, all ones, signed extrema, and the 32-bit boundary. Eight values remain
  live beneath the operands in pressure variants. Feature-disabled compilation
  rejects each instruction. An overflow-before-divide-by-zero probe verifies
  trap order.
- Wasmtime Core 3 regression corpus passed: 215 modules and 690 assertions,
  enforced by `TestWasmtimeCore3Corpus` (not the full official Core 3 suite).
- Native Linux/amd64 on hub@hub (Ryzen 7 7800X3D): the compact-import and wide
  arithmetic execution/codec/pressure/trap tests passed with the allocation
  checker enabled.

Five-sample ordinary compile comparison, against the starting revision:

| Fixture | Baseline median | Current median | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: | ---: |
| Small scalar, default config | 6,103 ns | 6,030 ns | 19,202 unchanged | 74 unchanged |
| Small scalar, explicit Core 3 | 6,007 ns | 6,069 ns | 19,058 unchanged | 73 unchanged |

These microbenchmarks establish no allocation increase and roughly unchanged
latency for these fixtures only. Broader compile/RSS measurements remain needed.
Raw outputs are in ignored `.validation/wasm3-parity/compile-{before,after}.txt`.

## Performance work still required

wasm.fyi's source-built performance script produced 90 long execution contracts,
3 lifecycle probes, and 9 compile probes. Its local configured wasm-bench checkout
lacks the AOT adapter; an isolated copy of the existing hub native adapter was
retrieved to restore measurement capability without modifying that checkout.
The local SIMD fork is `f303ed2`; its translator and an AOT adapter build
successfully. The initial 90-contract comparison is complete: Wago passed 77; w2c2 passed 51,
with 27 unsupported and 12 failed. All 51 successful pairs match artifacts and
met the 50 ms sample-duration target. See [the full initial comparison](wasm3-feature-baseline.md).
Per-feature gap diagnosis, optimized lowerings, missing host profiles, and
both-platform compile/RSS measurements remain open.

The subsequent thread checkpoint passed all five previously rejected defined
shared-memory contracts, bringing cumulative Wago coverage to 82/90. Its
wait/notify contract improved by 48.7% across three launches after the zero-timeout
lowering. Native execution tests pass on both architectures. See
[thread implementation and measurement evidence](wasm3-threads-progress.md).
The eight remaining host-profile rejections are JSPI and JS strings. This
checkpoint does not establish w2c2 parity or complete thread conformance.

The next checkpoint executes ordinary shared scalar/SIMD memory accesses and
adds two wasm.fyi contracts (cumulative Wago coverage 84/92). Correcting the
isolated AOT adapter's thread-profile admission and linking pthread/futex support
produces seven successful exact-artifact thread comparisons. Wago remains
1.555–7.097 times slower on the memory/atomic kernels; the fence-only kernel has
a larger gap because Clang collapses its guest loops to one fence and closed-form
arithmetic. The linked thread report retains those results and configuration.

The LSE and constant-bounds checkpoint reduces the exact atomic-add and CAS
workloads by 72.7% and 67.4% respectively versus the preceding Wago capture.
Current times remain 1.209 and 1.140 times w2c2; other kernels retain the gaps
above. Sequential compile samples show unchanged allocation counts and code
size for the measured fixtures. The thread report records CPU admission,
codec/differential validation, alternating measurements, and raw evidence.

Shared copy/fill adds two independently verified wasm.fyi contracts, bringing
cumulative Wago coverage to 86/94. Native execution and codec checks pass on both
architectures. On the 33-byte arm64 workloads, fill remains 1.919 times w2c2 and
overlapping copy 1.117 times w2c2 after unrolling; the thread report records the
exact capture identities and remaining limits.

The shared-growth checkpoint passes native/codec/concurrent/guarded-race checks
on both architectures and adds size-query and growth-lifecycle contracts.
Cumulative long execution coverage is 87/95. Zero-growth/size queries are about
10.6 times faster than w2c2 on the new workload, while live extent reads add cost
to several earlier shared-access kernels. Nonzero growth's short single-operation
samples remain unqualified for sustained parity. Compile fixture allocations
and generated code size remain unchanged; the thread report records the gaps.

The stable-backing cache checkpoint retains live extent reads and reduces the
controlled shared scalar/SIMD workloads by about 9%, fill by 18%, and overlapping
copy by 22% relative to the growth checkpoint. Native/codec/ABI/pressure/guarded
race checks pass on both architectures. Controlled ordinary compile differences
are below 0.7%, with unchanged allocation counts and code size on the measured
fixtures. Per-feature parity, broader composition, and full conformance remain
open; the thread report retains both timing captures and the controlled evidence.

Shared data initialization now passes native, codec, concurrent-growth, and
guarded race tests on both architectures. Its new independently verified
wasm.fyi contract brings cumulative long execution coverage to 88/96 (combined
checkpoints, not a full same-revision rerun). On the 33-byte passive-copy workload,
direct ARM64 NEON accesses reduce the alternating launch median by 8.7%, from
467,268 ns to 426,593 ns per 262,144 operations. w2c2 measures 181,232 ns, leaving
a 2.354 ratio. Compile fixture samples retain approximately prior allocation
bytes/counts and identical imported-worker code size; broader compile memory
and full feature parity remain open.

The next passive-copy checkpoint preserves allocated scalar/SIMD values instead
of flushing the operand stack. Both architecture pressure/codec/cache/ABI and
guarded race checks pass. Its alternating ARM64 median improves another 30.6%
(429,827 to 298,301 ns), against a newly interleaved 190,241 ns w2c2 median,
leaving a 1.568 ratio. Compile fixture allocation footprint and code size remain
near the prior checkpoint. Full coverage, conformance, and parity remain open.

The host-import checkpoint passes guarded race checks for threads, cross-instance
calls, GC frame roots, and GC host callbacks on ARM64 and AMD64. Alternating
ARM64 execution captures show no material regression across the 11 existing
thread kernels. Alternating compile captures preserve allocations and worker
code size. Preserving owned typed bindings reduces the pure identity kernel to
a 3.216 ratio against the peer; an opaque Go callback improves 4.2 times but
retains a 76.4 ratio against the C callback. Callback parity remains open. The
pure case uses existing ARM64 native leaf arithmetic specialization. A new shared-memory table
contract measures 0.432 times the peer median on ARM64. Native AMD64 testing
exposed and repaired exported-table producer-context selection. The ARM LSE
metadata flag now occupies padding, restoring the 784-byte Compiled footprint.
Full feature coverage, official conformance, and overall parity remain open.

Shared memory64 now executes atomic accesses, size/grow and wait/notify on both
native architectures with full-width bounds and helper addresses. Scalar/SIMD
and bulk access checks, codec reloads, blocking waits, growth and cancellation
pass guarded race tests. Two new wasm.fyi contracts verify against independent
oracles in Wago and V8; the current w2c2 translator rejects memory64 limits,
so peer parity for those contracts is unmeasured. Alternating compile fixture
medians stay within 1.3%, with unchanged scalar allocations and worker code size.
Constant-length shared-memory64 data initialization now preserves full-width
destinations, with imported/defined segment and growth tests. Unshared memory64
atomics also use canonical backing and pass guarded race checks on both native
architectures. Multiple memories with threads, wider proposal combinations,
full conformance and overall feature/performance parity remain open.

Unshared memory32 dynamic and constant atomic accesses now select canonical
backing, repairing native faults exposed by targeted tests and the wasm.fyi
RMW kernel. Fresh qualified ARM64 captures retain 1.107 and 1.175 peer ratios
for unshared load/store and RMW add. A matched 218-module Core 3 compile capture
shows equivalent allocated bytes/counts, native code size and process RSS, with
a 1.35% lower compile-interval median; this is a local corpus check, not all
feature combinations or memory-instantiation costs.

The complete pinned official Core 3 suite now passes on both native architectures:
258 files, 2,226 modules, 58,238 assertions, zero failures/skips/gaps. Both runs
use checksummed WABT 1.0.41 assets and the reference interpreter built from
`9d36019973201a19f9c9ebb0f10828b2fe2374aa`; 28 files use the interpreter text
fallback. This establishes the current Core 3 suite result, independently of
performance parity and the remaining phase 4/5 work.

The pinned WebAssembly/threads proposal suite also passes on both native
architectures under guarded race execution: all 13 scripts, 51 modules and
312 assertions. The harness now preserves shared-module declarations and runs
workers concurrently, including nested workers and blocking wait/notify. Three
repeated suite/regression runs pass on both architectures. Multiple memories
with threads, wider proposal combinations, phase 5 embedding behavior and
feature-by-feature performance parity remain open.

The SSH control connection persists for 24 hours; detached tmux session
`wago-wasm3-parity` survives disconnection. The dedicated remote workspace is
`~/.cache/wago-wasm3-parity/wago`.

## Indexed threads checkpoint (2026-10-12)

`acecc24d` admits defined unshared secondary memories; `141e4622` also admits
imported unshared secondary backing while preserving its common execution lease.
`ed977b84` lowers indexed atomic instructions against the selected backing and
resolves memory64 memarg offsets from that memory's address type. Targeted and
broader guarded race tests pass on ARM64 and AMD64. The selected-memory matrix
covers all 63 numeric atomic opcodes, wait/notify, both call ABIs, constant and
dynamic addresses, imported backing, mixed memory32/64 address widths, and
artifact reloads. Additional shared memories remain gated.

wasm.fyi `926655b` adds private secondary ordinary-access/copy contracts;
`8d65088` adds secondary atomic load/store and RMW contracts. All four execute
verified arithmetic oracles in Wago. The current peer rejects or misdecodes these
multi-memory instructions, so no same-feature performance ratio is available.
See `wasm3-threads-progress.md` for revision-specific timings and compile costs.

The requested native Go analogues for JS Promise Integration and Web CSP were
cancelled before implementation. Browser-specific behavior remains outside this
native embedding; no Go async host-call or compile-policy API was added.

## Shared secondary checkpoint (2026-10-12)

`2aa65ce0` removes the remaining shared-memory-index admission gate. Selected
shared memories now publish growth through the synchronized helper and scalar,
SIMD and bulk bounds use their authoritative live extent. Memory size and zero
page growth select the correct backing. Instances with a private primary and a
shared secondary receive threaded invocation/native-state leases too, while
imported unshared resources retain the common execution lease.

Shared active-data initialization locks one segment backing at a time, avoiding
self-deadlock when memory declarations alias the same Memory. Tests cover this
aliasing, one wait queue across aliases, distinct queues for separate backing,
blocking waits, cancellation and growth visibility within a live native entry.
Guarded race checks pass on ARM64 and AMD64; both pinned official threads suites
pass again. wasm.fyi `1ce2a5c` adds shared secondary atomic, wait/size and copy
contracts, verified through Wago and an independent V8 oracle. The peer still
rejects indexed atomic memargs, so no same-feature parity ratio exists.

## Refreshed performance contract (2026-10-12)

wasm.fyi `d64d55c` packages the fork's transitive SIMD headers and its futex
runtime compiler inputs. Linking all three runtime sources makes the existing
wait/notify peer contract executable again. Eight current threaded contracts now
have interleaved, three-process, verified 50-ms steady captures for Wago and both
checked/unchecked w2c2 profiles. Earlier default peer runs disabled bounds and
alignment checks; they remain explicitly labelled unchecked targets.

Wago is faster than the checked peer on the six refreshed memory/numeric atomic
kernels. It remains 2.586x slower on wait mismatch/notify and about 1,054x slower
on the fence-only arithmetic kernel. The faster unchecked peer also retains
broader gaps. Clang coalesces repeated fences and computes the local arithmetic
loop in closed form; it keeps a real hardware barrier. Wago notify still enters
Go even when no waiter is queued. These are the next concrete optimization
paths, with broader feature parity still open. See `wasm3-threads-progress.md`
and the tracked peer runtime manifest for exact configurations and evidence.

## Empty notify checkpoint (2026-10-12)

`48b1eed3` completes the ARM64/AMD64 empty-notify shortcut introduced in
`c7f4f790` and corrected on AMD64 in `0a41c011`. Bounds and alignment checks
remain before returning zero. A shared backing publishes registering and queued
waits before the value comparison; any pending wait sends notify through the
locking Go helper. Codec products carry a runtime requirement that older
loaders reject. No basedata or Compiled structure grows.

On the same Apple M4 Max workload, three interleaved processes per profile give
median 0.639 ms versus 14.071 ms before, 5.364 ms for checked w2c2 and 1.953 ms
for unchecked w2c2, per 262,144 operations. Every timed sample verifies its
result. This closes the measured single-actor wait-mismatch/empty-notify gap;
it does not establish concurrent throughput parity. Both native architectures
pass guarded race checks and all 51 modules/312 assertions in the pinned
official threads suite. A regression test blocks registration at the queue
registry and fails on the previous publication order.

Alternating compile runs show Core 3 corpus time -1.59%, unchanged generated
code and negligible allocation differences. The 27 threads workloads show
time +1.76%, allocation bytes +0.12%, generated code +0.91%, and peak RSS
+0.51%. Ordinary compile fixtures remain within about 1% of baseline.
The tracked `docs/benchmarks/wasm3-2026-10-12/notify-empty-results.json` records
versions, process medians, compile measurements and raw-evidence hashes.
Fence arithmetic and broader feature performance parity remain open.

## Integer induction sums and fence checkpoint (2026-10-12)

`99eeef13` introduces and `60c27eba` corrects the controllable `integer-sum-loop` optimization on ARM64 and
AMD64. It recognizes a proven zero-based unsigned unit-stride local sum and
computes the complete wrapping i32/i64 result in closed form. It preserves the
do-while final counter (`max(1,n)`) and one barrier per reduced loop entry when
fences occur. Memory accesses, calls, traps, other escapes, enclosing loops,
and interrupt-polling functions are excluded. An untouched declared-local proof scans original assignments in a bounded
source prefix, including assignments consumed by emitter fusions. No runtime
structure grows.

The measured fence workload drops from 311,924 ns to 153 ns per invocation on
Apple M4 Max, versus 281 ns for checked w2c2 and 279 ns for unchecked w2c2.
The copy with only its fence removed has the same improvement, establishing
that arithmetic loop elimination closes this measured gap. This is one
single-actor contract, not a concurrent ordering or all-feature parity claim.

Both native architectures pass guarded race tests, i32/i64 unsigned-extreme
execution against an arbitrary-precision oracle, policy overrides, both calling
conventions, codec execution and saved stack values. The pinned official Core 3
suite passes 2,226 modules/58,238 assertions on each architecture; official
threads passes 51 modules/312 assertions on each. The broad ARM64 statistics
suite retains the same 44 failure names as the b54c7110 baseline, with none new.

A larger six-process alternating Core 3 capture gives time +0.12%, unchanged
allocation traffic/code size, retired instructions +0.17% and peak RSS -0.52%.
The initial three-process capture had a wider timing spread and +4.11% median
latency, with essentially unchanged cycles; both captures are retained. Threads
time is +0.42%, allocation bytes effectively unchanged and code size +0.09%.
`docs/benchmarks/wasm3-2026-10-12/integer-sum-results.json` retains exact
versions, per-process medians and evidence hashes. Broader proposal composition,
remaining unchecked-peer gaps and feature-by-feature parity remain open.

## Bounded addresses against declared minimums (2026-10-12)

`65f3c62e` adds `memory-min-bounds` on ARM64 and AMD64. A bounded unsigned
expression analysis proves masked/shifted addresses and entire accesses fit the
selected memory's immutable declared minimum. It preserves source operations,
memory loads/stores and atomic alignment checks, and uses no new runtime or
per-local fields. Unknown/wrapping ranges saturate conservatively; zero minima
and accesses crossing the minimum retain live-extent checks.

Native guarded race tests pass for memory32/64, shared/unshared primary and
secondary memories, both calling conventions, original/codec products, traps,
policy overrides and growth from zero minimum. Both pinned conformance suites
pass on both architectures again. Broad ARM64 statistics retains its 44 parent
failure names; constant-store assertions now account for valid check removal.

The eight threaded workloads have a fresh, interleaved three-process comparison
against checked and unchecked w2c2. Atomic load/store is now 149,327 ns versus
162,583 ns for the unchecked peer. Ordinary load/store improves 30.77% and bulk
fill improves 11.79%, but still trail unchecked w2c2 by 1.45x and 1.52x. SIMD
remains 2.30x slower, despite receiving both minimum proofs. Peer assembly
retains ordinary loads and narrows SIMD load-plus-lane extraction to a scalar
load; that narrowing is the next concrete path, preserving full original bounds.

Alternating compile captures show Core 3 latency -0.43%, allocation bytes
-0.14%, code bytes -0.80% and peak RSS +0.59%. Threads latency is +0.03%,
allocation bytes -1.51%, code bytes -7.74% and peak RSS -0.64%. These are small
descriptive local cohorts. The tracked `memory-min-results.json` receipt retains
exact profiles, medians and hashes. Broader feature-by-feature parity remains open.

### Vector load lane extraction

`9914eafb` adds immediate scalar lane extraction from vector loads on both
native architectures. It keeps the full original 16-byte bounds check, including
signal-based guard configurations, and retains an actual memory read. Every lane,
integer signedness, float edge bits, unaligned floats, memory32/64, shared and
unshared primary/secondary memories, both ABIs and codec products are covered.
Native guarded race tests and both pinned conformance suites pass on both
architectures. ARM64 statistics retains the same 44 known parent failures.

A fresh wasm.fyi three-process comparison shows SIMD load/store falling from
240,398 to 148,599 ns per invocation (-38.19%), still 1.405x slower than unchecked
w2c2. Other unchecked-peer gaps persist: ordinary 1.373x, bulk 1.507x, RMW
1.163x and CAS 1.068x. Full feature parity remains open.

Alternating production compile measurements show Core 3 latency +0.27%,
threads latency -0.11%, essentially unchanged allocations, and peak RSS
-0.35%/+2.08%. The tracked `simd-load-extract-results.json` report preserves
revision/source fingerprints, process medians, compile costs and evidence hashes.

### Straight-line block operand residency

`96cbe746` keeps operands resident through small blocks proven to contain no
branches, nested control or calls. The bounded scan leaves the control frame
and stack-height contract intact and adds no fields or packed flags. Older
local/memory snapshots, trap ordering and enclosing-if operands are covered
with both ABIs, codec products and guard configurations. Both architectures
pass native guarded race tests, pinned Core 3 (2,226 modules/58,238 assertions)
and official threads (51 modules/312 assertions); ARM64 retains the same 44
known statistics failures. A malformed old block fixture now declares its
actual i32 result, preserving all canonical-address checks.

Fresh wasm.fyi measurements show ordinary shared load/store improving 27.08%,
from 139,226 to 101,525 ns, versus unchecked w2c2 at 101,923 ns. This contract
reaches measured parity (0.996x); SIMD still trails by 1.347x, bulk by 1.486x,
RMW by 1.160x and CAS by 1.056x. The SIMD peer shows substantial inter-process
variation, retained in the report. These results do not prove feature-wide parity.

Core 3 compile latency is +2.75% in the initial three-process cohort and +1.53%
in a separate six-process repeat; the latter shows essentially unchanged
allocations and peak RSS +0.99%. Threads latency is +0.52%, allocation bytes
-0.09%, code bytes -0.19% and peak RSS +0.51%. The tracked
`straight-block-results.json` preserves both cohorts and emitted-loop hashes.
General address reuse is the next SIMD candidate; broader compositions and
feature-by-feature parity remain open.
