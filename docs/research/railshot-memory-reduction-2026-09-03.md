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

Trap patch lists were the next exact-width sidecar visible in the profile. Their
branch offset used a host-width `int` beside two `uint32` fields, padding each
record to 16 bytes. Both backends now narrow at an explicit checked boundary and
retain a 12-byte `trapSite`; AMD64 reserves all-ones for its existing internal
no-common-jump state. `json-as` saves approximately 1,072 B/op on both targets
without changing allocation count, while the trap-free `many_funcs` fixture is
unchanged. The result reinforces the layout audit rule: range-check native
offsets once where they enter retained metadata, then keep sidecars uniformly
32-bit until an encoder API requires `int`.

ARM64 direct-call relocations had the same avoidable host-width layout: two
`int` fields and one boolean occupied 24 bytes although both retained values are
bounded code/function indexes. Checked construction and finalizer remapping now
retain two `uint32` fields, and the module patcher rejects an all-ones invalid
sentinel before indexing. The record is 12 bytes. Native `json-as`, which keeps
326 call relocations, falls from approximately 210,841 to 206,929 B/op with the
same 770 allocations; the 3,912-byte delta exactly equals 326 times the 12-byte
record saving. `many_funcs` has no retained direct-call relocation backing and
is unchanged. This applies uniformly to every direct call and changes no call
admission or code-selection policy.

AMD64 now uses the same 12-byte record for direct calls and its existing shared
GC-stub calls. The stub discriminator remains explicit and is checked before the
otherwise-unused function target, while sites and real function targets cross
one checked narrowing boundary. Emulated Linux/AMD64 `json-as` falls from
approximately 197,577 to 193,649 B/op with the same 748 allocations; allocator
rounding makes the 3,928-byte observed delta slightly larger than the raw
12-byte-per-record payload reduction. Relocation-free `many_funcs` remains in
the same allocation class. The five-sample timing ranges overlap, so this is a
retained-memory result rather than an emulated latency claim.

The post-sidecar ARM64 allocation profile makes the next priority clear:
operand-arena allocation accounts for roughly three quarters of Railshot-owned
allocation space. The first NodeID stage replaces only the physical stack's
previous/next pointers with 32-bit chunk/slot coordinates. Chunk growth cannot
invalidate a coordinate, lookup needs no map or allocation, and the bounded
8,192-node chunk size leaves ample room in the 16-bit slot field. `elem` falls
from 64 to 56 bytes. Native `json-as` allocation falls from 206,928 to 196,688
B/op and `many_funcs` from 78,040 to 75,992 B/op, both with unchanged allocation
counts. Six interleaved `GOGC=off` pairs on `many_funcs` had mixed latency signs
and a roughly +0.7% median delta. The remaining two deferred-child pointers
still make the backing scannable; converting those through the same coordinate
domain is the next structural stage, not a parallel compiler path.

The identical AMD64 physical-link prototype saved the same 2,048 B/op on
`many_funcs` and 10,240 B/op on `json-as`, but it did not pass the compile-time
gate. Six alternated `GOGC=off` pairs in one Linux/AMD64 emulation environment
showed an approximately 4% median `json-as` regression. The prototype was
removed. This is not native-target proof, but it is enough to reject a default
change in the absence of contrary native evidence. AMD64 should not grow a
target-specific workload switch; its next attempt should remove all node
pointers and reduce lookup cost structurally, then qualify that one uniform
representation on native AMD64.

A follow-up ARM64 prototype converted both deferred-child pointers through the
same chunk/slot coordinate and proved the intended memory ceiling: `elem` became
pointer-free and 48 bytes, `json-as` fell again from 196,688 to 167,792 B/op,
and `many_funcs` fell from 75,992 to 71,896 B/op. It nevertheless failed the
latency gate. Native `GOGC=off` samples were roughly 3–5% slower because the
pointer-oriented lowering API turned every child read into a two-level chunk
lookup; a cached chunk-zero view did not materially recover the loss. That
prototype was removed. The result narrows the viable design: complete NodeID
conversion should include register-user tables and other transient node owners,
allowing a flat movable `[]ValueNode` and one-index access. Merely replacing
stored pointers while preserving pointer-shaped APIs is not balanced enough.

A sampled allocation profile also attributed about 4 MiB of repeated
`json-as` allocation to ARM64 finalizer peepholes, whose mixed
`map[int]bool` holds both branch-target sites and rare synthetic markers. A
prototype moved ordinary target sites to a sorted compact slice while retaining
the map only for rare markers. It saved just 240 B/op on `json-as`, increased
allocation count from 770 to 774 through slice growth, and regressed native
`GOGC=off` compile time by roughly 6%; `many_funcs` saved 648 B/op and three
allocations but also slowed. The prototype was removed. The profile label
included caller-owned finalizer growth and did not establish that the hash map
was the actionable payload; future finalizer cuts need retained-size evidence,
not attribution alone.

The global-hint accumulator's dense eligibility byte was easier to remove
without a tradeoff. Eligibility now occupies the unused high bit of its
separate epoch mark, while the hotness score retains all 32 bits and therefore
keeps exact saturation and ranking. Comparisons mask the bit, and rollover of
the remaining 31-bit epoch clears the marks before reuse. On the existing
1,024-function/1,024-global sparse-use hint benchmark, allocation falls from
93,208 to 92,184 B/op and from 20 to 19 allocations, exactly one byte per global
plus the removed backing allocation. Eight native ARM64 `GOGC=off` timing
samples overlap. This is one representation for both targets and all modules;
it does not depend on the number of touched globals beyond ordinary sizing.

Constant-division magic-number derivation was another profile-visible source
that could be deleted rather than pooled. The old architecture-neutral helper
constructed several `math/big.Int` values for each qualifying divisor. Its
largest numerator is `2^127`, while the proposed quotient is strictly below
`2^64`; one `bits.Div64` therefore computes the same quotient and remainder
without heap storage. The doubled proposed multiplier is needed only modulo the
selected 32- or 64-bit width, and the doubled-remainder comparison is expressed
as `rem >= divisor-rem` to avoid overflow. A deterministic test compares the
entire result tuple with the former big-integer construction for more than
20,000 random and boundary divisors. Native ARM64 `json-as` falls from 196,680
to 191,496 B/op and from 770 to 567 allocations; emulated Linux/AMD64 falls from
193,648 to 188,448 B/op and from 748 to 544 allocations. Timing samples are
neutral to favorable on both screens, while division-free `many_funcs` is
unchanged. This removes general compile-time machinery without changing the
existing constant-division admission or generated algorithm.

ARM64's direct-call relocation profile then exposed geometric slice growth that
could use the already-completed body scan. The 32-byte function summary had two
padding bytes, now occupied by a saturated count of local direct-call sites.
Functions with at least eight sites reserve their compact 12-byte records once;
smaller sets keep append growth because optional inlining can erase their only
relocation and they cross too few allocator size classes to repay a reserve. An
initial reserve-for-any-call prototype demonstrated that boundary directly:
`many_funcs` gained 48 B/op and one allocation because a small call set was
inlined, so that form was removed. With the bounded target-cost rule,
`many_funcs` returns exactly to 75,992 B/op and 40 allocations, while native
`json-as` falls from 191,496 to 190,248 B/op and from 567 to 544 allocations.
Six `GOGC=off` timing samples overlap. Counts saturate conservatively and append
remains the correctness fallback. The rule uses only decoded local-call count
and target record-growth cost, never module or workload identity.

The next ARM64 control-state split removes GC-only slice headers from ordinary
merge records. `base`, parameter, and result root vectors now live in a lazy
72-byte `ctrlFrameRoots` arena parallel to the existing depth-owned merge arena;
the common `ctrlFrameMerge` falls from 232 to 160 bytes. Scalar compilation does
not allocate the root sidecar. Exact-GC frames retain the same vectors, and
slot moves, release clearing, worker teardown, and resource-ledger accounting
move both arenas together. Native `json-as` falls from 190,248 to 188,840 B/op
with 544 allocations unchanged, while `many_funcs` remains unchanged because
it does not allocate merge backing. Eight `GOGC=off` timing samples overlap.
The sidecar is admitted only when an actual root bit is recorded, an exact
semantic condition shared by every function.

AMD64 has the same ownership seam and a larger cold family: eight root and
reference-fact slices previously occupied every 280-byte merge record. Moving
them into one lazy, depth-parallel 192-byte GC arena reduces the common record
to 88 bytes. Both arenas move and clear together, and their capacities remain
fully represented in the worker resource ledger. Packing the four merge/GC
peak and discard capacities into their exact 32-bit domains prevents the new
sidecar header from pushing ordinary scratch into a larger allocation class.
Emulated Linux/AMD64 `json-as` falls from 188,448 to 185,120 B/op with 544
allocations unchanged; `many_funcs` remains exactly 70,480 B/op and 35
allocations. Six `GOGC=off` timing samples are neutral to favorable, and the
complete backend suite passes. As on ARM64, only actual semantic root or fact
state allocates the secondary arena.

ARM64's dense global-register table was scratch in intent but not in lifetime:
resetting the reusable `fn` dropped its backing, so every function in a module
with globals allocated and initialized another table. The compiler now carries
that backing across resets, reslices it, and fills it with `regNone` before
installing the next function's module and value pins. The register byte's unused
high bit also stores the dirty value-pin flag, deleting the parallel dense
`[]bool`; all physical AArch64 registers fit in five bits and the all-ones
`regNone` sentinel remains distinct. Native `json-as` falls from 188,840 to
186,120 B/op and from 544 to 459 allocations. Eight `GOGC=off` timing samples
are neutral to favorable, while global-free `many_funcs` stays exactly at
75,992 B/op and 40 allocations. This is unconditional ownership repair, not a
global-count or workload selector.

AMD64 now uses the same lifetime and packed-state representation. Its physical
registers occupy only four low bits, and every global accessor, call
spill/reload path, module-boundary synchronizer, and GC native-stub preservation
loop decodes the register before use. Emulated Linux/AMD64 `json-as` falls from
185,120 to 182,400 B/op and from 544 to 459 allocations, exactly matching the
ARM64 reduction; eight `GOGC=off` timing samples are favorable. Global-free
`many_funcs` remains exactly 70,480 B/op and 35 allocations. Focused packed
state, GC, and allocation-free reuse tests pass with the complete backend
suite. The scratch ownership is unconditional on both targets.

### 3. Repeated-work audit

Two earlier repeated-work candidates are already gone in current source, so
they must not be planned or implemented again. Module setup resolves effective
synchronous-host-call use once before dispatching functions, and function
attempts consume the validated hint's local count—including retries—instead of
recounting local declarations. The current profile points to operand backing and
structured-region scratch, not either of those old scans.

These are the safest first PRs: no generated-code decision needs to change.

The September 4 audit found and removed one later redundant walk: AMD64 still
classified every loop body, sorted its assigned locals, and retained the result
after the GC-fact consumer was deleted. Its only remaining reader incremented a
diagnostic hoistability counter and did not select or omit a bounds check. Removing
that walk improves the native loop-control compile fixture by about 8.2%, reduces
its B/op by 5.3%, and removes one allocation with identical corpus native code.

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
- The handwritten SWAR recognizers explicitly targeted producer-emitted sequences
  described as `json-as`, `utf-as`, or `xjb-as` shapes. A current full-corpus
  `CodegenStats` census found widen/pack/parse hits only in json-as, utf-as, and
  their synthetic fixture, plus multiply-high only in the xjb-mulhi fixture. The
  paths were removed rather than retained without independent-producer evidence.
  Five native ARM64 500 ms samples measure the cost at +14.5% for the real
  utf-as scalar conversion and +29.6% for the synthetic combined pack/parse
  loop. The non-synthetic execution-corpus geomean moves +0.8% in a shorter
  five-sample watchpoint; excluding the directly affected utf-as export leaves
  +0.4%. This is therefore an explicit generality and maintenance decision, not
  a performance claim.
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
of reserving a register across every function. Move surviving SIMD patterns into
the planned generated matcher, name them by semantics, and require a
producer-independent correctness proof.

## Things that can be cut easily

1. **Repeated module scans:** resolve synchronous-host-call mode once.
2. **Per-function `moduleGlobal []bool`:** use the bounded pin list/mask.
3. **Default-off experiments with no active qualification:** loop prechecks,
   AMD64 `v128-sink`, `affine-lea`, `tee-spill-elide`, and exact GC-reference
   facts were retired after their focused fixtures and broad screens failed the
   normal gates. Do not let an indefinite experiment become a permanent
   alternate path.
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

## September 4 production-policy re-audit

After the GC-fact removal, production Railshot source references 121 distinct
`WAGO_*` controls, down from the earlier 138-count audit. No production condition
compares a module name, function name, producer name, benchmark name, body hash, or
memorized body byte sequence. Named workloads appear in rationale comments and
tests, not selector inputs.

That boundary is now checked by `TestProductionPolicyRejectsWorkloadIdentity`.
The test parses all AMD64, ARM64, and shared production Go sources independent of
host build tags. It rejects workload/corpus string literals, name-section reads
outside diagnostic naming, hashing imports, and byte-prefix/suffix/containment
matching. Exact byte equality remains available to native-code deduplication, while
instruction optimizations continue to operate on decoded semantics and immediates.

Only two catalog entries remain explicitly experimental:

- AMD64 `bmi2-rorx` is not a dormant workload path. The public runtime enables it
  only after host CPUID proves BMI2, and selection depends on the target feature
  plus a constant rotate shape. Its prior broad result was neutral but its focused
  SHA-256 result exceeded noise; removing the public CPU/cache contract requires a
  fresh native qualification rather than a mechanical deletion.
- ARM64 `loop-region-pins` remains opt-in. It is driven by structured loop effects
  and register availability rather than identity, and its prior screen showed a
  substantial execution win alongside mixed regressions and a small compile cost.
  It should be requalified for promotion or deletion, but is not an evidence-free
  easy cut.

The remaining low-risk cleanup seam is therefore mature rollback plumbing, not a
third default-off compiler. Move measurement-only overrides from package-global
environment variables to per-compilation test policy as each optimization completes
its current qualification; preserve public behavior and native-code parity while
deleting the old branch. This should be handled in small families because the 121
controls include diagnostics, safety or feature switches, and resource budgets that
must not be removed as if they were equivalent peephole toggles.
