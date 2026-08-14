# Generated native-code size plan

Status: architecture and implementation plan

Source baseline: `main` at `7f7a5f46f03578a7bc59c224e3ffe32f5e3eba47`

Measurement status: source-level audit only; no new corpus measurements were
produced for this plan. Percentage targets are proposed acceptance goals, not
measured predictions.

Implementation measurements are recorded separately in the
[generated native-code size results ledger](generated-native-code-size-results.md).

This plan complements the broader
[single-pass optimization research](singlepass-optimization-research-and-issues-2026-08.md),
the [code-image pipeline plan](code-image-pipeline-plan.md), and Wago's current
[no-whole-function-IR decision](no-ir-plan.md). `ROADMAP.md` remains the project
priority ledger; this document owns the generated native-code size design.

## Executive decision

The highest-leverage next layer is not a traditional optimizer or another
compiler tier. It is a small, allocation-bounded symbolic finalizer after the
current forward Railshot lowering:

```text
existing summary scan
        |
        v
one forward Wasm lowering
Valent/deferred expression trees
        |
        v
maximal-safe machine encodings
+ labels
+ compact relaxation records
        |
        v
bounded function finalizer
- remove dead holes and NOP reservations
- relax branches and frame adjustments
- remap native offsets
- compact in place
        |
        v
objective-aware module layout
- function alignment
- hot/cold placement
- adapter, stub, and literal islands
        |
        v
patch module relocations
seal executable mapping
```

This preserves the useful meaning of single-pass compilation:

- One forward semantic/code-generation pass over each Wasm body.
- No production SSA.
- No reconstructed CFG.
- No global liveness or graph-coloring register allocator.
- No whole-function machine IR.
- No arbitrary disassembly of finalized bytes.
- No second optimization pass over Wasm.
- Only a relocation-like finalization step over explicitly recorded sites.

Production Railshot remains a direct, no-whole-function-IR backend. Valent
operand-stack state, deferred trees, labels, relaxation sites, and metadata
marks are bounded compiler state. The experimental SSA implementation remains
isolated from production.

The work is organized around four foundations:

1. Exact byte attribution.
2. Bounded symbolic finalization.
3. Explicit Speed, Balanced, Size, and Embedded objectives.
4. Costed decisions instead of fixed profitability thresholds.

## Review findings

The architecture is consistent with current Railshot and with the repository's
existing optimization direction. Five details are important during
implementation:

1. AMD64 frame adjustments are still emitted as seven-byte `imm32` forms.
   ARM64 already patches its three-word reservation to a one-word semantic form
   for small frames, or to NOPs for zero frames, but the reserved words remain in
   the image. The proposed finalizer reclaims those bytes; it does not replace
   the existing correctness-preserving patch logic.
2. Function-local compaction and module layout are separate stages. A function
   finalizer can remap local labels, traps, safepoints, pool references, and
   function-relative call relocation fields. Module calls and cross-function
   islands are patched only after compacted function sizes and final module
   offsets are known.
3. Offset remapping needs an explicit boundary convention. Marks may refer to
   the boundary before or after a replaced fragment. A mark inside a deleted
   fragment is invalid unless that `RelaxKind` defines how it moves. Finalizer
   tests must cover both boundaries around every deletion.
4. `WAGO_EXPLAIN=size` should extend the existing `WAGO_EXPLAIN=1` behavior and
   structured stats API rather than create an unrelated diagnostics path.
5. ARM64 immediate-frame relaxation must model both legal immediate encodings:
   the unshifted 12-bit form and the shifted-by-12 form. Unsupported sizes retain
   the current register-materialized sequence.

## Current strengths

Wago already has many transformations that improve speed and size together:

- Deferred Valent-tree lowering, tree reordering, and associative covering.
- Memory-operand folding and immediate-form selection.
- Register merge values across structured control flow.
- Register-ABI internal calls and direct prepared entry.
- Host-adapter elision for non-addressable functions.
- Bounds certificates and loop prechecks.
- Pinned locals and globals, local-result sinking, and store/load forwarding.
- Shared GC resolver stubs and function-local trap-stub sharing.
- Bounded inlining.
- SIMD constant caching and function-local pools.
- Direct serial emission into a module-owned code image.
- Reused module and function scratch storage.

Host-adapter analysis is particularly valuable: wrappers are emitted only for
exports, start functions, `ref.func`-reachable functions, and other externally
addressable cases, rather than for every local function.

The serial paths already emit into checked module-owned buffers and reuse
per-function scratch. A shrinking finalizer can therefore preserve the current
low-memory path. Parallel workers can finalize a function before appending it to
their worker arena. Both backends still align every module function to 16 bytes,
and addressable register-ABI functions may additionally align their internal
entry.

## Where bytes are lost

### Final offsets become authoritative too early

Some existing rewrites can prove an instruction redundant but cannot change
downstream offsets safely.

AMD64 branch-pair folding changes:

```asm
jcc  over
jmp  target
over:
```

to an inverted conditional branch while retaining the five bytes occupied by
the old near jump. ARM64 retains a four-byte NOP for the analogous fold. Its
adjacent store/load forwarding pass can also replace a redundant load with a
NOP but cannot remove the word.

These are direct evidence for controlled compaction.

### Frame sites reserve worst-case forms

AMD64 emits seven-byte `sub/add rsp, imm32` forms and patches their immediate
after body generation. Small and zero frames therefore cannot use a shorter
physical encoding.

ARM64 reserves three instructions at every adjustment site. The current small
frame patcher selects a single immediate `SUB/ADD SP,#imm` and replaces the
other words with NOPs; a zero frame becomes three NOPs. Register-ABI prologues,
epilogues, and tail-transfer sites use the same reservation strategy.

### Alignment is unconditional

Every function is currently aligned to 16 bytes on AMD64 and ARM64. Addressable
functions may also align the internal entry, and AMD64 aligns its module GC-stub
island. A small function can therefore contain module padding, an adapter,
internal-entry padding, frame holes, cold stubs, and a literal pool around a
small useful body.

The magnitude is a measurement question. The structural problem is already
clear and is tracked by [issue #327](https://github.com/wago-org/wago/issues/327).

### Inlining uses body-byte thresholds, not native net cost

The current inliner uses a default encoded Wasm body ceiling of 160 bytes and a
rough 24-byte estimate for the removed call sequence, then admits every eligible
site. It does not exactly price native bytes added at the site, argument binding,
local zeroing, frame and spill growth, lost frame elision, removed dead callee
bytes, adapter reachability, or loop hotness.

Inlining can therefore be either a major size win or a major size regression.

### Constant pools are function-local and allocation-heavier than necessary

AMD64 pool entries currently retain a copied byte slice and a per-constant site
slice. Constants are deduplicated only within a function. There are two separate
projects:

1. Remove compile-time allocation while preserving function-local pools.
2. Deduplicate across functions only when a module or clustered pool wins on
   total native bytes and target range.

This is tracked by [issue #333](https://github.com/wago-org/wago/issues/333).

### Existing stats do not attribute every byte class

`CodegenStats` reports total code bytes, frame size, spills, reloads, bounds
checks, calls, pins, peephole hits, and GC categories. It does not yet explain
where every emitted byte went. Without byte-class attribution, visually obvious
patterns can be mistaken for corpus-level leverage.

## Exact per-site opportunities

These are architectural deltas, not corpus estimates.

| Current pattern | Compact form | Saving |
| --- | --- | ---: |
| AMD64 `jmp rel32` | `jmp rel8` | 3 bytes |
| AMD64 `jcc rel32` | `jcc rel8` | 4 bytes |
| AMD64 folded branch pair | Delete retained hole | 5 bytes |
| AMD64 `sub/add rsp,imm32` | `sub/add rsp,imm8` | 3 bytes |
| AMD64 zero frame adjustment | Delete instruction | 7 bytes |
| ARM64 three-word frame adjustment | One immediate `SUB/ADD` | 8 bytes |
| ARM64 zero frame adjustment | Delete all three words | 12 bytes |
| ARM64 folded branch pair | Delete retained NOP | 4 bytes |
| ARM64 forwarded store/load result | Delete retained NOP | 4 bytes |
| AMD64 stack/reference `disp32` | `disp8` | 3 bytes |
| AMD64 unnecessary REX | Omit prefix | 1 byte |
| AMD64 ALU `imm32` | `imm8` | 3 bytes |
| AMD64 `movabs r64,imm64` | Legal 32-bit form | Up to 5 bytes |
| Optional 16-byte alignment | Remove or reduce padding | AMD64 0-15; ARM64 0-12 bytes |

The encoder already selects short immediates and displacements when information
is available. The broader opportunity is to preserve enough symbolic
information for final layout, slot, frame, and register decisions to expose
those encodings more often.

## Bounded finalizer design

### Record only variable sites

Do not create a machine node per instruction. Fixed bytes remain direct output.
Record only sites that may shrink, disappear, change displacement, move due to
layout, or reference a label, slot, pool item, or metadata PC.

```go
type RelaxKind uint8

const (
	RelaxJmp RelaxKind = iota
	RelaxJcc
	RelaxFrameSub
	RelaxFrameAdd
	RelaxDeadHole
	RelaxAlignment
	RelaxStackRef
	RelaxPoolRef
	RelaxJumpTableRef
)

type RelaxSite struct {
	Off      uint32 // offset in maximal encoding
	Target   uint32 // label, slot, or pool ID
	Aux      uint32 // condition, immediate, width, or other compact data
	Kind     RelaxKind
	LongLen  uint8
	ShortLen uint8
	Choice   uint8
}

type CodeLabel struct {
	Off uint32
}

type CodeMark struct {
	Off   uint32
	Index uint32
	Kind  MarkKind
	Side  MarkSide // boundary-before or boundary-after
}
```

Marks cover call relocation fields, trap PCs, safepoints, root maps, EH handler
addresses, adapter returns, internal entries, jump-table bases and entries,
literal references, plugin relocations, and debug/source mappings.

The slices live in reusable compiler scratch. Ordinary fixed instruction
emission gains no per-instruction heap allocation.

### Emit maximal-safe forms

Forward lowering emits the current known-correct long form, records the
relaxable site and symbolic target, then continues as today. Examples include a
near AMD64 branch, current frame reservations, deletable branch-fold holes, and
optional alignment fragments.

### Solve shrinkage monotonically

For AMD64 branches:

1. Start with all branches long.
2. Apply known deletions and frame shrinkage.
3. Calculate provisional final label offsets.
4. Mark branches that fit `rel8`.
5. Recalculate offsets until no new branch shortens.

A choice only moves from long to short. Initially use linear passes with a
conservative iteration cap. If the cap is reached, leave unresolved sites long.
Correctness never depends on maximal relaxation. Add worklists or prefix-delta
structures only if measurement shows the linear solver is material.

Optional alignment is not part of this monotonic loop. Decide and freeze it at
a defined layout stage, then patch branches against that layout.

### Compact in place

Because finalization only shrinks the maximal encoding, destination offsets do
not exceed source offsets. Copy fixed spans left and re-encode sites into the
same writable function tail:

```go
src, dst := 0, 0
for _, site := range sites {
	copy(code[dst:], code[src:site.Off])
	dst += int(site.Off) - src
	dst += emitChosenForm(code[dst:], site)
	src = int(site.Off) + int(site.LongLen)
}
copy(code[dst:], code[src:oldLen])
newLen := dst + oldLen - src
```

Then apply final function-local displacements, remap metadata, and commit only
`code[:newLen]`. Module relocations are patched after compacted function offsets
are assigned.

### Never disassemble arbitrary bytes

The finalizer operates on explicit fragments:

```text
fixed machine code
relaxable machine site
opaque plugin machine code
jump-table data
literal data
alignment
cold stub
```

This avoids treating jump-table or literal bytes as instructions. Opaque plugin
output remains unchanged unless the plugin supplies compatible relocation and
finalization annotations.

## Native byte ledger

Add `NativeSizeReport` beside the existing operational counters.

### Per-function byte classes

- Function-start alignment and adapter-to-internal padding.
- Host adapter, internal prologue, stack fence, and interrupt check.
- Parameter homing and local initialization.
- Hot body and control branches.
- Call argument setup, call instruction, and result handling.
- Spill stores, spill reloads, and local/global synchronization.
- Bounds checks and trap branches.
- Epilogue, trap/GC stubs, and EH handlers.
- Jump-table code/data and literal pool.
- NOP or dead reservation bytes.
- Inline-added bytes and removed-call bytes.

### Encoding histograms

AMD64 should count near and short-eligible branches; REX prefixes; `disp0`,
`disp8`, and `disp32`; `imm8`, `imm32`, and `imm64`; seven-byte and zero frame
adjustments; and `movabs` values eligible for narrower forms.

ARM64 should count three-word, one-word, and zero frame adjustments; folded
branch and forwarded-load NOPs; redundant move words; MOVZ/MOVN/MOVK sequence
lengths; literal loads; and branch veneers.

### Module byte classes

- Inter-function and stub-island padding.
- Hot bodies and cold adapters/trap/GC/EH fragments.
- Literal and jump-table data.
- Mapped page count and last-page internal fragmentation.
- Serialized metadata bytes.

Keep five metrics distinct:

1. Raw executable instruction and data bytes.
2. Mapped executable pages.
3. Serialized `.wago` bytes.
4. Linked Go executable bytes.
5. Compiler peak memory.

Unqualified “generated code size” means item 1.

Expose a stable structured report through `ModuleStats.NativeSize`. Extend the
existing explain surface so `WAGO_EXPLAIN=size` selects size detail while
`WAGO_EXPLAIN=1` remains backward-compatible. Collection stays opt-in and the
nil-stats production path stays allocation-neutral.

## Optimization objectives

Expose four coherent objectives, while retaining individual internal knobs for
tests and bisection:

```go
type OptimizationObjective uint8

const (
	OptimizeSpeed OptimizationObjective = iota
	OptimizeBalanced
	OptimizeSize
	OptimizeEmbedded
)
```

### Speed

Enable performance-neutral compaction: branch shortening, dead-hole deletion,
compact frame adjustments, branch-to-next and redundant-move deletion, smaller
displacements/immediates, and cold packing that adds no hot-path work. Retain
alignment and inlining only where measurement supports them.

### Balanced

Remain the default. Enable neutral compaction, adaptive alignment,
native-byte-aware inlining, bounded machine combination, and cold sharing that
does not add common-path work. Permit only a small measured code-growth budget
for clear runtime wins.

### Size

Disable optional function/internal-entry alignment, retain loop alignment only
for statically hot measured cases, inline only for non-positive net bytes except
for a tiny proved class, and more aggressively consider cold sharing, module
pooling, compact jump tables, bounded cold outlining, and lower growth budgets.

### Embedded

Build on Size and allow a separately versioned product contract: offline AOT,
fixed imports, explicit bounds, no target plugin registry, a compact ABI, and an
exact flash/RAM resource manifest. This aligns with
[issue #334](https://github.com/wago-org/wago/issues/334) and
[issue #335](https://github.com/wago-org/wago/issues/335).

### Immutable per-compilation policy

Compile the objective into a small immutable policy rather than mutating
package-global booleans:

```go
type CodegenPolicy struct {
	Objective OptimizationObjective
	Flags [2]uint64
	FunctionAlignLog2 uint8
	InternalAlignLog2 uint8
	LoopAlignLog2 uint8
	InlineGrowthBudget int32
	MaxMachineWindow uint8
	MaxRelaxIterations uint8
}
```

Do this before objective-specific behavior. It completes the direction in
[ADR 0001](adr/0001-runtime-local-optimization-selection.md) and removes the
compile-scoped global rebinding/serialization tracked by
[issue #399](https://github.com/wago-org/wago/issues/399).

## Implementation campaign

### Phase 0: corpus and ledger

- Add exact byte classes and encoding histograms.
- Separate raw bytes, mapped pages, artifact bytes, and peak memory.
- Add deterministic machine-readable output and top-N per-function reports.
- Establish micro fixtures for branches, tiny functions, calls, frames,
  `br_table`, SIMD pools, GC, EH, atomics, and both bounds modes.
- Retain macro baselines including SQLite, Ruby, esbuild, json-as, security-rule,
  WasmGC, SIMD/hash, and many-function workloads.

Every optimization report includes native bytes, mapped pages, compile time,
compile B/op, peak RSS, runtime, and corpus hit count.

### Phase 1: immutable policy and identity finalizer

- Replace mutable optimization execution state with `CodegenPolicy`.
- Record sites, labels, marks, and fragment boundaries.
- Run identity finalization with all current encodings retained.
- Verify byte-for-byte and metadata parity with the old path.
- Keep a rollout kill switch such as `WAGO_FINALIZE=0`.

The identity matrix covers `Entry`, `InternalEntry`, adapter returns, every call
kind, tail calls, trap PCs, safepoints, root maps, EH, tables, pools, inline
source attribution, and plugin relocations.

### Phase 2: reclaim known dead space

AMD64:

- Delete branch-fold holes.
- Delete zero frame adjustments and select `imm8` when legal.
- Delete branches to the next instruction and explicit dead reservations.

ARM64:

- Physically compact three-word frame reservations to one or zero words.
- Delete branch-fold and store/load-forwarding NOPs.
- Delete branches to the next instruction and redundant moves.
- Repatch every affected branch and mark.

These changes should be execution-neutral or positive: fewer fetched bytes and
NOPs with unchanged useful instruction selection and ABI.

### Phase 3: AMD64 branch relaxation

Implement fixed-point `jmp rel8` and `jcc rel8`, after applying known deletions.
Keep calls as `rel32` and out-of-range branches near. Bound iteration and fall
back to correct long forms. This is the core of
[issue #326](https://github.com/wago-org/wago/issues/326).

### Phase 4: objective-aware alignment

Move wrapper, internal-entry, function, loop, stub, literal, and jump-table
alignment decisions to an explicit layout owner.

- Speed: align only sufficiently large or statically hot functions/loops with
  measured benefit.
- Balanced: use a padding budget; initially no more than
  `min(15, bodyBytes/16)` for function entry alignment.
- Size/Embedded: byte-pack AMD64 functions and use mandatory four-byte ARM64
  instruction alignment unless data constraints require more.

Begin with adaptive contiguous layout. Separate hot internal bodies from cold
wrappers only after finalization and metadata remapping are stable.

### Phase 5: frames and slots

- Generalize frame elision without removing roots, EH state, stack fences, call
  alignment, or live spills.
- Represent frame requirements explicitly (`NeedResultsPtr`, `NeedGCState`,
  `NeedEHState`, `NeedWrapperBuffers`, and `NeedStackSpills`).
- Place frequently accessed unpinned AMD64 locals at small displacements with
  deterministic ties and conservative GC/EH admission.
- Generalize compact i32 storage while preserving type, alignment, and root-map
  correctness.
- Later, retain bounded spill lifetime/access information and pack hot spill
  slots without introducing global liveness.

### Phase 6: bounded machine window and provenance

Use a fixed-capacity symbolic operation window, initially around 24 operations,
in reusable scratch. Flush at targets, joins, calls/safepoints, trap-order and EH
boundaries, atomics, opaque plugin fragments, embedded data, or capacity.

Admit local rewrites such as move-chain collapse, self-move deletion,
spill/reload cancellation, forwarding, dead temporary overwrite, direct result
sinking, address folding, redundant extension/mask removal, and fallthrough
selection. Add target-specific exact-cost rules for AMD64 encoding forms and
ARM64 compare/branch/select/address forms.

Back this with bounded value facts: upper bits known zero, sign extension,
low-bit width, boolean, nonzero, and unknown/dirty state. Propagate facts through
existing values, intersect at joins, and invalidate across unknown calls and
plugin operations. This is the direction of
[issue #438](https://github.com/wago-org/wago/issues/438).

### Phase 7: costed selection and inlining

Use a compact deterministic cost tuple for Valent selection:

```go
type SelectionCost struct {
	Bytes uint16
	HotUops uint16
	Latency uint16
	RegNeed uint8
	FlagNeed uint8
	ColdPenalty uint8
}
```

Objectives compare these fields lexicographically, with the first new spill
priced heavily. Apply the model gradually to tree order, covers, immediates,
memory operands, constants, helper calls, branches/selects, jump tables, and
inlining.

Inlining estimates native bytes added at a site against removed call setup,
result handling, dead callee body, padding, and adapter bytes. It also prices
caller frame/spill growth and lost frame elision. Track actual inline region
bytes to calibrate estimates offline. Size inlines only negative-net sites,
apart from a tiny explicitly justified class. Single-call body merging requires
exact addressability analysis for exports, start, tables, `ref.func`, mutable
tables, cross-instance references, and plugins.

### Phase 8: module-scale layout

- Choose `br_table` forms by exact code, table, alignment, duplicate-target,
  and branch-behavior cost.
- Mark table data separately and consider 8/16/32-bit or target-ID entries.
- Replace slice-keyed function pools with fixed constant keys and flat reusable
  site storage before attempting module pooling.
- Add AMD64 module literal islands and ARM64 clustered islands only at a measured
  size crossover.
- Separate hot bodies, warm entries, cold adapters, trap/GC/EH fragments,
  literals, and tables.
- Share adapters or invariant cold tails only when thunks, veneers, and added
  hot-path branches are included in the total cost.
- Track and, where safe, decommit unused page tails before RX sealing while
  preserving W^X.

Serialized artifact compaction remains a separate metric and workstream even
when it consumes the finalizer's function lengths and delta-encoded native PCs.

## Later bounded workstreams

These are deliberately sequenced after the first finalizer campaign. Each must
earn complexity through the byte ledger and corpus hits.

### Region-local common-subexpression reuse

A fixed 16-32-entry open-addressed table may reuse repeated pure expressions
inside one effect-free region. Clear it at every control or effect boundary and
admit only constants, versioned local/global reads, non-trapping integer and
bitwise operations, shifts, and proven-safe address arithmetic. Do not admit
loads without immutability/alias proof, division/remainder, trapping operations,
calls, GC, atomics, or ambiguous floating-point expressions.

Increment a small version on each local/global assignment. Reuse only when
recomputation costs more bytes than materialization plus uses and the retained
value does not create a spill. This remains lower priority than layout, frames,
and branches.

### Register allocation with encoded-size cost

On AMD64, prefer low registers only as a tie-breaker after spill count,
critical-path cost, and move count. A saved REX byte does not justify an added
move, spill, larger frame, `disp32`, save/restore, or lost frame elision.
Prioritize encoded-size ties for frequently used and loop-carried values.

ARM64 has no REX cost, but register choices still affect specialized forms,
pair loads/stores, argument moves, callee-save state, and scratch availability.
Use the exact selected sequence rather than a generic preference for more pins.

### ABI and adapter reduction

The existing separate `Entry` and `InternalEntry` tables permit a future layout
with dense hot internal bodies and dense cold wrappers. Start with independent
alignment; split sections only after metadata remapping is proved.

For repeated physical signatures, compare duplicated wrappers with small
function-specific thunks plus a shared signature adapter. A more aggressive
Size/Embedded form may use immutable descriptor-driven dispatch. Both choices
must include thunk bytes and added indirect branches; Speed/Balanced should not
add host-entry work without measured support.

Expand parallel argument moves to reuse already-correct argument registers,
sink constants and frame loads directly to destinations, skip staging dead
values, retain results in return registers for their next consumer, and allocate
cycle-breaking scratch only for actual cycles. Attribute setup bytes by call
kind.

Fixed-import direct calls belong only to the separately versioned Embedded
product. The general runtime retains dynamic import semantics.

### Cold traps, bounds, GC, and EH

Measure module-shared trap tails that keep source/function attribution in a
compact site ID, local mini-stub, table, or immediate. AMD64 can generally use
module-scale relative branches; ARM64 must include conditional range and veneer
cost. Share only the invariant trap-record/restore tail.

Entry stack-fence and interrupt checks may share address setup or loads, but
must retain both safety properties. Bounds work continues through bounded fact
intersection, adjacent-range checks, loop-invariant bases, vector/bulk ranges,
and known GC-array lengths while preserving Wasm trap order across stores,
calls, division, allocation, atomics, and other observable traps.

Apply the same exact crossover to GC write barriers, cast slow paths,
allocation refill, bounds failure, and EH restoration/dispatch tails. A shared
five-byte helper reached by a five-byte call is not a win. Products without EH
must remain free of EH scaffolding.

### Serialized artifact size

`.wago` size is a separate track. Once final native layout is stable, consider
function lengths instead of duplicate absolute entry arrays, compact
`InternalEntry-Entry` deltas, delta-encoded PCs, interned or run-length root
masks, optional debug/name sections, and streaming code. Never report these as
generated native-code savings.

## Recommended PR sequence

1. Native byte ledger; no codegen change.
2. Immutable per-compilation policy and objective enum; no behavior change.
3. Identity finalizer with complete offset remapping.
4. ARM64 physical frame/NOP compaction.
5. AMD64 frame relaxation.
6. Branch-fold hole deletion on both architectures.
7. AMD64 short-branch relaxation.
8. Branch-to-next and redundant-tail deletion.
9. Objective-aware function and internal-entry alignment.
10. AMD64 local-frame ordering and `disp8` reporting.
11. Generalized compact i32 frames.
12. Native-byte-aware inlining estimator and actual-byte feedback.
13. Generalized provenance facts.
14. Fixed-capacity machine window, identity first, then cleanup rules.
15. Costed Valent covers.
16. Costed and explicitly fragmented `br_table` lowering.
17. Allocation-free function-local constant pools.
18. Measured module/cluster literal islands.
19. Cold adapter separation.
20. Objective-gated shared adapters and cold-stub crossovers.
21. Separately versioned Embedded ABI and AOT resource model.

Each PR remains independently measurable and reversible. A later PR must not be
needed to justify an earlier PR's correctness or resource bound.

## Priority matrix

| Workstream | Byte leverage | Performance risk | Compile-memory risk | Priority |
| --- | --- | ---: | ---: | ---: |
| Exact byte ledger | Enables all later work | None | Very low, opt-in | P0 |
| Identity finalizer/remapper | Enables all compaction | None | Low | P0 |
| ARM64 physical frame compaction | High on framed functions | None | Very low | P1 |
| AMD64 frame compaction | Medium-high | None | Very low | P1 |
| Branch-fold hole deletion | Medium-high | Neutral/positive | Very low | P1 |
| AMD64 short branches | High in control-heavy code | Neutral/positive | Low | P1 |
| Adaptive alignment | Very high for tiny functions | Low-medium | None | P1 |
| Frame/local ordering | High for local-heavy AMD64 | Low | Low | P1 |
| Native-aware inlining | Potentially very high | Medium | Low | P1 |
| Provenance facts | Medium-high | Low with proofs | Very low | P1/P2 |
| Bounded machine window | Medium-high | Low | Fixed/bounded | P2 |
| Costed `br_table` | Workload-specific high | Low-medium | Low | P2 |
| Function-local pool cleanup | Compile-memory win | None | Positive | P2 |
| Module literal islands | SIMD-specific medium | Medium | Low | P2 |
| Cold adapter/stub islands | High in feature-heavy modules | Medium | Low | P2 |
| Compact Embedded ABI | Very high for Embedded | Product-specific | Positive | Separate |

## Validation gates

Every stage runs the Wago suite, applicable official WebAssembly tests,
cross-backend differential tests, randomized arithmetic/control tests, both
bounds modes, calls and cross-instance paths, tables/reference calls/tails,
SIMD, atomics, GC, EH, plugins/custom instructions, and artifact reload.

Finalizer-specific adversarial tests include:

- Branches exactly at short-range boundaries and cascading shortening.
- Deep control and large functions with many labels.
- Jump tables, literals, and opaque fragments mixed with relaxable sites.
- Multiple adapters and internal entries.
- Frame sizes at 0, 127, 128, 4095, 4096, shifted-immediate boundaries, and
  larger values.
- Tail-transfer frame adjustments.
- Marks on both boundaries of deleted ranges and rejection of undefined
  interior marks.
- ARM64 branch/literal range boundaries and veneers.
- Register-exhaustion retry, failed compilation, and arena rewind.
- Serial/parallel byte identity for the same target and policy.

Initial Balanced gates, subject to Phase 0 noise calibration:

```text
runtime geomean: no significant regression beyond 0.5%
important workload: regressions over 1.5% require investigation and approval
compile wall time: no more than 3% regression
compile B/op: no regression
peak RSS: no more than max(1 MiB, 2%)
correctness: zero exceptions
native bytes: every enabled transform demonstrates corpus-level reduction
```

Initial Size gates:

```text
runtime geomean: within 2%
critical workload: no unexplained regression over 5%
compile wall time: within 5%
peak RSS: within max(2 MiB, 3%)
native bytes: materially below Balanced on representative macro modules
```

For alignment and sharing changes, record instructions, cycles, branch misses,
I-cache/front-end behavior, code-page faults, and hot-page residency where the
platform supports reliable counters.

## Non-goals and guardrails

- No traditional whole-function IR or production SSA tier.
- No generic decoding of finalized machine bytes.
- No blind removal of alignment.
- No blind AMD64 low-register preference.
- No helper sharing without an exact crossover calculation.
- No assumption that inlining is inherently profitable.
- No removal or weakening of bounds safety, stack fences, interruptibility, GC
  roots, type checks, trap attribution, or EH semantics.
- No production exploration of many variants. Offline laboratories may discover
  rules; production Railshot consumes a static bounded catalog and makes one
  costed decision.

## Initial goalposts

These are engineering targets to calibrate after the byte ledger, not forecasts.

```text
Speed
  native code: at least 5% corpus-geomean reduction
  runtime: neutral or faster
  compile B/op: neutral

Balanced
  native code: 10-20% target range
  runtime geomean: within 0.5%
  compile time: within 3%
  peak RSS: within 2%

Size
  native code: at least 15% on large macro modules
               at least 25% on many-small-function modules
  runtime geomean: within 2%
```

Many-small-function modules should have the largest target because alignment,
adapters, frame scaffolding, and near branches dominate that class. SIMD-heavy
and GC/EH-heavy modules may also benefit disproportionately from pooling and
cold-code sharing.

## First campaign

Stop the first campaign after six deliverables:

1. Native byte ledger.
2. Immutable objective policy.
3. Identity finalizer and complete offset remapping.
4. ARM64 physical frame/NOP compaction.
5. AMD64 frame and branch relaxation.
6. Objective-aware alignment.

This attacks every function boundary, addressable internal entry, framed
function, branch-heavy function, retained branch-fold hole, and many-tiny-function
module without fundamentally changing instruction selection, register
allocation, or hot-path semantics.

After that, the three highest-leverage projects are native-byte-aware inlining,
frame/local-slot layout for short AMD64 displacements, and the fixed-capacity
symbolic machine combiner backed by provenance facts.

The decisive rule is:

> Keep Wago's forward semantic compiler, but stop making final machine layout
> irreversible at the instant an instruction is selected.

A small amount of explicit symbolic finalization should unlock most of the code
size benefits normally associated with a larger compiler pipeline while
retaining Railshot's low-memory, fast-compilation character.

## Implementation result: ARM64 single-bit test branches

The ARM64 finalizer now recognizes an explicitly recorded, bounded subset of
single-bit mask tests followed immediately by `B.EQ` or `B.NE`. In Size and
Embedded objectives it rewrites the pair to `TBZ` or `TBNZ`, deletes the now
dead conditional-branch word, and lets the existing offset remapper compact and
repatch the function. The candidate list is fixed-capacity function scratch;
ordinary instructions are not represented as machine nodes, and arbitrary code
bytes are never decoded. `WAGO_ARM64_NO_SINGLE_BIT_BRANCH=1` is the independent
rollback switch.

This is intentionally a narrow finalizer seam: the producer records only
single-bit `TST` instructions that it emitted, while the consumer proves
adjacency, condition, branch range, target safety, and deletion capacity. That
keeps the interface deeper than a generic peephole scanner and preserves opaque
plugin and embedded-data boundaries.

Measured on the checked-in ARM64 Size corpus:

```text
rollback native bytes:  75,241,428
candidate native bytes: 75,233,428
net reduction:              8,000 (0.0106%)
selected sites:             2,083

Ruby compile median:   607,949,666 -> 601,127,500 ns/op (-1.12%)
esbuild compile median:330,125,042 -> 331,809,166 ns/op (+0.51%)
compile allocation class: unchanged

JSON serialize median: 18,598 -> 18,514 ns/op (-0.45%)
JSON deserialize median:37,615 -> 37,176 ns/op (-1.17%)
runtime allocations: zero in both configurations
```

The candidate produced 8,332 bytes of direct instruction shrinkage; downstream
fragment layout made the exact corpus result 8,000 bytes. The full test suite,
ARM64 backend race tests, compact corpus/fuzz run, and complete Size execution
corpus passed.

## Implementation result: AMD64 single-bit mask tests

AMD64 has a smaller architecture-specific counterpart to the ARM64 single-bit
branch fold. For a proved one-bit mask, Size and Embedded now emit
`BT r,imm8` and consume CF directly instead of emitting `TEST r,imm32` and
consuming ZF. This is an instruction-selection choice rather than a finalizer
rewrite: the bit index and register width are known during forward lowering,
and `BT` has no target-range constraint. The previous `TEST` path remains under
`WAGO_AMD64_NO_SINGLE_BIT_MASK_TEST=1` as the exact rollback oracle.

Measured on the checked-in AMD64 Size corpus on `hub`:

```text
rollback native bytes:  66,055,508
candidate native bytes: 66,040,168
net reduction:             15,340 (0.0232%)
selected sites:             7,186

Ruby compile median:   1,014,338,527 -> 1,020,912,107 ns/op (+0.65%)
esbuild compile median:  547,613,019 ->   550,339,312 ns/op (+0.50%)
compile allocation movement: noise-level

JSON serialize median:       22,488 -> 22,545 ns/op (+0.25%)
JSON deserialize median:     40,520 -> 40,498 ns/op (-0.05%)
SIMD JSON serialize median:  27,076 -> 26,996 ns/op (-0.30%)
SIMD JSON deserialize median:51,709 -> 51,586 ns/op (-0.24%)
runtime allocations: zero in both configurations
```

Ruby contributed 11,039 bytes, regexmatch 1,329, SQLite 1,311, esbuild
1,234, and the remaining modules 427 bytes; no module grew. Exact encoder tests,
the full AMD64 backend, its race suite, the compact/fuzz corpus, and the complete
Size execution corpus with finalizer validation passed.

## Implementation result: ARM64 logical-immediate constant moves

ARM64 Size and Embedded now materialize a constant with the one-word
`ORR Rd,ZR,#imm` move alias when the architectural logical-immediate encoding is
available and the existing shortest `MOVZ/MOVN` plus `MOVK` sequence would take
more than one word. Equal-size one-word constants retain their former encoding,
so every selection is an exact byte win. Balanced and Speed retain their prior
bytes. `WAGO_ARM64_NO_LOGICAL_MOVE_IMMEDIATE=1` is the rollback oracle, stored
on each assembler rather than consulted as mutable state during emission.

Measured on the checked-in ARM64 Size corpus:

```text
rollback native bytes:  75,233,428
candidate native bytes: 75,036,220
net reduction:            197,208 (0.2621%)
shorter selections:        47,114

Ruby compile median:   601,595,666 -> 611,807,709 ns/op (+1.70%)
esbuild compile median:333,273,896 -> 340,449,271 ns/op (+2.15%)
compile allocations: unchanged
```

Ruby contributed 117,024 bytes, esbuild 34,800, SQLite 15,808, regexmatch
11,948, wasm3 8,396, Lua 7,240, and the rest of the corpus 1,992 bytes.

The first five-sample execution pass measured scalar JSON serialization at
+0.83%, scalar deserialization at +2.40%, SIMD serialization at -1.08%, and
SIMD deserialization at -1.04%, all allocation-free. Because scalar
deserialization crossed the Balanced investigation threshold, a serialized
alternating ten-pair rerun was taken and confirmed the effect (rollback median
37,671 ns/op; candidate median 38,553 ns/op, +2.34%). The transform is therefore
restricted to Size/Embedded, where it remains below the 5% important-workload
guardrail, instead of being treated as performance-neutral compaction.

Focused encoder and objective-policy tests, the ARM64 backend and race suite,
the compact/fuzz corpus, and the complete Size execution corpus with finalizer
validation passed.
