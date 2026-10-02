# Dragline Master Plan

This document consolidates the sibling-engine architecture, Dragline’s compiler design, implementation strategy, memory model, experimental program, verification system, and release gates.

It supersedes the earlier idea that Railshot would optimize first and Dragline would continue from Railshot output.

---

# 1. Core architectural decision

Railshot and Dragline are **independent sibling compiler engines** targeting the same Wago runtime.

They share validated Wasm and stable runtime contracts. They do not share compiler IR, register allocation, instruction selection, internal calling conventions, optimization state, or native code.

```text
                             Wasm bytes
                                 │
                                 ▼
                        Decode and validate
                                 │
                                 ▼
                    Immutable validated module
                                 │
                    ┌────────────┴────────────┐
                    │                         │
                    ▼                         ▼
                Railshot                  Dragline
            private compiler          private compiler
                    │                         │
                    ▼                         ▼
          Railshot code image       Dragline code image
                    │                         │
                    └────────────┬────────────┘
                                 ▼
                           Wago runtime
```

In `--compiler=dragline` mode:

* Railshot does not run.
* Railshot does not produce an intermediate result.
* Dragline compiles directly from validated Wasm.
* Dragline is free to use any internal representation, calling convention, register model, frame layout, or optimization.

In `--compiler=railshot` mode, Dragline is not initialized.

They are sibling **engines**, not two separate implementations of memories, tables, GC, snapshots, imports, or host calls. The Wago runtime remains shared.

---

# 2. Product positioning

## Railshot

Railshot owns:

* Minimum startup latency.
* Minimum compiler memory.
* Tiny and cold functions.
* Embedded and constrained systems.
* Immediate JIT compilation.
* Default compatibility path.
* Compiler fallback.
* Initial execution in a future tiered mode.
* Differential reference implementation.

## Dragline

Dragline owns:

* Maximum steady-state execution performance.
* Long-lived modules.
* Server and high-throughput workloads.
* Native CPU specialization.
* Profile-guided optimization.
* AOT artifacts.
* SIMD, crypto, parser, database, and WasmGC kernels.
* Expensive machine scheduling and allocation.
* Wago-specific runtime/compiler co-design.

Dragline is allowed to compile more slowly than Cranelift. Its reason to exist is generated-code quality.

A mature Dragline that only matches Cranelift while compiling slightly faster would not clear the strategic bar. Railshot already covers fast compilation.

---

# 3. Performance target

These are engineering gates, not predictions.

## Compatibility mode

Against current Cranelift configured for speed, its quality allocator, equivalent CPU features, and equivalent bounds strategy:

```text
neutral optimized-Wasm geometric mean:
    target ≥5% faster

same-Wasm LLVM backend:
    target within 0–5%

compiler latency:
    permitted up to approximately 3× Cranelift

peak compiler memory:
    target ≤1.5× Cranelift
    target <50–60% of LLVM
```

## Native/profile mode

```text
neutral optimized-Wasm:
    target 8–15% faster than Cranelift

same-Wasm LLVM backend:
    target approximately within ±3%

selected hardware kernels:
    may beat LLVM

Wago-specific calls, GC, bounds, and runtime state:
    target 15–35% faster than generic runtime paths
```

## Kill criterion

Once Dragline has:

* Consumer-driven selection.
* Accurate CPU cost models.
* Pre-RA scheduling.
* A splitting quality allocator.
* Spill placement and rematerialization.
* Post-RA scheduling and repair.
* Native target support.
* Wago-specific call, bounds, and GC specialization.

it must show approximately a 5% broad lead over Cranelift on neutral optimized Wasm.

If it cannot, the project must explicitly determine whether:

1. The cost model is wrong.
2. The scheduler/allocator interaction is weak.
3. The allocator is insufficient.
4. The source Wasm leaves too little backend opportunity.
5. Dragline’s real advantage is limited to Wago-specific workloads.
6. An external optimizing backend is more rational.

---

# 4. Non-negotiable architecture rules

1. Railshot never imports Dragline.
2. Dragline never imports Railshot.
3. Neither engine consumes the other’s IR or native code.
4. Explicit engine selection never performs hidden delegation.
5. Each engine owns its complete compiler pipeline.
6. Each engine may use a private internal ABI.
7. Only stable runtime-facing contracts are shared.
8. Profiles are keyed to original Wasm, not native offsets.
9. Artifacts include engine identity.
10. Dragline complexity must not enter Railshot’s hot path.
11. Railshot design limitations must not constrain Dragline.
12. A shared helper is justified only when it is architecture-neutral and stable.
13. Small duplication is preferable to a bad abstraction.
14. The runtime must not import either engine’s internal compiler packages.

A CI dependency test should reject:

```text
dragline → railshot
railshot → dragline
runtime → dragline internals
runtime → railshot internals
```

---

# 5. User-facing configuration

## CLI

Canonical form:

```bash
wago run --compiler=railshot app.wasm
wago run --compiler=dragline app.wasm
```

Convenience aliases:

```bash
wago run --railshot app.wasm
wago run --dragline app.wasm
```

Target and engine are independent:

```bash
# Portable optimized artifact
wago compile \
  --compiler=dragline \
  --target=compat \
  --objective=speed \
  app.wasm

# Host-specialized optimized artifact
wago compile \
  --compiler=dragline \
  --target=native \
  --objective=speed \
  app.wasm

# Fast native compilation
wago run \
  --compiler=railshot \
  --target=native \
  app.wasm
```

Future modes:

```text
--compiler=auto
--compiler=tiered
```

`auto` is a router policy.

`tiered` independently invokes Railshot and Dragline at different times from the original Wasm.

Neither means Railshot delegates half a function to Dragline.

## Go API

```go
type CompilerEngine uint8

const (
    CompilerRailshot CompilerEngine = iota
    CompilerDragline
)

type CompileOptions struct {
    Compiler  CompilerEngine
    Objective OptimizationObjective
    Target    TargetConfig
    Profile   *profile.Module
    Fallback  CompilerFallback
}
```

Example:

```go
cfg := wago.NewRuntimeConfig().
    WithCompiler(wago.CompilerDragline).
    WithOptimizationObjective(wago.OptimizeSpeed).
    WithTarget(wago.TargetNative)

compiled, err := cfg.Compile(wasmBytes)
```

---

# 6. Fallback policy

## Strict Dragline

```bash
wago run --compiler=dragline app.wasm
```

should mean:

> Compile through Dragline or return a Dragline error.

It must not silently return a Railshot artifact while claiming Dragline was used.

This is required for:

* Correct benchmarking.
* Reproducibility.
* Feature coverage tracking.
* Finding unsupported operations.
* Compiler debugging.

## Whole-module fallback

An explicit fallback policy may support:

```bash
wago run \
  --compiler=dragline \
  --compiler-fallback=railshot \
  app.wasm
```

Initial behavior:

1. Attempt the complete module with Dragline.
2. If an unsupported feature or budget condition prevents complete compilation, discard the incomplete Dragline result.
3. Compile the complete module through Railshot.

No mixed internal ABIs are required.

## Per-function mixing

Do not support ordinary per-function fallback in Dragline 1.0.

Mixed modules require:

* Cross-engine bridges.
* Cross-engine exception handling.
* Cross-engine stack maps.
* Call-site patching or stable veneers.
* Code lifetime management.
* A tier-boundary ABI.

That belongs to the future tiering system.

---

# 7. Shared input contract

Both engines receive the same immutable input:

```go
type CompilerInput struct {
    Module      *wasm.ValidatedModule
    Runtime     RuntimeContract
    Target      TargetConfig
    Objective   OptimizationObjective
    Bounds      BoundsStrategy
    Profile     *profile.Module
    HostEffects []HostFunctionEffects
}
```

## Validated module

Contains canonical Wasm facts:

* Types.
* Signatures.
* Function bodies.
* Imports.
* Exports.
* Memories.
* Tables.
* Globals.
* Elements and data.
* Tags.
* Wasm feature declarations.
* Validation information.

It does not contain:

* Valent state.
* RailSSA.
* RailMach.
* Railshot register decisions.
* Dragline register decisions.
* Native offsets.
* Native instruction information.

## Runtime contract

Describes stable Wago runtime facts:

```text
runtime ABI revision
instance-data layout
memory/table/global descriptors
host transition contract
trap interface
exception interface
GC interface
stack-fence interface
interrupt interface
snapshot constraints
```

Each engine is free to exploit the same contract differently.

## Profile

Profiles must be backend-neutral:

```text
function index
Wasm instruction offset
structured control edge
call site
indirect target
allocation site
```

A profile gathered during Railshot execution can be consumed by Dragline because it describes Wasm behavior, not Railshot machine code.

---

# 8. Shared output contract

Both engines produce a runtime-facing result:

```go
type CompiledModule struct {
    Engine      CompilerEngine
    Image       codeimage.Image
    Functions   []CompiledFunction
    HostEntries []uint32

    Traps       []TrapSite
    Safepoints  []Safepoint
    Exceptions  []ExceptionRecord
    Sources     []SourceMapping

    Artifact ArtifactIdentity
}
```

The runtime does not inspect compiler IR.

The engines may differ in:

* Internal ABI.
* Frame layout.
* Function ordering.
* Register usage.
* Constant-pool layout.
* Slow-path organization.
* Adapter strategy.
* Tail-call convention.
* Safepoint implementation.

They only need to satisfy the shared runtime-facing metadata and entry contracts.

---

# 9. ABI model

There should be three distinct ABIs.

## 9.1 Runtime boundary ABI

Shared and versioned.

Used for:

* Host-to-Wasm entry.
* Wasm-to-host calls.
* Traps.
* Exceptions.
* Runtime reentry.
* Stack switching.
* Exported entries.
* GC safepoints.

Each engine may generate different wrappers implementing the same boundary.

## 9.2 Engine-private ABI

Not shared.

Railshot and Dragline independently choose:

```text
argument registers
result registers
callee-saved registers
context registers
frame headers
stack-slot organization
multi-result convention
tail-call convention
root representation
```

Dragline can therefore use:

* More register results.
* Function-specific clobber contracts.
* Private pinned runtime registers.
* Register bundles.
* Same-memory call conventions.
* No-collect leaf conventions.
* CPU-specific internal conventions.

Railshot does not need to implement them.

## 9.3 Tier boundary ABI

Added only when mixed-tier modules are supported.

A tiered Dragline function may expose:

```text
private Dragline entry
generic cross-tier bridge entry
```

Railshot callers enter through the bridge.

Dragline callers in the same optimized cluster use the private entry directly.

This preserves Dragline freedom while enabling tiering.

---

# 10. Package layout

```text
src/core/compiler/
    compiler.go
    input.go
    output.go
    target.go

    profile/
    codeimage/
    artifact/
    runtimeabi/

    railshot/
        amd64/
        arm64/
        finalizer/
        ...

    dragline/
        summary/
        ssa/
        effects/
        optimize/
        pressure/
        mach/
        railspec/
        select/
        schedule/
        regalloc/
        layout/
        finalize/
        verify/
        replay/
        amd64/
        arm64/
```

Runtime:

```text
src/core/runtime/
    instance/
    memory/
    table/
    globals/
    gc/
    host/
    traps/
    exceptions/
    snapshot/
    pool/
```

---

# 11. Dragline overview

```text
validated Wasm
        │
        ▼
module summaries
        │
        ▼
direct RailSSA construction
        │
        ▼
sparse semantic optimization
        │
        ▼
optional semantic specialization
        │
        ▼
register-pressure shaping
        │
        ▼
lower once to RailMach
        │
        ▼
integrated ordering and instruction selection
        │
        ▼
machine combination
        │
        ▼
pre-RA scheduling
        │
        ▼
register allocation
        │
        ▼
bounded feedback retry
        │
        ▼
post-RA optimization
        │
        ▼
profile-guided layout
        │
        ▼
Dragline finalization
```

The important distinction is:

```text
small semantic optimizer
large physical-quality focus
```

Cranelift currently maintains reusable function IR, CFG, dominator, loop-analysis, and allocator contexts and runs CFG cleanup and aegraph optimization before target compilation.

Wazevo uses a compact sequence of lowering, allocation, post-allocation work, and encoding and restricts local producer folding through effect groups and single-use checks.

Dragline should retain their compactness while investing much more effort in pressure, scheduling, splitting, and post-allocation quality.

---

# 12. Module summaries

Dragline must not retain module-wide function IR.

Use dense function-indexed summaries and CSR edge arrays.

```go
type FuncSummary struct {
    BodyOffset uint32
    BodySize   uint32

    SemanticOps uint32
    ProfileHits uint64

    DirectEdgeStart uint32
    DirectEdgeCount uint16

    BlockCount   uint16
    LoopCount    uint16
    MaxLoopDepth uint8

    EffectMask      uint32
    OpportunityMask uint32

    EstimatedGPRPressure uint8
    EstimatedVecPressure uint8

    ABIClass    uint8
    Addressable bool
}
```

Summary analysis records:

* Direct callees.
* Indirect calls.
* Host calls.
* Calls that may reenter.
* Memory accesses.
* `memory.grow`.
* Table mutation.
* Global mutation.
* GC allocation.
* Possible collection.
* Exception behavior.
* SIMD density.
* Fixed-register operations.
* Approximate pressure.
* Addressability.
* Candidate ABI class.
* Inlining cost.
* Native target opportunities.

## Call graph

Store:

```text
funcEdgeOffsets[function+1]
directEdges[]
```

Compute SCCs.

Compilation order may be callee-first for direct acyclic calls, even though final code layout is determined later.

This enables precise callee contracts without tying compilation order to native layout.

---

# 13. Function selection

## Explicit Dragline AOT

For `--compiler=dragline --objective=speed`:

* Compile all supported functions.
* Skip only trivial wrappers where Dragline cannot plausibly improve execution.
* Use Railshot only through explicit whole-module fallback.

## Future tiered mode

Priority:

```text
estimated future executions
× estimated Railshot quality debt
× expected remaining module lifetime
÷ estimated Dragline compile cost
```

Railshot quality-debt data may include:

```text
frame loads and stores
spill-like traffic
checks
call shuffles
SIMD fallback paths
helper transitions
native bytes
```

These metrics influence scheduling priority only. Dragline correctness never depends on them.

## Opportunity mask

The summary pass sets:

```text
HasLoops
HasHighPressure
HasSIMD
HasGC
HasIndirectCalls
HasRepeatedLoads
HasBoundsOpportunity
HasInliningOpportunity
HasTargetFeatureOpportunity
NeedsQualityScheduler
NeedsQualityAllocator
```

Passes skip themselves when no opportunity exists.

---

# 14. RailSSA

## 14.1 Goals

RailSSA should be:

* Wasm-native.
* Typed.
* Block-argument SSA.
* Dense.
* Pointer-free in hot data.
* CFG-oriented.
* Explicit about effects and traps.
* Specialized during construction.
* Smaller than a one-node-per-Wasm-op graph where possible.

## 14.2 IDs

```go
type ValueID uint32
type InstID  uint32
type BlockID uint32
type TypeID  uint16
type RegionID uint16
```

Zero is reserved as invalid.

## 14.3 Instruction record

Conceptually:

```go
type Inst struct {
    Op    uint16
    Type  uint8
    Flags uint8

    A   uint32
    B   uint32
    C   uint32
    Aux uint32
}
```

Variable operands live in a flat operand slab.

Avoid:

* Go interfaces.
* Per-node allocation.
* Pointer-linked user lists.
* Heap-allocated operand vectors.

## 14.4 Block record

```go
type Block struct {
    InstStart uint32
    InstCount uint32

    ParamStart uint32
    ParamCount uint16

    PredStart uint32
    PredCount uint16

    SuccStart uint32
    SuccCount uint16

    Region RegionID
    Flags  uint16

    Weight uint32
}
```

## 14.5 Locals disappear during construction

Normally:

```text
local.get → current ValueID
local.set → update local environment
local.tee → update and return same ValueID
drop      → use bookkeeping only
```

Do not create nodes and later run a pass to remove them.

## 14.6 Block arguments

Use block arguments instead of separate heap-allocated phi nodes.

Each edge carries:

* Operand-stack values.
* Live local values.
* Optional effect values where necessary.

This aligns well with Wasm’s structured merges and avoids explicit phi-object overhead.

## 14.7 Source order

Each instruction receives:

```text
Wasm source offset
source rank
region ID
```

Source order is a scheduling prior.

Wasm binaries have generally already passed through a source compiler and often Binaryen or another optimizer. Dragline should not reorder operations merely because it can.

## 14.8 Region tree

Maintain a compact lexical region tree:

```text
function
  block
  loop
  if
    then
    else
  try
    catch
```

Each block has a `RegionID`.

This gives cheap:

* Loop nesting.
* Structured dominance.
* Merge ownership.
* Pressure regions.
* Split boundaries.
* Shrink-wrap candidates.
* Trace and layout hints.

---

# 15. RailSSA construction

## 15.1 Structured prepass

Before building SSA, make one compact scan to record:

* Block boundaries.
* Loop headers.
* Locals assigned in loops.
* Locals live through merges.
* Result arity.
* Exceptional merge requirements.
* Branch targets.
* Source branch hints.
* Approximate pressure.
* Inlining candidates.

## 15.2 Main construction

Compile the Wasm body once:

1. Maintain current local environment.
2. Maintain operand-stack `ValueID`s.
3. Create semantic instructions.
4. Create control blocks.
5. Pass block arguments on branches.
6. Precreate obvious loop parameters.
7. Seal blocks after predecessors are known.
8. Remove trivial parameters.

For unusual EH or future control proposals, use lazy SSA sealing:

* An unsealed block may request an incomplete block parameter.
* Once predecessors are known, populate its incoming values.
* Collapse the parameter if every incoming value is equal.

## 15.3 Eager specialization

During construction:

* Fold constants.
* Fold immutable globals.
* Propagate exact types.
* Eliminate copies.
* Avoid redundant conversions.
* Construct specialized memory/check/GC operations.
* Preserve exact Wasm semantics.

Do not create a generic graph and expect later passes to recover all Wasm information.

---

# 16. Effect model

Dragline should avoid universal heavyweight alias analysis.

Use abstract heaps:

```text
LinearMemory[index]
Table[index]
Global[index]
GCHeader
GCStruct[type, field]
GCArray[type]
ImportState
RuntimeState
HostUnknown
```

Every operation declares:

```text
reads
writes
may grow memory
may allocate
may collect
may reenter
may throw
may trap
```

## Heap epochs

Per block:

```text
heap epoch
```

A write advances affected heap epochs.

A load/CSE key includes:

```text
heap
epoch
address
type
alignment
offset
```

An unknown host call invalidates the heaps declared by its effect contract.

## Effect groups

Each instruction receives a compact effect-group ID.

A local selector may fold or reorder instructions within one group when:

* No observable effect intervenes.
* Trap order is preserved.
* The producer is safely movable.

Wazevo already uses instruction-group identity and reference counts to constrain folding across effects.

Dragline should generalize the idea without building a large general effect graph.

---

# 17. Trap and exception ordering

Checks must be explicit semantic operations until target lowering.

Examples:

```text
BoundsCheck
NullCheck
TypeCheck
TableBoundsCheck
IndirectSignatureCheck
IntegerTrapCheck
StackFenceCheck
InterruptPoll
```

Each check records:

```go
type CheckData struct {
    Kind       CheckKind
    TrapCode   TrapCode
    SourcePC   uint32
    OrderIndex uint32
    Weight     uint32
}
```

`OrderIndex` prevents legal-looking rewrites from changing which observable trap occurs first.

A check may later become:

* A separate branch.
* A fused access.
* A guard-page omission.
* A hoisted range check.
* A shared cold failure path.
* Proven unnecessary.

EH edges must remain explicit.

---

# 18. Sparse semantic optimization

Use one fused worklist rather than a long sequence of whole-function passes.

## Core `SparseSimplify`

Handles:

* Constant propagation.
* Copy propagation.
* Sparse conditional propagation.
* Pure GVN.
* Known bits.
* Integer ranges.
* Nullability.
* Exact reference types.
* Dead instructions.
* Dead blocks.
* Trivial block arguments.
* Branch simplification.
* Redundant checks.
* Simple load forwarding.
* Store-to-load forwarding.
* Immutable global folding.

## Budgets

```text
rewrite fuel
new-node limit
worklist insertion limit
alternatives/value limit
block-version limit
```

When a limit is reached, the graph remains valid and compilation continues.

No global “run every pass until fixed point” loop.

## Lazy use lists

When complete use lists are required:

1. Count uses.
2. Prefix-sum.
3. Fill one flat CSR use array.
4. Run the analysis.
5. Discard or reuse the array.

Do not maintain linked use lists after every graph edit.

---

# 19. Loop optimization

Dragline is not a general source optimizer.

Initial loop work should focus on backend value:

* Induction recognition.
* Integer range propagation.
* Bounds certificates.
* Loop-invariant check hoisting.
* Pressure-sensitive LICM.
* Loop-carried value normalization.
* Stable context-pointer hoisting.
* SIMD mask retention.
* Cold-exit separation.
* Recurrence recognition.
* Running-pointer canonicalization.

Avoid initially:

* General loop vectorization.
* Polyhedral analysis.
* Broad unrolling.
* Arbitrary software pipelining.
* Large loop cloning.

Tiny constant-trip unrolling can be added after pressure and native-code growth are accurately modeled.

---

# 20. Bounds certificates

Do not immediately delete checks.

Produce semantic certificates:

```go
type BoundsCertificate struct {
    Memory      uint16
    AddressRoot ValueID
    MinOffset   int64
    MaxOffset   int64
    AccessWidth uint8

    Scope       RegionID
    Invalidated EffectMask
}
```

The backend chooses:

* Explicit check.
* Loop precheck.
* Combined range check.
* Guard-page omission.
* Hardware-assisted access.
* Generic fallback.

Calls that may grow the memory invalidate relevant certificates.

Same-memory calls that cannot grow memory may preserve them.

---

# 21. Semantic block versioning

This is a major Wago-specific experiment.

A hot block may be specialized for one or two incoming facts.

Examples:

```text
reference is non-null
reference has exact final type T
indirect target is function A
bounds certificate B is valid
memory identity is fixed
collector cannot run
object is fresh
```

Shape:

```text
specialized hot version
generic fallback version
```

No deoptimization is required. A failed guard branches to the generic path.

Initial limits:

```text
maximum versions/block: 2
facts/version key:      1
hot blocks only
global native-byte budget
```

Start with:

1. Exact non-null final GC type.
2. One common indirect-call target.
3. One existing bounds certificate.
4. Same-memory call context.

---

# 22. Inlining

Inlining is not the first Dragline optimization. Backend quality comes first.

Once selection, scheduling, and allocation are competitive, add bounded direct inlining.

## Construction model

Inline during RailSSA construction.

Do not construct and retain a separate inlinee graph.

## Profitability

Estimate:

```text
saved:
    call
    argument moves
    result moves
    caller spills
    callee prologue/epilogue
    stack checks
    interrupt checks
    newly removable checks

cost:
    native operations
    code growth
    caller pressure
    caller frame growth
    lost layout locality
    duplicated cold code
```

Strong positive signals:

* Makes caller call-free.
* Enables frame elision.
* Enables regional allocation.
* Exposes constants.
* Exposes exact GC facts.
* Removes a single-use callee body.
* Hot direct call.

Negative signals:

* Adds spills.
* Disables a good register region.
* Duplicates a multi-caller body.
* Copies large cold paths.
* Adds calls to a formerly call-free function.

Initial restrictions:

* Direct internal calls.
* One inline depth.
* No recursive SCC.
* No unrestricted EH.
* Global code-growth budget.

---

# 23. WasmGC semantic optimization

Keep WasmGC operations high-level until late lowering.

Track:

```text
known null/non-null
exact canonical type
known final type
known struct/array kind
known array length
fresh/unpublished object
known generation
known remembered/card state
```

## Initial optimizations

1. Redundant null-check elimination.
2. Redundant final-type cast elimination.
3. Exact field-offset selection.
4. Fresh-object field forwarding.
5. Barrier elimination for unpublished fresh objects.
6. Nonescaping allocation elimination.
7. Narrow root publication.
8. Shared cold failure paths.

WebKit’s current B3 WasmGC pass demonstrates that useful dead-allocation elimination can be implemented with targeted user classification rather than universal escape analysis.

## Later allocation folding

Recognize:

```text
allocate parent
allocate child
initialize both
publish parent
```

Potential transformation:

* Reserve several objects/handles at once.
* Perform one refill/collection check.
* Initialize transactionally.
* Remove barriers between fresh objects.
* Publish once.

This requires careful roots, publication, collection, and trap semantics.

---

# 24. Register-pressure shaping

This is a mandatory named phase.

Its objective is:

```text
minimize weighted simultaneous physical-register demand
while preserving or improving important dependency paths
```

Separate pressure classes:

* GPR.
* FP/SIMD.
* Predicate/flags.
* Fixed architectural registers.
* GC references across safepoints.

## Transformations

* Sink cheap pure calculations.
* Avoid unprofitable LICM.
* Hoist only when reuse exceeds pressure cost.
* Split cold uses.
* Delay induction increments.
* Shorten boolean lifetimes.
* Shorten flags lifetimes.
* Move call results toward consumers.
* Reduce unnecessary block arguments.
* Canonicalize target address forms.
* Mark rematerializable values.
* Prefer destructive reuse of dead operands.
* Separate hot and cold regions.

## Why it is separate

Once pressure is too high:

* Scheduler quality deteriorates.
* Selection alternatives become unavailable.
* Fixed-register conflicts increase.
* Spills appear.
* Frames grow.
* Calls need more preservation.
* Post-RA repair becomes less effective.

Pressure should be shaped before lowering commits to machine choices.

---

# 25. Rematerialization

## Standard recipes

```text
constant
zero/sign extension
base + constant
scaled address
immutable global
runtime directory pointer
type/layout ID
```

## Affine recipes

Experimental:

```text
base + index × scale + displacement
induction + constant
length − index
memory base + zero-extended address
type table + type ID × stride
context base + field offset
```

A recipe is valuable when reconstruction is absorbed into:

* AMD64 addressing.
* `LEA`.
* ARM64 shifted operand.
* ARM64 extended operand.
* ARM64 pre/post-index form.
* Immediate encoding.

Cost:

```text
added instructions
added latency
consumer folding
source values already live
number of uses
cross-bank cost
```

Do not reduce nominal pressure by creating a worse critical path.

---

# 26. RailMach

RailMach is a dense machine SSA representation.

It contains:

* Generic machine operations.
* Target pseudos.
* Virtual registers.
* Fixed-register constraints.
* Tied/destructive operands.
* Abstract stack slots.
* Immediates.
* Memory addresses.
* Flags/conditions.
* Calls and clobber masks.
* Safepoints.
* Roots.
* Traps.
* Relocations.
* Source mappings.
* Encoded-size estimates.

## Progressive lowering

```text
generic machine op
    ↓
target-legal op
    ↓
register-bank selected
    ↓
instruction selected
    ↓
scheduled
    ↓
physically allocated
    ↓
encoded
```

Generic and selected operations may temporarily coexist.

Do not introduce:

* SelectionDAG.
* A legalization DAG.
* A third full machine representation.

Graal explicitly separates pre-allocation optimization, allocation, post-allocation optimization, and final metadata analysis. Dragline should preserve a similarly explicit boundary while using much denser structures.

---

# 27. RailSpec

RailSpec is Dragline’s declarative target-description system.

A target rule describes:

```text
semantic pattern
input/output types
operand forms
immediate ranges
target features
register banks
fixed registers
tied operands
early clobbers
implicit uses/defs
flags behavior
encoding
native bytes
latency
uops/resources
fusion relationships
formal semantics
```

Conceptually:

```text
rule amd64_add64_regmem {
    match:
        add64(lhs, load64(addr))

    require:
        same_effect_group
        load_has_one_use
        target supports form

    result:
        ADD64rm lhs, addr

    output:
        reuse lhs

    effects:
        reads_memory
        writes_flags

    cost:
        bytes
        latency
        uops
        pressure
}
```

RailSpec generates:

* Go matcher decision trees.
* Encoders.
* Legality tests.
* Register use/def information.
* Clobber masks.
* Scheduler information.
* Machine verifier rules.
* Positive tests.
* Near-miss tests.
* Formal proof inputs.

No runtime rule interpreter.

No solver in production.

---

# 28. Instruction selection

## 28.1 Consumer-driven lowering

Begin from observable roots:

```text
branch
check
store
call
return
block result
safepoint
```

Walk producers backward.

Possible result states:

```text
GPR value
FP/SIMD value
immediate
memory operand
address
flags/condition
fixed target state
rematerialization recipe
```

## 28.2 Integrated `SelectOrder`

Local expression ordering and instruction selection should be solved together.

A dynamic-programming state should include:

```text
result form
required bank
peak temporary demand
latency
resource cost
native bytes
destructive reuse
```

This prevents:

```text
choose expression order
then discover the selected instruction requires more registers
```

or:

```text
choose minimum-instruction cover
then spill because its evaluation order is poor
```

## 28.3 Selector levels

| Selector     | Use                                  |
| ------------ | ------------------------------------ |
| `TreeCover`  | Default, single-use expression trees |
| `DAGCover`   | Hot, bounded pure shared DAGs        |
| `ExactCover` | Offline/Dragline Max tiny regions    |

## 28.4 Machine combination

Use shallow producer-linked combination.

Do not repeatedly scan arbitrary instruction pairs.

Initial families:

* Copy chains.
* Extension chains.
* Load + extension + consumer.
* Compare + branch.
* Arithmetic-produced flags.
* Carry/borrow chains.
* Shift/add/address.
* Call result + sink.
* Store/reload cancellation.
* ARM64 update-address forms.
* SIMD reduction + scalar consumer.
* Fixed-register operations.

---

# 29. Target templates

Some Wasm operations expand into long structured sequences.

Use generated or handwritten parameterized templates for:

* Checked float-to-int conversion.
* Wasm-correct FP min/max.
* Atomics.
* ARM64 LL/SC loops.
* Bulk memory.
* GC allocation.
* GC type checks.
* Host transitions.
* Multi-result wide arithmetic.
* Signature adapters.

Templates instantiate into RailMach using virtual registers.

For fixed cold paths or boundary trampolines, copy-and-patch stencils may be used.

Do not use stencils as the primary Dragline backend because they constrain global register allocation and produce variant explosion.

---

# 30. Machine scheduling

Scheduling is a primary Dragline feature.

## 30.1 Dependency graph

Per block or bounded hot region, construct flat dependency arrays for:

* True value dependencies.
* Memory dependencies.
* Abstract-heap ordering.
* Trap ordering.
* Exception ordering.
* Calls.
* Fixed registers.
* Flags.
* Safepoints.
* Fusion preferences.

## 30.2 Default scheduler

Bidirectional list scheduling.

Candidate priority includes:

```text
critical-path urgency
processor-resource availability
register-pressure delta
source-order distance
load latency
flags lifetime
fusion opportunity
code size
```

Source order is a real prior.

## 30.3 Fusion-aware scheduling

RailSpec target profiles should describe:

```text
CMP/TEST + branch
load + operation
move elimination
ARM64 CBZ/CBNZ
ARM64 TBZ/TBNZ
ARM64 pre/post-index memory
ARM64 LDP/STP
compare/select
```

A scheduler may move compatible instructions together to unlock fusion or pairing.

## 30.4 Schedule portfolio

For profile-hot or high-debt functions:

### A — source-stable

* Minimal movement.
* Low pressure.
* Preserve producer scheduling.

### B — latency/resource

* Critical path.
* Load latency.
* Port/resource balance.
* Independent recurrence interleaving.
* Fusion.

### C — pressure

* Delay definitions.
* Advance final uses.
* Shorten lifetimes.
* Reduce vector pressure.
* Avoid fixed-register overlap.

Candidates are generated sequentially.

Only one full candidate exists in memory.

---

# 31. Register allocation

## 31.1 Logical instruction positions

Each instruction exposes positions such as:

```text
before
early-use
normal-use
definition
late-clobber
after
```

This accurately models:

* Tied operands.
* Destructive destinations.
* Early clobbers.
* Calls.
* Fixed-register instructions.
* Uses and definitions that need not overlap.

Split and block-transition moves live in side tables so instruction numbering remains stable.

## 31.2 Primary allocator: `RAGreedy`

Packed arrays:

* Live segments.
* Use positions.
* Affinity bundles.
* Fixed intervals.
* Spillsets.
* Split products.
* Rematerialization data.
* Abstract stack slots.

Progressive stages:

1. Assign preferred/free register.
2. Evict cheaper interference.
3. Split hot and cold regions.
4. Split around loops.
5. Split around calls.
6. Split around fixed registers.
7. Local split around uses.
8. Rematerialize.
9. Spill.

These stages guarantee progress. LLVM uses an explicit sequence of assignment, splitting, stronger splitting, and eventual spilling for the same reason.

## 31.3 Spill cost

```text
dynamic store cost
+ dynamic reload cost
+ reload critical path
+ broken affinity
+ frame growth
+ GC/root cost
− rematerialization value
```

Use profile and loop weights.

## 31.4 Spill placement

Do not automatically store at definition.

Attempt:

* Common-dominator store.
* Hot-region register residence.
* Boundary spills.
* Cold-only reloads.
* Loop-entry/exit splits.
* Call splits.
* Per-use rematerialization.
* Shared spillsets.
* Deletion of stores with no remaining reload.

## 31.5 Affinities

Sources:

* Block arguments.
* Copies.
* Call ABI positions.
* Result registers.
* Tied operands.
* Hot edges.
* Loop backedges.

An affinity is a preference, not an absolute constraint.

## 31.6 Fixed-register handling

Represent directly:

* Division.
* Remainder.
* Shift-count register.
* Return registers.
* Context registers.
* Flags.
* Architecture-specific operations.

Split around fixed constraints instead of reserving the register for the whole function.

## 31.7 Stack-slot coloring

After spill decisions:

* Build slot interference.
* Reuse nonoverlapping slots.
* Respect size and alignment.
* Preserve exact GC reference meaning.
* Put hot slots at short displacements.
* Enable ARM64 pairing where profitable.
* Reuse call argument/result areas when safe.

## 31.8 Fallback allocator: `RALinear`

For very large or simple functions:

* Active/inactive intervals.
* Lifetime holes.
* Splitting.
* Next-use eviction.
* Fixed intervals.
* Rematerialization.
* Canonical stack slots.
* Loop-weighted cost.

## 31.9 Experimental allocator: `RASSA`

Research branch:

```text
machine SSA
    ↓
SSA-aware spill-region placement
    ↓
insert split/reload SSA values
    ↓
physical assignment
    ↓
parallel-copy resolution
```

Promote only if it improves both:

* Peak allocator memory.
* Dynamic spill/move quality.

---

# 32. Interprocedural register usage

Dragline’s private ABI makes actual callee-clobber information practical.

LLVM already contains infrastructure that records the physical registers a callee actually clobbers after allocation and propagates that mask into callers.

## Dragline approach

1. Build direct call graph.
2. Compute SCCs.
3. Compile acyclic callees before callers.
4. Record actual physical clobber mask.
5. Allocate callers using precise masks.

For recursive SCCs:

* Use conservative SCC contracts initially.
* Optionally compile a hot SCC once, collect masks, and perform one bounded SCC recompilation.

## ABI classes

Start with finite classes:

```text
General
LeafScalar
LeafFP
LeafVector
NoCollectLeaf
SameMemory
SameCollector
TinyDirect
```

Actual clobber masks refine the class.

## Benefits

* Fewer caller spills.
* Better cross-call residency.
* Smaller save/restore sets.
* Better tail calls.
* Less context synchronization.
* Better leaf code.

---

# 33. Bounded spill-feedback retry

After the first schedule and allocation, calculate:

```text
profile-weighted spill loads
profile-weighted spill stores
critical-path reloads
fixed-register shuffles
broken affinities
frame growth
call-preservation traffic
native-code growth
```

If debt exceeds a threshold, retry once.

Possible changes:

* Select pressure schedule.
* Select latency schedule.
* Change target cover.
* Change destructive destination.
* Increase rematerialization.
* Split before call/loop.
* Disable one inline.
* Change allocator.
* Change ABI class.
* Change register bank.

Invariants:

```text
maximum attempts: 2
no recursive retries
hot/high-debt only
one full candidate live
best complete candidate retained
```

---

# 34. Post-register-allocation optimization

This is core compiler functionality.

Required passes:

* Reload folding.
* Redundant spill-store deletion.
* Physical copy propagation.
* Move-chain collapse.
* Register reassignment.
* Partial-register repair.
* False-dependency breaking.
* Compare/branch adjacency repair.
* Macro-fusion repair.
* ARM64 pair formation.
* ARM64 pre/post-index realization.
* Constant placement.
* Shrink wrapping.
* Cold-path separation.
* Tail-call cleanup.
* Short physical scheduling window.

WebKit’s ARM64 canonicalization pass deliberately constructs pre/post-index-compatible forms using dominance information rather than hoping they appear accidentally.

Dragline should perform semantic canonicalization before selection and final realization after physical allocation.

---

# 35. Wago-specific call optimization

## Direct call effects

Classify:

```text
same instance
same memory
same table
same collector
cannot grow memory
cannot collect
cannot reenter
cannot throw
leaf host call
unknown host call
```

Preserve across safe calls:

* Memory base.
* Memory size.
* Table base.
* Immutable globals.
* Bounds certificates.
* Exact GC facts.
* Runtime context pointers.
* Rematerialization recipes.

## Host imports

Registered host functions should expose effect contracts.

A numeric nonreentrant leaf import may use:

* Minimal register save.
* No generic root publication.
* No unnecessary table/global synchronization.
* Direct register results.
* Preserved memory context.

Generic host adapter remains the fallback.

## Cross-instance calls

Preclassify at instantiation:

```text
same memory
different memory
same collector domain
different collector domain
host wrapper
```

Install direct target/context metadata.

---

# 36. Indirect-call specialization

Use profile information for one or a few common targets.

```text
if target == A:
    direct call or inline A
else if target == B:
    direct call B
else:
    generic indirect call
```

No deoptimization.

A miss takes the generic path.

Limits:

* Two or three targets.
* Hot sites only.
* Stable target histogram.
* Global code-growth budget.
* Exact signature guard.
* Cold generic fallback.

---

# 37. Target modes

```go
type TargetMode uint8

const (
    TargetCompatibility TargetMode = iota
    TargetNative
    TargetExplicit
    TargetFatNative
)
```

## Compatibility

* Baseline ISA.
* Generic CPU model.
* Portable artifact.
* Conservative vector policy.
* Deterministic output.

## Native

* Exact feature bitset.
* CPU family/tuning profile.
* CPU-specific instruction alternatives.
* CPU-specific scheduler model.
* Preferred vector width.
* Bulk-memory thresholds.
* Native artifact identity.

## Explicit

* User-provided triple.
* Feature set.
* CPU model.
* Cross-compilation.

## Fat native

* Compatibility function.
* One or two selected native clones.
* One-time entry-table resolution.
* No hot-loop feature checks.

Clone only if:

```text
hot function
+ genuine feature-specific opportunity
+ projected gain exceeds code-memory cost
```

---

# 38. CPU cost models

Production model:

```text
latency
uops
execution resources
issue width
load-use latency
store forwarding
branch cost
macro-fusion
move elimination
partial-register behavior
cross-bank cost
decode/front-end pressure
native bytes
```

Calibration sources:

* Real PMU measurements.
* Native codelets.
* LLVM scheduling data.
* `llvm-mca`.
* uiCA.
* Facile.
* `llvm-exegesis`.
* Vendor manuals.
* Wago macrobenchmarks.

Static models are not authoritative.

Important changes must win on actual hardware.

---

# 39. Block layout

Use actual finalized block sizes and profile counts.

An ExtTSP-like algorithm should account for:

* Fallthrough.
* Conditional/unconditional branches.
* Forward/backward distance.
* Edge count.
* Block bytes.
* Hot-chain density.
* Cold separation.
* Source order as tie-breaker.

LLVM’s ExtTSP implementation uses chain merging, multiple merge arrangements, profile-weighted jump distances, and explicit chain-size limits.

## Layout stages

1. Function-local block order.
2. Hot/cold block splitting.
3. Function clustering.
4. Adapter/stub/literal layout.
5. Final branch relaxation.

Later AOT experiment:

* Interprocedural hot-block stitching.

---

# 40. Finalizer ownership

Railshot’s completed bounded-finalization work should not become a compiler dependency for Dragline.

Instead, extract only semantics-neutral primitives where appropriate:

```text
labels
relaxable fragments
relocation records
metadata marks
offset remapping
in-place compaction
executable arena support
```

## Railshot owns

* Railshot fragment generation.
* Railshot layout decisions.
* Railshot finalizer policy.
* Railshot code-size objectives.

## Dragline owns

* Dragline block layout.
* Hot/cold placement.
* Branch forms.
* Literal islands.
* Veneers.
* Machine-specific relaxation policy.
* Dragline metadata placement.

Both may use a shared low-level `codeimage/relax` library.

Dragline should never import `railshot/finalizer`.

---

# 41. Compiler memory design

## Two substantial IRs maximum

```text
RailSSA
RailMach
```

No:

* SelectionDAG.
* Sea of Nodes.
* Third full machine IR.
* Module-wide function IR.
* Several complete candidates in memory.

## Arenas

```text
Arena A: RailSSA and CFG
Arena B: semantic analyses/edit buffers
Arena C: RailMach
Arena D: liveness/register allocation
Arena E: finalizer/layout scratch
```

Once RailMach is valid:

* Release RailSSA instruction storage.
* Release semantic use lists.
* Retain only source/trap/rematerialization metadata required by machine stages.

JSC’s B3/Air split demonstrates the value of releasing the semantic representation before the full physical backend runs.

## Dense storage

Prefer:

```text
[]Inst
[]Block
[]uint32 operands
[]uint32 edges
[]uint32 uses
[]uint64 bitsets
```

Avoid:

```text
[]*Node
maps for dense IDs
linked lists
interface operands
per-node allocations
```

## Initial record targets

| Structure            |      Target |
| -------------------- | ----------: |
| RailSSA instruction  | 16–24 bytes |
| RailMach instruction | 24–32 bytes |
| Value facts/remat    |  4–12 bytes |
| Use entry            |     4 bytes |
| Live segment         | 12–16 bytes |
| CFG edge             |  8–12 bytes |
| Block                | 24–48 bytes |

These are design targets, not current measurements.

## Candidate reuse

```text
generate candidate
allocate
score
save compact result
reset candidate arena
generate next candidate
```

Peak memory does not multiply with schedule count.

## High-water trimming

After pathological functions:

* Retain normal capacities.
* Drop unusually large buffers.
* Do not permanently retain one giant function’s scratch.

---

# 42. Memory budgets and downgrade

```go
type CompileBudget struct {
    SummaryBytes  uint64
    SSABytes      uint64
    AnalysisBytes uint64
    MachBytes     uint64
    RABytes       uint64

    RewriteFuel  uint64
    NativeGrowth uint64
}
```

When approaching a budget:

1. Disable block versioning.
2. Disable optional inlining.
3. Reduce selector alternatives.
4. Use one schedule.
5. Disable backend retry.
6. Downgrade `RAGreedy` to `RALinear`.
7. Fall back to Railshot only if necessary.

Budget exhaustion should not discard completed valid work unless there is no safe continuation.

---

# 43. Compilation latency design

## Opportunity-gated passes

No useful loop:

```text
skip loop analysis
```

No repeated memory operations:

```text
skip abstract-heap forwarding
```

Low pressure:

```text
use simple pressure pass
```

No shared DAG:

```text
use tree selector
```

No target-specific opportunity:

```text
skip native clone analysis
```

## Transactional edits

Complex transformations stage:

```text
insert
replace operand
split block
replace terminator
remove
```

Then:

1. Validate complete transformation.
2. Check budgets.
3. Commit.

No half-applied graph mutations.

## Analysis validity

A small bitset tracks:

```text
CFG
uses
dominators
loops
effects
liveness
pressure
layout
```

No general-purpose pass manager is needed.

## No global fixed-point pipeline

Use:

* One sparse semantic worklist.
* One pressure phase.
* One optional loop phase.
* One machine combine worklist.
* One post-RA window.

---

# 44. Artifacts and cache identity

Artifact identity includes:

```text
compiler engine
engine revision
runtime ABI revision
private ABI revision
module hash
target triple
feature bitset
CPU tuning ID
bounds strategy
GC/runtime configuration
optimization objective
profile hash
```

Railshot and Dragline artifacts must never collide.

A function-level Dragline cache key may include:

```text
Wasm function body hash
signature/type dependency hash
runtime contract hash
target hash
profile hash
callee contract digest
Dragline revision
```

Function artifact contains:

* Relocatable native bytes.
* Entry properties.
* Private ABI class.
* Actual clobber mask.
* Relocations.
* Trap/safepoint metadata.
* Required target features.
* Constant/stub references.

---

# 45. Optional Dragline compiler daemon

Not required for Dragline 1.0, but design serialization boundaries so it remains possible.

```text
Wago runtime process
    ├── Railshot
    ├── profile collection
    └── Dragline client
              │
              ▼
        Dragline daemon
        ├── compiler arenas
        ├── CPU models
        ├── function cache
        ├── offline-derived policies
        └── compilation workers
```

Benefits:

* Dragline memory does not inflate runtime RSS.
* Compiler crashes are isolated.
* Multiple processes share code cache.
* Compiler resources stay warm.
* AOT and tiering use one service.
* Large target models are loaded once.

Request must contain:

```text
module/function hash
validated feature set
runtime contract version
target fingerprint
bounds strategy
GC configuration
profile
objective
Dragline revision
```

Returned artifacts must be independently validated before mapping executable memory.

---

# 46. Tiering

## Initial tiered execution

1. Compile module through Railshot.
2. Begin execution.
3. Gather backend-neutral profile.
4. Identify hot functions or call clusters.
5. Compile original Wasm through Dragline.
6. Install Dragline code at call boundaries.

Dragline never reads Railshot code.

## No OSR initially

A running Railshot frame finishes as Railshot.

New calls may enter Dragline.

This avoids:

* Reconstructing live values.
* Mapping Railshot frames to RailSSA.
* Mid-loop replacement.
* Deoptimization infrastructure.

## Hot call clusters

Compile:

* Hot SCC.
* Hot caller/callee chain.
* Hot helper group.

Within the cluster, use Dragline’s private ABI.

Only cluster boundaries require bridges.

---

# 47. Verification

## RailSSA verifier

Checks:

* Types.
* Dominance.
* Block-argument arity.
* Edge arguments.
* Effect ordering.
* Trap ordering.
* Exceptional edges.
* Use-before-definition.
* Bounds certificates.
* Source mappings.

## RailMach verifier

Checks:

* Target legality.
* Register banks.
* Fixed constraints.
* Tied operands.
* Clobbers.
* Calls.
* Safepoints.
* Roots.
* Trap continuity.
* Scheduling dependencies.

## Post-RA verifier

Checks:

* No virtual registers.
* No overlapping physical assignments.
* All constraints satisfied.
* Split transitions complete.
* Parallel copies resolved.
* Stack slots noninterfering.
* Root locations exact.
* Calls preserve required values.

## Final-code verifier

Checks:

* Relocation ranges.
* Branch targets.
* Function boundaries.
* Trap offsets.
* Safepoint offsets.
* Entry offsets.
* Source mappings.
* Artifact identity.

---

# 48. Rule verification

RailSpec rules should support offline verification.

Each rule records:

```text
semantic hash
Wasm model version
ISA model version
proof tool version
feature predicate
covered types
proof status
```

Near-miss tests should violate one precondition at a time:

```text
one-use requirement removed
alignment changed
feature missing
upper bits unknown
intervening write inserted
trap order changed
register constraint violated
```

Production contains only generated matchers and encoders.

---

# 49. Fuzzing and chaos mode

Required modes:

* Random valid Wasm.
* Wasm proposal feature combinations.
* RailSSA mutation.
* RailMach mutation.
* Rule-guided target pattern fuzzing.
* Register-pressure stress.
* Fixed-register stress.
* Random legal schedules.
* Random physical-register choices.
* Random spill tie-breaking.
* Random block order.
* Finalizer offset stress.
* Cross-engine differential tests.
* Artifact round trip.
* Concurrent compile/instantiate/close.

Compare:

```text
reference interpreter
Railshot
Dragline deterministic
Dragline chaos
other engines where practical
```

Every crash produces a function replay artifact:

```text
Wasm body
module type context
target
objective
profile fragment
runtime contract
random seed
compiler revision
```

---

# 50. Benchmark program

## Suites

1. Neutral arithmetic and control.
2. Local- and pressure-heavy functions.
3. SIMD.
4. Crypto and hashing.
5. JSON and text.
6. Memory and bounds.
7. Calls and ABI.
8. Host imports.
9. Indirect calls.
10. WasmGC.
11. SQLite-like applications.
12. Large compile stress.
13. Native-code-size stress.
14. Short-lived end-to-end.
15. Long-lived sustained execution.

## Compiler metrics

```text
wall time
CPU time
B/op
allocations
peak live compiler bytes
RailSSA instructions
RailMach instructions
rewrite count
selection alternatives
schedule candidates
live segments
splits
spills
reloads
rematerializations
physical moves
frame bytes
native bytes
finalizer savings
```

## Execution metrics

```text
wall time
cycles
instructions retired
branches
branch misses
L1I misses
L1D misses
LLC misses
frontend stalls
backend stalls
loads/stores
uops/resource pressure
```

## Fair comparisons

Cranelift:

```text
current version
speed optimization
quality allocator
same CPU features
same bounds strategy
same Wasm
```

LLVM:

```text
same Wasm
same runtime semantics
same bounds strategy
same target CPU
```

Native source compiled with Clang/GCC is a ceiling, not a direct backend comparison.

---

# 51. Research program

## Tier A — required architecture

1. Dense RailSSA.
2. Dense RailMach.
3. Sparse semantic optimizer.
4. Mandatory pressure shaping.
5. RailSpec.
6. Integrated ordering and selection.
7. Fusion/resource scheduler.
8. Progressive greedy allocator.
9. Precise spill placement.
10. IPRA actual clobbers.
11. Stack-slot coloring.
12. Bounded feedback retry.
13. Strong post-RA stage.
14. Actual-size profile layout.
15. Wago runtime specialization.

## Tier B — high-confidence experiments

### Affine rematerialization

Target:

* Address-heavy loops.
* Parsers.
* Hashes.
* Runtime metadata.
* GC layouts.

### Semantic block versioning

Target:

* Exact GC type.
* Non-null references.
* Indirect target.
* Bounds certificate.
* Same-memory context.

### Schedule portfolio

Target:

* Hot difficult functions.
* Recurrence-heavy loops.
* SIMD pressure.
* Fixed-register conflict.

### Native CPU models

Target:

* AVX/BMI/FMA.
* ARM64 LSE/Dot/I8MM.
* Vector-width policy.
* Bulk-memory thresholds.

### WasmGC allocation elimination

Target:

* Constructor-heavy GC programs.
* Fresh temporary objects.
* Initialization-only allocations.

### Function artifact cache

Target:

* Repeated builds.
* Shared modules.
* Tiering.
* Compiler daemon.

## Tier C — research branches

### SSA-native allocator

Goal:

* Lower allocator memory.
* Better spill-region placement.

### Trace allocator

Goal:

* Profile-hot branchy functions.
* Possible parallel allocation.

### NOLTIS/DAG selector

Goal:

* Shared pure DAGs.

### PBQP selector

Goal:

* Multi-result and irregular target forms.

### Unison/SLOTHY oracle

Goal:

* Quality ceiling.
* Heuristic discovery.

Exact solvers should remain offline or Dragline-Max-only.

### Cross-bank spill cache

Goal:

* Severe GPR or SIMD pressure.

### Joint stack/register allocation

Goal:

* Better slot offsets and ARM64 pairs.

### Selective SLP

Goal:

* Small obvious scalar packs.

### Two-stage software pipeline

Goal:

* Tiny hot affine loops.

### Interprocedural block stitching

Goal:

* I-cache/frontend-heavy AOT applications.

### Learned advisor distillation

Goal:

* Better split, eviction, scheduling, inlining, and layout heuristics.

Production should initially execute deterministic reviewed logic rather than runtime ML inference.

---

# 52. Security objectives

Possible future objective:

```text
SecureHardened
ConstantTime
```

Restrictions may include:

* No new secret-dependent branches.
* No speculative load motion across barriers.
* Restricted table specialization.
* Restricted block versioning.
* Verified spill placement.
* Spectre mitigations.
* PAC/MTE/MPK integration where available.

These require separate correctness definitions and benchmark suites.

---

# 53. Implementation roadmap

# Phase 0 — sibling boundary

Deliver:

* `CompilerEngine`.
* Compiler router.
* Shared immutable input.
* Shared runtime-facing output.
* Runtime ABI revision.
* Artifact engine identity.
* Dependency checks.
* `--railshot` and `--dragline`.
* Strict Dragline error behavior.

No code-generation behavior changes.

---

# Phase 1 — measurement foundation

Deliver:

* Per-function compiler stats.
* Peak-live-memory accounting.
* Replay artifact.
* Railshot quality-debt metrics.
* Cranelift and LLVM harness.
* Backend-neutral profile schema.
* Target fingerprint schema.

Exit:

* Every future pass can report time, memory, bytes, and execution effect.

---

# Phase 2 — RailSSA

Deliver:

* Dense IDs.
* Instruction storage.
* Operand slabs.
* Blocks and CSR edges.
* Structured prepass.
* Direct local SSA.
* Block arguments.
* Loop parameters.
* Lazy sealing.
* Effects.
* Checks/traps.
* EH edges.
* Verifier.
* Evaluator.
* Dumps.

Exit:

* RailSSA exactly reproduces Wasm semantics.
* No per-instruction Go allocation.
* Memory stays inside representation targets.

---

# Phase 3 — minimum RailMach

Deliver:

* Dense machine SSA.
* Generic operations.
* AMD64 scalar lowering.
* ARM64 scalar lowering.
* Fixed constraints.
* Stable scheduler.
* Initial `RALinear`.
* Private Dragline ABI.
* Traps and safepoints.
* Dragline finalizer.
* Cross-tier-free whole-module compilation.

Exit:

* Dragline executes supported scalar modules independently of Railshot.

---

# Phase 4 — semantic optimizer

Deliver:

* `SparseSimplify`.
* Known bits/ranges.
* Abstract heaps.
* Load forwarding.
* Check elimination.
* Bounds certificates.
* Immutable globals.
* GC type/null facts.
* Dead code/block removal.

Exit:

* Compiler remains bounded.
* Real check/load reductions appear without broad compile-time growth.

---

# Phase 5 — pressure foundation

Deliver:

* Pressure analysis.
* Pressure-sensitive LICM.
* Cheap-op sinking.
* Cold-use separation.
* Rematerialization.
* Induction placement.
* Block-argument reduction.
* Affine-rematerialization prototype.

Exit:

* Pressure-heavy functions materially improve before introducing the final allocator.

---

# Phase 6 — quality allocation

Deliver:

* Logical positions.
* Live segments.
* Affinity bundles.
* Progressive `RAGreedy`.
* Loop/call/fixed-register splitting.
* Common-dominator spills.
* Rematerialization.
* Spillsets.
* Stack-slot coloring.
* Physical copy propagation.
* Diagnostics.

Exit:

* Significantly less dynamic spill traffic than `RALinear`.
* Compiler memory remains within budget.

---

# Phase 7 — RailSpec and selection

Deliver:

* RailSpec schema.
* Core generated AMD64 rules.
* Core generated ARM64 rules.
* Encoders and legality tests.
* `SelectOrder`.
* Tree covers.
* Address folding.
* Memory folding.
* Flags handling.
* Producer-linked combination.
* Rule tests and near misses.

Exit:

* Selector quality can be directly compared against Railshot, Wazevo, and Cranelift disassembly.

---

# Phase 8 — scheduling and feedback

Deliver:

* Dependency DAG.
* Resource model.
* Fusion data.
* Source-stable scheduler.
* Latency/resource scheduler.
* Pressure scheduler.
* Candidate scoring.
* Sequential candidate evaluation.
* Post-RA scheduler.
* Bounded retry.

Exit:

* Dragline reaches the first serious Cranelift performance gate.

---

# Phase 9 — private ABI and IPRA

Deliver:

* Direct call SCCs.
* Callee-first compilation.
* Actual clobber masks.
* ABI classes.
* Multi-result register ABI.
* Same-memory calls.
* Same-collector calls.
* Tail-call improvements.
* Function-contract caching.

Exit:

* Call-heavy workloads show substantially lower caller spill and save/restore traffic.

---

# Phase 10 — Wago runtime specialization

Deliver:

* Host effect contracts.
* Bounds preservation.
* Exact memory/table/global state.
* Indirect target specialization.
* WasmGC fast paths.
* Fresh-object barriers.
* Allocation elimination.
* Narrow root publication.
* Shared cold failure paths.

Exit:

* Wago-specific corpus clearly exceeds generic runtimes.

---

# Phase 11 — native target mode

Deliver:

* CPU model IDs.
* Target feature bitsets.
* Native cache keys.
* CPU scheduling tables.
* Feature-specific rules.
* SIMD/crypto forms.
* Bulk-memory calibration.
* Vector-width policy.
* Native clones.
* PMU calibration tools.

Exit:

* Native mode clears the stronger Cranelift target.

---

# Phase 12 — layout and AOT quality

Deliver:

* Actual-size ExtTSP.
* Hot/cold block split.
* Function clustering.
* Adapter/stub/literal placement.
* Profile artifact format.
* Function-level compilation cache.

---

# Phase 13 — explicit production release

Deliver:

* Complete feature coverage.
* Strict Dragline mode.
* Compatibility target.
* Native target.
* Stable artifacts.
* Full fallback policy.
* Production diagnostics.
* Release benchmark gates.

---

# Phase 14 — compiler daemon

Deliver:

* Function artifact serialization.
* RPC schema.
* Local daemon.
* Shared cache.
* Artifact verification.
* Lifecycle and failure isolation.

---

# Phase 15 — tiering

Deliver:

* Railshot profile collection.
* Hot-function selection.
* Dragline compilation queue.
* Cross-tier bridge ABI.
* Call-boundary installation.
* Code reclamation.
* No OSR initially.

---

# 54. Initial PR sequence

1. Add sibling compiler router and engine identity.
2. Put existing Railshot behind the router unchanged.
3. Add strict empty Dragline engine.
4. Add compiler stats and replay schema.
5. Add shared runtime effect contracts.
6. Add RailSSA IDs and storage.
7. Add RailSSA CFG and block arguments.
8. Add direct Wasm-to-RailSSA construction.
9. Add RailSSA verifier/evaluator.
10. Add RailMach storage.
11. Add AMD64 scalar echo backend.
12. Add ARM64 scalar echo backend.
13. Add Dragline-private internal ABI.
14. Add initial liveness and linear allocation.
15. Add sparse semantic optimizer.
16. Add pressure analysis and rematerialization.
17. Add progressive greedy allocator.
18. Add stack-slot coloring.
19. Add RailSpec generator.
20. Add consumer-driven selector.
21. Add machine combiner.
22. Add resource scheduler.
23. Add pressure scheduler.
24. Add post-RA optimizer.
25. Add bounded feedback retry.
26. Add IPRA actual clobber propagation.
27. Add Wago call/bounds specialization.
28. Add WasmGC specialization.
29. Add native CPU profiles.
30. Add profile-guided block layout.

Do not prioritize broad inlining, block versioning, solver integration, or native clones before PRs 17–24 demonstrate a strong physical backend.

---

# 55. First practical milestone

The first meaningful end-to-end Dragline should be intentionally narrow:

```text
Wasm
  ↓
RailSSA
  ↓
constants/copies/DCE
  ↓
PressureShape
  ↓
RailMach
  ↓
backward tree selection
  ↓
source-stable scheduler
  ↓
splitting linear allocator
  ↓
post-RA copy/reload cleanup
  ↓
Dragline finalizer
```

No:

* Inlining.
* Block versioning.
* DAG selection.
* Exact solver.
* ML.
* Native clones.
* General loop transformation.
* Tiering.

The first benchmark question is:

> How much execution quality comes from function-wide SSA, pressure shaping, consumer-driven selection, and a real allocator before adding an ambitious optimizer?

If that compiler does not begin approaching Railshot quickly and moving toward Cranelift, fix the backend architecture before adding more semantic passes.

---

# 56. Final definition

Railshot:

```text
validated Wasm
→ direct lowering
→ bounded local intelligence
→ immediate code
```

Dragline:

```text
validated Wasm
→ small semantic SSA
→ sparse semantic optimization
→ pressure shaping
→ rich machine SSA
→ integrated selection/order
→ fusion/resource scheduling
→ progressive splitting allocation
→ one bounded feedback retry
→ post-RA optimization
→ profile/native layout
→ maximum-performance code
```

The architectural thesis is:

> **Railshot makes Wago fast to start. Dragline must make Wago exceptionally fast to run.**

Dragline’s competitive advantage should not come from recreating LLVM’s source-language optimizer. It should come from an unusually strong physical backend, exact Wasm and Wago semantics, a private internal ABI, native CPU exploitation, and the ability to spend bounded extra compilation effort only where it produces better machine code.
