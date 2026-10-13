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

### Adjacent SIMD address reuse

`e9185d74` reuses a vector store's address for an adjacent real load when a
bounded proof finds the same nontrapping local/constant expression, selected
memory and offset. It preserves the original full-span store check and actual
read, adds no persistent fields, and falls back for guards/custom instructions.
Every extraction lane, float edge bits, changed/mismatched addresses, local
snapshots, memory32/64, shared/unshared primary/secondary memories, both ABIs,
codec products and profiling are covered. Both native architectures pass
native guarded race tests, pinned Core 3 and threads; ARM64 retains its same
44 known statistics failures.

The fresh wasm.fyi SIMD contract improves 31.46%, from 147,903 to 101,376 ns,
versus unchecked w2c2 at 108,308 ns (0.936x). Ordinary also retains measured
parity on its contract (0.988x). Bulk/RMW/CAS still trail by 1.462x/1.159x/
1.068x, and broader feature-wide and cross-platform parity remain unproven.

Core 3 and threads compile latency are each approximately +1.80% in three
alternating processes; allocations are effectively unchanged, and peak RSS
changes -0.11%/+0.25%. The tracked `simd-address-results.json` preserves exact
source fingerprints, process medians and emitted-loop hashes. Bulk-fill
spills, bounds and scalar chunk lowering are the next measured gap to address.

### Native fill block residency

`e43a4700` admits native `memory.fill` into the existing bounded straight-line
block proof, preserving its lowering and control frame while removing entry/
exit spills. Constant/dynamic/zero lengths, dispatch boundaries, byte
truncation, selected memory32/64, shared/unshared memories, both ABIs, codec
products, older reads and trap ordering pass on both architectures. Pinned
Core 3 and official threads pass again; ARM64 retains its 44 known statistics
failures.

The fresh bulk-fill contract improves 6.09%, from 281,294 to 264,174 ns,
but still trails unchecked w2c2 by 1.376x. Other small changes and peer process
variation do not establish additional parity wins. Core 3 compile latency is
-0.06%, threads +0.19%, with unchanged code/allocations and peak RSS
-0.35%/-0.38%. The tracked `bulk-block-results.json` retains exact profiles,
process medians, costs and emitted-loop hashes. Bulk bounds and scalar chunk
lowering, broader compositions and full performance parity remain open.

### Constant-fill minimum bounds and secondary grow-only context

`44998d24` fixes a native crash when a module only grows shared secondary memory:
helper requirements now inspect every grown memory, rather than only memory 0.
Grow-only memory32/64 modules execute both ABIs and survive codec reload;
unshared growth alone does not request the helper context.

`17b57ff4` proves existing constant-fill spans against the selected declared
minimum, retaining address evaluation and writes while dropping a proven live
bounds check. Zero/wrapping/unknown/cross-minimum accesses fall back. Zero-minimum
and post-growth cases are covered. ARM64 guarded race tests and both pinned
conformance suites pass; full statistics retains its same 44 known failures.
Linux/AMD64 test binaries cross-compile, but native validation remains pending
because hub SSH connections time out.

The fresh bulk-fill contract improves 8.98%, from 256,003 to 233,014 ns,
still 1.242x slower than unchecked w2c2. Core 3 compile latency is -0.24%,
threads -2.63%, with small allocation/code reductions and peak RSS -0.46%/
+0.13%. `bulk-min-results.json` preserves exact profiles, costs, native loops
and the remote-validation limitation. Vector fill lowering, native AMD64
revalidation and full feature-by-feature parity remain open.

### Constant native vector fills

`0c1ae29d` broadcasts the low byte into native vectors for 16–64 byte constant
fills. Overlapping final stores stay inside the requested span, and the full
bounds check precedes every write. Earlier reads/traps and the original value
local are preserved. The independent `bulk-fill-vector` selection retains the
scalar path for comparison. This uses mandatory native vector support without
requiring guest SIMD instructions.

The fresh wasm.fyi bulk-fill contract improves 31.43%, from 235,261 to 161,310 ns,
versus unchecked SIMD-capable w2c2 at 194,986 ns (0.827x). This proves local
parity for that contract; broad compositions and native AMD64 parity remain
unproven. Core 3 compile latency changes -4.49%, threads +1.13%; allocations
are unchanged/-0.21%, code unchanged/-0.29%, and peak RSS +0.81%/+1.79%.
Small cohort differences are descriptive. `bulk-vector-results.json` preserves
source fingerprints, all process medians, costs and emitted-loop evidence.
ARM64 pinned Core 3/threads pass and the full backend retains its exact 44
baseline statistics failures. Native AMD64 remains pending hub SSH access.
RMW/CAS block reconciliation and full feature-by-feature parity remain open.

### Native atomic block residency

`4680ca19` admits native atomic load/store/RMW/CAS/fence operations into the
existing bounded straight-line block proof. Wait/notify helper paths remain
excluded. Native lowering keeps memory barriers, earlier reads and trap order;
memory32/64, primary/secondary memories, both ABIs and artifact reload pass.
ARM64 guarded race tests and pinned Core 3/threads pass; the full backend retains
its exact 44 baseline statistics failures. AMD64 binaries cross-compile, while
hub SSH remains unavailable for native validation.

Fresh wasm.fyi contracts improve atomic load/store 8.45%, RMW 13.50%, and CAS
5.60%. RMW is 235,514 ns versus unchecked w2c2 237,939 ns (0.990x); CAS is
292,612 versus 291,637 ns (1.003x). One unchecked CAS process has CV 0.139;
these observations do not prove broader parity. The current RMW loop retains
its accumulator and shared base, with real STLR/LDADDAL operations and no loop
stack spills. Core 3/threads compile latency changes +0.97%/+0.22%, allocations
are unchanged, code unchanged/-0.79%, and peak RSS +1.28%/+0.76%.
`atomic-block-results.json` retains matched profiles and evidence. Refreshing
the full 90-contract feature corpus and native AMD64 validation remain required.

### Full execution corpus refresh

A fresh wasm.fyi build produces 111 execution contracts rather than the old 90.
At adapter revision `4680ca19`, Wago passes 103 in each of three processes; eight
JavaScript host profiles remain unsupported. The SIMD fork passes 67 in each
process, with 22 unsupported and 22 failed contracts. All 67 successful pairs
match bytecode and meet the 50 ms sample-duration target. See
[the current comparison](wasm3-feature-refresh.md) and `feature-refresh-results.json`.
Large direct-call, numeric, global-recurrence and conversion gaps remain.
Wide arithmetic/compact imports were absent from this 111-contract capture,
and native AMD64 validation remains pending. Full parity is not achieved.

### Modular affine integer reductions

`b9696c08` extends the zero-counter, unsigned unit-stride sum proof to bounded
pure affine expressions, including reordered/repeated terms and constant-only
terms. Modulo-width arithmetic preserves wrapping coefficients and offsets;
nonlinear, mixed-width, effectful and changed-counter expressions fall back.
The do-while constant contribution uses max(1,bound), including bound zero.
Original-source initialization, invariant bound, alias exclusions and fence
retention remain. ARM64 conformance/guarded race tests pass with both ABIs,
codec, arbitrary coefficients and extreme unsigned bounds. AMD64 cross-compiles;
native execution remains pending hub access. Statistics retains its same 44
baseline failures.

The fresh multiply-add contract falls 84,381 -> 102.60 ns (822x), versus unchecked
w2c2 292.44 ns (0.351x). Its scalar SIMD baseline falls 79,253 -> 101.96 ns.
Direct calls remain 198,017 ns versus 291.70 ns (679x), so call-containing sums
remain an important gap. Actual adapter-native images confirm a closed-form
kernel with the outer 64-call repetition still present.

Six alternating compile processes per revision, across two identical cohorts,
show Core 3 +0.85%, threads +3.22%, with unchanged allocation/code totals and
peak RSS +0.35%/+1.02%. The initial threads cohort was +5.60% and variable;
both raw cohorts remain in the evidence. These descriptive differences merit
keeping recognition work bounded and avoiding unnecessary work on failed
candidates. `affine-sum-results.json` retains exact profiles, costs, before/
after adapter images and validation hashes. Full feature parity remains open.

### Pure-helper integer reductions

`d74e57cd` composes bounded unary local integer leaf summaries into the existing
affine sum proof. Imported/effectful/recursive/mixed-width helpers, declared
locals and more than four helper calls reject. Disabling inlining retains
calls. A common single-counter fast path avoids expression-stack work; effectful
starts reject early. Source initialization, unsigned bounds, aliases, do-while
zero bounds and retained fences remain. Stateful helper invocation counts,
both widths/ABIs, codec, extreme bounds, guarded race and pinned ARM64
conformance pass; full statistics retains its exact 44 failures. AMD64 binaries
cross-compile, but hub still times out for native execution.

Direct-call improves 196,243 -> 112.65 ns (1,742x), versus unchecked w2c2
288.94 ns (0.390x). Other small cohort differences make no new parity claims.
Actual before/current images come from the pinned adapters and workload config.
Six alternating compile processes per revision across two retained cohorts give
Core 3 +0.52%, threads +0.87%, with unchanged allocation/code totals; peak RSS
+0.58%/-0.76%. The first Core 3 cohort was +4.85%, prompting an identical second
capture. `pure-call-sum-results.json` retains both cohorts and exact evidence.
Global-state loops, conversions, sign/narrow operations and broader feature
parity remain open.

### Runtime immutable-global sums

`bdb5b094` reduces a bare immutable integer global getter to one invocation-time
read multiplied by the unsigned do-while count. It handles defined and imported
values without baking initializer bits into code. Both widths/ABIs, disabled
inlining, canonical instance values after metadata edits/codec, negative values
and extreme unsigned bounds pass. Mutable globals and fenced global-read loops
remain on the original lowering. ARM64 pinned conformance/guarded race passes;
full statistics retains its exact 44 failures. AMD64 test binaries cross-compile;
native execution remains pending hub connectivity.

The immutable-global contract improves 72,522 -> 130.58 ns (555x), versus
unchecked w2c2 291.39 ns (0.448x). Mutable exported-read remains 66,396 versus
290.90 ns (228x), requiring further work. Actual adapter images retain the
runtime global read. Three alternating compile processes per revision give
Core 3 +0.40%, threads -2.34%, with unchanged allocation/code totals and peak
RSS +1.05%/+0.25%. `global-sum-results.json` retains exact profiles, images,
validation evidence and the mutable-getter diagnostic. That diagnostic already
loads the mutable value once into a pinned register; reusing the existing pin
proof is the next candidate. Full feature parity remains open.

### Existing pinned mutable-global sums

`58c783ce` extends bare getter reduction to mutable globals only when the backend
already has a stable native value pin. The original call-free loop reads that
register, so no additional host/concurrency invariant is introduced. Missing
pins and fenced getters retain their loops. Same-function reads after reduction
retain the original global value; both widths/ABI selections, imports, canonical
initialization/codec values and previous sum cases pass. Pinned ARM64 Core 3/
threads and guarded race pass; full statistics retains the exact 44 failures.
AMD64 test binaries cross-compile; native execution remains pending hub access.

Mutable exported-read improves 65,544 -> 128.19 ns (511x), versus unchecked w2c2
285.48 ns (0.449x). Three alternating compile processes per revision give Core 3
+0.17%, threads +2.31%, unchanged allocation/code totals and peak RSS +0.35%/
-0.88%. `pinned-global-sum-results.json` retains exact profiles, actual adapter
images and validation hashes. State-changing global recurrences, conversions,
sign operations, phase 4 benchmark coverage and full parity remain open.

### Source-built wide-arithmetic contracts

wasm.fyi `9a11c7f` adds four original execution kernels for `i64.add128`,
`i64.sub128`, `i64.mul_wide_s`, and `i64.mul_wide_u`, with varying full-width
operands and observable checksums of both result words. A separate base-65536
limb oracle checks every one of the 4096 inputs per instruction, including
carry/borrow and signed multiplication. All 12 small contracts (sizes 1, 64,
4096) pass through the existing Wago adapter. No runtime or adapter production
change is required: the selected default feature set already admits them.

On Apple M4 Max ARM64, three independent processes each pass all four
4096x64 performance contracts with five verified samples of at least 50 ms.
Process-median times are 221,503 ns for add, 222,680 ns for subtract,
173,337 ns for signed multiply, and 174,586 ns for unsigned multiply.
The pinned SIMD-capable w2c2 fork rejects opcodes 0xfc13 through 0xfc16;
Node/V8 also rejects them. There is no successful peer pair and no numeric
parity claim. `wide-arithmetic-results.json` retains recipes, hashes, all
36 performance/rejection rows and all 12 small-contract checks. This
corpus-only change does not alter Wago compile behavior. Compact imports
coverage and the broader feature performance gaps remain open.

### Finite-sign proof for unsigned saturating sums

`a5d84624` extends the bounded original-source affine expression proof with
finite floating-point signs. Unsigned i32/i64 conversion produces a finite
nonnegative f32/f64; negation reverses the proven sign; unsigned saturation of
a proven nonpositive value yields zero, including negative zero. The remaining
integer term can use the existing modular sum reduction. Signed conversions,
unknown signs, mismatched widths, unfinished float values, effects and traps
fall back. No persistent compiler state or ABI fields are added.

The current wasm.fyi negative-clamp contract falls from 132,006 ns to 84.79 ns,
versus unchecked w2c2 279.40 ns (0.303x), across three independent processes per
profile, five verified samples each and minimum 50 ms batches. Actual adapter
native images shrink from 432 to 384 bytes. The signed-zero contract remains
147,448 ns versus 276.45 ns (533x); its gap is explicitly still open.

Shared proof tests, public execution across both integer/float widths, both
ABIs, optimization off/on, codec reload and extreme unsigned bounds pass.
Guarded race tests and the full pinned Core 3/threads conformance gates pass.
AMD64 public tests cross-compile; native execution remains pending. Full backend
statistics were not rerun for this change. Core 3 compile latency changes
-0.48%, threads +2.95%; allocation totals are effectively unchanged (Core 3
varies by two pool allocations over 21,800 compiles), with identical code totals.
Peak RSS changes +0.35%/-0.50%. Small scalar/Core 3 scalar/imported-worker
compile medians change +0.63%/+1.06%/+0.88%, with unchanged allocation counts.
`float-sign-results.json` retains raw performance, alternating compile captures,
exact source fingerprints, image hashes and conformance output. These results
establish parity for this contract; the full feature parity objective remains open.

### Exact copysign bits in integer reductions

`cd47c90f` extends the bounded original-source affine proof to exact float
constants, known sign bits, copysign, integer reinterpretation and constant
`i32.eq`. Unsigned integer conversion proves a clear sign bit; negation reverses
it, including zero. Copysign keeps all magnitude bits, including subnormals,
infinities and NaN payloads. Only constant magnitudes with a known matching-width
sign qualify; unknown signs/magnitudes, mismatched widths, variable comparisons,
traps and effects fall back. Stack state stays local to the bounded inspector;
no persistent compiler or ABI fields are added.

The fresh signed-zero contract falls 148,490 -> 99.53 ns, versus unchecked w2c2
288.18 ns (0.345x), over three independent processes per profile and five verified
samples of at least 50 ms. The negative-saturation and integer multiply-add
controls retain parity at 84.55/100.18 ns versus 287.85/293.56 ns. Actual signed-zero
adapter images shrink 432 -> 416 bytes. These are contract-specific results.

Exact-bit tests cover f32/f64 zeros, signs, subnormals, infinities and NaN
payloads; public execution covers both ABIs, optimization off/on, codec reload
and extreme unsigned bounds. Shared tests and guarded race tests pass. Pinned
Core 3 passes 2,226 modules/58,238 assertions with zero failures/skips/gaps;
threads passes too. AMD64 public tests cross-compile, but hub SSH again times out.
Full backend statistics were not rerun. Core 3/threads compile latency changes
+1.72%/-0.87%, allocation totals are effectively unchanged (seven pool allocations
vary over 21,800 Core 3 compiles), and code totals are identical. Peak RSS changes
+1.15%/+1.13%. `copysign-results.json` retains all 27 performance rows, alternating
compile captures, source fingerprints, image hashes and complete conformance
output. Full feature and cross-platform performance parity remain open.

### Uniform integer SIMD lane reductions

`b4ef4c22` extends the bounded original-source affine proof to i32x4/i64x2
splats, lane-wise add/subtract and affine multiplication, followed by extraction
of any valid lane. Every lane has the same modular affine value; extraction
recovers that scalar value for the existing integer sum reduction. Nonlinear
products, mixed lane widths, vector/scalar mixing, invalid lanes and unextracted
vector results fall back. The proof adds no persistent compiler or ABI fields.

The fresh SIMD add/multiply contract falls 108,225 -> 100.57 ns, versus unchecked
w2c2 306.63 ns (0.328x), across three independent processes per profile and five
verified samples of at least 50 ms. Scalar SIMD arithmetic, signed-zero and
negative-saturation controls remain below their peer medians. One current
negative-saturation process has CV 0.125; its raw qualified samples are retained.
Actual target adapter native image size changes 400 -> 416 bytes: the reduction
trades a small code increase for removing repeated vector arithmetic.

Shared tests, public execution across both lane widths and every lane, wrapping
coefficients, both ABIs, optimization off/on, codec reload and extreme bounds
pass. Guarded race tests and pinned Core 3/threads conformance pass. AMD64 public
tests cross-compile; native execution remains pending hub access. Full backend
statistics were not rerun. Core 3/threads compile latency changes +0.10%/+2.80%,
allocation totals are effectively unchanged (five pool allocations vary over
21,800 Core 3 compiles), and corpus code totals are identical. Peak RSS changes
-1.72%/-0.63%. `simd-affine-results.json` retains all 36 performance rows,
alternating compile captures, source fingerprints, native image hashes and full
conformance output. Broader SIMD and full feature performance parity remain open.

### Full execution corpus refresh after arithmetic proofs

A fresh wasm.fyi `9a11c7f` build yields 115 execution contracts, plus four
lifecycle and nine compilation probes kept separate. At Wago `b4ef4c22`, all
107 supported execution contracts pass in each of three processes; eight
JavaScript host profiles remain unsupported. The pinned SIMD w2c2 fork passes
67, reports 26 unsupported and 22 failed contracts in each process. All 67
successful pairs match bytecode and meet the 50 ms sample minimum.

The full capture retains the gains for affine arithmetic, pure helpers, invariant
globals, negative saturation, exact copysign bits and uniform SIMD lanes: eight
formerly large gaps now have current Wago medians below the peer. Twenty-nine
paired contracts still exceed a 1.1x triage ratio, led by i64 global splitting
(350x), global swapping (335x), mutable accumulation (237x) and opaque Go
callbacks (75x). Timing variation is retained, including a maximum process CV
of 0.362 on bulk fill; small differences require targeted confirmation.
`feature-refresh-oct13-results.json` retains all 690 raw rows, hashes, recipes
and rejected outcomes; [the refreshed comparison](wasm3-feature-refresh.md)
supersedes the earlier table. Wide arithmetic passes but has no supported peer
pair; compact imports remain absent. Full feature and native AMD64 parity remain
open. No compiler changes or new compile-regression claims are made by this
report-only refresh; the latest production cost evidence remains in
`simd-affine-results.json`.

### Source-built compact imports reveal and repair decoder errors

wasm.fyi `78f4c4b` adds both proposal encodings and an ordinary-import control,
with eight repeated identity imports and calls to the first and last expanded
function indices. Wire tests check the actual 0x7f/0x7e group bytes; independent
arithmetic oracles cover sizes 1, 64 and 4096. All artifacts reproduce byte-for-byte
with pinned wasm-tools 1.260.0.

The initial Wago run rejected both grouped forms. Earlier hand-built tests had
used the expanded import count instead of the outer encoded-group count, and
shared only an external kind rather than the complete external type. `7228b4b1`
corrects both decoder rules, bounds expansion by input bytes and metadata budget,
and updates those tests. Mixed/empty groups, shared memory types, public feature
admission, native code equivalence and codec reload pass. Older native compact
checks used the incorrect hand-built encoding and do not validate this fix.

Pinned WABT 1.0.41 also produces a malformed group count when --enable-all
compacts ordinary imports; wasm-tools independently rejects that output. Core 3
conformance now explicitly enables only Core 3 producer features through
`spectest.Release3WABTFlags`, excluding experimental compact encodings. Core 3
again passes 2,226 modules/58,238 assertions with zero failures/skips/gaps, and
threads passes. Full decoder tests and the guarded-build race test (explicit
bounds for serializable artifacts) pass. The final AMD64 public test binary
cross-compiles; native execution remains pending hub access.

All nine small contracts and all three performance contracts pass in Wago.
The three actual adapter native images are byte-identical, each 940 bytes.
Three-process execution medians are 20,845,056 ns for module groups, 21,083,778 ns
for type groups and 21,076,320 ns for ordinary imports. V8 and w2c2 reject both
grouped encodings. The ordinary control is 298,882 ns in w2c2 (70.52x Wago/peer),
exposing a separate multiple-import identity host-call gap; it is not a compact
peer comparison. Full feature parity remains open.

Alternating Core 3/threads compile captures change latency +2.99%/+2.32%, with
effectively unchanged allocations and identical code totals. The report retains
all 27 performance/rejection rows, all nine correctness checks, original failure
evidence, image hashes, source recipes, compile captures and full conformance
output in `compact-import-results.json`. This closes the missing compact-import
corpus coverage while leaving its peer comparison and host-call gap unresolved.

### ARM64 native leaves shared by aliased host imports

`3c8b9b52` removes the compile-only single-import restriction on native scalar
leaf candidates. Instance admission permits multiple cells only when every
binding has the same typed i32 shape and callback code, no plugin gates or GC
domains, and the independent runtime body proof admits straight-line arithmetic
with no closure, receiver, memory or Go state reads. One existing instance-owned
native leaf is shared across those cells. Different bodies, captured state and
effectful callbacks retain Go dispatch. No persistent compiler/ABI fields are
added. Single-import admission avoids reflection; bounded native entry still
requires the stricter single-import proof, and native aliases retain syscall
scheduling rather than treating callbacks as Go safe points.

The eight-import ordinary control improves 20,454,833 -> 1,063,738 ns (19.23x),
versus unchecked w2c2 296,750 ns (3.585x). Actual compact module/type groups
improve to 1,076,354/1,071,151 ns, but the peer still rejects their wire encodings.
The single-import thread identity control remains 610,460 -> 609,475 ns. The
initial opaque-control capture changes +3.78%; an alternating three-process
confirmation changes +0.64%, retaining both cohorts rather than claiming a
regression or gain. Ordinary-control compiled native images are byte-identical
at 1,184 bytes: the gain comes from per-instance dispatch binding.

Regression tests cover signed/wrapping values, optimization rollback, artifact
reload fallback, mixed callbacks, identical-code closures with different captured
values, retained owned references, and GC progress/cancellation with one Go P.
Guarded race and pinned Core 3/threads conformance pass. AMD64 public tests
cross-compile; this lowering remains ARM64-only, and recent shared changes still
need native hub validation. Full backend statistics were not rerun.

Core 3 compile latency changes +1.32%, with effectively unchanged allocations
and identical code totals. Threads changes +4.78% in the initial three-process
cohort and -1.27% in a targeted alternating follow-up; the combined six-process
median changes +2.87%, with unchanged allocations/code. `leaf-alias-results.json`
retains all 45 primary performance rows, six opaque-confirmation rows, both
compile cohorts, native image hashes and full conformance output. The remaining
3.58x alias-call gap, opaque callback gap and full feature parity remain open.

### Defined global assignment reductions and Remote AMD64 validation

`32c26ea3` extends the bounded integer reduction proof to same-iteration
assignments and forwarding among at most four defined mutable integer globals.
Every global read must follow a proven assignment in the current iteration;
imported cells, unknown prior values, trapping expressions, memory accesses and
fenced writes reject the proof. Final values use canonical global setters, so
borrowed prefix values and dirty pinned globals keep their normal semantics.
The existing untouched-counter and interruptibility restrictions remain in force.
No persistent ABI or function fields are added.

On Apple M4 Max, matched core/mutable-global swap contracts improve
98,662 -> 171 ns and 101,822 -> 166 ns (578x/613x), versus unchecked w2c2
292/290 ns. All 18 process rows have qualified durations and verified exact
outputs. The inspected core contract native image changes 464 -> 496 bytes;
the reduction preserves final assignments while removing the repeated loop.
Alternating three-process compile captures change Core 3 latency -0.73% and
threads +0.34%, with effectively unchanged allocations and identical corpus code
totals. Small scalar compilation changes -0.33%/-1.35%; the imported-worker
control changes +1.52%, with effectively unchanged allocation totals.

Hub is unavailable for maintenance. Native Linux AMD64 validation now runs on
`root@remote`, in `/root/.cache/wago-wasm3-parity`, using the persistent
`wago-wasm3-parity` tmux session and SSH connection reuse configured for 24 hours.
The complete `TestInteger.*Sum` set passes there and on ARM64, including both
widths/ABIs, optimization rollback, artifact reload, preserved stack prefixes,
final global publication, and unsigned extrema. ARM64 guard-page race tests and
pinned Core 3/threads conformance pass. AMD64 performance is not measured, and
full backend statistics were not rerun.

`global-write-sum-results.json` retains the primary and earlier baseline rows,
exact plan/runtime configurations, alternating compile captures, image hashes,
remote binary hash and validation output. Global recurrences that read prior
values, opaque callbacks, other feature gaps and full parity remain open.

### Mixed-width integer affine reductions

`81fba84c` adds a bounded typed-integer fallback when the existing single-width
expression proof cannot handle unsigned widening, low-word wrapping or constant
shifts. Widening requires a nonwrapping unsigned affine expression across the
counter's entire unsigned domain. Right shifts additionally require that the
discarded remainder cannot carry anywhere in that domain. This proves arithmetic
from its operands rather than treating small workload bounds as assumptions.
Same-iteration defined-global writes retain their own width; canonical setters
now widen or truncate the final counter appropriately. The existing pure
single-width, floating-sign and uniform-vector proofs retain their common path.
No persistent ABI or function fields are added.

The core/mutable-global split-i64 contracts improve 104,701 -> 130 ns and
104,606 -> 131 ns (806x/796x), versus unchecked w2c2 294/295 ns. All 54 primary
process rows have qualified durations and verified outputs. The inspected core
native image changes 416 -> 432 bytes. The four prior-value recurrence controls
have byte-identical before/current native images; their medians change between
-0.40% and +2.08%, retaining the variation and their remaining gaps rather than
claiming those recurrences are solved.

Alternating three-process compile captures change Core 3 latency +1.39% and
threads +1.49%, with effectively unchanged allocations and identical corpus code
totals. Small scalar/Core 3 compile controls change +0.79%/+1.53%; the imported
worker control changes -0.56%. Native ARM64 and Remote Linux AMD64 integer-sum
execution tests pass, including both ABIs, artifact reload, optimization rollback,
unsigned extrema, old borrowed global values and final stores in both width
conversion directions. Deterministic differential shift tests cover wrap/carry
boundaries and a spread across the counter domain. ARM64 guarded race and pinned
Core 3/threads conformance pass.

Remote diagnostic validation exposed an inherited AMD64 test assumption: reg ABI
alone does not guarantee a mutable-global value pin when locals compete for the
finite GP pool. The same failure is reproduced on `32c26ea3`. `f039e489` tests both
colder and hotter global sources, asserts their actual pin states, and checks
reduction admission against those states. The targeted AMD64 diagnostic suite
now passes; production pin allocation is unchanged. Full backend statistics
were not rerun, and native AMD64 performance remains unmeasured.

`mixed-width-sum-results.json` retains all primary and earlier baseline captures,
compile cohorts, source plans, image hashes, original red test, inherited failure
and corrected diagnostic output. Mutable accumulation, carry/float recurrences,
callbacks and full feature parity remain open.

### Additive defined-global recurrence reductions

`fb801d8f` proves one defined mutable integer recurrence `g <- g+A*i+B` and an
affine returned term in the old/current global and loop counter. Recognition is
bounded to 32 operations/128 bytes and a fixed stack. Imported cells, other
globals, repeated stores, nonlinear updates, conversions, calls, memory accesses,
trapping arithmetic and fenced recurrence bodies reject the proof. Existing
untouched-counter, interruptibility, GC/EH and enclosing-loop restrictions remain.
Runtime global seeds are read at invocation; they are not specialized to corpus
initializers. Canonical setters preserve old stack values and publish final cells.
No persistent ABI or function fields are added.

For `N=max(1,Bound)`, the recurrence sum uses `T(N)` and `choose(N,3)`.
`T(N)` divides its even factor before multiplication, and division by three uses
its inverse modulo the integer width. This preserves wrapping results across the
entire unsigned i32/i64 range. Final state is `g_initial+A*T(N)+B*N`. Tests use an
independent big-integer oracle with exact division before modular reduction,
including nonzero runtime seeds, signed coefficients, constant/cancelled terms,
reads before and after the store, both ABIs, artifact reload, rollback, unsigned
extrema, old borrowed global stack values, separate post-return reads, and traps
that must occur before any reduction store.

The core/mutable-global accumulation contracts improve 69,944 -> 147 ns and
69,965 -> 147 ns (476x/475x), versus unchecked w2c2 294/290 ns. All 72 primary
process rows have qualified durations and verified outputs. The inspected core
native image changes 400 -> 464 bytes. All six controls have byte-identical
before/current native images; their timing changes range -0.56% to +1.95%. The
split-i64 and global-swap controls retain their peer advantage; carry-overflow
remains approximately 2.67-2.69x the peer and is not solved by this proof.

Alternating three-process compile captures change Core 3 latency -0.09% and
threads +0.28%, with effectively unchanged allocations and identical corpus code
totals. Native ARM64 and Remote Linux AMD64 integer-sum execution and targeted
compiler diagnostic tests pass. ARM64 guarded race and pinned Core 3/threads
conformance pass. Full backend statistics were not rerun, and native AMD64
performance remains unmeasured.

`global-recurrence-sum-results.json` retains all primary and earlier captures,
compile cohorts, source plans, image hashes, original red test and validation
output. Floating/carry recurrences, callbacks, branch/bit operations, conversions
and full feature parity remain open.

### Fresh local full-corpus checkpoint — 2026-10-13

The completed local wasm.fyi refresh records Wago `e0067d93` against the
SIMD-capable Clang w2c2 `f303ed27` fork across 118 feature and 148 main
contracts per engine, with all four lifecycle phases and 15 verified timing
samples on successful rows. Existing history and failed/unsupported outcomes
are retained; local database integrity checks pass. The refreshed
[feature comparison](wasm3-feature-refresh.md#fresh-local-snapshot--2026-10-13)
and [raw cohort report](docs/benchmarks/wasm3-2026-10-13/current-feature-cohort.json)
replace the older full-corpus ranking for triage. Opaque host callbacks remain
75.72x the peer, branch-mixed 9.11x, and sign/bit operations roughly 6.28–7.62x.
This checkpoint does not establish full parity or new compile-cost neutrality.
The server remains local at http://localhost:8788/benchmarks/; no production
deployment was performed.

The isolated owned-opaque host capture reproduces the gap at 76.18x the peer
(14.199 ms versus 0.186 ms). A separate matched-workload Go CPU diagnostic
confirms bounded host-segment and bounded numeric admission are false despite
the scalar ABI and private threaded execution. The existing fixed host-return
loop accounts for 75.84% cumulative samples; native resume, syscall transitions
and context refresh are prominent. C callback source retains `noinline` plus
an asm memory effect; native peer call attribution remains unchecked. The
[diagnostic report](docs/benchmarks/wasm3-2026-10-13/opaque-host-diagnosis.json)
retains both fresh matched captures, the red parity command/output, temporary
benchmark source, oracle verification, and sampled CPU output. This is diagnosis,
not a performance improvement or evidence of compile-cost neutrality. The
temporary test was removed from the production tree.

### Private shared-memory control reuse — 2026-10-13

Wago `17c78d48` reuses instance-private threaded control after owned host
callbacks, while refreshing the shared-memory directory on every return.
Migration, changed context versions, and GC execution still restore context.
The regression covers defined/imported shared memory, concurrent growth,
both register ABIs, and artifact roundtrips. ARM64 normal/guarded-race tests,
pinned Core 3/threads conformance, and native AMD64 targeted tests pass.

A fresh locked comparison uses the same six contracts, four phases and
15 timing samples per successful row against Wago `e0067d93` and Clang w2c2
`f303ed27`. Opaque callbacks fall from 14.145 ms to 11.136 ms (21.3%), but
remain 60.87x the peer's 0.183 ms. The other five steady controls change
between -0.9% and +1.6%; their small differences require further confirmation.
All six generated Wago native images retain their code size.

Three alternating compile process pairs compare the exact parent `5458bb4f`
(documentation-only changes since `e0067d93`) with `17c78d48`. Median Core 3
corpus latency changes -1.39%, threads +1.34%; generated code is identical,
threads allocations are identical, and Core 3 differs by six allocations and
600 bytes across 21,800 compiles. Median process RSS changes +0.23% and -2.00%,
respectively. These are targeted measurements, not proof of all compile costs.

The [retained report](docs/benchmarks/wasm3-2026-10-13/host-control-reuse.json)
contains all phase samples, contract/artifact hashes, raw compile measurements,
red/green regression output, correctness checks, and six local publication
receipts. The new Wago results are available at http://localhost:8788/benchmarks/.
No production deployment occurred. Full parity and native AMD64 timing remain open.
