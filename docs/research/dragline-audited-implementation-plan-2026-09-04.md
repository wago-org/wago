# Audit verdict

The prior plan is **directionally strong but should not be executed verbatim**. I would rate it about **8/10 on architecture and 6/10 on implementation order**.

Its central diagnosis is correct:

> Dragline’s largest remaining architectural debt is the coexistence of a sophisticated machine-SSA backend and a separate structured native emitter, especially because `v128` cannot currently enter RailMach.

However, the plan overreaches in four places:

1. It proposes a more general low-level type system than Dragline currently needs.
2. It migrates SIMD before establishing the explicit selected-instruction seam, which risks reimplementing the same lowering twice.
3. It elevates segmented live ranges too early despite the current dominant gap being vector lowering rather than scalar spilling.
4. It treats deletion of the structured emitter and side tables as goals in themselves rather than decisions that must clear performance, compile-time, memory, and reliability gates.

The corrected direction is:

```text
Rebase and establish exact-head truth
        ↓
Add minimal first-class v128 machinery
        ↓
Add explicit selected-opcode/form seam
        ↓
Move SIMD through that seam family by family
        ↓
Calibrate scheduling on actual execution costs
        ↓
Measure false interference
        ↓
Add segmented ranges and true splitting only where justified
        ↓
Choose between structured and same-IR fast compilation empirically
        ↓
Retire duplicate lowering only after replacement is better
```

---

# 1. Verified current state

The branch remains:

```text
PR:       #544
Head:     04901f738680f6e76f4833f3ff53195d2ece3137
Main:     c46f2129edb52e6f30f4d0bfc5ae105cfde0c84d
State:    draft, open, not currently mergeable
```

Current `main` contains the large Railshot memory-cleanup change from PR #560, while the Dragline branch remains based on its parent. The merge surface includes real conflicts in CLI/configuration, Railshot lowering, GC facts, frame roots, runtime API, and optimization registration. Reconciliation with `main` is therefore genuinely the first task, not paperwork that can be postponed.

The branch’s exact-head AMD64 result is already close to Cranelift:

```text
Ryzen 7 7800X3D
36 exports from 30 application modules
five alternating 500 ms rounds
CPU 7 pinned

Dragline throughput:
    95.58% of Cranelift
```

The remaining AMD64 deficit is concentrated heavily in SIMD and string-processing modules. The broader ARM64 numbers are favorable, but they were captured at an earlier engine revision and are not an exact-head Dragline-versus-Cranelift result.

Several older diagnoses are now stale:

- Bulk memory and saturating conversions have production lowering.
- RailMach already has typed `F32` and `F64` values in the FPR bank.
- The current issue is not an absence of scalar FP typing inside RailMach; it is incomplete routing, structured-emitter paths, result conventions, and the absence of a vector machine type.
- The old state in which every application was slower than Railshot no longer describes current ARM64 results.

From the commit-exact source audit:

- RailMach has `I32`, `I64`, `F32`, `F64`, and `Ref`, but no `V128`.
- The physical banks are GPR, FPR, and flags.
- Any function containing a typed `v128` is excluded from RailMach and remains on the structured emitter.
- Some scalar loop shapes are also deliberately left on the structured path because it still generates better code for those cases.
- RailMach instructions remain largely identified by `wasm.InstrKind`; selected forms and post-RA choices are carried partly through side plans.
- Live intervals have one contiguous `Start`/`End` span; regional fragments provide bounded local register re-entry but are not general split child ranges.
- Late SSA exit is real and mature enough to preserve.
- The ARM64 and AMD64 target files contain two substantial function-emission paths each.

Focused Dragline package tests and `go vet` passed against the reconstructed exact-head source. That supports continuing incrementally rather than pursuing a clean-sheet rewrite.

---

# 2. Audit scorecard

| Recommendation in the prior plan | Verdict | Correction |
|---|---|---|
| Synchronize with current `main` first | **Correct and mandatory** | Preserve current main’s removals; do not revive retired Railshot mechanisms |
| Create a canonical exact-head performance report | **Correct** | Include dynamic emitter attribution and separate historical result epochs |
| Add first-class `v128` to RailMach | **Correct; highest priority** | Use a minimal `TypeV128`, not a general-purpose LLVM-like type algebra |
| Route all SIMD through RailMach | **Correct destination** | Migrate incrementally after adding an explicit selected-opcode/form seam |
| Add explicit target-selected instructions | **Correct** | Mutate or refine the existing machine IR; do not create a third full IR |
| Expand RailSpec substantially | **Correct** | Expand from real migrated forms; do not design the full DSL before the forms exist |
| Add segmented live ranges | **Technically correct** | Measure false interference first; implement holes before arbitrary split products |
| Replace schedule scoring | **Correct** | Use constrained multi-objective/Pareto selection, not one uncalibrated weighted sum |
| Retire structured emitters | **Reasonable long-term goal** | Make retirement contingent on measured replacement quality |
| Delete post-RA side arrays | **Too broad** | Remove side tables that encode hidden target choices; retain sparse metadata and transient analyses |
| Use one encoder per architecture | **Too absolute** | Require one normal function-code semantic path; specialized stub/template encoders are legitimate |
| Keep only two IRs | **Mostly correct** | `StackFunc` may remain a compact decoding stream if released early and no longer drives native emission |
| Implement all target forms in the machine IR | **Correct** | Keep uncommon payloads in sparse side tables to avoid widening every instruction |
| Require 105–110% of Cranelift immediately | **Too aggressive as a migration gate** | Use parity and module-floor gates first; retain 105–110% as a strategic target |
| Cap Go allocation near Railshot | **Wrong primary metric** | Use peak-live bytes and RSS as primary; B/op is allocation volume, not residency |

---

# 3. The largest correction: use a minimal vector type

The prior plan proposed a generalized representation such as:

```go
type LowType struct {
    Kind     TypeKind
    Bits     uint16
    Lanes    uint16
    RefClass uint16
}
```

That is broader than necessary and likely to inflate storage and complexity.

LLVM needs a generic `LLT` because its generic machine operations must represent arbitrary incoming scalar, pointer, and vector types. LLVM also stores generic virtual-register types in a separate table and destroys that table after instruction selection specifically to limit persistent memory cost.

Dragline has a much narrower input language. Wasm opcodes already encode most lane semantics:

```text
i8x16.add
i16x8.mul
i32x4.shl
f32x4.min
f64x2.convert_low_i32x4_s
```

The allocator usually needs to know:

```text
size
register bank
spill size
spill alignment
root/reference behavior
```

It does not need the complete lane interpretation attached to every virtual register.

## Recommended first extension

```go
type MachineType uint8

const (
    TypeInvalid MachineType = iota
    TypeI32
    TypeI64
    TypeF32
    TypeF64
    TypeV128
    TypeRef
)
```

Keep:

```text
TypeV128 → BankFPR
```

on both current targets. Wazevo follows this basic model: `v128` belongs to its floating/vector register class, while its architecture-specific ABI and spill code use 128-bit FPR/XMM loads and stores.

Do not rename `BankFPR` merely to make it sound vector-aware unless the rename materially improves correctness. Document that it means the architectural FP/vector bank.

Add only:

```text
16-byte spill size
16-byte spill alignment
vector move
vector constant materialization
vector block-edge transfer
vector ABI argument/result location
vector call-clobber handling
vector root prohibition
```

Keep exact lane shape in one of:

- The machine opcode.
- The existing SIMD descriptor.
- A sparse vector-immediate side table.
- `Aux` for compact encodable cases.

Introduce `V256` or `V512` only if packet lifting later produces actual wider internal values. Do not design the current allocator around speculative future widths.

---

# 4. The implementation order should change

The earlier plan said, approximately:

```text
add vector type
→ migrate all SIMD
→ introduce explicit selected machine instructions
```

That order risks reproducing the current problem inside the RailMach finalizer:

```text
Wasm SIMD opcode
+ selection side arrays
+ giant target switch
```

You would migrate SIMD once into the existing implicit representation and then migrate it a second time into the eventual explicit selected representation.

## Correct order

```text
minimal TypeV128
        ↓
typed vector ABI/spills/edges
        ↓
selected-opcode/form seam
        ↓
vector foundation through selected forms
        ↓
remaining SIMD families
```

The selected-opcode seam need not be complete before SIMD work begins. It only needs to be real enough that the first migrated SIMD operations use the final model.

A good first slice is:

```text
GenericV128Const
GenericV128Load
GenericV128Store
GenericV128And
GenericV128Or
GenericV128Xor
        ↓
ARM64_MOVI / LDR_Q / STR_Q / AND_V16B / ORR_V16B / EOR_V16B
AMD64_MOVDQU / PAND / POR / PXOR or VEX equivalents
```

Once those forms work through selection, allocation, late SSA exit, post-RA verification, and encoding, the rest of SIMD can follow the same architecture.

---

# 5. Explicit selected instructions are still the right destination

The prior plan correctly identified a major representation problem.

Current final emission effectively consumes:

```text
Wasm opcode
+ selection plan
+ allocation
+ late SSA exit
+ post-RA rewrite plan
+ several target-side arrays
```

The post-RA plan is verified and not merely ad hoc, but final emitters still reconstruct too much of the physical instruction decision.

Production backends generally make the selected target operation explicit before final encoding:

- LLVM GlobalISel progressively transforms generic MIR into target-specific MIR using the same underlying machine representation.
- JSC Air is an explicit assembly-like IR containing target opcodes, operand forms, virtual temporaries, physical registers, stack slots, and operand timing roles.
- Wazevo lowers into machine-specific instructions, allocates, performs post-RA processing, and then encodes.

## Do this without creating another IR

Retain the existing `railmach.Inst` storage and introduce an opcode namespace:

```go
type MOpcode uint16

const (
    // Generic.
    OpGenericAdd32 MOpcode = iota
    OpGenericAdd64
    OpGenericLoad128

    // AMD64 selected.
    OpAMD64Add64RI
    OpAMD64MovdquRM
    OpAMD64PxorRR

    // ARM64 selected.
    OpARM64AddXRI
    OpARM64LdrQ
    OpARM64EorV16B
)
```

Possible record shape:

```go
type Inst struct {
    Aux          uint64
    OperandStart uint32
    Result       VReg
    Source       uint32
    OperandCount uint16
    Op           MOpcode
}
```

That can potentially preserve the existing 24-byte record if `MOpcode` replaces `wasm.InstrKind` rather than being added beside it.

A separate `FormID` is useful only if one opcode has several semantically distinct encodings that the operand kinds cannot disambiguate cheaply. Prefer generated opcode/form combinations over widening every instruction.

## Preserve sparse side metadata

Side tables remain appropriate for uncommon data:

```text
shuffle masks
large immediates
jump tables
call signatures
trap metadata
GC roots
source locations
constant-pool payloads
rare target templates
```

The rule should be:

> A side table may carry uncommon payload or analysis state, but it must not be the only place that says what native instruction an ordinary machine operation means.

---

# 6. Do not make “one emitter” an ideological requirement

The prior plan’s long-term convergence goal is sound, but its deletion requirement is too absolute.

Real systems deliberately use multiple compilation tiers:

- Wasmtime keeps Winch and Cranelift as distinct compilers because their compilation-cost models are fundamentally different.
- WebKit applies important WasmGC fast paths in both its BBQ baseline tier and OMG optimizing tier.
- LLVM GlobalISel was designed so fast and optimized selectors can share a core machine representation and pipeline even while choosing different effort levels.
- TPDE demonstrates that an SSA-adapted backend can combine analysis, selection, allocation, and emission very cheaply when low latency is the main objective.

The problem is not simply that two function emitters exist. The problem is that Dragline currently has **two broad semantic-lowering authorities inside one optimizing engine**, with fixes and feature work duplicated across them.

## Better target

Eventually provide two effort policies over one typed machine foundation:

```text
DraglineFast
    generic Machine SSA
    direct selection
    source-stable scheduling
    RALinearQ
    no retry
    minimal post-RA

DraglineQuality
    semantic optimization
    full selection
    schedule alternatives
    RAGreedyP
    post-RA quality
    bounded retry
```

Both use:

```text
the same machine value types
the same target opcodes
the same ABI model
the same encoder
the same trap/access-width verification
```

The existing structured emitter should remain during migration as:

- Correctness oracle.
- Performance oracle.
- Emergency feature fallback within strict Dragline development.
- Comparison point for giant-function compilation.

After SIMD and ordinary scalar operations are available through the shared machine path, benchmark three options on giant functions:

```text
A. Existing structured emitter
B. Fast Machine-SSA policy
C. Full quality Machine-SSA policy
```

Retire the structured path only if B is acceptably close in compilation cost and no worse in output quality. If it wins materially on giant cold functions, keeping a narrowly isolated fast path may be rational.

The architectural gate should therefore be:

> No ordinary operation family has two independently maintained target-selection implementations.

That is more useful than “exactly one function named emit.”

---

# 7. Segmented live ranges are valid, but too early as the next major project

The current one-span interval representation is a genuine limitation:

```go
type LiveInterval struct {
    Start uint32
    End   uint32
    ...
}
```

LLVM explicitly represents a live range as multiple segments with holes, for example:

```text
[1,20), [50,65), [1000,1001)
```

and uses unions of those segments to model physical-register interference.

Optimized linear scan has long shown the value of lifetime holes, use-position splitting, fixed intervals, moving split positions out of loops, and eliminating unnecessary spill stores.

WebKit’s newer allocator similarly attributes important quality gains to live-range splitting, rematerialization, pinned-register coalescing, and spill-slot coalescing. Its cache-oriented B+ tree substantially reduced allocator time for very large functions.

However, the branch’s own strict scalar corpus currently reports essentially no ordinary spill debt. The largest measured AMD64 deficit is concentrated in SIMD/string modules that do not enter RailMach at all. Segmented allocation cannot improve code that never reaches the allocator.

## Revised sequence

### Stage A — instrument false interference

Without changing allocation, calculate:

```text
contiguous interval length
true block-live length
dead-gap length
segment count
false overlap pairs
weighted false overlap
register pressure with and without holes
```

Suggested metrics:

```go
type LivenessDebt struct {
    ValuesWithHoles       uint32
    TotalSegments         uint32
    FalseLivePositions    uint64
    FalseInterferencePairs uint64
    PeakPressureCurrent   uint16
    PeakPressureSegmented uint16
}
```

### Stage B — multiple segments, one assignment

Allow one value to have several live segments, but still assign one physical location to the entire value.

This immediately removes false interference without introducing split products and transfer boundaries.

### Stage C — split children

Only after Stage B shows measurable remaining pressure, introduce child ranges for:

- Before and after calls.
- Hot loop versus cold exit.
- Fixed-register conflicts.
- Sparse distant uses.
- Safepoint boundaries.

### Stage D — data-structure upgrade

Do not implement a B+ tree merely because WebKit uses one. Begin with:

```text
0–2 segments inline
overflow in a flat function-local slab
sorted small-vector interference
```

Move to a tree or interval map only when profiling proves large-function queries dominate compile time.

This sequence captures most of the benefit while containing implementation and memory risk.

---

# 8. The scheduler diagnosis is right, but the scoring proposal needs refinement

The current final schedule score is principally:

```text
weighted spill debt
copy cycles
physical copies
copy motion
fixed-register repairs
broken fusions
schedule kind
```

It does not directly encode critical-path latency or processor-resource throughput.

The prior audit understated one detail: production already contains target- and size-specific preference rules that sometimes allow a latency schedule when its spill/copy debt remains within a bounded relationship to the retained schedule. Therefore, Dragline is not completely incapable of preferring latency. The problem is that the policy uses **coarse proxies and hand-selected thresholds** rather than a measured execution model.

The broad diagnosis remains valid. The branch’s historical strict-corpus metrics showed almost every function choosing source-stable scheduling and no generic latency/resource winner. That should be treated as a model warning, although it must be refreshed at exact head because production preference overrides have evolved.

## Do not replace it with one simple weighted sum

A score such as:

```text
cycles
+ 8 × spills
+ 3 × copies
− 2 × fusions
```

will be fragile until target costs are calibrated.

Research shows why. Shobaki and colleagues found that a scheduling heuristic with **slightly more spilling** could execute faster because it achieved a better balance between register pressure and instruction-level parallelism. A strict spill-first ordering is therefore wrong—but blindly monetizing every cost in one scalar is also risky.

## Use constrained multi-objective selection

With only two or three candidates, there is no need for a complex optimizer.

### Step 1: reject semantically or physically unacceptable candidates

Hard constraints:

```text
must-fusion broken
fixed-register repair invalid
copy cycle exceeds cap
frame exceeds budget
hot spill increase exceeds objective-specific cap
code growth exceeds objective-specific cap
```

### Step 2: form a nondominated frontier

Candidate A dominates B if it is no worse in all of:

```text
estimated critical path
estimated resource cycles
hot spill cycles
copy/fixed-repair cycles
native bytes
```

and better in at least one.

### Step 3: select by objective

For `Speed`:

```text
minimum estimated execution cycles
then native bytes
then source stability
```

For `Balanced`:

```text
minimum estimated cycles + bounded byte penalty
```

For `Size`:

```text
minimum bytes subject to a maximum cycle-regression threshold
```

## Improve the scheduler itself

Cranelift’s open scheduling design proposes:

```text
Last Use Count
→ Critical Path
→ source order
```

because LUC reduces pressure while critical-path height recovers instruction-level parallelism.

A practical Dragline priority should be:

```text
1. mandatory adjacency/fusion
2. last-use count
3. critical-path height
4. target resource availability
5. pressure delta
6. source order
```

For latency-oriented mode, critical path can move ahead of LUC only while predicted bank pressure remains below a configured threshold.

## Opportunity-gate alternate schedules

Do not allocate and score three schedules for every function.

Use source order alone when:

```text
ready width never exceeds one
critical path ≈ instruction count
no target fusion alternatives exist
pressure is below capacity
block is tiny
```

Generate alternatives only when the dependency graph has actual scheduling freedom.

Exact combinatorial systems such as Unison demonstrate that integrated schedule/allocation decisions can outperform normal compiler heuristics, but their main value for Dragline is as an offline quality oracle, not a production dependency.

---

# 9. Memory and compile-time gates need correction

The previous plan mixed three different quantities:

```text
Go B/op
peak live compiler storage
process RSS
```

They are not interchangeable.

The PR’s broad Go-engine report explicitly describes `B/op` as total Go-heap allocation volume for decode, validation, and compilation, not peak live heap or RSS.

An optimizing compiler may allocate and reuse several generations of temporary storage while still maintaining low peak residency. Conversely, low B/op does not prove low RSS if large arenas remain retained.

## Primary gates

Track per function and module:

```text
peak compiler-owned live bytes
peak Go heap after GC
fresh-process peak RSS
p50/p95/p99 compile latency
largest-function latency
largest-function workspace
retained worker capacity after compilation
```

## Secondary gates

Track:

```text
B/op
allocs/op
total bytes copied
arena high-water mark
candidate-workspace bytes
```

## Better targets

### Ordinary application corpus

```text
compile wall geometric mean:
    ≤1.5× Cranelift

peak RSS geometric mean:
    ≤0.75× Cranelift

peak compiler-owned function workspace:
    explicit absolute budget, initially 32–64 MiB

native code:
    ≤1.15× Cranelift during migration
```

### Giant modules

```text
p99 compile latency:
    no unbounded increase

worst module:
    ≤3× Cranelift intermediate
    ≤2× Cranelift mature target

retained worker capacity:
    trimmed after exceptional functions
```

### Railshot comparison

Treat Railshot as the minimum-cost baseline, not a realistic requirement that a fully optimizing compiler allocate only 1.5× as much.

A better intermediate Dragline target is:

```text
ordinary B/op:
    ≤3× Railshot

peak live bytes:
    bounded independently

steady worker retention:
    ≤2× normal-function high water
```

Tighten this only after duplicate emitters and side structures are removed.

---

# 10. The performance gates should be staged

The prior plan made these architectural completion requirements:

```text
compatibility ≥105% of Cranelift
native ≥110% of Cranelift
every important module ≥95%
```

Those remain reasonable **strategic goals**, but they are too aggressive as mandatory gates for every migration phase.

At exact head, AMD64 is approximately 95.58% of Cranelift on the paired application corpus. Moving SIMD into the machine backend may plausibly erase that gap, but assuming an immediate additional 10% broad gain is unsupported.

## Recommended gates

### Architecture convergence gate

```text
AMD64 app geomean:
    ≥100% of current Dragline before the refactor

each migrated SIMD family:
    no >3% regression from structured emitter

compile wall:
    no >10% regression for affected modules

peak live bytes:
    no >15% regression
```

### SIMD milestone 1

```text
current worst SIMD/string modules:
    ≥85% of Cranelift throughput
```

### SIMD milestone 2

```text
each major SIMD/string module:
    ≥95% of Cranelift

full AMD64 application corpus:
    ≥100% of Cranelift
```

### Compatibility release

```text
geomean:
    at least parity with Cranelift

important module floor:
    ≥95%

strategic target:
    ≥105%
```

### Native/profile release

```text
geomean:
    ≥105% realistic release target

strategic/stretch target:
    ≥110–115%

target-specialized kernels:
    permitted substantially larger wins
```

This avoids incentivizing benchmark-specific decisions simply to satisfy an arbitrary broad percentage.

---

# 11. Safety requirements need to move earlier

The prior plan placed most verification work after the architecture changes. Some invariants must exist **before the first SIMD operation is migrated**.

Recent Wasmtime/Cranelift defects illustrate the exact risks:

- An `f64x2.splat` lowering could perform a 16-byte memory read when Wasm required only 8 bytes.
- An `f64.copysign` optimization similarly widened a load beyond the semantically required bytes.
- An AArch64 memory64 lowering computed one address for the bounds check and another for the actual memory access, enabling a sandbox escape.
- A historical x86-64 Cranelift addressing bug expanded a Wasm address beyond the intended effective-address range.

## Required machine-memory descriptor

Every selected memory operation should retain something equivalent to:

```go
type MemoryAccess struct {
    MemoryIndex   uint16

    SemanticWidth uint8
    EncodedWidth  uint8

    AddressValue  VReg
    Offset        uint64
    Alignment     uint8

    BoundsProof   CertificateID
    TrapSite      TrapID
}
```

Verifier invariants:

```text
checked address == accessed address
semantic width == architecturally touched width
unless Wasm explicitly permits the wider access

constant offset participates identically in:
    proof
    bounds check
    selected addressing form

no truncation/extension divergence
trap source remains exact
```

For folds such as scalar-to-vector splat or copysign:

```text
do not fold a scalar memory producer into an instruction whose memory form
touches more bytes than the scalar operation
```

This verifier is more important than early scheduling improvements.

---

# 12. The structured stream should be demoted, not immediately deleted

`StackFunc` is a compact 16-byte-per-instruction source stream with sparse side metadata. It is useful for:

- One-time Wasm decoding.
- Structured-control reconstruction.
- Source identity.
- Rare immediates.
- Debugging.
- Building DraglineSSA.

The problem is its lifetime and authority, not necessarily its existence.

## Near-term

Keep it as:

```text
Wasm decode product
→ DraglineSSA builder input
→ machine lowering source map
```

## After unified lowering

Release or reset its backing arrays as soon as:

```text
DraglineSSA / generic Machine SSA
```

has all required source, trap, and semantic metadata.

## Later decision

Measure whether direct:

```text
Wasm → DraglineSSA
```

would materially reduce compile time or memory. Do not assume another parser rewrite is automatically beneficial.

The selected machine path and duplicate emitter are much larger sources of architectural debt than the compact source stream itself.

---

# 13. Revised implementation roadmap

## Phase 0 — synchronize and freeze a baseline

Rebase onto `c46f2129` or the then-current `main`.

Conflict policy:

- Accept current main’s removal of retired Railshot-specific facts and bounds-hoist machinery.
- Preserve engine-neutral runtime ABI, GC root, artifact, and configuration contracts.
- Move any fact used only by Dragline into Dragline.
- Do not restore a mechanism to Railshot simply because Dragline once shared its type.
- Requalify GC frame roots, native stack fencing, host calls, signal bounds, cache trust, CLI selection, daemon, and tier installation.

Required:

```text
make test
cd bench && go test ./...
git diff --check
complete platform and conformance CI
```

---

## Phase 1 — exact-head observability

Generate one machine-readable status artifact and one Markdown projection.

Add:

```text
function count by emitter
native bytes by emitter
dynamic export time by emitter
sampled cycles by emitter on Linux
operation-family counts by emitter
v128 function count and dynamic weight
false-interference metrics
schedule ready-width metrics
```

Distinguish:

```text
static number of functions
dynamic execution share
native byte share
compile-time share
```

A thousand cold functions should not outweigh one hot SIMD kernel.

---

## Phase 2 — vector-capable machine foundation

Add only:

```text
TypeV128
16-byte stack slots
16-byte alignment
FPR/vector-bank allocation
v128 block arguments
v128 edge affinities
v128 late SSA exit
v128 private-call parameters/results
v128 exact clobbers
```

Do not yet migrate arithmetic.

Tests must cover:

```text
register→register edge
register→spill
spill→register
spill→spill
parallel vector cycle
vector live across call
vector parameter/result
mixed scalar/vector multi-result
```

---

## Phase 3 — explicit target-form seam

Introduce the target opcode namespace in the existing machine instruction.

Add first-class selected forms for:

```text
copy
constant
load
store
bitwise
call
return
branch
```

Introduce exact semantic/encoded memory-width verification.

The first vector operations must use this seam. Do not add another broad SIMD switch to the RailMach finalizer.

---

## Phase 4 — SIMD migration

Migrate in measured families:

| Order | Family | Reason |
|---:|---|---|
| 1 | Constants, moves, loads, stores, bitwise | Proves storage, ABI, allocation, and memory correctness |
| 2 | Integer add/sub/comparisons/shifts | Common and structurally regular |
| 3 | Splat and lane extract/replace | Tests scalar/vector bank crossings |
| 4 | Widening, narrowing, extension multiply | Important to text and crypto |
| 5 | Shuffle and swizzle | Central to current string/SIMD gaps |
| 6 | Dot, pairwise, reductions, movemask | Architecture-sensitive |
| 7 | Floating vector arithmetic | Requires exact NaN/min/max semantics |
| 8 | Relaxed SIMD | Native-feature and determinism policy |

For each family:

```text
old structured emitter vs new machine path
same Wasm
same target
same bounds mode
alternating fresh runs
```

Require correctness, exact trap behavior, code-size control, compile-cost control, and application-level improvement.

Only remove the blanket `v128` routing exclusion after every admitted vector operation has a machine path.

---

## Phase 5 — selected-instruction convergence

After vector forms prove the model, migrate scalar selection:

```text
integer immediates
address modes
compare/branch
scalar FP
conversions
direct calls
indirect calls
bulk memory
GC/runtime templates
```

For each migrated family:

- Selection is complete before final emission.
- Final emitter validates and encodes; it does not choose the instruction again.
- Delete only the side arrays that carried that now-explicit choice.
- Retain sparse source, trap, call, and uncommon-payload metadata.

Split the target files by responsibility:

```text
legalize
select
abi
postra
encode
stubs
traps
```

Do not perform a file-only refactor before the representation changes; that would merely distribute the same hidden state across more files.

---

## Phase 6 — scheduler 2.0

First add metrics:

```text
average and maximum ready width
critical-path length
LUC distribution
pressure by bank
candidate frontier
predicted cycles
measured cycles
```

Then implement:

```text
LUC + critical-path + target-resource priority
```

Replace ad hoc large-function/FPR preferences gradually, retaining them until the new model is proven.

Candidate selection:

```text
hard correctness/debt bounds
→ nondominated frontier
→ objective-specific cycle/size decision
```

Run alternate candidates only where the dependency DAG has meaningful freedom.

---

## Phase 7 — liveness and allocation

### 7A: false-interference instrumentation

Do this before representation changes.

### 7B: segmented liveness with one location

Support holes while retaining one assignment per value.

### 7C: true split child ranges

Add:

```text
call splits
fixed-register splits
hot/cold splits
loop splits
safepoint splits
```

### 7D: allocator structure

Use flat small segment storage first.

Adopt an interval tree/B+ tree only if profiles show interference queries dominate large-function compile cost.

Preserve late SSA exit throughout. Regalloc2 and SSA-allocation research both support retaining block-parameter semantics and split ranges through allocation rather than materializing all copies early.

---

## Phase 8 — bounds and cold paths

Build on the existing demand-proof infrastructure.

Add:

- Affine loop address expressions.
- Multi-access certificates.
- Conditional prechecks for possibly zero-trip loops.
- Explicit checked-address/accessed-address identity.
- Cold trap pseudo-blocks before layout.
- Exact per-access native width.
- Signal-mode and explicit-mode independent qualification.

Cold trap materialization is partially present already through cold trap patches, so this is a refinement and representation cleanup, not a new subsystem.

---

## Phase 9 — choose the final fast-path architecture

Now compare:

```text
existing structured emitter
fast policy on common Machine SSA
full quality Machine SSA
```

Measure:

```text
small function compile time
large function compile time
peak live bytes
native bytes
execution
engineering duplication
```

Possible outcomes:

### Outcome A — common Machine SSA wins

Retire structured native emission.

### Outcome B — structured path remains materially better for giant cold functions

Keep it as a tightly bounded internal Dragline fast path, with:

- Shared target opcode/encoding definitions where possible.
- No independent semantic optimization.
- No application identity routing.
- Explicit capability threshold.
- Separate performance and correctness gates.

### Outcome C — common Machine SSA is close but expensive

Create a `FastMachine` mode:

```text
no expensive RailSSA passes
single source-stable schedule
RALinearQ
no retry
minimal post-RA
```

The decision must be measured. GlobalISel’s shared fast/optimized core pipeline is evidence that this architecture is practical, while Wasmtime’s distinct Winch and Cranelift compilers are evidence that complete unification is not universally optimal.

---

## Phase 10 — native target and PGO work

Only after the unified vector path is competitive:

- Zen 4/5 scheduling and vector costs.
- Intel client/server scheduling.
- Apple M-series costs.
- Neoverse costs.
- AVX2/AVX-512 width selection.
- BMI2, APX, LSE, MOPS, DotProd, I8MM.
- Indirect-call target profiles.
- Memory-operation size profiles.
- Non-local inlining.
- ExtTSP-like layout.
- Selective native clones.
- Vector packet lifting.

Loop-aware SLP and packet widening remain plausible, but they should follow correct, competitive first-class `v128` lowering rather than compensate for its absence.

---

## Phase 11 — footprint convergence

After each operation family leaves the structured emitter:

- Release corresponding structured-emitter scratch.
- Remove duplicate target logic.
- Release `StackFunc` earlier.
- Free DraglineSSA storage once Machine SSA has retained required metadata.
- Lazily allocate schedule candidates.
- Avoid retaining candidate workspaces after exceptional giant functions.
- Remove obsolete side plans.
- Re-measure planner capacity by category.
- Trim oversized worker buffers.

TPDE’s results reinforce that adapting directly to domain-specific SSA and avoiding unnecessary representation translation can significantly reduce compilation cost. The lesson is to remove redundant storage after the architecture stabilizes, not prematurely omit the machine representation required for quality.

---

# 14. Revised near-term PR sequence

The next practical series should be:

1. **Rebase onto current `main` and reconcile retired Railshot machinery.**
2. **Generate canonical exact-head ARM64/AMD64 status reports.**
3. **Add dynamic structured-versus-RailMach attribution.**
4. **Add schedule-freedom and false-interference metrics.**
5. **Add `TypeV128` and vector spill/alignment support.**
6. **Add vector edge transfers and late SSA-exit tests.**
7. **Add fully banked vector private-call ABI.**
8. **Add selected-opcode namespace and exact memory-access descriptor.**
9. **Migrate vector constants, moves, loads, stores, and bitwise operations.**
10. **Migrate integer vector arithmetic, comparison, and shifts.**
11. **Migrate splats, lanes, widening, and narrowing.**
12. **Migrate shuffles, swizzles, reductions, movemask, and dot operations.**
13. **Migrate floating and relaxed SIMD.**
14. **Remove the blanket `v128` RailMach exclusion.**
15. **Refresh exact AMD64/ARM64 Cranelift comparison.**
16. **Migrate ordinary scalar target decisions into selected opcodes.**
17. **Add LUC and critical-path scheduling metrics.**
18. **Replace schedule selection with constrained multi-objective selection.**
19. **Add segmented liveness with one assignment.**
20. **Add true split products only where measured debt remains.**
21. **Compare structured versus FastMachine giant-function policy.**
22. **Retire duplicate emitter families only after their cutover gates pass.**

This sequence is intentionally different from the previous plan: explicit selected forms begin **before** broad SIMD migration, while segmented allocation moves **after** vector parity and scheduler calibration.

---

# 15. Verification program

The branch already has unusually strong verifiers. The migration should extend them rather than introduce a separate verification framework.

## Per-family shadow compilation

For each migrated operation family:

```text
compile with structured emitter
compile with new Machine-SSA path
execute both
compare all observable state
```

Compare:

```text
results
trap kind
trap source
linear memory
tables
globals
host calls
GC roots
exceptions
```

## Selected-instruction verifier

At each stage:

```text
generic Machine SSA
bank-selected Machine SSA
target-selected Machine SSA
post-RA Machine SSA
```

verify:

- Type compatibility.
- Register bank.
- Operand timing roles.
- Fixed/tied constraints.
- Target feature.
- Semantic access width.
- Actual access width.
- Address equivalence.
- Clobbers.
- Effects.
- Trap order.
- Root and safepoint locations.

## Rule-guided fuzzing

RGFuzz found 20 previously unknown bugs across six Wasm engines by extracting compiler lowering rules and generating inputs directed at them. RailSpec makes Dragline a natural target for the same strategy.

Generate:

```text
valid rule match
one-use violation
immediate boundary
feature absent
different lane shape
intervening memory effect
fixed-register collision
trap-order near miss
access-width near miss
cross-bank copy
```

## Liveness-directed fuzzing

CLIR’s liveness-guided, structure-aware approach found 24 Cranelift bugs in its reported 72-hour campaign, including backend crashes and miscompilations across architectures.

Generate:

```text
multiple liveness holes
long vector values
calls at pressure peaks
block-argument cycles
fixed-register overlap
mixed scalar/vector banks
references across safepoints
hot/cold range splits
```

## Translation validation

Begin with small target-selected rules, not whole-function validation.

The `arm-tv` project found 45 previously unknown LLVM AArch64 backend miscompilations while checking ABI and machine-code translations, which is strong evidence that backend translation validation can still find defects in a mature compiler.

Initial Dragline candidates:

```text
address folds
byte swaps
rotate forms
compare/branch fusion
vector shuffle patterns
narrow/widen patterns
bounds-check elimination
```

---

# 16. Research experiments worth retaining

These remain valuable, but none should interrupt the vector and machine-representation convergence.

| Experiment | Timing | Purpose |
|---|---|---|
| Exact Unison/SLOTHY oracle | After explicit selected ops | Find schedule/allocation quality debt |
| SSA-native allocation | After segmented-range metrics | Compare allocator memory and spill quality |
| Selector synthesis | After RailSpec has real forms | Generate and prove additional rules |
| Affine rematerialization | After true live holes | Shorten address/induction lifetimes |
| Trace/superblock scheduling | After basic scheduler calibration | Improve branch-heavy hot functions |
| Vector packet lifting | After v128 parity | Exploit wider native vector units |
| Learned heuristic distillation | After stable metrics | Improve scheduler/allocator decisions offline |

Recent selector-synthesis work reduced AArch64 and RISC-V rule-generation time from days to under two hours and achieved performance within roughly 4% of existing LLVM GlobalISel on its reported SPEC integer evaluation. That supports an offline RailSpec laboratory, not online synthesis.

Affine live-range reduction is promising for regular address calculations, but its strongest published 2026 results target a spill-free accelerator architecture. It should be treated as an experiment, not assumed to transfer directly to general CPUs.

---

# 17. Final audited recommendation

The prior plan’s **destination is right**:

```text
DraglineSSA
    ↓
typed machine SSA
    ↓
explicit selected target instructions
    ↓
scheduling
    ↓
high-quality allocation
    ↓
late SSA exit
    ↓
post-RA
    ↓
encoding
```

Its **execution order should be corrected**:

```text
not:
    general type system
    → all SIMD
    → selected instruction redesign
    → full segmented allocator
    → delete structured emitter

but:
    exact-head baseline
    → minimal TypeV128
    → selected-opcode and memory-width seam
    → SIMD family migration
    → scheduler calibration
    → measured liveness holes
    → true splits where profitable
    → empirical structured-emitter cutover
```

The three highest-priority engineering moves are now:

1. **Rebase onto current `main` without resurrecting the Railshot mechanisms it deliberately removed.**
2. **Add `TypeV128`, a banked vector ABI, and exact semantic memory-width verification.**
3. **Introduce explicit selected machine opcodes before moving the first SIMD family.**

The prior plan was also correct that the branch should not be rewritten from scratch. The sibling boundary, RailSSA analyses, machine SSA, late SSA exit, private ABI/IPRA, cache, daemon, profiles, tiering, root metadata, and verifier infrastructure are substantial assets.

The goal is not to discard those assets. It is to make the production backend converge so they all operate on the same fully typed, explicitly selected machine program.
