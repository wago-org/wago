# Railshot memory reduction follow-up — 2026-09-03

Base inspected: `779e5e65842359c1c7b169f1af299097853a71ad` on 2026-09-03.

This note covers opportunities beyond
[`railshot-memory-compile-latency-code-quality-plan.md`](../railshot-memory-compile-latency-code-quality-plan.md).
It separates facts in primary sources, observations of current Wago source, and
hypotheses that still need measurement.

## Conclusions

The plan is directionally right, but current `main` has already implemented two
of its later ideas in bounded form: AMD64 deferred nodes carry a register-demand
label and use it for safe tree ordering, and both backends have a fixed-capacity
machine-operation window for ABI register shuffles. Do not build either again;
extend them only after measuring their remaining misses.

The largest unplanned memory target is the control stack. The next best targets
are width reduction for compiler-only indexes, removal of repeated module scans,
sparse representation of bounded module-global pins, and consolidation or
deletion of the large experiment/kill-switch surface.

Recommended order:

1. Remove repeated work and small avoidable allocations.
2. Split `ctrlFrame` into a compact common record and pooled cold sidecars.
3. Narrow compiler-only indexes and flatten per-function metadata.
4. Make scratch retention feature-aware and discard outlier backing.
5. Retire unshipped/default-off experiments and old rollback switches.
6. Replace corpus-tuned thresholds with explicit, architecture-derived costs.

The first implementation slices accompanying this report already apply the
general, code-neutral items: exact sparse retained global hints, a 64-byte
`funcHints` record instead of 200 bytes after moving immutable-table proofs to
module-owned storage, moving the dense global accumulator to scan-only scratch,
and replacing three retained slice headers with checked 32-bit sidecar ranges; one
module-level synchronous-host-call
classification, reuse of the validated local count on every attempt, and the
bounded module-global pin list in place of a per-function dense membership
bitmap. The follow-up statistics slice adds deterministic retained-hint and
failed-attempt byte accounting plus opt-in stage timing. The first control-stack
slice then moves EH-only state into a lazy sidecar, groups scalar fields to
remove alignment holes, and avoids backing allocations for all-false scalar
GC-root vectors. The common frame shrinks from 472 to 408 bytes on AMD64 and
from 416 to 368 bytes on ARM64. A generated 128-deep scalar-block benchmark
dropped from 283 to 155 allocations per AMD64 compile and from about 229.7 KiB
to 209.8 KiB; median latency was effectively flat across the before/after local
screens.

The compact hint-range slice reduced the fixed record from 120 to 64 bytes on
both architectures. On ARM64, the 1,024-function sparse-global stress benchmark
dropped from 208,088 to 134,168 B/op and from 24 to 21 allocations. Full
`many_funcs` compilation dropped from 150,776 to 125,240 B/op and from 42 to 39
allocations. Five-sample default-GC medians moved by +0.4%, within the 1.5%
investigation gate; with GC disabled the median improved by 1.3%. Native-code
hashes for many-function, global-heavy, indirect-dispatch, and json-as fixtures
were identical to the parent revision.

Subsequent exact width, ordering, and flag packing reduced the retained summary
to 40 bytes on both architectures. ARM64 now packs its first-64-locals
entry-initialization bitmap into the unused high bits of the existing local-score
sidecar. The existing declared-local initialization loop reads those bits while
it already visits each local. This reduces the ARM64 record again to 32 bytes
without adding storage, an allocation, a separate scan, or a workload selector.
Local hotness retains 31 exact bits,
which is far beyond every current threshold; its arithmetic saturates at that
limit and all consumers mask the metadata bit. Full `many_funcs` compilation
drops by 2,560 B/op and `json-as` by 384 B/op, while an interleaved six-pair
GC-off timing screen is effectively unchanged and matched native-code hashes
remain identical.

AMD64 applies the same exact packing and direct consumption, reducing its
retained summary from 40 to 32 bytes as well. Emulated Linux/AMD64 full-compile
allocation drops by 2,560 B/op on `many_funcs` and about 384 B/op on `json-as`,
with allocation counts unchanged. Six-pair interleaved GC-off medians are
effectively flat on both modules, and matched corpus plus entry-initialization
native-code hashes remain identical. The representation is unconditional on
both targets and is selected by no producer, module, function, hash, or corpus
identity.

ARM64 then removed a duplicate host-width local-count array by writing the
validated count directly into the compact summary before scanning. The
1,024-function stress case dropped again from 134,168 to 125,976 B/op and from
21 to 20 allocations; `many_funcs` dropped from 125,240 to 122,552 B/op and
from 39 to 38 allocations. This follows function structure only and introduces
no workload or corpus-specific admission path.

The first retry-elimination slice found both remaining ARM64 corpus retries had
the same structural cause: three module-global registers plus the memory-size
register reduced the allocatable file, while 17 optional whole-function pins
left only three transient registers. Ordinary lowering can simultaneously
protect three inputs or temporaries and request a result. A target-derived limit
now reserves four unreserved registers after module roles are removed. It uses
only the architecture's register obligations, never workload identity.

Across every module in `bench/corpus`, retry count fell to zero. Ruby attempts
fell from 17,454 to 17,452, eliminating 4,299 input bytes, 109,648 node bytes,
and 4,060 emitted bytes from failed attempts. The successful first attempts keep
16 pins rather than recompiling with all pins disabled, shrinking Ruby's native
image by 2,592 bytes. Five one-shot compile medians moved from 608.95 to 610.21
milliseconds (+0.2%). The old retry remains only as a measured oracle until the
adversarial pressure suite and AMD64 also demonstrate zero production hits.

This is a staged reduction, not satisfaction of the 32--48-byte common-record
target. Index-width, further retention work, sidecar pooling, and policy deletion
remain subject to the gates below.

A smaller initial control-merge reserve was measured and rejected. Reducing the
ARM64 reserve from 16 records to four left `many_funcs` unchanged at about
78,088 B/op and 40 allocations, but made merge-heavy `json-as` grow through two
intermediate backings: allocation increased from about 255,747 to 258,819 B/op
and from 936 to 938 objects. The fixed reserve is therefore not an easy cut.
Further control-memory work should split independently cold feature families or
narrow exact domains instead of trading retained capacity for allocation churn.

The first worker-lifecycle implementation keeps the initial operand chunk and
can release an unused overflow suffix. Eagerly doing so after one smaller
neighbor, however, caused ordinary modules with varying function sizes to
discard and regrow the same pointer-rich chunks. Trimming now requires two
consecutive lower-demand functions and retains enough chunks for the larger of
that pair. Sustained small work still collapses giant high-water, while a single
small body no longer provokes churn. Chunk capacities are bounded at 8,192 and
now occupy `uint16`; the trim count uses `uint32`, reducing the stack owner from
80 to 72 bytes without narrowing an admissible chunk count. Native ARM64
`json-as` falls from 255,280 to 222,512 B/op (-12.8%) and 936 to 935 allocations;
emulated Linux/AMD64 falls from 241,760 to about 208,992 B/op (-13.6%) and 909 to
908 allocations. `many_funcs` changes only by the eight-byte owner reduction.
Timing samples overlap. The rare shrink transition still clears every
scratch-owned `*elem` path and retained chunk capacity before removing slice
headers, and the resource ledger still reports reservation, peak, retention,
and discarded bytes. Giant-lane scheduling and byte-weighted admission remain
open.

## Primary-source facts

- Go's collector traces pointers transitively, and slices, maps, strings,
  interfaces, and channels contain pointers that participate in that object
  graph. Pointer-bearing compiler state therefore costs more than its payload
  bytes alone. The official runtime exposes `/gc/scan/heap:bytes` and GC assist
  CPU separately, so both effects are measurable. Sources: [Go GC guide](https://go.dev/doc/gc-guide),
  [`runtime/metrics`](https://pkg.go.dev/runtime/metrics).
- LLVM's bump allocator `Reset` releases every slab except the current slab,
  explicitly bounding retained high-water instead of retaining the complete
  largest allocation. V8 zones provide the stronger function-lifetime model:
  `DeleteAll` returns every segment to the allocator. Sources:
  [LLVM `BumpPtrAllocatorImpl::Reset`](https://llvm.org/doxygen/classllvm_1_1BumpPtrAllocatorImpl.html),
  [V8 `Zone::DeleteAll`](https://chromium.googlesource.com/v8/v8.git/+/refs/heads/13.2.1/src/zone/zone.cc).
- LLVM `SmallVector` keeps a small number of entries inline to avoid heap
  allocation without removing the large-input fallback. Cranelift uses compact
  typed integer entity references, dense vector-backed maps, and pooled
  `EntityList`s specifically to reduce index and list footprint. Sources:
  [LLVM `SmallVector`](https://llvm.org/docs/doxygen/classllvm_1_1SmallVector.html),
  [Cranelift entity data structures](https://docs.rs/cranelift-entity/latest/cranelift_entity/),
  [Cranelift `PrimaryMap`](https://docs.rs/cranelift-entity/latest/cranelift_entity/struct.PrimaryMap.html).
- Liftoff and Winch both retain the baseline-compiler principle of one-pass
  direct lowering without a whole-function IR. TPDE's useful addition is to
  combine selection, allocation, and encoding rather than materialize another
  global representation. Sources: [V8 Liftoff](https://v8.dev/blog/liftoff),
  [Winch design](https://github.com/bytecodealliance/wasmtime/blob/main/winch/README.md),
  [TPDE paper](https://aengelke.net/pubs/2602-cgo1.pdf).

These sources support representation and lifetime choices; they do not prove a
Wago speedup. The gates below remain necessary.

## Current-source observations

### 1. `ctrlFrame` is a larger pointer-rich hot record than the plan recognizes

At the inspected base, AMD64's common control-frame record contained fifteen
slice headers and one map, including type vectors, GC-root vectors, GC-fact
vectors, local-state snapshots, and EH catches. ARM64 had the same structural
issue, with additional cold-edge and loop-pin lists. The first staged cut has
removed EH fields from ordinary records, but every ordinary `block`, `loop`, and
`if` still pays for cold GC, loop, and merge fields even when none are used.

Sources: [AMD64 `ctrlFrame`](../../src/core/compiler/backend/railshot/amd64/control.go#L34),
[ARM64 `ctrlFrame`](../../src/core/compiler/backend/railshot/arm64/control.go#L44).

The operand-node conversion remains important, but compacting control frames can
be staged earlier and kept byte-identical. A plausible common record is:

```go
type ControlFrame struct {
    Height, LoopStart uint32
    EndsStart         uint32
    Sidecar           uint32
    ParamN, ResultN  uint16
    BranchN          uint16
    Kind, Flags      uint8
    Result0          uint8
}
```

Variable lists should be ranges into pointer-free, function-owned pools. GC/EH,
loop, and register-merge state should live in tagged sidecars allocated only
when that feature occurs. This follows the dense-ID/pool approach in
`cranelift-entity`; it does not require an IR.

**Hypothesis:** a 32–48 byte common frame will materially reduce heap scan and
cache traffic in deeply nested scalar code. Prove it with `unsafe.Sizeof`,
`/gc/scan/heap:bytes`, deep-control `B/op`, and identical native hashes.

### 2. Several compiler indexes are needlessly host-width

At the inspected base, `localSlot []int`, relocation offsets/targets, control
heights/counts, and most fields in parallel `funcResult` used 64-bit `int` on
supported hosts. The module and metadata layers already use `uint32` for
function indexes, local indexes, code offsets, frame offsets, and safepoint IDs.

Sources: [AMD64 function state](../../src/core/compiler/backend/railshot/amd64/compile.go#L240),
[AMD64 relocations](../../src/core/compiler/backend/railshot/amd64/call.go#L47),
[parallel result metadata](../../src/core/compiler/backend/railshot/amd64/compile.go#L870),
[root-plan compact indexes](../../src/core/compiler/backend/railshot/shared/gc_frame_roots.go#L49).

Use checked conversion at the decoder/compiler boundary, then retain `uint32`
internally. Pack booleans and small enums into flags. This is general input-size
accounting, not a workload heuristic.

ARM64 now retains local slots as `[]uint32`. Validated functions fit exactly,
and the compiler rejects an oversized synthetic inline frame before consuming
its stored homes. A generated 4,000-local compile saves exactly 16,384 B/op
(414,098 to 397,714, with 38 allocations unchanged); ordinary `many_funcs` and
`json-as` save 32 and 120 B/op because their shorter arrays straddle only nearby
allocator size classes. Five-sample stress timings overlap and matched corpus
hashes are identical.

AMD64 now uses a target-specific but equally bounded packing rather than a
second sidecar: accepted frame offsets occupy the low 24 bits of each `uint32`,
and the compact finalizer's exact reference count occupies the high eight. The
native stack-fence limit is roughly 256 KiB, versus a 16 MiB packed-offset
domain. The finalizer stores at most 255 rewrite sites; a 256th reference marks
the recorder inexact and disables reordering. A generated 4,000-local compile
saves the same 16,384 B/op (494,696 to 478,312, with 42 allocations unchanged),
while `many_funcs` and `json-as` save 32 and 120 B/op in both ordinary and
compact-finalizer modes. Emulated timings overlap, focused slot-order execution
passes, and matched corpus hashes are identical. Narrowing and flattening
remaining relocations is still a hypothesis and should be rejected if checks
add measurable compile time.

The per-function GP pin candidate was another host-width record hidden in
scratch: one Boolean before an `int` index made the otherwise scalar record 24
bytes. Both backends now retain the exact validated index as `uint32` and order
the fields into a 12-byte pointer-free record. Ranking and admission are
unchanged. In a generated 4,000-integer-local compile, geometric slice growth
makes the measured saving roughly 137.2 KiB/op on each architecture: 397,714 to
260,512 B/op on ARM64 and 478,312 to 341,112 B/op on Linux/AMD64, with allocation
counts unchanged. Five-sample stress timings improved slightly, focused backend
tests pass, and matched corpus hashes are identical. This suggests auditing
scratch element layout is at least as important as auditing the slice headers.

The float/vector candidate list had the same issue in a simpler form: it stored
validated original-local indexes in a reusable `[]int`. It is now `[]uint16` on
both backends. The walk stops at `nLocals`, which validation caps at 65,535; it
does not include appended inline scratch, so the conversion is exact. A
generated 4,000-`f64`-local compile falls from 226,984 to 123,688 B/op on ARM64
and from 348,728 to 245,432 B/op on Linux/AMD64, removing three allocations on
each target. Native ARM64 median time improves slightly and emulated AMD64
samples overlap. Ordinary `many_funcs` and `json-as` remain in the same
allocator classes. No rank or admission policy changed.

Call staging also retained a host-width slot prefix for every live operand,
despite consuming those values only as bounded native-frame spill indexes. Both
backends now retain `uint32` prefixes and reconstruct `int` only at address-use
sites. A generated synchronous-host-call function with 1,000 values live below
the call falls from 309,029 to 296,741 B/op on ARM64 and from 453,538 to 441,250
B/op on Linux/AMD64, removing one geometric-growth allocation on each target.
Five-sample timings overlap; ordinary `many_funcs` and `json-as` remain in the
same size classes, and matched corpus hashes are identical. Oversized functions
remain rejected by the architecture's native-frame check before any compiled
artifact is published.

An allocation-space profile of native ARM64 `json-as` compilation then showed
`stack.alloc` responsible for 56.5% of sampled bytes. The common 64-byte node is
still the fundamental target, but the profile also exposed lifecycle churn:
overflow chunks were being allocated again after eager one-function trimming.
The two-function bounded hysteresis above removes roughly 32 KiB per compile on
both architectures without changing node sizing, instruction selection, or
native bytes. This is a useful ordering result: eliminate avoidable arena churn
before undertaking the larger pointer-to-ID migration, so that migration is
measured against an honest allocation baseline.

The same profile attributed another sampled megabyte to the per-loop
`map[uint32]bool` used only to retain modified-local membership. On ARM64 this is
now a sorted, deduplicated segment of one reusable `[]uint16` arena; open control
frames retain only a 32-bit start, 16-bit count, and validity bit. Validation
already limits function locals to 65,535, and an unknown scan remains
conservative rather than looking invariant. Native `json-as` falls from roughly
222,498 to 211,923 B/op (-4.8%) and from 935 to 770 allocations, with overlapping
timings; `many_funcs` stays in the same allocation class. This is a reusable
general pattern for other temporary index sets: retain a compact sorted segment
when mutation is batch-built and lookup happens later, instead of paying a Go
hash table per structured region.

AMD64 now uses the same representation in both its ordinary loop scan and the
combined versioning/hoist scan. Its exact GC-reference invalidation walks the
compact modified-local segment, while invariant membership remains binary
search. Range metadata exactly replaces the former map pointer, leaving the
232-byte cold control-merge record unchanged. Emulated `json-as` compilation
falls from roughly 208,980 to 198,659 B/op (-4.9%) and from 908 to 748
allocations; `many_funcs` stays in the same allocation class and timing samples
overlap. The default-off loop-versioning experiment does not receive a new
admission path; only its shared fact representation changes.

### 3. Repeated-work audit

Two earlier repeated-work candidates are already gone in current source, so
they must not be planned or implemented again. Module setup resolves effective
synchronous-host-call use once before dispatching functions, and function
attempts consume the validated hint's local count—including retries—instead of
recounting local declarations. The current profile points to operand backing and
structured-region scratch, not either of those old scans.

These are the safest first PRs: no generated-code decision needs to change.

### 4. Bounded module-global pins still allocate dense per-global state

The module pin set is bounded by the target register list, but each function
allocates `moduleGlobal = make([]bool, nGlobals)`. `globalReg` and `globalDirty`
also retain dense per-global arrays even when only a handful of globals are
pinned.

Sources: [pin selection](../../src/core/compiler/backend/railshot/amd64/compile.go#L2254),
[dense installation state](../../src/core/compiler/backend/railshot/amd64/compile.go#L3055).

Represent module pins as the already-existing small sorted `[]moduleGlobalPin`.
Represent function-local global pins as a compact list plus a bounded lookup
cache, or retain a dense table only above a measured access-count crossover.
At minimum, replace `moduleGlobal []bool` with a small mask/list and reuse rather
than allocate it per function.

**Hypothesis:** the minimum change removes one allocation per function whenever
module pins are active. The sparse redesign wins most on many-global modules and
must not slow hot `global.get/set`; benchmark that path directly.

### 5. Parallel metadata can be flat and pointer-free

Parallel compilation currently keeps `relocs [][]callReloc`, a `funcResult` per
function containing another relocation slice and `error` interface, per-worker
code/literal arenas, and then a final joined code allocation.

Sources: [parallel setup](../../src/core/compiler/backend/railshot/amd64/compile.go#L1513),
[`funcResult`](../../src/core/compiler/backend/railshot/amd64/compile.go#L870).

Keep the existing final join—the repository has already shown direct image
population can lose latency—but flatten relocations and other variable metadata
into per-worker pointer-free pools and store `(worker, start, count)` in compact
results. Store the first error out-of-band instead of one interface in every
result. This is analogous to Cranelift's pooled compact entity lists and does not
alter scheduling or output ownership.

The error interface and duplicate relocation header have since moved out of
each result. ARM64 now also narrows the remaining worker identity, worker-arena
range, body length, and internal-entry offset to checked 32-bit fields, reducing
`funcResult` from 88 to 64 bytes. A per-worker code arena above 4 GiB is rejected
before append rather than truncated. Four-worker native benchmarks reduce
`many_funcs` by about 6.8 KiB/op and `json-as` by about 1.8 KiB/op with unchanged
median allocation counts and compile timings within the normal investigation
gate. AMD64 now uses the same checked representation, including its two literal
pool indexes, reducing its record from 104 to 72 bytes. That removes exactly
9,632 bytes of result payload for 301 functions and 1,536 bytes for 48 functions.
Emulated four-worker full-compile allocation was consistently lower but too
scheduler-dependent for a precise aggregate claim; the record size and boundary
tests are the architecture-independent evidence.

### 6. Scratch retention should be per buffer, not per worker as a whole

The scratch object retains more than twenty variable-capacity buffers, including
GC facts, roots, types, moves, intervals, constants, and finalizer metadata. A
single unusual function can raise several capacities for the remainder of the
module.

Source: [AMD64 transient scratch](../../src/core/compiler/backend/railshot/amd64/compile.go#L478).

Apply an explicit retention class to each buffer:

- fixed inline backing for frequent tiny vectors;
- reusable backing capped at a measured ordinary-function percentile;
- ephemeral overflow discarded after the function;
- lazy allocation gated by function feature bits already gathered in hints.

LLVM's reset policy and V8's zone lifetime establish the general pattern. Do not
use `sync.Pool` until the retained size of each object is bounded.

### 7. Current `main` is ahead of two plan steps

AMD64 nodes already have `regNeed`, and `labelDeferredNode` computes bounded
register demand used by tree ordering. Both backends already use a fixed
24-operation machine window for call-register moves.

Sources: [AMD64 node label](../../src/core/compiler/backend/railshot/amd64/stack.go#L161),
[tree labeling](../../src/core/compiler/backend/railshot/amd64/treeorder.go#L33),
[AMD64 machine window](../../src/core/compiler/backend/railshot/amd64/regcopy.go#L17),
[ARM64 machine window](../../src/core/compiler/backend/railshot/arm64/regcopy.go#L17).

The plan should treat these as existing foundations. Extend pressure prediction
to the function summary and extend the window beyond ABI shuffles only when
counters identify a profitable family.

## Corpus-specific-path audit

I searched production Railshot Go sources for benchmark/corpus names, module or
function names, body hashes, and byte equality/prefix tests used as conditions.

**Observed:** no production optimization dispatches on a module name, function
name, benchmark name, body hash, or whole-body identity. Existing gates are based
on instruction shape, types, effects, calls, body size, local count, feature
bits, and target capabilities.

That does not mean all policy is workload-independent:

- Module-global pin thresholds are explicitly tuned in comments to keep
  `json-as` candidates and reject `blake-as` candidates. The predicate is
  general, but the constant is corpus-fitted rather than derived from a cost
  model. Sources: [AMD64 threshold](../../src/core/compiler/backend/railshot/amd64/compile.go#L2255),
  [ARM64 threshold](../../src/core/compiler/backend/railshot/arm64/compile.go#L1795).
- The handwritten SWAR recognizers explicitly target producer-emitted sequences
  described as `json-as`, `utf-as`, or `xjb-as` shapes. They match exact semantic
  expression patterns rather than identities, so they are usable by any module,
  but they are bespoke paths and increase maintenance surface. Sources:
  [AMD64 SWAR rules](../../src/core/compiler/backend/railshot/amd64/swar.go#L5),
  [ARM64 SWAR rules](../../src/core/compiler/backend/railshot/arm64/swar.go#L5).
- Some general gates were selected because named corpus workloads regressed,
  for example leaf-only inlining. The condition itself is a general semantic
  class, which is acceptable, but its constants still need adversarial and
  synthetic validation. Source: [inline policy](../../src/core/compiler/backend/railshot/amd64/inline.go#L178).

Use this rule going forward:

> Production may dispatch on validated semantics, effects, bounded resource
> estimates, and target costs. It must never dispatch on producer identity,
> module/function names or indexes, benchmark membership, hashes, or memorized
> byte bodies.

Replace `extraBar` with an explicit benefit/cost estimate: dynamic weighted
global accesses saved versus prologue/epilogue traffic and the opportunity cost
of reserving a register across every function. Move surviving SWAR/SIMD patterns
into the planned generated matcher, name them by semantics, and require a
producer-independent correctness proof. Delete patterns whose corpus-wide hit
count or execution benefit does not clear a declared threshold.

## Things that can be cut easily

1. **Repeated module scans:** resolve synchronous-host-call mode once.
2. **Per-function `moduleGlobal []bool`:** use the bounded pin list/mask.
3. **Default-off experiments with no active qualification:** `v128-sink`,
   `loop-precheck`, and exact
   GC-ref facts are compiled into production but disabled by default.
   AMD64 `affine-lea` and `tee-spill-elide` were subsequently retired after
   their focused fixtures and broad screens failed the normal gates.
   Either promote the remaining paths against the standard gates or delete
   them. Do not let an indefinite experiment become a permanent alternate path.
4. **Host-width operand indexes and separate root booleans:** both backends now
   store exact 32-bit indexes and pack GC/EH root state beside value facts,
   reducing each common operand node from 72 to 64 bytes without changing
   selection.
5. **Mature rollback environment switches:** the production Railshot packages
   currently reference 138 distinct `WAGO_*` variables. Preserve a small number
   of safety-critical or public controls; migrate measurement-only toggles to
   test-only policy construction, then delete old package globals and branches.
   `CodegenPolicy.Selection` already provides a per-compilation bitset and is the
   natural single owner. Sources: [optimization selection](../../src/core/compiler/optimization/catalog.go#L69),
   [codegen policy](../../src/core/compiler/backend/railshot/shared/policy.go#L5).
6. **Duplicated architecture-neutral code:** hint aggregation, eligibility
   tracking, compact metadata records, and resource accounting should live in
   `shared`. Keep target selection and emission in the architecture packages.

Items 3, 5, and 6 primarily reduce binary size and code-quality burden; they should not
be claimed as compile-heap wins without measurements.

## Measurement gates for these additions

| Change | Required evidence |
|---|---|
| Repeated-scan removal | Identical artifacts; fewer body/import visits; no latency regression |
| Compact control frames | Common record at most 48 B; lower deep-control `B/op` and scan heap; identical artifacts |
| Narrow indexes | Checked overflow tests at every boundary; identical artifacts; lower local/reloc metadata bytes |
| Sparse module pins | No allocation per function for module-pin membership; global-heavy execution statistically flat |
| Flat parallel metadata | Lower alloc count and peak live heap at workers 2/4/8; join latency statistically flat |
| Per-buffer retention caps | Giant-then-tiny forced-GC retention near ordinary baseline; no allocation churn on repeated medium functions |
| Knob/rule deletion | Full catalog, spec, differential, and benchmark gates; net source and binary-size decrease |

All benchmark thresholds must be based on shape-generated stress modules plus the
full heterogeneous corpus. A corpus may validate a general policy, but it must
never select the policy.
