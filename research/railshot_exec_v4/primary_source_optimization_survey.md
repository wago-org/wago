# Railshot execution optimization survey: primary-source findings

- Date: 2026-09-08
- Wago baseline: [`a83fc004`](https://github.com/wago-org/wago/tree/a83fc0040c8fd85edebeaa1e59fd291f76712aa6)
- wazero source inspected: [`6edbb8c0`](https://github.com/tetratelabs/wazero/tree/6edbb8c01a5f7ad6ea4e3de0493667683e8baecd)
- Wasmtime/Cranelift source inspected: [`66801692`](https://github.com/bytecodealliance/wasmtime/tree/668016926adfd1b8a79dbce894f1e203d8892599)
- LLVM source inspected: [`6a183b88`](https://github.com/llvm/llvm-project/tree/6a183b8853b79a134402332cf4a776a8090d1b88)

## Executive conclusion

There is a credible path to materially faster broad-corpus execution without adding
an SSA tier, removing checks, or recognizing named workloads. It is not one magic
rewrite, and primary sources do not justify promising a 30% geomean before measurement.
The highest-value strategy is to improve three general mechanisms that are already
native to Railshot:

1. turn the existing bounded local-event and shadow-residency metadata into guarded,
   phase-sensitive register leases;
2. expand target-specific tree covering and call/result coalescing at the point where
   Valent expressions are materialized; and
3. make the existing native finalizer a bounded recorded-site optimizer for branches,
   holes, and cold paths, without decoding or rebuilding the function.

The proposed “tuning” step is sound if it means **choosing policy from summary facts
before body emission, then applying monotone rewrites to explicitly recorded sites after
emission**. It should not mean recompiling a function, searching arbitrary instruction
streams, or retaining a second optimizer IR. Cranelift's machine buffer is a strong
precedent: it emits once, records labels/fixups, and later handles veneers and simple
branch edits rather than rescanning a high-level IR ([source](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/codegen/src/machinst/buffer.rs#L1-L120)).

One large PR can contain this campaign, but it should be internally split into atomic
commits and independently registered knobs. Otherwise benchmark attribution and
security review become impractical.

## Scope and anti-bias rule

A candidate is “general” here only when all of the following are true:

- Its matcher is expressed in Wasm semantics, target instruction capabilities, CFG
  shape, or bounded compiler facts—not module names, hashes, source languages, exported
  names, corpus membership, or constants selected because one benchmark uses them.
- Its correctness applies to every valid module satisfying explicit predicates.
- Its admission threshold is derived from an architectural/resource limit or from a
  predeclared, corpus-independent cost model.
- It has adversarial near-miss tests and an independent configuration switch.
- It is retained only after an all-corpus paired test, with compilation latency,
  allocation volume, native size, and correctness treated as simultaneous gates.

Semantic instruction covers such as “an add whose right operand is an encodable
immediate” are general even if some corpora benefit more than others. A matcher for a
particular constant sequence observed only in BLAKE or one parser is biased unless the
sequence is a standardized operation or an independently established compiler idiom
with broad producer coverage. Cranelift's ISLE rules illustrate the proper level: rules
describe typed operations, immediates, sinkable loads, and target forms, and the
generated decision tree shares matching work
([ISLE reference](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/isle/docs/language-reference.md)).

## What the competitors do—and what Wago should not copy

wazero's current Wazevo path builds SSA, runs dead-block, dominator, redundant-phi,
dead-code, block-layout, loop-forest, and finalization passes
([pass sequence](https://github.com/tetratelabs/wazero/blob/6edbb8c01a5f7ad6ea4e3de0493667683e8baecd/internal/engine/wazevo/ssa/pass.go#L9-L70)).
It then lowers to machine IR, performs global register allocation, post-register-
allocation work, and encodes
([backend pipeline](https://github.com/tetratelabs/wazero/blob/6edbb8c01a5f7ad6ea4e3de0493667683e8baecd/internal/engine/wazevo/backend/compiler.go#L155-L200)).
That machinery provides global visibility but necessarily materializes and traverses
more state than Railshot.

Cranelift similarly runs an optimizing e-graph before lowering and register allocation
([context pipeline](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/codegen/src/context.rs#L130-L205)).
LLVM's documented pipeline includes instruction selection, scheduling, global register
allocation, prologue/epilogue insertion, and late machine-code optimization
([LLVM code generator](https://llvm.org/docs/CodeGenerator.html)). These are useful
oracles for missed forms, not suitable architectures to transplant wholesale while
requiring Wago to remain roughly 2.7x faster to compile and 6–7x lower in compiler
memory.

Railshot already has the right low-cost substrate:

- an allocation-conscious byte scan that collects call/memory/control shape,
  loop-weighted local scores, last uses, event metadata, and bounded arena estimates
  ([AMD64 hints](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/amd64/hints.go#L20-L125));
- a capped local-event tape and telemetry-only shadow regional planner, documented in
  the project's optimization record
  ([roadmap](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/OPTIMIZATIONS.md#storage-model-and-register-allocation));
- on-the-fly Valent tree materialization and target-specific selection;
- straight-line bounds certificates with explicit invalidation barriers
  ([AMD64 bounds path](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/amd64/memory.go#L385-L640)); and
- bounded finalizers with compact offset maps, relocation remapping, architecture-range
  validation, and fail-safe fallback
  ([AMD64 finalizer](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/amd64/finalize.go#L15-L210),
  [ARM64 finalizer](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/arm64/finalize.go#L206-L290)).

The campaign should deepen those mechanisms rather than introduce a second compiler.

## Ranked opportunity matrix

| Rank | General optimization family | Expected breadth | Execution upside | Compile/memory cost | Risk | Recommendation |
|---:|---|---|---|---|---|---|
| 1 | Phase-sensitive regional residency driven by the existing event tape | Integer-heavy loops, interpreters, parsers, crypto, compression, language VMs | High where frame traffic remains | One already-bounded event walk; reused scratch | High allocator correctness risk | Implement first behind a new knob, with conservative fallback |
| 2 | Expanded target instruction covers and destination coalescing | Nearly every compute/memory corpus | Medium-high, cumulative | Constant-time local matching; no new retained graph | Medium | Implement as small semantic rule groups |
| 3 | Call-live windows, result-to-destination coalescing, limited multi-result register ABI | Call-heavy, recursion, dispatch, language runtimes | High for affected rows | Fixed arrays proportional to ABI register count | High GC/EH/ABI risk | Implement in narrow, separately gated slices |
| 4 | Recorded-site branch threading, fallthrough repair, short-branch relaxation, and cold-tail compaction | Branch-heavy code, dispatch, large VMs; native-size benefit everywhere | Medium; potentially high for I-cache-bound code | Bounded site/fixup work only | Medium | Extend the existing finalizer, never decode arbitrary bytes |
| 5 | Address-expression covers and bounds-certificate widening | Memory, parsers, compression, image/data processing | Medium-high | Constant-state facts plus bounded lookahead | Critical if check/use diverge | Expand straight-line proofs first; loop proofs only after formalized validator |
| 6 | SIMD semantic cover expansion and vector residency | Crypto, codecs, numerical/data processing | High but feature-limited | Small lookahead/Valent cover | High semantic portability risk | General and worthwhile; qualify separately from scalar geomean |
| 7 | Tiny pure-region instruction scheduling | In-order/latency-bound ARM cores, some dependency chains | Low-medium and architecture-dependent | Fixed 4–8 node window | High trap/order risk; possible compile drag | Prototype last; reject unless broad counters and timing agree |
| 8 | Mutable-table inline caches | Dynamic-language dispatch and virtual calls | Potentially high but narrower | Per-site code/data plus epoch protocol | High concurrency/lifecycle risk | Research-only until table epochs are first-class |

## 1. Phase-sensitive register residency

### Proposed mechanism

Promote the existing shadow planner from telemetry to an optional lowering policy. It
already splits local versions at definitions, calls, and structured boundaries and
reports avoided loads, synchronization debt, and pressure debt. The production form
should:

1. retain only the existing capped, pointer-free event representation;
2. rank no more than the current bounded candidate count;
3. lease registers at exact activation events rather than pinning a local for the whole
   function;
4. end leases at last use, definitions, calls that do not preserve the register, joins,
   EH boundaries, and unknown/custom operations;
5. refuse a lease whenever it would reduce the target-derived transient-register floor;
6. write back dirty state before every path on which the canonical frame value is
   observable; and
7. fall back to the existing whole-function/region allocator on overflow or ambiguity.

The current regional allocator demonstrates the desired fail-soft shape: bounded body
and local counts, worker scratch reuse, no spill merely to create a cache, last-use
ownership transfer, and hotness-gated eviction
([implementation](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/amd64/interval_region.go#L1-L190)).
The next version should improve lifetime precision, not become global virtual-register
allocation. Linear scan is attractive for JITs because it processes ordered live
intervals efficiently, but a full implementation still requires interval construction
and global fixups; the original Poletto–Sarkar paper is the appropriate performance/
quality baseline, not a mandate to add it
([paper](https://doi.org/10.1145/330249.330250)).

### Architecture policy

- **AMD64:** prioritize avoiding spills and memory traffic, but account for fixed-role
  registers and extra encoding bytes. Prefer callee-saved registers only when a lease
  spans enough weighted work to repay save/restore; do not consume a scratch register
  merely because the nominal pool has room.
- **ARM64:** exploit the larger GP file and fixed-width encoding, but preserve the
  proven transient floor. Prefer X19–X28 for genuinely long leases and caller-saved
  registers for short call-free segments; pair save/restores only when alignment and
  liveness prove both stores are required.
- **Both:** GP, FP, and v128 banks need separate pressure/debt models. A scalar score
  must not silently justify a vector lease.

### Proof obligations

At every structured merge, all incoming edges must agree on value and dirty/canonical
state, or the lease must be materialized before the edge. Calls must use a declared
clobber/preservation contract. GC references cannot be held outside the precise root
protocol, and EH/cancellation/trap paths must observe the same canonical state as normal
paths. Differential allocator fuzzing is justified: Cranelift describes its register
allocator as separately fuzzed with symbolic verification because allocator failures
undermine all upper-layer correctness
([Cranelift README](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/README.md)).

Suggested switch: `regional-version-residency` (default off until full qualification).

## 2. Instruction selection and bounded scheduling

### High-confidence covers

Extend Valent selection with typed, target-capability rules rather than opcode-sequence
special cases:

- destination-driven binary/unary/convert lowering, including `local.set` and return
  consumers, so an owned old destination or final-use local becomes the output;
- AMD64 load-as-operand coverage for integer, scalar float, and vector operations when
  the load is single-use, alias-safe, and may legally trap at that exact evaluation
  point;
- AMD64 base + index*scale + displacement covers that reuse the exact address value
  consumed by the bounds proof;
- ARM64 shifted/extended operand covers (`add/sub`, compares, address generation),
  conditional select/increment forms, and load/store pair formation for adjacent,
  independently proven accesses;
- multiply-add/subtract and widening/narrowing SIMD covers only where the Wasm opcode's
  rounding, saturation, lane, and NaN semantics permit the target instruction;
- constant materialization reuse scoped to one straight-line region and one register
  bank, invalidated at calls/joins/unknown effects.

This follows Sethi–Ullman’s general result that expression evaluation order affects
register demand and storage references
([original paper](https://doi.org/10.1145/321607.321620)), while staying within the
existing bounded Valent tree. Cranelift’s x64 selector independently demonstrates broad
rules for sinkable loads, immediates, integer operations, floats, and vectors rather
than workload recognition
([x64 rules](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/codegen/src/isa/x64/lower.isle#L90-L115),
[load rules](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/codegen/src/isa/x64/lower.isle#L3060-L3168)).
wazero likewise reconstructs target address modes from SSA addends on ARM64
([source](https://github.com/tetratelabs/wazero/blob/6edbb8c01a5f7ad6ea4e3de0493667683e8baecd/internal/engine/wazevo/backend/isa/arm64/lower_mem.go#L270-L360)); Railshot should recover only the same local forms from Valent nodes, without SSA.

### Scheduling boundary

A small scheduler is worth testing only for a fixed window of already-deferred,
non-trapping, side-effect-free nodes. Build dependencies in stack arrays, choose between
ready nodes using target latency and projected pressure, then discard the window. Never
move a load, store, division trap, conversion trap, call, atomic, cancellation poll,
GC safepoint, or custom instruction across another observable operation.

LLVM's scheduler explicitly balances latency against register pressure and builds a DAG
with pressure tracking
([source](https://github.com/llvm/llvm-project/blob/6a183b8853b79a134402332cf4a776a8090d1b88/llvm/lib/CodeGen/MachineScheduler.cpp#L1750-L1770)).
Reproducing that machinery would violate Wago's cost target. A four-to-eight-node,
pure-only window is the maximum sensible experiment; it should remain off if it does not
show gains on both out-of-order desktop and low-end/in-order ARM hardware.

Suggested switches: `machine-cover-v2`, architecture-specific sub-switches for rule
families, and experimental `pure-window-schedule`.

## 3. Calls, returns, and dispatch

### Direct calls

The most general remaining gains are data movement, not removing validation:

- coalesce one- and two-result calls directly into final local/return registers;
- add a limited two-result register ABI using the established target ABI banks;
- preserve only proven-live values across a call, and only in registers whose callee
  contract permits it;
- keep call arguments as deferred leaves until their final ABI destination, then solve
  parallel moves with fixed-size storage;
- make callee “preserves pins” metadata exact and conservative; any indirect, imported,
  recursive, EH, GC, or custom boundary defaults to clobbering.

Railshot already stages mixed GP/FP arguments in bounded arrays and avoids round trips
for register-resident arguments
([AMD64 call staging](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/amd64/call.go#L1890-L1990),
[ARM64 staging](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/arm64/call.go#L1968-L2108)).
The safe next step is to reduce the `flushBelow`/result reconciliation region, not to
change the wrapper boundary.

### Indirect calls

Every ordinary `call_indirect` must retain table bounds, null, dynamic type, target
context/home, descriptor-kind, GC-domain, and ABI-route checks unless a stronger,
instance-valid proof makes one redundant. The Wasm semantics execute a table lookup,
cast/type check, and call, trapping on invalid cases
([Wasm 3.0 execution](https://webassembly.github.io/spec/core/exec/instructions.html#exec-call-indirect)).
Wago's current lowering explicitly performs these checks and limits immutable-table
specialization to proven cases
([AMD64 lowering](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/amd64/call.go#L2609-L2775),
[ARM64 lowering](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/arm64/call.go#L2279-L2445)).

A mutable-table inline cache is safe only with a monotonically changing table epoch
updated by every guest and host mutation path (`table.set`, `grow`, `fill`, `copy`,
`init`, element changes, linking/rebinding, and host API changes). The fast path must
compare the epoch, then use a snapshot whose code pointer, signature identity, home
context, and ABI kind were published consistently. Atomic ordering is required when a
table can be shared. This is general but not a first-wave optimization: its lifecycle
and concurrency surface is larger than its likely whole-corpus benefit.

Suggested switches: `multi-result-reg-abi`, `call-live-window`, and experimental
`table-epoch-inline-cache`.

## 4. Memory addressing and bounds-check proofs

wazero records a known safe bound for a base SSA value within a block, reuses the
absolute address, and otherwise computes an unsigned extended address and checks the
end against current memory length
([source](https://github.com/tetratelabs/wazero/blob/6edbb8c01a5f7ad6ea4e3de0493667683e8baecd/internal/engine/wazevo/frontend/lower.go#L4322-L4405)).
Railshot's straight-line certificate is already the analogous no-IR mechanism. Improve
it in this order:

1. **Certificate value versions.** Key facts by local/global version rather than only
   source identity, using the bounded event tape. This permits safe retention across
   unrelated definitions while invalidating exactly on a source write.
2. **Address/check unification.** Produce one address descriptor/value consumed by both
   the comparison and the final access. Selection may fold it into a target addressing
   mode only if the emitted access is proven to compute the same mathematical address.
3. **Adjacent access groups.** One maximum-extent check may cover multiple loads or
   stores only when all accesses use the same versioned base, overflow is impossible,
   and no intervening instruction can trap or expose reordered effects.
4. **Affine loop certificates, experimental.** Admit only canonical induction variables
   with a constant nonzero step, statically bounded trip condition, invariant base, and
   checked first/last byte computation in a widened domain. Reject wraparound, multiple
   entries/backedges, calls, EH, atomics, shared-memory races, memory/table mutation,
   source mutation, and any earlier observable effect whose order relative to an OOB
   trap would change.

The loop form is not “bounds-check removal”; it is a stronger check moved to a point
that dominates all covered accesses. If that dominance and ordering proof is not
available, keep every original check. Linear memory can grow, so a previously valid
upper-bound certificate remains valid only when it uses the same address value and the
runtime memory cannot shrink; memory64 arithmetic additionally requires carry/overflow
proofs.

This area has the campaign's highest security severity. The Wasm specification requires
an out-of-bounds access to trap
([memory semantics](https://www.w3.org/TR/wasm-core-2/#memory-instances%E2%91%A0)).
A 2026 Cranelift bug let the checked and accessed addresses diverge and produced an
arbitrary host-memory read/write primitive and sandbox escape; the advisory is rated
Critical
([GHSA-jhxm-h53p-jm7w](https://github.com/bytecodealliance/wasmtime/security/advisories/GHSA-jhxm-h53p-jm7w)).
Therefore every new address/bounds cover needs exhaustive width/offset/wrap tests,
differential execution, guard-page and explicit-mode tests, memory32/memory64 tests,
and generated-code validation that the checked value is the accessed value.

Spectre masking/fencing and signal-based protections are independent security features.
An optimization must not disable them; it must either preserve their data dependency or
decline the rewrite. Cranelift explicitly treats heap, table, and indirect-branch Spectre
mitigations as part of its security posture
([README](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/README.md)).

Suggested switches: `versioned-bounds-certs`, `memory-op-cover`, and experimental
`affine-loop-bounds`.

## 5. Bounded end-of-function tuning/fixup

### Recommended design

Call this a **recorded-site finalizer**, not a second optimization pass. During normal
emission, append compact records only for sites the emitter understands:

```text
site = {offset, kind, encoded length, target/fixup id, proof flags}
```

The finalizer may then:

- delete a branch to the immediately following label;
- thread a label through an otherwise empty unconditional-jump block;
- invert `conditional; unconditional` when the conditional target is the fallthrough;
- select short AMD64 branches when the final displacement fits;
- reclaim known NOP/hole/frame-reservation bytes;
- preserve intentional loop/function alignment while deleting incidental padding;
- deduplicate or place cold trap tails when all callers use the same trap identity; and
- insert an ARM64 veneer when a recorded branch is out of architectural range.

Every rewrite must be monotone (same size or smaller, except pre-reserved veneers),
bounded by fixed policy limits, and followed by exact relocation/metadata remapping.
Opaque plugin bytes, jump-table data, source maps, trap PCs, GC callsites, entry offsets,
inline maps, and cancellation sites must be represented as fragments or hard barriers.
If any range, map, fragment, or budget validation fails, emit the identity result.

This is directly aligned with Cranelift's `MachBuffer`, which records label uses,
deadlines, islands, veneers, and latest branches and documents the invariants needed for
semantic preservation and avoidance of quadratic behavior
([source and invariants](https://github.com/bytecodealliance/wasmtime/blob/668016926adfd1b8a79dbce894f1e203d8892599/cranelift/codegen/src/machinst/buffer.rs#L1-L165)).
Railshot already has size-preserving branch folding that revalidates the exact emitted
byte shape before mutation
([AMD64 peephole](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/backend/railshot/amd64/peephole.go#L10-L75)).
Extending its record format is safer and cheaper than scanning arbitrary native bytes.

### Cost model

- Site records: capped per function; compact integers; scratch reused by the compiler
  worker.
- Relaxation: a small fixed number of monotone iterations, or one ordered worklist over
  sites affected by deletions.
- Mapping: the existing bounded offset-deletion map, extended only after memory and
  latency measurement.
- Admission: functions exceeding site, byte, or iteration limits use identity
  finalization—not an allocation-heavy fallback.

Suggested switches: `late-branch-thread`, `short-branches`, and `cold-tail-layout`, each
separate even if they share the finalizer.

## 6. SIMD

SIMD work is general when it implements standardized vector semantics across all valid
inputs. The portable SIMD proposal intentionally exposes a common 128-bit subset, while
hardware instruction sets differ
([SIMD proposal](https://github.com/WebAssembly/spec/blob/main/proposals/simd/SIMD.md)).
Accordingly:

- classify covers by exact lane width, signedness, saturation, NaN/rounding behavior,
  and out-of-range lane semantics;
- build shuffle lowering from the 16-byte immediate as a general permutation problem,
  selecting one- or two-input target forms without memorizing corpus masks;
- combine splat/load/extend/narrow/reduction forms only under exact single-use and trap
  ordering;
- retain v128 locals and constants by bank-specific phase scores;
- use AVX/AVX2/AVX-512 only through validated CPU feature policy and artifact identity;
- keep ARM64 NEON and AMD64 rules separate when their semantics differ.

Relaxed SIMD explicitly permits one of a defined set of results for operations such as
FMA and swizzle, but does not permit arbitrary substitution
([proposal overview](https://github.com/WebAssembly/relaxed-simd/blob/main/proposals/relaxed-simd/Overview.md)).
The optimization should document which permitted choice each architecture makes and
test every allowed edge class. Strict SIMD operations must never silently use relaxed
semantics.

Suggested switches: `simd-cover-v2`, with CPU-feature-specific children only when a
user-visible independent toggle is useful.

## 7. Architecture-specific priorities

### AMD64

Prioritize memory-operand covers, short branches, destination coalescing, frame/slot
packing, and removal of redundant moves/zero-extensions. The smaller GP register file,
fixed-role arithmetic registers, variable-length encoding, and rich memory operands
make pressure and code layout tightly coupled. Any policy that “uses one more pin” must
account for extra spills and REX bytes, not just avoided local loads.

Most promising AMD64 order:

1. versioned regional leases with a hard transient floor;
2. broader typed load-as-operand and final-destination covers;
3. two-result ABI plus result-to-local/return coalescing;
4. short-branch/fallthrough finalization while preserving loop alignment;
5. SIMD cover expansion.

### ARM64

Prioritize shifted/extended operands, address formation, phase-local use of the larger
register file, load/store pairs, constant reuse, and careful scheduling on lower-end
cores. Fixed-width instructions make size changes predictable, but conditional branches
have finite range; Arm documents ±1 MiB for `B.cond` and ±128 MiB for unconditional
branches
([Arm A64 overview](https://developer.arm.com/-/media/Files/pdf/graphics-and-multimedia/ARMv8_InstructionSetOverview.pdf)).
The finalizer must validate every remapped range and use veneers rather than truncating
an offset.

Most promising ARM64 order:

1. versioned regional leases while preserving the tested scratch floor;
2. extended-register/shifted-operand and address covers;
3. pair loads/stores for proven adjacent frame/context accesses;
4. call/result coalescing;
5. pure-window scheduling, only after the above is stable.

## Security and correctness ledger

No optimization should be enabled without explicit evidence for its applicable row:

| Boundary | Must remain true | Consequence if violated | Required evidence |
|---|---|---|---|
| Linear memory | Checked effective address exactly equals accessed address; unsigned overflow cannot validate wraparound | Host OOB read/write, sandbox escape | Boundary-value differential tests, explicit/guard modes, memory32/64, generated-code checks |
| Tables/indirect calls | Runtime length, null, type, home/context, ownership/domain, and ABI kind are checked or removed only by an instance-valid immutable proof | Type confusion, wrong-instance access, arbitrary native target/crash | Mutable/host mutation tests, cross-instance tests, GC/EH tests, descriptor adversaries |
| Register allocation | Every use sees the defining value; fixed/clobbered registers and merge states are exact | Silent miscompile; pointer/root corruption | Symbolic allocator checker or model, randomized programs, pressure extremes, both architectures |
| Calls/GC/EH | Roots are published at every safepoint; canonical state exists on trap/unwind/host paths | Use-after-free, leaked/wrong references, corrupt unwinding | Forced collection, nested host reentry, EH, tail-call, cancellation tests |
| Trap ordering | No effectful or independently trapping operation crosses a moved/elided trap | Observably wrong host/memory state or wrong trap | Near-miss tests around stores, calls, divisions, conversions, atomics, custom ops |
| SIMD | Exact strict or permitted relaxed result for all lanes/NaNs/out-of-range inputs | Silent data corruption | Spec vectors plus randomized differential execution per CPU feature set |
| Native finalization | All branches, entries, relocations, trap/source PCs, GC sites, fragments, and plugin regions remap exactly | Jump into data/wrong code, lost traps/roots | Identity-mode byte equality, map validators, max-range and opaque-fragment tests |
| Cancellation/stack safety | Polls, fences, and trap cells remain reachable with their original concurrency semantics | Unstoppable guest, stack corruption, denial of service | Infinite-loop cancellation, concurrent close, recursion/fence stress |

Security checks are optimization inputs, not obstacles to delete. The proper patterns
are proof-gated elision, cached stable metadata with freshness rules, and cold outlining.
When proof is incomplete, the old checked path must remain byte-for-byte available.

## Registration and implementation seams

All major additions should use Wago's existing catalog. `Definition` owns the public
name/label/description/default/experimental/architectures, `NewBindings` rejects missing
or unknown architecture bindings, and `Selection.EnabledOption` gives the lowering path
a pre-resolved bit test rather than a string/map lookup
([catalog](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/core/compiler/optimization/catalog.go#L1-L120)).
Runtime-local selection already flows through `RuntimeConfig.WithOptimization`
([configuration](https://github.com/wago-org/wago/blob/a83fc0040c8fd85edebeaa1e59fd291f76712aa6/src/wago/config.go#L442-L470)).

Concrete seams:

| Work | AMD64 | ARM64 | Shared/public |
|---|---|---|---|
| Metadata/event collection | `railshot/amd64/hints.go` | `railshot/arm64/hints.go` | `railshot/shared/*hints*.go`, residency summaries |
| Regional residency | `amd64/interval_region.go`, `compile.go`, `localstate.go`, `control.go` | same files under `arm64/` | event/shadow planner in `railshot/shared` |
| Tree covers/addressing | `amd64/emit.go`, `memory.go`, `fp.go`, `simd.go`, `fuse.go` | corresponding ARM64 files | only semantic predicates that truly match both targets |
| Call/result work | `amd64/call.go`, `compile.go`, ABI tests | corresponding ARM64 files | `railshot/abi`, runtime descriptor layouts |
| Late tuning | `amd64/finalize.go`, `peephole.go` | corresponding ARM64 files | bounded offset maps/fragments in `railshot/shared` |
| Knobs | `amd64/knobs.go` | `arm64/knobs.go` | `compiler/optimization/catalog.go`, CLI generated schema, artifact/cache identity |
| Evidence | backend stats/goldens and `src/wago` execution tests | same | `bench/cmd/explain`, all-corpus paired benches |

Each knob needs:

- catalog definition and architecture binding;
- immutable capture into `CodegenPolicy`/artifact identity;
- runtime config and CLI/schema coverage;
- an on/off structural counter or code-shape assertion;
- semantic equivalence and adversarial near-miss tests;
- a stable environment A/B oracle where applicable; and
- documentation of default, experimental status, and rollback.

Do not expose one umbrella `v4` switch as the only control. A convenience profile may
select several registered options, but the individual mechanisms must remain separable.

## Recommended campaign order and gates

### Stage A: evidence and activation of existing metadata

1. Add no new retained metadata. Use the existing shadow planner to report per-function
   projected benefit/debt alongside actual loads, stores, spills, and code size.
2. Activate only profitable phase leases behind `regional-version-residency`.
3. Require exact stats-on/off code equality and overflow fallback equality when the new
   knob is off.
4. Qualify on all scalar and SIMD corpora, not a chosen subset.

### Stage B: machine covers

Land rule families in separate commits/switches: final destination, address modes,
ARM64 shifted/extended operands, then SIMD. Every rule gets positive and near-miss
structural tests plus randomized semantic differential tests. Delete rules whose broad
paired result is neutral after accounting for code size and compile resources.

### Stage C: calls

Add two-result register ABI, then call-live windows. Test direct, recursive, imported,
indirect, tail, cross-instance, host reentry, GC, EH, cancellation, and both bounds
modes. Do not combine an indirect-check change with ABI movement; they need independent
review and attribution.

### Stage D: finalizer

Add recorded-site branch threading and short-branch relaxation using the existing
offset maps and identity fallback. Validate function entries, internal entries,
relocations, jump tables, trap PCs, GC sites, source maps, custom/plugin fragments, and
maximum branch ranges. Compare code layout/disassembly whenever timing changes, because
alignment can masquerade as an optimization.

### Stage E: proof-heavy and experimental work

Only after the prior stages: versioned adjacent bounds groups, affine loop bounds,
mutable-table epoch caches, and pure-window scheduling. These have the largest proof
surface and should not be needed to establish whether the lower-risk campaign is
working.

### Aggregate acceptance gate

Use predeclared corpus membership and report per-row ratios plus geomean; never hide a
major regression behind one large win. Use interleaved/paired samples on the same
machine, process configuration, bounds mode, CPU feature set, Go version, and exact
commits. Gate at minimum:

- execution: target broad-corpus geomean and disclose every row;
- compilation: Wago remains at least 2.7x faster than wazero on the agreed compile
  workload/geomean;
- compiler memory: Wago remains within the agreed 6–7x-lower target, with B/op and
  allocs/op reported separately;
- native size and runtime RSS: no material unexplained regression;
- correctness: root suite, bench module, Wasm spec/regression suites, race where
  relevant, cross-builds, randomized differential execution, and architecture-native
  execution;
- toggles: each mechanism independently changes only its intended codegen and can be
  disabled without changing validation or safety policy.

The 30% execution objective should remain an outcome gate, not a heuristic input. If
the general mechanisms above do not reach it, report the measured gap and continue
finding general bottlenecks; do not lower the baseline, change weights after seeing
results, specialize a corpus, or remove a security boundary.

## Primary sources consulted

- Wago current source and optimization record at
  [`a83fc004`](https://github.com/wago-org/wago/tree/a83fc0040c8fd85edebeaa1e59fd291f76712aa6).
- wazero/Wazevo current source at
  [`6edbb8c0`](https://github.com/tetratelabs/wazero/tree/6edbb8c01a5f7ad6ea4e3de0493667683e8baecd).
- Wasmtime/Cranelift current source at
  [`66801692`](https://github.com/bytecodealliance/wasmtime/tree/668016926adfd1b8a79dbce894f1e203d8892599).
- LLVM current source and official code-generator documentation at
  [`6a183b88`](https://github.com/llvm/llvm-project/tree/6a183b8853b79a134402332cf4a776a8090d1b88)
  and [llvm.org](https://llvm.org/docs/CodeGenerator.html).
- [WebAssembly Core Specification](https://webassembly.github.io/spec/core/),
  [portable SIMD proposal](https://github.com/WebAssembly/spec/blob/main/proposals/simd/SIMD.md),
  and [Relaxed SIMD proposal](https://github.com/WebAssembly/relaxed-simd/blob/main/proposals/relaxed-simd/Overview.md).
- Poletto and Sarkar, [“Linear Scan Register Allocation”](https://doi.org/10.1145/330249.330250),
  ACM TOPLAS 21(5), 1999.
- Sethi and Ullman, [“The Generation of Optimal Code for Arithmetic Expressions”](https://doi.org/10.1145/321607.321620),
  JACM 17(4), 1970.
- Arm, [A64 instruction-set overview](https://developer.arm.com/-/media/Files/pdf/graphics-and-multimedia/ARMv8_InstructionSetOverview.pdf).
- Bytecode Alliance, [GHSA-jhxm-h53p-jm7w](https://github.com/bytecodealliance/wasmtime/security/advisories/GHSA-jhxm-h53p-jm7w),
  a primary security case study for checked/accessed-address divergence.
