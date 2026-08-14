# Unified Railshot performance plan

## Scope

This plan treats the **generated-code-size reduction work as complete** and part of the baseline. Branch relaxation, compaction, objective-aware alignment, byte attribution, and related finalization work are not repeated here.

The completed finalizer remains useful as the last stage of the performance pipeline, but it should stay responsible for:

* Layout.
* Branch/fixup resolution.
* Compaction.
* Islands and veneers.
* Native-PC metadata remapping.

It should **not** grow into a semantic optimizer. Semantic and machine-local optimization should happen before finalization.

## Implementation progress

### 2026-08-14 — immutable optimization selection

The issue #399 compiler lease has been removed from production compilation.
Railshot resolves each architecture's complete optimization selection into an
immutable, bounded `[2]uint64` policy before module work begins. AMD64 and ARM64
carry pre-resolved option tokens through module summaries, worker-owned function
state, lowering, and finalization. The obsolete `Apply`/`ApplySnapshot` path that
temporarily installed selections into backend globals has been deleted.

The acceptance evidence includes opposing-policy concurrent compilation tests
on both architectures, race coverage, deterministic generated-byte comparisons,
and the earlier serial/concurrent benchmarks recorded in
`docs/generated-native-code-size-results.md`. Selection resolution itself remains
zero-allocation. Process-default mutation remains only as a compatibility and
low-level test adapter; it affects subsequently resolved policies and is never
installed for the lifetime of a compilation.

A serialized five-sample Darwin/ARM64 `many_funcs` compile check against the
pre-completion commit measured 262,356 ns/op versus 259,674 ns/op (+1.03%), with
allocations unchanged at 342 allocs/op and B/op effectively unchanged at 138.8
KiB. This remains inside the ordinary local-work compile-time gate.

### 2026-08-14 — local-traffic causality ledger, first slice

The shared opt-in per-function ledger now distinguishes parameter-home stores,
declared-local zero stores, ordinary allocator spills/reloads, structured-merge
stores/reloads, and call-preservation stores/reloads on both backends. Counters
are incremented only at exact emitted frame accesses, remain fixed-size, and are
absent from reports when every value is zero. Code-neutrality tests compare
stats-enabled and disabled output bytes.

A serialized five-sample Darwin/ARM64 `many_funcs` compile check measured a
261,932 ns/op median versus 262,813 ns/op before the ledger (-0.34%, treated as
noise), with 342 allocs/op and approximately 138.8 KiB/op unchanged. This first
slice establishes the baseline for definite assignment, lazy parameter homing,
regional residency, and call-effect work; argument/result moves, directory
derivations, branch counts, and register-bank transfers remain to be attributed.

### 2026-08-14 — reusable call-layout scratch, first #316 slice

AMD64 and ARM64 call-boundary lowering now retain the logical operand layout in
owner-local compiler scratch while `flush` canonicalizes the stack. The common
case uses 64 inline one-byte entries per serial compiler or parallel worker, so
small modules add no heap allocation. Wider stacks share one typed overflow
buffer across functions; capacities above 4 KiB are dropped at the next reset
so one pathological function cannot pin its maximum for the rest of a module.

On a focused Darwin/ARM64 module with eight wide tail-call boundaries, a
serialized three-sample, two-second benchmark reduced compile allocations from
95 to 88 per module and B/op from 60,584 to 59,912. Median compile time was
77,086 ns/op versus 76,458 ns/op (+0.82%). The ordinary small tail-call fixture
remained at 41 allocs/op and did not regress. This is the first deliberately
narrow extraction from #316; local/fact tables, effect summaries, matcher
scratch, and relocation ownership remain separate follow-up slices.

### 2026-08-14 — shared sign-extension provenance, first #438 slice

The AMD64 and ARM64 Valent backends now use one shared one-byte `ValueFacts`
vocabulary. Existing upper-zero and boolean facts are joined by explicit
sign-extension facts for 8-, 16-, and 32-bit producers. Signed loads and Wasm
sign-extension operations establish those facts; target emitters consume them
only for a same-machine-type extension, so an i32-to-i64 extension remains a
tested near miss. Facts survive ordinary materialization and spills without a
side table, and the existing 64-byte storage and 112-byte Valent-node size gates
remain unchanged.

The repository corpus records five `sign-ext-elim` hits across
`tests/regressions/fuzzcases/1797c.wasm` and `1797d.wasm`. Their affected
function bodies shrink by 12 and 8 native bytes respectively on Darwin/ARM64;
module alignment absorbs the first module's total-size reduction. A serialized
three-sample, two-second `many_funcs` compile benchmark measured 247,678 ns/op
versus 247,394 ns/op (+0.11%), with 343 allocs/op unchanged. Exact low-bit
widths, nonzero/alignment facts, structured local-fact joins, and additional
target consumers remain follow-up #438 work.

### 2026-08-14 — immutable integer globals as constants

Defined immutable i32/i64 globals whose initializer is exactly one literal are
now summarized once per module and lowered as ordinary Valent constants on both
backends. The summary is sparse and its single slice header lives in serial or
worker-owned compiler scratch rather than in every `funcHints`; the ARM64
`funcHints` size therefore remains 200 bytes. Modules with no admitted globals
allocate no summary storage. Imported globals, mutable globals, references,
floats, type-mismatched initializers, and extended constant expressions remain
conservative near misses.

The repository corpus records 28 `immutable-global-fold` hits across 16 of
1,333 checked-in Wasm modules. Every hit module shrinks on Darwin/ARM64: 404
native bytes in total, with individual module reductions from 4 to 52 bytes.
Positive i32/i64 cases execute through the native ARM64 harness, and a mutable
global is a tested codegen near miss. A serialized three-sample, two-second
`many_funcs` compile benchmark measured a 249,793 ns/op median versus 252,596
ns/op at the preceding commit (-1.11%, treated as noise), with 343 allocs/op
unchanged and B/op effectively unchanged near 138.9 KiB.

The accompanying rematerialization audit found that Railshot already retains
constants, deferred local/global reads, and deferred addresses symbolically
until their first sink. A new general recipe layer is therefore deferred until
spill attribution identifies a value that is currently materialized and stored
despite being cheaper and semantically safe to reconstruct.

### 2026-08-14 — entry-overwritten parameter homes

The existing one-pass entry-prefix proof now applies to unpinned parameters as
well as declared-local zeroing. When a parameter's first access is a
`local.set` or `local.tee` before any structured-control edge, AMD64 and ARM64
skip its canonical frame-home store. Reads before the write remain a tested
near miss. The proof stays bounded to the existing 64-local mask and is disabled
by the established EH and exact GC-root-plan gates; it adds no summary storage,
body scan, retry, or execution allocation.

The repository corpus records 22 `entry-param-home-elide` hits: 20 in
`bench/corpus/ruby.wasm` and two in `bench/corpus/lua.wasm`. Ruby shrinks by 80
native bytes on Darwin/ARM64 with unchanged spill high-water marks; Lua's two
removed stores are absorbed by existing module alignment. A serialized
three-sample, two-second `many_funcs` compile benchmark measured a 252,713 ns/op
median versus 252,897 ns/op at the preceding commit (-0.07%, treated as noise),
with 343 allocs/op and approximately 138.9 KiB/op unchanged.

### 2026-08-14 — never-read local initialization

The same one-pass local-access scan now records a temporary 64-bit `local.get`
mask. At the end of the scan, locals with no reads are marked as having an
unobservable initial value. This removes parameter-home stores and declared-local
zeroing even when no entry-prefix write exists. The mask lives only in the stack
scanner, so `funcHints` remains 200 bytes on ARM64 and ordinary compilation adds
no allocation. AST-only functions and locals beyond index 63 keep the established
eager fallback; EH and exact GC-root plans continue to clear the optimization.

Across 1,333 checked-in Wasm modules, the broader proof records 2,440 parameter
home eliminations in 24 modules, up from 22 at the preceding commit, plus 170
additional declared-local initialization eliminations. Every affected module
shrinks on Darwin/ARM64, by 11,256 native bytes in total; the largest reduction
is 5,792 bytes in `bench/corpus/script.wasm`. A serialized three-sample,
two-second `many_funcs` compile benchmark measured a 249,592 ns/op median versus
251,990 ns/op (-0.95%), with 343 allocs/op and B/op unchanged.

### 2026-08-14 — bounded overwrite-aware regional eviction

Regional integer residency now copies the active forward Wasm reader and scans
at most 64 operations when it must release a register. If an active local's next
access is an overwrite rather than a read, that local is preferred and its dirty
old value is not stored to the frame. Any structured-control boundary, malformed
immediate, exhausted fuel, pending memory-address borrow, or pending Valent read
falls back to the existing score-based store-and-evict path. The active reader is
stored inline in reusable function state; a rejected pointer-based prototype was
not retained because escape analysis raised `many_funcs` from 343 to 644
allocations/op.

The repository corpus records 159 `interval-region-dead-evict` hits across
`ruby.wasm`, `blake3sum.wasm`, `blake-as.wasm`, and `blake-as-simd.wasm`.
Darwin/ARM64 native code shrinks by 336 bytes total (320 in Ruby and 16 in
blake3sum); the other module totals are alignment-neutral, and spill high-water
marks are unchanged. Five-second execution medians measured blake-as at 397,135
ns/op versus 396,693 ns/op (+0.11%) and blake-as-simd at 413,469 ns/op versus
415,788 ns/op (-0.56%), both with zero B/op and allocations. The standard
`many_funcs` compile median was 248,779 ns/op versus 249,127 ns/op (-0.14%),
with 343 allocs/op and B/op unchanged.

### 2026-08-14 — bounded ARM64 loop-summary ownership

ARM64 loop residency and bounds-hoist admission no longer allocate one map or
scan an unbounded body at every reachable loop header. Exact set-local facts now
occupy a 64-entry, 512-byte arena owned by the serial compiler or parallel
worker; each entry also holds saturated local get/set counts for residency
scoring. Nested frames hold only range descriptors and rewind the arena when
they close. The copied reader has independent 1,024-operation and 16 KiB limits,
and large immediate vectors are capped separately. Exhaustion, malformed input,
or structured EH discards partial facts, marks every loop effect conservatively,
and disables both residency and invariance proofs for that loop.

Cap tests cover operation fuel and local-arena exhaustion, reader restoration,
partial-fact removal, and conservative flags. Existing admitted `fib_iter` and
`sieve` functions produce byte-for-byte equivalent code and identical explain
counters. Five-sample Darwin/ARM64 compile medians measured `fib_iter` at 7,405
ns/op versus 7,334 ns/op (+0.97%), while allocations fell from 37 to 35 and B/op
fell from 17,375 to 17,240. `sieve` measured 12,834 ns/op versus 13,651 ns/op
(-5.98%), with allocations falling from 52 to 44 and B/op from 31,568 to 31,056.
A rejected 256-entry version was not retained because its 1 KiB fixed arena
raised small-module compile B/op by roughly 6.6%.

### 2026-08-14 — measured ARM64 loop-residency scoring

The opt-in ARM64 loop region now chooses at most two integer locals by saturated
static get+set count, with stable local-index tie-breaking, instead of choosing
the first set locals. Admission is limited to functions with at most eight
locals; a measured 10-local `json-as-simd.deserializeN` case regressed by roughly
7% when X12/X13 were reserved despite unchanged spill counts. Explain mode now
attributes pin count, entry loads, branch-edge stores, exit stores, and score
buckets, while normal compilation retains the nil-stats path.

The narrower policy preserves the high-value `utf-as-simd` result: serialized
Darwin/ARM64 medians move from 57,212 to 42,944 ns/op for `convertN` (-24.9%) and
from 142,320 to 19,846 ns/op for `validateN` (-86.1%). Both return the same
results with the policy disabled and enabled (`710400` and `200`) and remain at
zero B/op and allocations. Comparable `json-as-simd` medians improve by 1.0%
for serialize and 0.9% for deserialize. Ordinary `json-as` serialize improves
0.9%, but deserialize remains about 1.5% slower; therefore loop residency stays
opt-in while trip-count/amortization admission is unresolved rather than being
enabled globally on the strength of the UTF result.

With the option disabled, all checked-in benchmark-corpus module sizes remain
identical to the preceding commit. A three-sample `many_funcs` compile median
was 252,370 ns/op versus 248,919 ns/op (+1.39%), with allocations unchanged at
343/op and B/op effectively unchanged near 138.9 KiB.

### 2026-08-14 — bounded ARM64 call-effect summaries, first slice

The existing ARM64 module scan now records compact direct `memory.grow` and
`global.set` effects plus same-module direct-call edges. A shared reverse
worklist computes transitive effects through recursive call cycles in
`O(functions + direct calls)` time per effect bit. The graph is capped at 2,048
functions and 4,096 calls; cap exhaustion and larger modules conservatively
classify every call-making function while retaining exact leaf effects. Tests
exercise recursive propagation, malformed edges, both caps, and the conservative
fallback. Imported, indirect, reference, AST-only, and custom-instruction calls
remain fully conservative.

ARM64 register-ABI direct calls use the summary to retain an existing explicit
linear-memory bounds certificate when the callee transitively cannot grow
memory. Certificates derived from mutable globals additionally require a
no-global-write proof, and a call result targeting the source local remains a
near miss. The optimization has an immutable per-compilation
`call-effect-bounds` option and `WAGO_ARM64_NO_CALL_EFFECT_BOUNDS=1` rollback
switch; guard mode and modules without memory allocate no effect state.

The benchmark corpus records 2,168 preserved certificates across 19 modules.
Darwin/ARM64 native code falls by 5,120 bytes in total; alignment absorbs some
individual removals. The large-graph fallback kept SQLite compile overhead to
about 13 KiB/op (+0.26%) and two allocations, versus a rejected unbounded
prototype's roughly 397 KiB/op increase. Median SQLite backend time was 50.12 ms
enabled versus 49.56 ms disabled (+1.1%, treated as noise). Reverse-order
raytrace execution measured 259.8 us/op enabled versus 259.4 us/op disabled
(+0.15%), and the measured JSON/JSON-SIMD rows remained within approximately
1.3%; all retained zero execution allocations.

### 2026-08-14 — finite ARM64 internal ABI classes, first slice

ARM64 direct-call metadata now stores a one-byte finite class rather than a
free-standing preservation boolean. The first two contracts are `General` and
`LeafScalar`. The established call-free integer leaf class remains intact, and
effect-safe memory-touching leaves may now join it when they have no declared
locals, global access, nested calls, or `memory.grow`. The callee reserves the
caller's pin bank and keeps parameters in incoming registers, so the caller can
avoid pinned-local/global preservation and post-call reload work. Every rejected
shape uses `General`; `WAGO_ARM64_NO_ABI_CLASSES=1` and the immutable
per-compilation `abi-classes` option restore the narrower admission policy.

Focused codegen tests prove the memory-leaf class removes the caller's
preservation store/reload pair, and native execution tests compare enabled and
disabled code through the real ARM64 wrapper. The corpus contains 1,544 admitted
memory leaves across 14 modules. Thirteen module images shrink, by 11,184 native
bytes in total, and none grows. A serialized five-sample benchmark with sixteen
calls to one admitted leaf measured 21.68 ns/op versus 22.19 ns/op (-2.3%), with
zero B/op and allocations. `many_funcs` compile B/op and allocations remain
unchanged; median time moved from 248,205 to 248,999 ns/op (+0.32%). SQLite also
kept identical B/op and allocations, with median backend time moving from 51.14
to 50.94 ms (-0.39%).

### 2026-08-14 — shared call effects and AMD64 bounds-state parity

The bounded call-effect collector now lives in `railshot/shared` and both target
scanners feed it during their existing module pass. Each backend keeps the same
2,048-function and 4,096-edge caps, transitive recursive propagation, exact leaf
fallback, and conservative treatment of imported, indirect, reference, AST-only,
and custom-instruction calls. Shared tests own propagation and cap behavior;
target tests prove each scanner's transitive `memory.grow` and `global.set`
classification.

AMD64 direct register-ABI calls now snapshot the fixed eight-entry bounds
certificate set and restore it only when the local callee transitively cannot
grow memory. A fused result drops the overwritten local's certificate, while a
callee that may write globals drops every global-derived certificate and retains
safe local-derived entries. `WAGO_AMD64_NO_CALL_EFFECT_BOUNDS=1` and the shared
immutable `call-effect-bounds` option retain blanket call invalidation.

Native Linux/AMD64 measurements on a Ryzen 7 7800X3D record 3,076 preserved
certificates across 19 corpus modules and 8,736 fewer native bytes. No measured
module grows. A serialized seven-sample benchmark with sixteen safe calls
measured 23.14 ns/op enabled versus 24.01 ns/op disabled (-3.6%), with zero B/op
and allocations. Reverse-order corpus checks were neutral to favorable:
raytrace measured 386.68 us/op versus 389.84 us/op (-0.81%), and JSON serialize
measured 23.68 us/op versus 23.91 us/op (-0.96%). `many_funcs` compile time,
B/op, and allocations were flat; SQLite added approximately 3 KiB/op (+0.05%)
and one median allocation while backend time moved from 82.18 to 82.06 ms.

---

# 1. North-star architecture

The target architecture is:

```text
Wasm module
    │
    ├── compact module/function summary scan
    │     local use and last-use summaries
    │     call graph and effect summaries
    │     loop/region candidates
    │     branch hints
    │     definite-assignment information
    │     optimization admission and scratch sizing
    │
    └── one forward Wasm lowering traversal
          │
          ├── Valent expression trees
          ├── packed value/provenance facts
          ├── rematerialization recipes
          ├── regional register residency
          ├── effect-aware calls
          ├── generated target-rule selection
          └── bounded symbolic machine window
                    │
                    ▼
          completed bounded finalizer
          branch/layout/relocation/compaction
                    │
                    ▼
               executable code
```

“Single-pass” means:

> One semantic traversal of the Wasm body, with bounded pending state and a relocation-like finalizer.

It does **not** mean every opcode must irreversibly emit its final instruction bytes immediately.

## Non-negotiable constraints

1. **No production whole-function SSA or general machine IR.**
2. **No retained CFG for ordinary compilation.**
3. **No whole-function live intervals or graph-coloring allocator.**
4. **No global PRE, general LICM, trace scheduling, or online e-graph.**
5. **No per-operation heap allocation.**
6. **Every bounded optimizer has a conservative fallback.**
7. **Serial compilation remains the predictable-memory mode.**
8. **Steady-state execution remains zero-allocation where it is currently zero-allocation.**
9. **Native-code growth remains budgeted because larger code also consumes memory and I-cache.**
10. **Semantic facts are shared; target instruction choices remain architecture-specific.**

The intended compiler workspace remains:

```text
module summaries
+ worker count × bounded worker scratch
+ O(locals + control depth + configured region capacity)
```

---

# 2. Current starting point

Railshot is already beyond an ordinary baseline compiler. The recently merged performance campaign added bounded ARM64 SIMD and conversion peepholes, removed redundant AMD64 memory32 work, reused substantial compiler scratch, and preserved zero execution allocations. Its reported AMD64 comparison showed Wago ahead of wazero on both compile and execution geometric means in that test matrix.

There are already two especially important precedents for this plan:

* AMD64 regional residency caches up to nine integer locals, but only for call-free, control-free functions within tightly bounded body/local limits.
* Direct-call liveness already uses a copied Wasm reader with a hard 64-operation fuel limit and two register masks to avoid unnecessary pre-call local stores.

Those prove the core strategy:

```text
small bounded lookahead
+ compact facts
+ safe fallback
= meaningful execution gains without a compiler architecture reversal
```

Several prerequisites remain open on current main:

* Optimizer settings still need to become immutable per-compilation state rather than architecture-global mutable bindings.
* Compiler arenas, dense tables, generation counters, combined scans, and effect summaries remain an explicit open workstream.
* Generalized upper-bit/value provenance remains open.
* ARM64 still lacks several proven AMD64 WasmGC native fast paths.

These should form the beginning of the critical path.

---

# 3. The coherent dependency graph

```text
P0: Measurement + immutable policy + scratch ownership
        │
        ├──────────────┐
        ▼              ▼
P1: Semantic facts   Offline rule/verifier foundation
    + rematerialize      │
        │                │
        ├──────────┬─────┘
        ▼          ▼
P2: Regional     P3: Call effects
    residency        + internal ABI classes
        │          │
        └─────┬────┘
              ▼
P4: Generated Valent selection
    + producer-linked machine combiner
              │
        ┌─────┴──────────┐
        ▼                ▼
P5: Target campaigns   P6: Structured transforms
    AMD64/ARM64/SIMD       if-convert/shrink-wrap
    WasmGC/bulk memory     frame/fence/poll placement
        │                │
        └───────┬────────┘
                ▼
P7: Profile-guided and research experiments
```

The order matters:

* Regional allocation without reliable facts causes spills and miscompilations.
* Call optimization without effect summaries forces conservative invalidation.
* A machine combiner without provenance cannot safely eliminate extensions or masks.
* Target pattern expansion without a rule/verifier framework becomes unmaintainable.
* Structured motion without stable effects and trap facts is too risky.

---

# 4. Phase 0 — establish the control plane

## 4.1 Rebaseline the remaining gaps

The existing broad performance issue contains numbers from before several major runtime and compiler improvements. Replace it with a current matrix that separates:

| Dimension    | Required splits                                          |
| ------------ | -------------------------------------------------------- |
| Architecture | AMD64, Darwin ARM64, Linux ARM64                         |
| Bounds       | Explicit, signal/guard                                   |
| API boundary | Ordinary invoke, prepared invoke, direct prepared        |
| Calls        | Direct, indirect, `call_ref`, host, cross-instance, tail |
| Workload     | Scalar, FP, SIMD, branch, local-heavy, memory, GC        |
| Lifetime     | Fresh instance, warmed instance, sustained instance      |
| Compilation  | Decode, validate, backend, full compile                  |
| Memory       | B/op, allocs/op, live heap, peak RSS, worker scratch     |

The goal is not another aggregate leaderboard. It is to identify the dominant remaining cost for each losing row:

```text
frame traffic
register moves
host boundary
call preservation
branching
bounds/address setup
SIMD synthesis
GC transition
collector work
I-cache
```

## 4.2 Add a performance-causality ledger

Extend opt-in statistics with per-function counts for:

```text
parameter-home stores
declared-local zero stores
ordinary spill stores/reloads
control-merge stores/reloads
call-preservation stores/reloads
call argument moves
call result moves
local-region cache hits/misses/evictions
rematerializations
memory base/size derivations
global/table directory derivations
bounds instructions
conditional and unconditional branches
host transitions
GC helper transitions
native GC stub calls
GP↔FP/vector transfers
SIMD shuffle/extract/insert operations
```

Every stack access should have a cause. “967 stack accesses” is actionable only when the report says:

```text
420 local-frame reloads
180 call-preservation reloads
140 merge canonicalizations
125 parameter homes
102 ordinary spills
```

Stats remain absent from normal compilation unless requested.

## 4.3 Remove process-global optimizer state

Complete issue #399 before introducing many more policies and experimental features.

Materialize one immutable backend policy:

```go
type BackendPolicy struct {
    Objective OptimizationObjective
    Effort    CompileEffort
    Features  TargetFeatures
    OptBits   [2]uint64

    RegionBudget      uint16
    MachineWindowSize uint8
    RewriteFuel       uint16
    MaxWorkers        uint8
}
```

The completed code-size objectives remain the product-level policy. Add only one orthogonal compiler-effort dimension:

```text
Minimal
Balanced
Throughput
```

Do not expose every region/window/fuel constant publicly.

This removes same-architecture compile serialization, makes concurrent A/B testing safe, and gives later optimizations an immutable policy source. Current architecture-global bindings serialize whole compilations and make independent settings fragile.

## 4.4 Complete the required subset of reusable scratch

Issue #316 should be divided rather than implemented as one enormous PR.

The first subset should provide reusable storage for:

```text
local facts
local next-use summaries
region descriptors
call-effect summaries
machine-window operations
rule matcher scratch
relocations and metadata marks
```

Use:

* Dense slices indexed by local/function/memory/table index.
* Generation counters where clearing dominates.
* Fixed inline storage for common tiny functions.
* Owner-local reuse on the serial path.
* Worker-owned scratch under parallel compilation.
* Capacity trimming after pathological functions.

Do not use one global `sync.Pool`.

## Phase 0 exit criteria

```text
same config + same module → deterministic bytes under concurrent compilation
same-architecture independent compiles no longer serialize
all performance counters have zero cost when disabled
one combined summary scan owns new summaries
pathological cap tests show bounded scratch
```

---

# 5. Phase 1 — build the semantic fact layer

This is the foundation for almost every later optimization.

## 5.1 Packed value provenance

Implement issue #438 as one architecture-neutral fact system.

A practical first representation:

```go
type ValueFacts uint16

const (
    FactUpper32Zero ValueFacts = 1 << iota
    FactSignExt8
    FactSignExt16
    FactSignExt32
    FactBoolean
    FactNonZero
    FactKnownAligned2
    FactKnownAligned4
    FactKnownAligned8
    FactImmutable
    FactNonNullRef
    FactExactRefType
    FactFreshRef
)
```

The exact representation can use packed fields rather than independent flags, but it should remain inline in existing value/storage state.

### Producers

Facts can originate from:

* Constants.
* Integer comparisons.
* `i32` arithmetic with known native-width behavior.
* Signed and unsigned loads.
* Explicit extensions/truncations.
* Known local frame loads.
* Known host/import result contracts.
* Immutable globals.
* GC constructors and casts.

### Joins

At a structured merge:

```text
result facts = intersection of reachable predecessor facts
```

For exact types:

```text
same exact type on every predecessor → retain exact type
otherwise → retain only common super-facts
```

### Invalidation

Drop or weaken facts across:

* Unknown host calls.
* Cross-instance calls without a contract.
* GC safepoints for raw resolved addresses.
* Physical spills that lose width information.
* Partial-register writes.
* Opaque plugin operations.
* Mutable loads and stores where relevant.

GCC’s combine and redundant-extension work rely on explicit known-bit/sign information rather than assuming native cleanliness from the source-language type.

## 5.2 First-class rematerialization

Introduce bounded recipes for values cheaper to recompute than spill and reload:

```go
type RematKind uint8

const (
    RematNone RematKind = iota
    RematConstant
    RematZeroExtend
    RematSignExtend
    RematBaseDisp
    RematScaledAddress
    RematImmutableGlobal
    RematMemoryDirectory
    RematTableDirectory
    RematGlobalDirectory
    RematCanonicalTypeID
)
```

Recipes must have fixed maximum depth, initially two.

A value may be evicted without a store when:

```text
rematerialization cost
<
store + future reload + frame growth cost
```

### Initial safe recipes

* Small integer constant.
* Zero.
* Sign/zero extension.
* `base + constant`.
* `base + index × scale + constant`.
* Immutable numeric global.
* Instance-stable directory pointer.
* Canonical type/layout ID.

### Exclusions

Do not rematerialize:

* Mutable memory loads.
* Trapping operations.
* Division/remainder.
* Atomics.
* GC handle resolution across collection.
* Mutable globals.
* Uncontracted host values.
* FP arithmetic where repeated evaluation could change Wago’s chosen observable NaN behavior.

## 5.3 Definite-assignment local initialization

During the existing summary scan, determine which declared locals may be read before a definite write.

At function entry:

```text
zero only may-read-before-write locals
```

Use structured bitsets:

```text
definitely written = intersection at merges
read before write   = union at merges
```

This removes prologue stores, reduces touched frame pages, and creates more frame-elision candidates.

## 5.4 Lazy parameter homing

Incoming parameters should begin as virtual locations backed by argument registers:

```text
stIncomingArg
```

Only store a parameter to its canonical frame slot when:

* Its register is needed for another value.
* It remains live across a clobbering call.
* A structured merge requires canonical storage.
* It must be published as a GC root.
* EH or host reentry requires a stable frame representation.

A parameter consumed once before any boundary should never touch the frame.

## 5.5 Immutable numeric global folding

Defined immutable numeric globals whose initializer is compile-time constant should become ordinary Valent constants.

Do not fold imported globals or reference values whose instance identity matters.

## 5.6 Generalize result sinking

Extend existing direct sinks to:

```text
call → integer local
call → FP local
call → vector local
call → reference local
call → global.set
call → memory store
call → return
call → next call argument
```

Do not force a call result through a canonical spill slot when its first consumer can use the ABI result register directly.

## Phase 1 exit criteria

```text
ordinary compilation allocation-neutral
facts survive positive cases and disappear on every near miss
random full-width differential tests pass
parameter-home and local-zero stores fall materially
extension/mask instructions fall on real corpus functions
no new ordinary spills
```

---

# 6. Phase 2 — regional register residency 2.0

This is the largest likely compiler-side execution win.

Current AMD64 regional residency is intentionally narrow: call-free, control-free, no EH, bounded body/local counts, integer locals, and at most nine active cache registers.

Expand it in five controlled steps.

## 6.1 Improve current eviction before broadening admission

Current selection is based largely on static local score. Replace it with a bounded next-use model.

Eviction order:

1. Dead local.
2. Local overwritten before next read.
3. Rematerializable local.
4. Clean local already synchronized to its frame slot.
5. Farthest next use.
6. Lowest loop-weighted remaining use density.
7. Cheapest reload.
8. Lowest target encoding benefit.

Do not cache:

* Known zero/constant values that are cheap to rematerialize.
* A local with one imminent final use.
* A local whose memory form already folds efficiently on AMD64.
* A local whose residency introduces a fixed-register conflict.
* A local whose register prevents a more valuable vector or address value.

## 6.2 Add FP and vector region banks

Maintain independent admission and pressure for:

```text
GP local cache
FP scalar local cache
v128 local cache
```

A function should be able to use GP regional residency without consuming vector registers, and vice versa.

Prioritize vector locals that are:

* Loop-carried.
* Used by repeated arithmetic/shuffle chains.
* Expensive to reload.
* Transferable directly into a vector result or store.

## 6.3 Multiple straight-line regions

Divide a function at hard barriers:

```text
call
control merge
safepoint
EH edge
atomic/fence
memory.grow
opaque plugin operation
```

The summary scan records only the top bounded set of region candidates:

```go
type RegionHint struct {
    StartPC      uint32
    EndPC        uint32
    WeightedUses uint32
    Flags        uint16
}
```

A call-making function can then have:

```text
uncached prefix
hot cached region
call
second cached region
return
```

without attempting to maintain arbitrary state across the call.

## 6.4 Simple loop residency

Admit one structured natural-loop class first:

```text
one loop header
one backedge
no nested loop
no EH
no indirect control
no host or foreign call
bounded exits
```

At loop entry:

* Load selected loop-carried locals once.
* Retain them across the backedge.
* Write dirty values only on exits where they remain live.
* Transfer dying values directly to consumers.

This uses the existing structured control stack, not a reconstructed CFG.

## 6.5 Small branch-local residency

For bounded `if` regions:

* Snapshot selected regional ownership.
* Allow each arm to use free registers independently.
* At merge, preserve only identical assignments.
* Synchronize divergent values through canonical storage.
* Intersect semantic facts.

Do not attempt arbitrary phi allocation.

## 6.6 Structured stack-slot overlay

Integrate lexical stack-slot reuse while building regional state.

Safe first cases:

```text
then scratch ↔ else scratch
sequential inlined-callee locals
call argument scratch ↔ call result scratch
mutually exclusive cold helper paths
region-specific temporary slots
```

At an `if`:

```text
save entry slot high-water
compile then
rewind
compile else using same slots
frame requirement = max(then, else)
```

Begin with non-reference scalar temporaries. Reference-slot overlay should land only after native root maps prove exact live meaning at every safepoint.

## Phase 2 target

For local-heavy targeted functions:

```text
local/frame memory traffic: 35–60% lower
targeted execution: 7–15% faster
whole remaining-gap suite: 1–3% faster
weighted full compile: <=3% slower
peak RSS: <=max(1 MiB, 2%)
```

---

# 7. Phase 3 — calls, ABI, and runtime boundaries

After frame traffic, call boundaries are the next major source of fixed work.

## 7.1 Compact function-effect summaries

The combined summary scan should produce:

```go
type FuncEffects uint32

const (
    EffectCallsHost FuncEffects = 1 << iota
    EffectCallsForeign
    EffectCallsIndirect
    EffectWritesMemory
    EffectGrowsMemory
    EffectWritesGlobals
    EffectWritesTables
    EffectAllocatesGC
    EffectCollectsGC
    EffectUsesEH
    EffectUsesAtomics
)
```

Compute transitive summaries over direct-call SCCs:

* Acyclic functions union direct-callee effects.
* Recursive SCCs union all member effects.
* Unknown indirect/host calls become conservative.
* Large edge sets may fall back to conservative summaries.

This is `O(functions + direct calls)` metadata, not function IR. Issue #316 already identifies call-effect summaries as part of the compiler-memory roadmap.

## 7.2 Preserve state across safe direct calls

A caller can retain:

| Callee lacks effect            | Caller may preserve                     |
| ------------------------------ | --------------------------------------- |
| `GrowsMemory`                  | memory-size cache                       |
| `WritesGlobals`                | numeric global facts/values             |
| `WritesTables`                 | immutable table/type facts              |
| `AllocatesGC` and `CollectsGC` | non-raw GC semantic facts               |
| `CallsHost`/`CallsForeign`     | lightweight execution context           |
| `UsesEH`                       | simpler callsite state                  |
| all observable effects         | pure expression/rematerialization state |

This should substantially reduce blanket invalidation.

## 7.3 Finite internal ABI classes

Do not create arbitrary per-function calling conventions initially.

Use a finite set:

```text
General
LeafScalar
LeafFP
LeafVector
TinyDirect
GCNonCollectingLeaf
```

Each class specifies:

* Argument registers.
* Result registers.
* Scratch registers.
* Preserved pin bank.
* Whether memory/global/GC context remains valid.
* Whether a frame, fence, or interrupt check is required.

The summary scan proposes a class. Compilation under a restricted class may retry once using `General` if register pressure exceeds the contract.

This captures the useful part of interprocedural register allocation without whole-program coloring.

## 7.4 Register ABI v2

Extend internal results to bounded banks:

```text
GP:     RAX, RDX / X0, X1
FP:     XMM0, XMM1 / V0, V1
vector: XMM0 / V0
reference: GP result with ownership metadata
```

Start with at most four scalar results and at most two per register bank. Wider signatures retain the existing fallback.

After a call, results remain symbolic physical locations rather than being immediately written to result slots.

## 7.5 Multi-result structured merges

Support a bounded merge descriptor:

```go
type MergeLocations struct {
    Count uint8
    Loc   [4]PhysicalLocation
}
```

Use parallel copies on edges. Fall back to slots for:

* Larger result sets.
* Complex mixed references.
* EH-sensitive shapes.
* Unresolvable cycles under the bounded move resolver.

## 7.6 Call-surviving pure expressions

A pending Valent expression may survive a call when it consists only of:

* Constants.
* Caller-local reads.
* Pure nontrapping integer operations.
* Immutable globals.
* Rematerializable context values.

Before the call, release any physical registers and retain only the bounded symbolic recipe.

Do not retain:

* Loads.
* Mutable globals.
* References requiring root publication.
* Division/remainder.
* Trapping conversions.
* FP operations with problematic recomputation semantics.

## 7.7 Native-byte- and pressure-aware inlining

Retain the existing bounded leaf inliner, but evaluate:

```text
removed:
  call instruction
  argument moves
  pre-call stores
  post-call reloads
  result moves
  callee frame/check work

added:
  callee instructions
  parameter binding
  local initialization
  caller frame growth
  caller spill pressure
  lost regional residency
  lost frame elision
```

Strongly favor inlining when it:

* Makes the caller call-free.
* Enables regional residency.
* Enables frame elision.
* Removes a single-use callee body.
* Exposes constants or exact GC facts.

Strongly penalize it when it:

* Disables a successful local cache.
* Introduces new spills.
* Duplicates a multi-caller body.
* Adds a call to a previously call-free function.

## 7.8 Prepared entry families

Generalize the successful direct prepared integer path through finite static trampolines:

```text
small scalar FP
small mixed GP/FP
v128 unary/binary
bounded multi-result
safe exact reference signatures
```

Do not dynamically generate one trampoline per signature.

Also add an explicit batch API for repeated tiny calls:

```text
enter foreign stack once
invoke N times
check trap between iterations
write caller-provided results
allocate nothing
```

## 7.9 Host effect classes

Explicit host registration can declare:

```text
may reenter
may grow memory
may mutate globals/tables
may allocate or collect GC
may block
numeric-only
```

The default is fully conservative.

A numeric, non-reentrant leaf host import can then use a narrow signature trampoline that:

* Saves only the required registers.
* Skips unnecessary root publication.
* Synchronizes only state the host can observe.
* Returns directly into internal ABI result registers.

Clang’s lowering benefits from carrying function memory effects, non-null facts, no-alias facts, and calling-convention information explicitly. Wago should use a much smaller semantic subset for internal and registered host calls.

## 7.10 Cross-instance specialization

Classify calls at instantiation:

```text
same instance
same runtime and same memory
same runtime, different memory
same collector domain
different collector domain
host wrapper
```

Prevalidate immutable identity and install:

* Direct target.
* Target basedata.
* Memory context.
* Collector-domain classification.
* Signature class.

Avoid rediscovering those properties at every call.

## Phase 3 target

```text
call-adjacent local stores/reloads: >=30% lower
small direct-call workloads: 10–25% faster
mixed/multi-result call workloads: 10–30% faster
ordinary call correctness unchanged
zero execution allocation retained
```

---

# 8. Phase 4 — generated selection and a bounded machine combiner

This is where the external compiler research should be incorporated systematically.

## 8.1 Wago rule schema

Create one source of truth for local target rules.

Each rule should declare:

```text
name
semantic input pattern
types
required value facts
effects and trap constraints
target features
physical-register constraints
output machine pattern
clobbers
cost
proof specification
```

Example:

```text
match:
  add(base, shift(index, k))

require:
  k in 1..3
  both values pure and nontrapping

target:
  scaled address instruction

cost:
  latency
  uops
  bytes
  register need
```

## 8.2 Generated decision trees

Cranelift’s ISLE compiles typed rewriting rules into ordinary efficient generated code, merging overlapping patterns into a shared decision tree.

A Wago-specific generator should output:

* Plain Go switches and conditionals.
* Rule IDs.
* Explain counters.
* Target predicates.
* Positive tests.
* Near-miss tests.
* Verification inputs.

No rule parser, map, reflection, or solver is present in production.

## 8.3 Offline formal verification

Model this after VeriISLE:

* Verify rule chains offline.
* Use bitvector semantics for integer rules.
* Model traps and state explicitly.
* Add target instruction semantics.
* Treat FP equivalence separately from bitwise integer equivalence.
* Use authoritative ISA semantics where practical.

VeriISLE demonstrates modular SMT verification of lowering rules and derives AArch64 semantics from Arm’s machine-readable specification.

Start with:

1. Pure integer rules.
2. Integer flags and carry.
3. Trapping integer operations.
4. SIMD bitwise rules.
5. Memory rules with one explicit region.
6. FP.
7. GC operations.

## 8.4 Fixed-capacity symbolic machine window

Use a reusable fixed array:

```go
const balancedMachineWindow = 24

type MachineOp struct {
    Op        uint16
    Dst       PhysicalLocation
    Src       [3]Operand
    Producer  [3]uint8
    Uses      uint8
    Effects   EffectMask
    Facts     ValueFacts
    WasmPC    uint32
}
```

Flush at:

```text
branch target
control merge
call
safepoint
trap-order barrier
EH edge
atomic/fence
opaque plugin output
capacity exhaustion
```

## 8.5 Producer-linked combination

Do not scan all pairs of machine operations.

For each consumer:

1. Follow direct producer links.
2. Explore at most four producer levels.
3. Query generated rules for that root operation.
4. Stage the replacement.
5. Validate effects, flags, physical constraints, and cost.
6. Commit transactionally.
7. Revisit only affected producers and users.

GCC’s combiner operates primarily on short, actual producer-consumer chains and substitutes earlier definitions into later users rather than performing arbitrary global search.

GCC’s newer late combine similarly stages definition substitution, validates all uses, and commits only after the complete transformation is legal.

## 8.6 Cost model

Use a small lexicographic cost, not one floating-point score:

```go
type SelectionCost struct {
    NewSpills    uint8
    CriticalPath uint8
    Uops         uint8
    Moves        uint8
    RegNeed      uint8
    Bytes        uint16
}
```

For Speed:

```text
new spills
critical path
uops/resource depth
moves
bytes
```

For Balanced:

```text
new spills
critical path/uops
moves
bytes
```

LLVM’s machine combiner explicitly checks critical-path and processor-resource depth rather than accepting every instruction-count reduction.

## 8.7 First rule families

Implement in this order:

1. Move-to-self and copy-chain deletion.
2. Cross-register-bank copy collapse.
3. Proven extension/mask elimination.
4. Load plus extension plus consumer.
5. Store-slot followed by load-slot forwarding.
6. Call-result to direct sink.
7. Compare/result/branch collapse.
8. Arithmetic-produced flags to branch/select.
9. Shift/add/address combinations.
10. Wide add/sub carry chains.
11. High-half multiply.
12. ARM64 address-update forms.
13. Target-feature-specific alternatives.

LLVM’s machine peephole work focuses on extensions, compares, foldable loads, and copy/bitcast chains—the same initial families.

## 8.8 Multi-consumer condition tokens

Retain one condition for at most three consumers:

```go
type ConditionToken struct {
    Producer  uint8
    Condition Cond
    Flags     FlagMask
    UsesLeft  uint8
}
```

Consumers can include:

* Branch.
* `select`.
* Boolean local.
* Boolean store.
* Carry/borrow chain.
* Wide comparison.

At an uncertain flags clobber, materialize the boolean.

GCC’s compare-elimination implementation uses a practical cap of three understood condition consumers, enough for common sign/equality and wide-comparison cases.

## 8.9 Bounded post-allocation renaming

Within the machine window, rename one physical def-use chain when it:

* Breaks a false dependency.
* Avoids partial-register hazards.
* Avoids a fixed-register conflict.
* Saves AMD64 REX prefixes without introducing moves.
* Enables ARM64 pair operations.
* Reduces cross-bank copies.

Never rename across calls or branch targets.

GCC’s register renamer builds local def-use chains and selects physical replacements based on conflicts and target preferences; Wago needs only a bounded local subset.

## Phase 4 target

```text
machine-window miss path: effectively neutral
no per-op allocation
ordinary window scratch fixed and reusable
rule fuel exhaustion falls back correctly
targeted sequences reduce instructions or dependency depth
whole remaining-gap suite improves without extra spills
```

---

# 9. Phase 5 — target-specific campaigns

After the shared infrastructure lands, optimize AMD64 and ARM64 according to their actual bottlenecks rather than forcing symmetry.

## 9.1 AMD64

Priority order:

### Fixed-register-aware selection

Improve operations involving:

```text
RAX/RDX multiply and divide
RCX variable shifts
BMI2 shifts and rotates
carry/borrow chains
byte-register constraints
```

Choose target features at compile time through an immutable feature bitset.

### Wide arithmetic

Recognize expanded wide-arithmetic idioms and lower directly to:

```text
ADD / ADC
SUB / SBB
MUL/IMUL high-half forms
BMI2 MULX where profitable and available
```

Keep carry as a condition token rather than an integer between limbs.

### Memory-operand selection

Use exact costs for:

```text
explicit load + ALU
versus
ALU with memory operand
```

Reject memory-destination read-modify-write forms when they lengthen a hot dependency chain, even if they are shorter.

### Physical-register encoding bias

Use low-register preference only as a final tie-breaker after:

```text
spill count
move count
critical path
```

### Bulk memory

Rebaseline and retune:

```text
tiny constant inline sequence
small dynamic vector loop
medium REP/vector path
large ERMS/FSRM path
backward overlap path
```

## 9.2 ARM64

### WasmGC native parity first

Complete issue #317 before adding broad new GC optimizer machinery:

* Final casts.
* Cast-plus-array-length.
* Final reference-array/struct reads.
* Scalar direct accesses.
* Nursery and remembered-object stores.
* Native constructor batches.
* Exact-reference facts.
* Late barrier-state lowering.

### Pre/post-index and pair operations

Recognize:

```text
load [p]; p += C
p += C; load [p]
load [p]; load [p+N]; p += 2N
```

Lower to:

```text
post-index load/store
pre-index load/store
LDP/STP
NEON chunk load/store
```

GCC’s auto-increment pass explicitly recognizes pre/post update forms around memory references and checks that the pointer update can be folded safely.

### Movemask consumer fusion

Do not materialize a complete scalar mask when the only consumer is:

```text
mask == 0
mask != 0
any bit set
all bits set
branch/select
```

Lower directly to a vector reduction/test and condition.

### Shuffle classifier

Classify static masks into:

```text
identity
splat
reverse
rotate/extract
zip/unzip
transpose
lane replace
table fallback
```

Select among:

```text
EXT
REV
ZIP
UZP
TRN
DUP
INS
TBL
```

### LSE and target features

Use:

* LSE atomics when available.
* LL/SC fallback otherwise.
* Dot-product/I8MM/FP16 only when semantics and features match.
* Target-specific cost profiles for Apple and server ARM cores.

## 9.3 SIMD/SWAR across both backends

Continue the existing successful strategy:

```text
offline sequence discovery
→ proof
→ benchmark
→ generated exact matcher
```

Do not run search online.

Prioritize:

* Movemask consumers.
* Reduction chains.
* Shuffle patterns.
* Widen/narrow chains.
* Byte packing/unpacking.
* High-half multiplication.
* Dot products.
* UTF/JSON state machines.
* Crypto rotations and lane permutations.

## 9.4 WasmGC runtime architecture

After ARM64 parity, proceed with:

1. Stable heap and handle backing.
2. Compact hot handle layout.
3. Faster exact type-kind metadata.
4. Fresh-object barrier elimination.
5. Batched allocation/initialization.
6. Shared native failure/refill tails.
7. Old-generation redesign only after telemetry proves it.

The compiler should preserve semantic GC facts across direct calls only when effect summaries prove no allocation or collection.

## Phase 5 target

Each campaign should have a distinct gate:

```text
AMD64 scalar/local campaign
ARM64 SIMD campaign
ARM64 GC campaign
bulk-memory campaign
```

Do not hide a regression in one architecture under an aggregate cross-architecture mean.

---

# 10. Phase 6 — narrow structured transformations

These borrow ideas from full compilers but use Wasm’s structured control to avoid a general CFG.

## 10.1 Tiny if-conversion

Admit only a bounded `if` whose arms are:

* Pure.
* Nontrapping.
* Call-free.
* Load-free initially.
* At most 8–12 Wasm operations total.
* Producing one scalar result or assigning the same local.

Lower to:

```text
AMD64 CMOVcc
ARM64 CSEL/FCSEL
vector blend when exact
```

Keep branches when one arm is strongly biased or the selected operations are expensive.

LLVM’s early if-conversion similarly uses strict block and dependency limits and excludes unsafe speculation.

## 10.2 Structured shrink wrapping

Target:

```text
entry
  cheap common fast path
  uncommon path needing frame/call/GC/EH setup
```

Initial form:

```text
frameless entry
fast checks
fast return

cold slow entry:
  allocate frame
  home required values
  perform call/helper work
  restore
  return
```

Admit only:

* One top-level early-return shape.
* No loop.
* No EH initially.
* No GC roots on the fast path.
* No frame-slot use on the fast path.
* Statistically or structurally cold slow path.

LLVM and GCC shrink wrapping place frame work only on paths that require it, but their general implementations rely on dominance and liveness. Wago should use a lexical structured subset.

## 10.3 Direct-call-chain stack-fence hoisting

For an acyclic direct-only call region:

1. Compute exact final frame sizes.
2. Compute maximum cumulative stack usage.
3. Check once at the externally addressable root.
4. Omit redundant internal checks.

Exclude:

* Recursive SCCs.
* Indirectly reachable functions.
* Exports/start functions below the root.
* Host reentry.
* Cross-instance entry.
* Unknown call targets.

## 10.4 Interrupt-poll coalescing

A tiny direct leaf may inherit a caller poll when it has:

* No loops.
* No calls.
* No unbounded bulk operation.
* Bounded instruction count.
* A caller poll within the documented cancellation budget.

Never remove polls from loops, recursion, or large leaves.

## 10.5 Exact structured loop hoisting

Hoist only operations proven invariant by the summary scan:

* Constant materialization.
* Immutable globals.
* Memory/table directory pointers.
* Stable context values.
* SIMD masks.
* Affine base setup.

Account for register pressure before hoisting. LLVM’s machine LICM likewise evaluates safety, pressure, latency, and block hotness rather than hoisting every invariant.

---

# 11. Phase 7 — profile guidance and research experiments

These should not block the main roadmap.

## 11.1 Optional offline profile input

Accept a profile keyed by module hash:

```text
function counts
call-edge counts
branch weights
loop weights
table target counts
```

The ordinary compiler remains one-pass. Profiles affect only:

* Regional effort.
* Inlining.
* Fallthrough choice.
* Function order.
* Alignment already supported by the completed size/layout work.
* Cold outlining/sharing.
* Optimization fuel.

Invalid or mismatched profiles are ignored.

## 11.2 Tiny equivalence sets at Valent sinks

Do not build a whole-function e-graph.

Allow at most:

```text
4 alternatives per node
rewrite depth 3
extraction fuel 96
```

Candidate alternatives:

```text
shift/add ↔ scaled address
mask/test ↔ bit-test
select ↔ branch/cmov/csel
affine address canonical forms
not/xor all-ones forms
```

Store an alternative only if it can plausibly beat the current form.

## 11.3 Two-lane or four-lane bounded SLP

Admission:

* Straight-line.
* Same operation and type.
* Adjacent or proven nonaliasing memory.
* No traps, calls, GC, atomics, or control.
* At most 16 scalar operations inspected.
* Two or four lanes only.

Instrument opportunities before implementation because optimized Wasm producers may already vectorize the useful cases.

## 11.4 Two-stage micro-pipelining

Do not implement Swing Modulo Scheduling.

Recognize only:

```text
innermost loop
single backedge
constant stride
prechecked memory range
no calls/GC/EH/atomics
body <= 8–12 operations
```

Generate at most:

```text
small prologue
two-stage kernel
small epilogue
```

Use a strict register-pressure and code-growth gate.

## 11.5 Implicit GC null checks

Guard/signal products could theoretically encode null handle resolution into a guaranteed faulting page and map the native fault PC to a Wasm null trap.

This is high risk. The experiment must prove:

* Exact null encoding.
* Stable reserved address range.
* Reliable fault classification.
* Correct source/trap attribution.
* No weakening of generation/type/bounds checks.
* No application to explicit-bounds embedded products.

LLVM’s implicit-null-check pass uses fault maps and a very small capped dependence search; Wago should be at least as conservative.

## 11.6 Software prefetch

Only consider:

* Hot innermost loops.
* Constant stride.
* Large working set.
* Sufficient trip count.
* PMU evidence of memory-latency stalls.
* No useful existing prefetch.

Keep it disabled by default until a representative workload wins consistently. LLVM similarly gates loop prefetching on target-provided distance, cache-line, and stride information.

## 11.7 Runtime tiering remains deferred

Do not add default runtime recompilation until:

* Local techniques plateau.
* Remaining time is concentrated in a small hot function set.
* A safe function-code replacement and reclamation protocol exists.
* Duplicate-code memory can be strictly budgeted.
* Offline profile compilation is insufficient.

---

# 12. Repository organization

A clean separation would be:

```text
railshot/shared/
    policy
    summaries
    effects
    value facts
    rematerialization
    optimization statistics

railshot/rules/
    semantic rule definitions
    generated matcher API
    common verification specifications

railshot/amd64/
    target costs
    target rule consumers
    machine window
    ABI classes
    target feature lowering

railshot/arm64/
    target costs
    target rule consumers
    machine window
    ABI classes
    target feature lowering

tools/
    rule generator
    rule verifier
    sequence harvester
    benchmark/profile joiner
```

The production backends should not import verifier or synthesis packages.

---

# 13. Recommended PR sequence

| Order | PR                                                    | Main result                                     |
| ----: | ----------------------------------------------------- | ----------------------------------------------- |
|     1 | Current performance rebaseline and causality counters | Establish actual remaining gaps                 |
|     2 | Complete #399                                         | Immutable, concurrent optimizer policy          |
|     3 | Compiler scratch subset from #316                     | Storage for facts, regions, effects, window     |
|     4 | Unified summary scan extensions                       | Definite assignment, effects, regions, next use |
|     5 | Complete #438                                         | Packed value provenance                         |
|     6 | Rematerialization recipes                             | Reduce spill/reload pressure                    |
|     7 | Definite-assignment zeroing and lazy parameter homing | Remove broad frame traffic                      |
|     8 | Generalized call/result sinks                         | Preserve ABI result registers                   |
|     9 | Existing regional cache next-use eviction             | Improve current admitted functions              |
|    10 | FP/vector regional residency                          | Extend register-bank coverage                   |
|    11 | Multiple straight-line regions                        | Support call-making functions                   |
|    12 | Simple loop residency                                 | Keep loop-carried locals in registers           |
|    13 | Call-effect summaries                                 | Preserve state across safe calls                |
|    14 | Internal ABI classes                                  | Reduce call preservation                        |
|    15 | Register ABI v2 and bounded multi-result merges       | Remove result-slot traffic                      |
|    16 | Rule schema, generator, and integer verifier          | Sustainable pattern infrastructure              |
|    17 | Machine-window identity path                          | Establish bounded symbolic emission             |
|    18 | Copy/extension/load/compare combines                  | First general local machine wins                |
|    19 | Multi-consumer condition tokens                       | Compare, branch, select, carry                  |
|    20 | Post-allocation register renaming                     | Break false dependencies and encoding waste     |
|    21 | ARM64 WasmGC parity from #317                         | Remove helper-bound ARM64 paths                 |
|    22 | ARM64 address-update, pair, and movemask campaign     | Target-specific execution gains                 |
|    23 | AMD64 fixed-register/wide-arithmetic campaign         | Crypto, bignum, hash gains                      |
|    24 | Prepared/host/cross-instance ABI specialization       | Reduce external boundary costs                  |
|    25 | Tiny if-conversion and structured shrink wrapping     | Optimize safe structured fast paths             |
|   26+ | Profile guidance and research experiments             | Only after evidence                             |

The rule-generator work can proceed in parallel with PRs 5–15, but the machine window should not begin until provenance and scratch ownership are stable.

---

# 14. Acceptance gates

## Per-PR default gate

```text
target workload:
    repeatable material improvement

whole corpus:
    no statistically significant regression beyond 0.5%

important individual row:
    >1.5% regression requires root cause

full compile weighted time:
    <=3% increase for ordinary local work
    <=5% for a major subsystem

compile B/op:
    <=2% increase

peak RSS:
    <=max(1 MiB, 2%)

execution allocations:
    unchanged on existing zero-allocation paths

native bytes:
    tracked to prevent I-cache regressions,
    even though size optimization is complete
```

## Cumulative core-program target

After phases 0–5:

```text
remaining-gap suite:
    8–15% additional execution improvement

whole broad corpus:
    1–5% additional geometric-mean improvement

weighted full compile:
    <=5–8% cumulative increase

peak compiler RSS:
    <=3% cumulative increase

steady-state execution allocations:
    unchanged
```

The remaining-gap suite is more important than forcing a large aggregate gain across rows where Wago is already ahead.

## Bounded-mechanism gate

Every bounded optimizer must have a test that:

1. Reaches its cap.
2. Falls back correctly.
3. Allocates no unbounded storage.
4. Produces deterministic code.
5. Does not retry indefinitely.
6. Preserves traps and source attribution.

## Rule gate

Every production rule requires:

```text
semantic rule source
generated matcher
proof or exhaustive-domain validation
positive test
single-precondition near-miss tests
architecture codegen assertion
corpus hit count
A/B benchmark
```

---

# 15. Explicit exclusions

This plan does not reopen:

* Branch relaxation.
* Final code compaction.
* Function alignment policy.
* Generated-code byte attribution.
* Code-size inlining objectives.
* Literal layout done as part of the output-size work.

It also rejects for the default compiler:

* Whole-function SSA.
* General CFG optimization.
* Full linear scan.
* Graph coloring.
* Full e-graph saturation.
* General LVN without new opportunity evidence.
* PRE.
* Trace scheduling.
* Full modulo scheduling.
* Online SMT or superoptimization.
* Unbounded dynamic tiering.
* Raising the Valent tree-height cap without a selector that proves a benefit.
* Indiscriminate permanent register pinning.
* Broad speculative bounds machinery without measured explicit-mode demand.

Cranelift’s final machine buffer is a useful model for recorded fixups and local branch transformations, but the completed Wago finalizer should remain bounded and separate from semantic optimization.

---

# 16. Immediate implementation order

The next four concrete changes should be:

1. **Complete immutable per-compilation optimizer state from #399.**
2. **Land the performance-causality ledger and current rebaseline.**
3. **Land the reusable scratch subset of #316 needed by facts and regions.**
4. **Implement #438 together with the first rematerialization recipes.**

After those, the highest-value performance sequence is:

```text
definite assignment + lazy parameter homing
        ↓
regional residency next-use
        ↓
simple loop and call-separated regions
        ↓
call effects + ABI classes
        ↓
register ABI v2
        ↓
generated machine combiner
        ↓
ARM64 GC/SIMD and AMD64 wide/fixed-register campaigns
```

That is the coherent path from Railshot’s current state to materially stronger execution quality without sacrificing its defining properties:

```text
single semantic pass
bounded compiler state
low peak memory
fast compilation
zero-allocation execution
deterministic conservative fallback
```
