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

The first implementation slice accompanying this report already applies the
general, code-neutral items: exact sparse retained global hints, a 152-byte
`funcHints` record instead of 200 bytes, one module-level synchronous-host-call
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

This is a staged reduction, not satisfaction of the 32--48-byte common-record
target. Index-width, further retention work, sidecar pooling, and policy deletion
remain subject to the gates below.

The first worker-lifecycle implementation keeps the initial operand chunk and
ratchets overflow retention to the just-completed function's actual chunk use.
Repeated large functions therefore reuse backing, while a smaller successor
releases the unused suffix. The rare shrink transition explicitly clears every
scratch-owned `*elem` path and retained chunk capacity before removing slice
headers; ordinary one-chunk functions take no cleanup call. The resource ledger
now exposes initial reservation, per-worker peak envelope, final retention, and
cumulative discarded bytes. A Linux/AMD64 `many_funcs` screen stayed at 36
allocations and about 158,968 B/op, with eight-run medians of 341.9 microseconds
before and 344.0 microseconds after, a 0.6% movement inside the 1.5% investigation
gate. Giant-lane scheduling and byte-weighted
admission are not yet implemented.

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

`localSlot []int`, relocation offsets/targets, control heights/counts, and most
fields in parallel `funcResult` use 64-bit `int` on supported hosts. The module
and metadata layers already use `uint32` for function indexes, local indexes,
code offsets, frame offsets, and safepoint IDs.

Sources: [AMD64 function state](../../src/core/compiler/backend/railshot/amd64/compile.go#L240),
[AMD64 relocations](../../src/core/compiler/backend/railshot/amd64/call.go#L47),
[parallel result metadata](../../src/core/compiler/backend/railshot/amd64/compile.go#L870),
[root-plan compact indexes](../../src/core/compiler/backend/railshot/shared/gc_frame_roots.go#L49).

Use checked conversion at the decoder/compiler boundary, then retain `uint32`
internally. Pack booleans and small enums into flags. This is general input-size
accounting, not a workload heuristic.

**Hypothesis:** narrowing `localSlot` alone halves that backing array; narrowing
and flattening relocations should also reduce parallel result metadata and
allocation count. Reject the change if checks add measurable compile time.

### 3. Easy repeated work exists today

Both backends call `moduleUsesSyncHostCalls` inside every function attempt. That
helper scans every imported function signature. On a module with `F` local
functions and `I` imports, this creates avoidable `O(F*I)` work, and a failed
register attempt repeats it. Compute the effective value once at module setup
and pass the resolved boolean.

Sources: [AMD64 call site](../../src/core/compiler/backend/railshot/amd64/compile.go#L2408),
[AMD64 module scan](../../src/core/compiler/backend/railshot/amd64/call.go#L934),
[ARM64 call site](../../src/core/compiler/backend/railshot/arm64/compile.go#L1933).

The function pre-scan already computes `nLocals`, yet each compile attempt calls
`countLocals` again. Use the validated hint count when hints are authoritative,
keeping the old calculation only for AST/test fallback.

Sources: [module hint local count](../../src/core/compiler/backend/railshot/amd64/compile.go#L1980),
[attempt recount](../../src/core/compiler/backend/railshot/amd64/compile.go#L2354).

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
3. **Default-off experiments with no active qualification:** `affine-lea`,
   `tee-spill-elide`, `v128-sink`, `loop-precheck`, and exact
   GC-ref facts are compiled into production but disabled by default.
   Either promote them against the standard gates or delete them. Do not let an
   indefinite experiment become a permanent alternate path.
4. **Mature rollback environment switches:** the production Railshot packages
   currently reference 138 distinct `WAGO_*` variables. Preserve a small number
   of safety-critical or public controls; migrate measurement-only toggles to
   test-only policy construction, then delete old package globals and branches.
   `CodegenPolicy.Selection` already provides a per-compilation bitset and is the
   natural single owner. Sources: [optimization selection](../../src/core/compiler/optimization/catalog.go#L69),
   [codegen policy](../../src/core/compiler/backend/railshot/shared/policy.go#L5).
5. **Duplicated architecture-neutral code:** hint aggregation, eligibility
   tracking, compact metadata records, and resource accounting should live in
   `shared`. Keep target selection and emission in the architecture packages.

Items 3–5 primarily reduce binary size and code-quality burden; they should not
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
