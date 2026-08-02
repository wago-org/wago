# WasmGC optimization research: V8, Cranelift, and Binaryen

Date: 2026-08-02

This note maps current V8, Cranelift/Wasmtime, and Binaryen WasmGC techniques
onto Wago's single-pass, no-whole-function-IR Railshot backend. It was written
before proceeding with native bump allocation because allocation speed is only
one part of the remaining Dew Map/Set gap, and optimizing allocations that a
better compiler would remove entirely is wasted work.

Source snapshots inspected:

- V8 `bf3d02947968c33781ad7a74e5e0234d9ac5d748`;
- Wasmtime `e8ac8c27f19939bfb1d26d920368d8b6028a67a9`;
- Binaryen `33abc59a3ba7eb86149b7d019d51cb820b13e63b`.

Primary sources:

- [V8 WasmGC typed optimization reducer](https://github.com/v8/v8/blob/bf3d02947968c33781ad7a74e5e0234d9ac5d748/src/compiler/turboshaft/wasm-gc-typed-optimization-reducer.h)
- [V8 WasmGC load elimination](https://github.com/v8/v8/blob/bf3d02947968c33781ad7a74e5e0234d9ac5d748/src/compiler/turboshaft/wasm-load-elimination-reducer.cc)
- [V8 Wasm escape analysis](https://github.com/v8/v8/blob/bf3d02947968c33781ad7a74e5e0234d9ac5d748/src/compiler/wasm-escape-analysis.cc)
- [V8 Liftoff WasmGC lowering](https://github.com/v8/v8/blob/bf3d02947968c33781ad7a74e5e0234d9ac5d748/src/wasm/baseline/liftoff-compiler.cc)
- [V8 high-performance WasmGC overview](https://v8.dev/blog/wasm-gc-porting)
- [Cranelift collector-independent WasmGC lowering](https://github.com/bytecodealliance/wasmtime/blob/e8ac8c27f19939bfb1d26d920368d8b6028a67a9/crates/cranelift/src/func_environ/gc.rs)
- [Cranelift copying-collector lowering](https://github.com/bytecodealliance/wasmtime/blob/e8ac8c27f19939bfb1d26d920368d8b6028a67a9/crates/cranelift/src/func_environ/gc/copying.rs)
- [Binaryen Heap2Local](https://github.com/WebAssembly/binaryen/blob/33abc59a3ba7eb86149b7d019d51cb820b13e63b/src/passes/Heap2Local.cpp)
- [Binaryen GUFA](https://github.com/WebAssembly/binaryen/blob/33abc59a3ba7eb86149b7d019d51cb820b13e63b/src/passes/GUFA.cpp)

## 1. What the other systems actually do

### 1.1 V8 baseline: direct accesses, allocation builtins, and fresh-object barriers

Liftoff emits direct object loads/stores and explicit or signal-backed null
checks. `struct.new`, `array.new`, and `array.new_fixed` allocate through V8
builtins, then initialize fields in generated code. Reference initialization
skips the write barrier when the object is freshly allocated in new space;
old/shared/pretenured objects retain the full barrier.

Wago has already converged on the important baseline parts:

- native final cast/read access;
- exact native nursery reference stores;
- cold helper fallback for old-space and Tiny barriers;
- fully initialized constructor stores without redundant clearing.

The remaining difference is that Liftoff trusts internal references and V8's
heap representation more broadly, while Wago's current shared native stubs
repeat complete ABI, compact-handle, heap-space, extent, and type validation.
Wago needs proof-carrying compiler facts before safely removing any of those
checks.

### 1.2 V8 optimizing tier: path-sensitive type facts

V8's Turboshaft WasmGC type analyzer propagates reference facts through control
flow. Facts include exact/concrete type, hierarchy, and nullability. In
particular:

- a successful `ref.cast` refines later uses;
- a true `ref.test` branch refines the tested value;
- `struct.new` produces an exact non-null reference;
- `array.new` produces a concrete non-null reference;
- `struct.get`, `struct.set`, and `array.len` prove their receiver non-null after
  they execute;
- facts merge conservatively at phis and loop headers.

The reduction pass then removes casts and null checks that the facts prove
redundant, replaces impossible checks with constants/traps, and narrows the
source type of checks that remain.

This is directly adaptable without building SSA. Railshot can maintain a small
per-local reference-fact table alongside its existing local storage state and
merge it through the existing structured-control stack.

### 1.3 V8 optimizing tier: WasmGC-specific load elimination

V8 has a dedicated WasmGC memory-content analysis rather than relying only on
generic load elimination. It tracks values by object identity, field offset,
type, width, and mutability. Important cases include:

- repeated `struct.get` of the same field;
- store-to-load forwarding after `struct.set`;
- an array allocation's known length forwarding directly to `array.len`;
- repeated `array.len` elimination;
- fresh allocations marked non-aliasing;
- immutable fields retained across operations that invalidate mutable fields;
- type-unrelated stores not invalidating each other's field facts;
- conservative invalidation when a call can expose an alias or write memory.

Wago cannot copy the graph-wide snapshot table without violating the no-IR
direction, but it can adopt bounded forms: one-entry or small fixed straight-line
field caches, constructor-length provenance, and fresh-object non-alias facts.

### 1.4 V8 escape analysis: remove dead allocation trees

V8's Wasm escape pass removes a raw allocation when all value uses are merely
stores into that allocation or otherwise dead. Crucially, after removing those
stores it revisits values stored into the dead object, allowing nested dead
allocations to disappear recursively.

That recursive behavior exactly matches a major Dew pattern:

```wasm
v128.const ...
v128.const ...
array.new_fixed $vector 2
i32.const 0
i32.const 22
struct.new $wrapper
drop
```

The outer struct and inner array are both unobservable. Removing only the outer
struct leaves the expensive inner allocation alive and does not materially
improve Dew.

### 1.5 Cranelift: collector-specific lowering

Cranelift separates generic WasmGC semantics from collector-specific code
generation through a `GcCompiler` interface. The generic layer performs static
specialization for top, bottom, concrete, nullable, and i31 reference types.
Collector implementations supply allocation and barrier lowering.

The current copying collector:

- keeps JIT-visible bump pointer and active-space-end fields;
- computes aligned allocation size with wider arithmetic to make overflow take
  the slow path rather than wrap;
- branches to a cold `gc_alloc_raw` slow path for collection/growth;
- returns both the compact heap reference and raw object pointer;
- initializes headers and fields directly in generated code;
- distinguishes initialization stores from general replacement stores;
- needs no read or write barrier for the semispace collector;
- tags GC heap accesses with a dedicated alias region and corruption trap.

This is a good blueprint for Wago native bump allocation, but it should follow
rather than precede dead-allocation elimination and reference-fact measurement.

### 1.6 Binaryen: whole-program type flow and Heap2Local

Binaryen's GUFA tracks possible contents through locals, globals, fields, calls,
and control flow. Its optimizing mode applies local cleanup after refining
contents. `Heap2Local` performs scalar replacement of nonescaping GC objects by
creating locals for their fields and rewriting accesses.

Full Heap2Local is not automatically a good fit for Wago. It can increase local
and frame pressure substantially, and Wago intentionally avoids whole-function
IR. A bounded dead-allocation/tree elimination is much cheaper and is strongly
supported by the Dew measurements below.

## 2. Dew experiments

The supplied input was never modified. All transformed modules were written
under `.tmp/research/` using Binaryen 116.

Feature flags used:

```text
--enable-gc --enable-reference-types --enable-multivalue --enable-simd
--enable-tail-call --enable-exception-handling --enable-memory64
--enable-multimemory
```

The most useful oracle was:

```text
wasm-opt dew-map-workload.wasm <features> --closed-world \
  --type-refining --vacuum -o .tmp/research/dew-type.wasm
```

Both the original and transformed module execute successfully in Wago.

### 2.1 Static changes

| Metric | Original | Type-refined oracle | Change |
| --- | ---: | ---: | ---: |
| module bytes | 712,494 | 670,448 | -5.9% |
| instructions | 273,599 | 265,407 | -8,192 |
| `struct.new` | 2,050 | 1,026 | -1,024 |
| `array.new_fixed` | 1,024 | 0 | -1,024 |
| `drop` | 1,535 | 511 | -1,024 |
| Wago static `hostsync` | 8,188 | 6,140 | -2,048 |
| generated native code | 7,479,004 B | 7,112,416 B | -4.9% |

The change removes exactly 1,024 recursively dead constructor trees, each
containing one fixed array and one struct. The remaining 6,140 helper sites are
2,050 live struct constructors, 2,046 array-store fallbacks, and 2,044
struct-store fallbacks after type renumbering and dead-tree removal.

### 2.2 Fresh Wago execution

Three interleaved, CPU-pinned runs gave:

| Module | Median range | Host bytes/op | Host allocations/op |
| --- | ---: | ---: | ---: |
| original | 0.938-0.949 ms | 924,536 | 90 |
| type-refined oracle | 0.630-0.633 ms | 524,512 | 68 |

This is approximately a **33% execution reduction**, **43% host-byte
reduction**, and **24% host-allocation-count reduction** before changing Wago's
allocator.

### 2.3 Why full Heap2Local is not first

`--heap2local --vacuum` removed 1,024 outer structs but retained all 1,024 inner
fixed-array allocations:

| Metric | Original | Heap2Local only |
| --- | ---: | ---: |
| `hostsync` | 8,188 | 7,164 |
| generated frame | 24,808 B | 82,152 B |
| host bytes/op | 924,536 | 870,856 |
| median | about 0.94 ms | about 0.95-0.96 ms |

It reduced host allocation but was neutral-to-slower and more than tripled the
native frame. This rejects a broad scalar-replacement implementation as the
next Wago change. Recursive dead-allocation elimination is the high-value
subset.

### 2.4 V8 tier comparison

On Node 26.3.0, forcing Liftoff-only execution measured approximately 115-120
microseconds for the original and 81-82 microseconds for the transformed
module. Forced TurboFan samples had a stable lower quartile around 24-25
microseconds for the original and 17-19 microseconds for the transformed
module, although GC/tiering phase changes made longer-run distributions
bimodal.

The transformed input therefore helps both V8 tiers. V8's optimizing tier still
wins much more from path-sensitive types, load elimination, alias analysis, and
optimized allocation than the source transformation alone.

## 3. Dew-specific opportunities ranked before bump allocation

### P0: recursively eliminate dead constructor trees

**Evidence:** 2,048 of 8,188 remaining helper sites disappear, and the oracle is
about 33% faster in Wago.

Required semantics:

- preserve evaluation order and all observable/trapping initializer expressions;
- eliminate only the allocation and initialization whose result cannot escape;
- recursively revisit constructor-valued initializers after removing the outer
  object;
- never remove `array.new`/`array.new_default` length-limit traps unless the
  length is statically proven admissible;
- begin with fixed-size `struct.new` and bounded `array.new_fixed` trees;
- do not treat host calls, mutable globals, table operations, reference stores,
  or potentially trapping numeric operations as pure;
- add an A/B flag and a static counter such as `gc-dead-new`.

No whole-function IR is necessary. The best Railshot shape is a bounded pending
constructor value integrated with the existing valent stack. It is materialized
before an effect, control boundary, safepoint, or unknown consumer; `drop` can
discard it and recursively discard constructor-only inputs while forcing any
observable input computations.

### P1: structured exact/non-null reference facts

A conservative offline scan of the flat Dew body found approximately **4,096
statically repeated casts** where a local had already been proven to hold the
same exact final type in the same structured region. This count clears facts at
block/loop merges and removes loop facts for locals assigned in the loop; it is
an estimate and should be replaced by a compiler-native counter before codegen
changes.

Suggested compact fact:

```text
unknown | null | nonnull-abstract(heap) | exact-nonnull(canonical-type)
```

Producers/refiners:

- `struct.new` and `array.new*`: exact non-null;
- successful `ref.cast`: target type and cast nullability;
- `ref.as_non_null`: non-null;
- `struct.get` reference result: declared field type;
- local copies preserve facts.

Merges take intersection. A loop drops facts for locals in `loopSetLocals`.
Facts describe the compact reference and survive collection; a cached raw object
pointer does not survive a safepoint.

Initial use should be count-only, then remove redundant final casts or select a
smaller access stub that skips an already-proven exact-type comparison while
retaining handle liveness and physical-extent validation.

### P2: bounded WasmGC load elimination

Measure these separately:

1. repeated `array.len` on the same local/reference;
2. `struct.set` followed by `struct.get` of the same field;
3. repeated immutable `struct.get`;
4. a fresh array's constructor length consumed by `array.len`;
5. repeated resolver/type/header work on the same compact reference before a
   safepoint.

Do not build V8's graph snapshot table. Use fixed-size per-function state, local
version numbers, and exact invalidation:

- mutable field facts invalidate on potentially aliasing stores and arbitrary
  host calls;
- immutable length/type facts survive ordinary calls but raw pointers do not;
- fresh nonescaping objects are non-aliasing until stored, passed, returned, or
  merged with another reference;
- any allocation/helper/safepoint invalidates cached raw object pointers;
- collection may rewrite root slots, so compact values must be reloaded from
  canonical locations when required.

### P3: reuse one resolved object across adjacent operations

Current fusions cover specific adjacent cast/access pairs. A bounded
`resolvedGCRef` value can generalize this without whole-function IR:

```text
compact source identity + exact type + raw header register + safepoint epoch
```

It is valid only until a helper call, allocation, collection, control merge,
source-local write, or register spill that loses the raw pointer. This could
remove repeated handle-table, space, header, extent, and canonical-type checks
across short straight-line runs while retaining the compact rooted value for
safepoints.

### P4: collector-specific inline bump allocation

After P0-P3 measurement, use the Cranelift copying-collector shape:

- publish JIT-visible nursery cursor, limit, and handle-reservation state;
- use overflow-safe wider arithmetic;
- branch to one cold helper that reserves/grows handles and collects;
- return compact reference plus raw pointer;
- write the exact header and all fields in generated code;
- refresh/reload native metadata after the slow path;
- expose constructor inputs as exact mutable roots before any slow-path call;
- keep Tiny and unsupported Throughput states on the existing helper path.

Wago's moving collector and indirect handle table make this more complicated
than Cranelift's direct heap-index reference. Space bytes and handle identity
must be reserved transactionally: neither half may become visible if the other
fails.

### P5: general old-space/Tiny barriers

V8's generation-aware barrier selection confirms Wago's nursery-only decision.
The next write optimization is not an unconditional direct store. It needs a
complete old-to-young remembered-set/card update for Throughput and a correct
incremental barrier for Tiny. Until those are proven, retain the current cold
helper fallback.

## 4. Implementation status and order

### Completed: recursive dead constructor trees

AMD64 now removes direct fixed-size constructor/drop pairs and uses bounded
postfix lookahead to replace an inner `struct.new` or `array.new_fixed` result
only when it flows untouched through push-only leaves into an immediately dropped
outer struct. This remains a local valent-stack optimization, not a body IR.

The implementation:

- removes non-trapping initializer trees without emitting code;
- forces any deferred trapping initializer in original bottom-to-top order;
- preserves explicit/guard memory trap behavior;
- retires the frontend's dense GC safepoint ID without creating a native callsite;
- has `WAGO_AMD64_NO_DEAD_GC_NEW=1` for differential A/B;
- changes allocation-forcing root tests to consume the allocated value through
  `ref.is_null`, so the fixture cannot be optimized away.

Measured on Dew:

| Metric | Disabled | Enabled |
| --- | ---: | ---: |
| `gc-dead-new` | 0 | 2,048 |
| `hostsync` | 8,188 | 6,140 |
| native code | 7,479,004 B | 7,104,256 B |
| fresh median | 0.93-1.04 ms | 0.62-0.67 ms |
| host bytes/op | 924,536 | 524,512 |
| host allocations/op | 90 | 68 |
| sustained median | about 1.28 ms | about 0.92-1.03 ms |

The plugin-complete stripped TinyGo candidate is 1,776,700 bytes and executes
Dew, +2,512 bytes over the preceding 1,774,188-byte release candidate.

### Completed: structured exact/non-null local facts

AMD64 now stores one `typeIndex+1` exact-non-null fact per GC reference local.
Constructor and successful final-cast values carry the same compact fact in the
existing storage metadata, so `local.set`, `local.get`, and local copies transfer
it without enlarging operand-stack elements. Local writes clear or replace the
fact. Structured control boundaries clear all facts, deliberately choosing a
sound conservative subset over SSA/phi state. Calls and collection do not clear
type identity, but no raw object pointer is retained.

Adjacent cast/access fusions remain first in lowering order. A standalone cast is
removed only when the current compact value already carries the exact final type.
`WAGO_AMD64_NO_GC_REF_FACTS=1` is the A/B oracle.

Measured on Dew:

| Metric | Facts disabled | Facts enabled |
| --- | ---: | ---: |
| `gc-ref-cast-elide` | 0 | 7,158 |
| `gcnative` | 50,101 | 42,943 |
| flushes | 91,011 | 83,853 |
| native code | 7,104,256 B | 6,957,024 B |
| fresh median | 0.60-0.61 ms | 0.57-0.58 ms |
| sustained median | 0.90-0.93 ms | 0.80-0.86 ms |

Host allocation remains 524,512 B/op and 68 allocations/op. The stripped
plugin-complete TinyGo binary is 1,777,788 bytes, +1,088 bytes over the preceding
build.

### Completed: bounded load-forwarding opportunity counters

The compiler now reports conservative exact-local opportunities without changing
machine code:

| Counter | Dew sites |
| --- | ---: |
| fused accesses with a prior exact array type | 4,092 |
| fused accesses with a prior exact struct type | 3,067 |
| repeated immutable `array.len` on the same unchanged exact local | 1,022 |
| same-field get/get forwarding | 0 |
| same-field set/get forwarding | 0 |

The field counters invalidate on local replacement, structured control, any
struct write, and polymorphic calls. Cast-result provenance is retained only in
otherwise-unused register-storage metadata so a following `struct.set` can be
attributed to its source local without growing operand elements.

Two runtime prototypes were measured and reverted:

- a one-entry frame-slot array-length cache removed 1,022 native resolver calls
  but grew generated code by 8,208 bytes. Fresh results were neutral/noisy and
  sustained results were slower in three of four interleaved rounds;
- separate exact-known array-length and struct-reference resolver stubs covered
  4,092 and 3,067 sites and reduced generated code by 64,919 bytes, but fresh
  and sustained execution remained neutral-to-slightly slower. Static sites are
  therefore not sufficient evidence that those paths are dynamically hot.

The count-only metrics remain; both runtime transformations were removed.

### Completed: executed synchronous-helper counters

A diagnostic `wago_gcstats` build exposes
`Instance.SetGCHelperStatsTracking(true)` and collector-domain counters through
`Instance.GCHelperStats()`. Production builds compile the dispatch hook away. Before
old-struct specialization, one fresh Dew call executed 1,724 synchronous Go helper
transitions: 1,038 allocation helpers and 686 mutation fallbacks, versus static
`hostsync=6,140`. All 686 mutations had old parents. The exact same per-call counts
persisted through 100 and 500 repeated invocations.

### Completed: barrier-safe old/large struct stores

The shared AMD64 final-reference struct-store stub now admits a fully validated
Throughput old/large parent when the child is non-young or the parent's stable
remembered bit is already set. It never mutates remembered metadata: a nursery child
behind an unremembered parent and every Tiny store still take the exact Go helper.
Dew removes 426 mutation transitions per call, leaving 1,298 total transitions:
1,038 allocations and 260 mutations. Generated code grows by 71 bytes. Six
interleaved rounds produce median-of-medians changes of about 0.846→0.755 ms fresh
and 0.880→0.839 ms sustained, with host allocation unchanged.

The remaining 260 mutation transitions were 258 array stores and two struct stores
that must create remembered membership. Of those array calls, 254 already had a
remembered old parent and a valid object card.

### Completed: existing-card old/large array stores

Collector native ABI v2 preserves the 112-byte v1 prefix and appends object-card
pointer/count metadata. The shared AMD64 final-reference array-store stub admits an
old/large Throughput parent only when remembered membership and a validated card slot
already exist. It can widen that stable interval in place, but never appends or
relocates metadata. Cardless/unremembered and Tiny paths remain helper-bound. Dew
removes another 254 mutation transitions, leaving 1,044 total: 1,038 allocation
helpers and six metadata-creating mutations. Generated code grows by 226 bytes.
Six interleaved rounds against the old-struct build show median-of-medians changes
of about 0.671→0.660 ms fresh and 0.763→0.725 ms sustained.

Remaining order:

1. Implement native bump allocation for the 1,038 live constructor transitions.
2. Keep the six remembered/card-creating mutations on the exact helper until a
   bounded metadata-growth path is proven worthwhile.
3. Prototype a bounded field-value cache only if dynamic counters identify a hot
   same-field family on another workload; Dew has no conservative get/get or
   set/get candidates.
4. Keep broad Heap2Local/scalar replacement deferred unless a different workload
   shows a win that justifies frame growth and control-flow complexity.

The key conclusion remains that **native bump allocation is worthwhile, but it
should optimize only live allocations**. V8's typed/load analyses provide the
next no-IR-compatible opportunities to reduce validation work around the
references and constructors that remain.
