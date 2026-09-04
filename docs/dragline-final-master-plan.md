# Dragline Final Master Plan

**Status:** implementation-ready architecture
**Supersedes:** all previous Railshot→Dragline pipeline proposals
**Primary objective:** produce code materially faster than Cranelift and close to LLVM quality while using substantially less compiler memory
**Deployment model:** Railshot and Dragline are independent sibling execution/compiler engines under the Wago runtime

The performance figures below are **release gates and engineering targets**, not current results or predictions.

---

# 1. Product model

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
           independent engine        independent engine
                    │                         │
                    ▼                         ▼
          Railshot code image       Dragline code image
                    │                         │
                    └────────────┬────────────┘
                                 ▼
                           Wago runtime
```

Railshot and Dragline are **siblings**, not stages.

In Dragline mode:

- Railshot does not run.
- Railshot does not optimize first.
- Dragline does not consume Railshot machine code.
- Dragline does not consume Valent trees or Railshot analysis.
- Dragline compiles directly from original validated Wasm.

In Railshot mode, Dragline is not initialized.

They share:

- Wasm decoding and validation.
- Stable runtime contracts.
- Target detection.
- Executable-memory primitives.
- Runtime-facing metadata formats.
- Artifact infrastructure.
- Optional backend-neutral profiles.

They do **not** share:

- Compiler IR.
- Optimization passes.
- Register allocation.
- Instruction selection.
- Scheduling.
- Internal calling conventions.
- Frame layout.
- Code layout policy.
- Compiler-specific finalization policy.

---

# 2. Strategic positioning

## Railshot

Railshot owns:

- Immediate startup.
- Minimum compiler memory.
- Tiny and cold functions.
- Embedded and constrained deployments.
- Short-lived modules.
- Default JIT compilation.
- Initial execution in future tiering.
- Fallback and recovery.
- Differential correctness reference.
- Size-first compilation.

## Dragline

Dragline owns:

- Maximum steady-state execution performance.
- Long-lived instances.
- Server and throughput-sensitive applications.
- Native CPU specialization.
- Profile-guided optimization.
- AOT artifacts.
- WasmGC-heavy applications.
- SIMD, cryptography, parsers, databases, and large kernels.
- Expensive scheduling and register-allocation decisions.
- Wago-specific runtime/compiler co-design.

Railshot already covers the normal baseline-compiler tradeoff. Dragline is therefore permitted to compile more slowly than Cranelift. Its justification is generated-code quality.

---

# 3. Hard success criteria

## 3.1 Compatibility mode

Compared against current Cranelift configured for speed, its quality allocator, equivalent host features, equivalent bounds strategy, and the same Wasm:

| Metric | Dragline target |
|---|---:|
| Neutral optimized-Wasm execution | **At least 5% faster** geometric mean |
| Same-Wasm LLVM backend | Within **0–5%** |
| Compilation latency | No more than roughly **3× Cranelift** |
| Peak compiler memory | No more than **1.5× Cranelift** |
| Peak memory versus LLVM | Less than **50–60% of LLVM** |
| Balanced native-code growth | Normally no more than **10–15%** without clear runtime value |

## 3.2 Native and profile-guided mode

| Metric | Dragline target |
|---|---:|
| Neutral optimized-Wasm execution | **8–15% faster than Cranelift** |
| Same-Wasm LLVM backend | Approximately within **±3%** |
| Selected target-specialized kernels | May outperform LLVM |
| Wago-specific calls, GC, bounds, host transitions | **15–35% faster** than generic runtime paths |
| Compilation latency | Up to roughly **3–5× Cranelift** for selected hot functions |
| Peak memory | Still materially below LLVM |

These are aggressive gates. A mature Dragline that merely matches Cranelift does not adequately justify its complexity.

---

# 4. Research basis

The architecture follows a consistent pattern visible across strong production backends:

- Cranelift uses a compact function-local IR, CFG/dominator/loop analyses, bounded aegraph optimization, VCode lowering, register allocation, block layout, and MachBuffer finalization. fileciteturn78file0L1-L7 fileciteturn76file0L1-L7
- LLVM’s backend invests heavily in fusion-aware scheduling, post-RA scheduling, regional spill placement, coalescing, shrink wrapping, target-specific physical rewrites, and profile-guided layout. fileciteturn84file0L1-L7 fileciteturn85file0L1-L7 fileciteturn97file0L1-L7
- LLVM can collect a callee’s actual physical-register clobbers and propagate the resulting mask to callers before caller allocation. fileciteturn75file0L1-L7 fileciteturn74file0L1-L7
- JSC’s B3/Air architecture separates compact semantic optimization from a strong machine backend and performs target-specific post-RA repair; its WasmGC and ARM64 passes illustrate the value of retaining semantic and target information until late compilation. fileciteturn155file0L1-L7 fileciteturn156file0L1-L7

Dragline’s distinguishing bet is:

> Use a smaller Wasm-specific semantic optimizer than LLVM or JSC, but build an unusually strong physical backend with exact Wago runtime knowledge.

---

# 5. Fixed architectural decisions

| Area | Final decision |
|---|---|
| Relationship to Railshot | Independent sibling engine |
| Compiler input | Original validated Wasm |
| Compilation unit | One function at a time |
| Module-wide retained state | Compact summaries only |
| Major function IRs | Exactly two: `RailSSA` and `RailMach` |
| High-level form | Block-argument SSA |
| Machine form | Machine SSA retained through register allocation |
| Main optimizer | Sparse, Wasm-specific, bounded |
| Main performance focus | Pressure shaping, selection, scheduling, splitting, post-RA |
| First allocator | Strong splitting linear scan |
| Quality allocator | Progressive greedy allocator |
| Experimental allocator | SSA-native spill placement/allocation |
| SSA destruction | After physical allocation |
| Scheduling | Pre-RA plus bounded post-RA |
| Backend feedback | At most one retry |
| Target description | Generated RailSpec rules |
| Production solvers | None |
| Production ML inference | None initially |
| Compiler fallback | Whole-module Railshot fallback initially |
| First release | Explicit Dragline AOT/JIT-before-instantiation |
| Runtime tiering | Later, no OSR initially |
| Default engine | Railshot |

---

# 6. User-facing configuration

## CLI

```bash
wago run --compiler=railshot app.wasm
wago run --compiler=dragline app.wasm
```

Aliases:

```bash
wago run --railshot app.wasm
wago run --dragline app.wasm
```

Target configuration is orthogonal:

```bash
# Portable Dragline artifact
wago compile \
  --dragline \
  --target=compat \
  --objective=speed \
  app.wasm

# Host-specialized Dragline artifact
wago compile \
  --dragline \
  --target=native \
  --objective=speed \
  app.wasm

# Host-specialized Railshot
wago run \
  --railshot \
  --target=native \
  app.wasm
```

Future modes:

```text
--compiler=auto
--compiler=tiered
```

`auto` is a routing policy.

`tiered` invokes Railshot and Dragline independently at different times from the original Wasm.

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
    Bounds    BoundsMode
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

# 7. Fallback semantics

## Strict Dragline

```bash
wago run --compiler=dragline app.wasm
```

means:

> Compile the module through Dragline or return a Dragline error.

It must not silently produce Railshot code.

This preserves:

- Benchmark integrity.
- Reproducibility.
- Feature-coverage visibility.
- Correct compiler diagnostics.
- User expectations.

## Explicit whole-module fallback

```bash
wago run \
  --compiler=dragline \
  --compiler-fallback=railshot \
  app.wasm
```

Initial behavior:

1. Attempt complete Dragline compilation.
2. If an unsupported feature, budget failure, or recoverable compiler failure occurs, discard the incomplete Dragline result.
3. Compile the entire module through Railshot.

## Per-function fallback

Not in Dragline 1.0.

Mixed-engine modules require:

- Cross-engine bridge ABI.
- Cross-engine stack maps.
- EH interoperability.
- Call-site patching or stable veneers.
- Code lifetime management.

That belongs to the tiering project.

---

# 8. Shared contracts

## 8.1 Compiler input

```go
type CompilerInput struct {
    Module      *wasm.ValidatedModule
    Runtime     RuntimeContract
    Target      TargetConfig
    Objective   OptimizationObjective
    Bounds      BoundsMode
    Profile     *profile.Module
    HostEffects []HostFunctionEffects
}
```

The validated module contains:

- Types and signatures.
- Function bodies.
- Imports and exports.
- Memories and tables.
- Globals.
- Segments.
- Tags and EH metadata.
- Feature declarations.
- Validation results.

It contains no engine-specific IR or native data.

## 8.2 Runtime contract

The runtime contract specifies:

```text
runtime ABI revision
instance-data layout
memory/table/global descriptors
host transition interface
trap interface
exception interface
GC interface
stack-fence interface
interrupt interface
snapshot constraints
```

Both engines consume the contract independently.

## 8.3 Compiler output

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

The runtime never needs access to RailSSA, RailMach, or Railshot internal state.

---

# 9. ABI architecture

## 9.1 Runtime boundary ABI

Shared and versioned.

Used for:

- Host-to-Wasm entry.
- Wasm-to-host calls.
- Traps.
- Exceptions.
- Runtime reentry.
- Stack switching.
- GC safepoints.
- Exported function entry.

Each engine can emit different wrappers implementing the same boundary.

## 9.2 Engine-private ABI

Railshot and Dragline independently choose:

```text
argument registers
result registers
callee-saved registers
context registers
frame headers
stack-slot organization
multi-result conventions
tail-call conventions
root representation
```

Dragline can introduce:

- More register results.
- Same-memory call classes.
- No-collect call classes.
- Function-specific clobber masks.
- CPU-specific conventions.
- Different internal tail-call rules.
- Private pinned runtime state.

## 9.3 Future tier ABI

Mixed Railshot/Dragline modules use an explicit bridge ABI.

A tiered Dragline function may expose:

```text
private Dragline entry
generic cross-tier bridge entry
```

Dragline-to-Dragline calls use the private entry.

Railshot-to-Dragline calls use the bridge.

---

# 10. Package boundaries

```text
internal/compiler/
    compiler.go
    input.go
    output.go
    target.go

    runtimeabi/
    profile/
    codeimage/
    artifact/

    railshot/
        ...

    dragline/
        summary/
        ssa/
        effects/
        proof/
        specialize/
        optimize/
        pressure/
        mach/
        railspec/
        select/
        schedule/
        regalloc/
        ssaexit/
        frame/
        layout/
        finalize/
        verify/
        replay/
        cache/
        amd64/
        arm64/
```

Dependency rule:

```text
runtime contracts
       ↑
compiler common
   ↑          ↑
railshot   dragline
```

Forbidden:

```text
railshot → dragline
dragline → railshot
runtime → engine internals
```

CI should enforce this.

---

# 11. Module summaries

Dragline retains no module-wide function IR.

Use dense function-indexed arrays and CSR call edges.

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

- Direct callees.
- Indirect-call sites.
- Host calls.
- Memory and table effects.
- `memory.grow`.
- Global mutation.
- GC allocation and collection potential.
- EH behavior.
- SIMD density.
- Fixed-register operations.
- Estimated pressure.
- Addressability.
- Inline cost.
- Target-feature opportunities.

## Call graph

```text
edgeOffsets[function+1]
directEdges[]
```

Compute SCCs.

Compile acyclic callees before callers where useful for exact clobber masks. Final code layout remains independent of compilation order.

---

# 12. Function selection

## Explicit Dragline

For:

```bash
--compiler=dragline --objective=speed
```

compile all supported functions except trivial wrappers where Dragline cannot plausibly recover its own compile and code-memory cost.

## Future tiered mode

Priority:

```text
expected future executions
× Railshot quality debt
× expected remaining lifetime
÷ Dragline compilation cost
```

Optional Railshot quality-debt metrics include:

```text
frame traffic
spill-like traffic
checks
branches
call shuffles
SIMD fallback paths
helper transitions
native bytes
```

These are prioritization hints only.

---

# 13. RailSSA

## 13.1 Goals

RailSSA is:

- Wasm-native.
- Typed.
- Block-argument SSA.
- Dense.
- CFG-oriented.
- Pointer-free in hot data.
- Explicit about effects and traps.
- Specialized during construction.
- Function-local.

## 13.2 IDs

```go
type ValueID  uint32
type InstID   uint32
type BlockID  uint32
type TypeID   uint16
type RegionID uint16
```

## 13.3 Instruction representation

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

Variable operands live in flat slabs.

Avoid:

- Per-node allocation.
- Go interfaces.
- Pointer-linked users.
- Maps for dense IDs.
- Heap-allocated operand slices.

## 13.4 Blocks

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

## 13.5 Locals disappear during construction

```text
local.get → current ValueID
local.set → update local environment
local.tee → update and return same value
drop      → use bookkeeping only
```

Do not emit and later remove administrative local operations.

## 13.6 Block arguments

Edges carry:

- Operand-stack values.
- Live locals.
- Result values.
- Any required semantic state.

No separately allocated phi nodes.

## 13.7 Structured region tree

Retain:

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

This supports:

- Loop nesting.
- Structured dominance.
- Pressure regions.
- Split boundaries.
- Layout.
- Shrink wrapping.
- Proof scopes.

---

# 14. RailSSA construction

## Structured prepass

One compact scan records:

- Control boundaries.
- Loop headers.
- Assigned locals.
- Merge-live locals.
- Result arity.
- Exceptional edges.
- Branch hints.
- Approximate pressure.
- Inline candidates.

## Main construction

1. Maintain local `ValueID` environment.
2. Maintain operand-stack values.
3. Build semantic operations.
4. Create blocks and edges.
5. Pass block arguments.
6. Precreate obvious loop parameters.
7. Use lazy block sealing where necessary.
8. Remove trivial block arguments.

## Eager specialization

During construction:

- Fold constants.
- Fold immutable globals.
- Remove aliases.
- Propagate obvious widths.
- Construct high-level memory, check, call, and GC operations.
- Preserve source order and trap information.

---

# 15. Effect model

Use compact abstract heaps rather than universal alias analysis:

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

## Effect epochs

A load/CSE key includes:

```text
heap
epoch
address
type
alignment
offset
```

A write advances affected epochs.

A call advances only the heaps allowed by its effect contract.

## Effect groups

Instructions receive compact group IDs.

Local selection and combination may fold operations within one group when no effect or observable trap prevents the transformation.

---

# 16. Checks, traps, and semantic obligations

Checks remain explicit until sufficiently late lowering:

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

Each records:

```go
type CheckData struct {
    Kind       CheckKind
    TrapCode   TrapCode
    SourcePC   uint32
    OrderIndex uint32
    Weight     uint32
}
```

## Semantic obligations

Rather than eagerly materializing every cleanup/check:

```go
type ObligationKind uint8

const (
    NeedUpper32Zero ObligationKind = iota
    NeedBounds
    NeedNonNull
    NeedExactType
    NeedRootPublished
    NeedMemorySizeFresh
    NeedTableSignature
)
```

A target operation may discharge an obligation.

Examples:

```text
AMD64 32-bit write
    discharges upper-zero obligation

guard-backed access
    discharges bounds obligation

exact-type specialized block
    discharges exact-type obligation

root publication at safepoint
    discharges root obligation
```

No unresolved obligation may remain before encoding.

---

# 17. Sparse semantic optimization

Use one fused worklist, not a long fixed-point pipeline.

## `SparseSimplify`

Handles:

- Constant propagation.
- Copy propagation.
- Sparse conditional propagation.
- Pure GVN.
- Known bits.
- Integer ranges.
- Nullability.
- Exact reference types.
- Dead instructions.
- Dead blocks.
- Trivial block arguments.
- Branch simplification.
- Redundant checks.
- Simple load forwarding.
- Store-to-load forwarding.
- Immutable-global folding.

## Hard budgets

```text
rewrite fuel
new-node limit
worklist-insertion limit
alternatives/value limit
block-version limit
```

On exhaustion, retain the valid current graph.

## Lazy use lists

1. Count uses.
2. Prefix-sum.
3. Populate flat CSR use array.
4. Run required pass.
5. Reuse or discard.

---

# 18. Demand-driven proof engine

Do not run expensive complete range analysis over every function.

A proof query is created only when:

- A hot check may be eliminated.
- A loop precheck may cover several accesses.
- A target instruction needs a semantic fact.
- Code motion is blocked by uncertainty.
- A GC fast path needs exact null/type information.

```go
type ProofRequest struct {
    Kind  ProofKind
    Value ValueID
    Block BlockID
    Aux   uint32
    Fuel  uint16
}

type ProofResult struct {
    Proven       bool
    Certificate  CertificateID
    Dependencies EffectMask
}
```

Proof sources:

- Constants.
- Known bits and ranges.
- Dominating comparisons.
- Loop induction.
- Block-version facts.
- Function effects.
- Bounds certificates.
- Exact GC types.
- Memory identity.
- Guard-page configuration.
- Snapshot specialization facts.

Suggested fuel:

```text
Balanced: 32–64 steps
Speed:    64–128
Max:      up to 256 or offline
```

Cache by:

```text
proof kind
value
scope
effect epoch
auxiliary property
```

---

# 19. Loop optimization

Initial loop work should improve backend quality:

- Induction recognition.
- Integer range propagation.
- Bounds certificates.
- Pressure-sensitive LICM.
- Check hoisting.
- Stable context-pointer hoisting.
- Running-pointer canonicalization.
- SIMD mask retention.
- Recurrence recognition.
- Cold-exit separation.
- Loop-carried block-argument simplification.

Do not initially implement:

- General loop vectorization.
- Polyhedral optimization.
- Broad unrolling.
- General software pipelining.
- Arbitrary loop cloning.

---

# 20. Semantic specialization

## 20.1 Block versioning

A hot block may receive:

```text
specialized version
generic fallback version
```

Potential facts:

- Reference is non-null.
- Reference is exact final type `T`.
- Indirect target is function `A`.
- Bounds certificate is valid.
- Memory identity is fixed.
- Collector cannot run.
- Object is fresh.

Initial limits:

```text
maximum versions/block: 2
facts/version:          1
hot blocks only
global native-byte budget
```

No deoptimization. A failed guard enters the generic version.

## 20.2 Linked specialization

A linked Dragline compile may know:

- Actual host imports and effects.
- Immutable imported globals.
- Memory and table identities.
- Whether memory can grow.
- Same-memory and same-collector relationships.

## 20.3 Snapshot specialization

A snapshot-specialized compile may know:

- Initialized memory/global/table state.
- Resolved function pointers.
- Stable object layouts.
- Immutable configuration values.

The specialization cache key includes only facts actually consumed by compilation.

`weval` demonstrates that snapshot-based Wasm partial evaluation can produce specialized functions and patch function pointers in an initialized image; its repository reports 3–5× improvement for its SpiderMonkey-interpreter use case, although that result is specific to that system. fileciteturn105file0L1-L13

---

# 21. Pressure shaping

`PressureShape` is mandatory.

Objective:

```text
minimize profile-weighted simultaneous physical-register demand
without lengthening important dependency paths
```

Separate pressure classes:

- GPR.
- FP/SIMD.
- Predicate/flags.
- Fixed architectural registers.
- GC references crossing safepoints.

Transformations:

- Sink cheap pure calculations.
- Avoid pressure-increasing LICM.
- Hoist only when reuse justifies live-range growth.
- Separate cold uses.
- Delay induction updates.
- Shorten flags and boolean lifetimes.
- Move call-result realization toward consumers.
- Reduce block arguments.
- Prefer destructive reuse of dying inputs.
- Canonicalize target-friendly addresses.
- Mark rematerializable values.

---

# 22. Rematerialization

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

The strongest cases become zero-extra-instruction target forms:

- AMD64 memory addressing.
- `LEA`.
- ARM64 shifted operands.
- ARM64 extended operands.
- ARM64 pre/post-index addressing.
- Folded immediates.

Cost must include:

```text
new instructions
critical-path change
consumer folding
source liveness
number of uses
cross-bank movement
effect on other live ranges
```

Rematerializable does not mean always rematerialize.

---

# 23. RailMach

RailMach is dense machine SSA.

Contains:

- Generic machine operations.
- Target pseudos.
- Virtual registers.
- Fixed-register constraints.
- Tied and destructive operands.
- Abstract stack slots.
- Memory operands.
- Flags and condition values.
- Calls and clobber masks.
- Safepoints.
- Root metadata.
- Traps and source locations.
- Relocations.
- Encoded-size estimates.
- Block arguments.

Progressive lowering:

```text
generic machine op
    ↓
target-legal op
    ↓
bank selected
    ↓
instruction selected
    ↓
scheduled
    ↓
physically allocated
    ↓
encoded
```

No SelectionDAG and no third full machine IR.

---

# 24. Keep machine SSA through allocation

Block-edge values remain affinities, not ordinary copies.

```go
type EdgeTransfer struct {
    Src    ValueID
    Dst    ValueID
    Type   MachineType
    Weight uint32
}
```

Allocator sees:

```text
incoming value prefers the same location as block parameter
```

Only after allocation does `LateSSAExit` emit unavoidable physical transfers.

Benefits:

- Less artificial interference.
- Fewer copies.
- Less critical-edge splitting.
- Better block-argument coalescing.
- Better scheduling.
- Better allocator visibility.

---

# 25. RailSpec target descriptions

RailSpec describes:

```text
semantic pattern
types
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

Generated output:

- Go matcher decision trees.
- Encoders.
- Legality checks.
- Register use/def metadata.
- Scheduler data.
- Fusion data.
- Machine verifier rules.
- Positive tests.
- Near-miss tests.
- Formal-verification inputs.

No runtime parser or solver.

Initial migration order:

1. Integer arithmetic.
2. Loads and stores.
3. Extensions.
4. Comparisons and branches.
5. Addressing.
6. Flags.
7. Calls.
8. Core SIMD.

---

# 26. Integrated selection and expression ordering

`SelectOrder` chooses both:

- Evaluation order.
- Target instruction cover.

Possible result forms:

```text
GPR
FP/SIMD register
immediate
memory operand
address
flags/condition
fixed target state
rematerialization recipe
```

Cost:

```go
type SelectCost struct {
    PeakGPR      uint8
    PeakVector   uint8
    FixedNeed    uint8
    Moves        uint8
    Latency      uint16
    ResourceCost uint16
    Bytes        uint16
}
```

Selector modes:

| Mode | Use |
|---|---|
| `TreeCover` | Normal single-use trees |
| `DAGCover` | Hot bounded pure shared DAGs |
| `ExactCover` | Offline or Max-mode tiny regions |

Initial implementation should use a handwritten consumer-driven selector. RailSpec generation follows once real rule shapes are understood.

---

# 27. Machine combination

Use shallow producer-linked combination.

Do not repeatedly scan arbitrary instruction pairs.

Initial families:

- Copy chains.
- Extension chains.
- Load + extension + consumer.
- Compare + branch.
- Arithmetic-produced flags.
- Carry/borrow chains.
- Shift/add/address.
- Call result + immediate sink.
- Store/reload cancellation.
- ARM64 update addressing.
- SIMD reduction + scalar consumer.
- Fixed-register arithmetic.

Every rule has:

- Semantic preconditions.
- Effect/trap constraints.
- Target feature predicate.
- Register constraints.
- Cost.
- Tests.
- Proof status where available.

---

# 28. Scheduling

## 28.1 Dependency DAG

Per block or bounded hot region:

- True data dependencies.
- Memory dependencies.
- Effect ordering.
- Trap ordering.
- EH ordering.
- Fixed-register constraints.
- Flags.
- Calls.
- Safepoints.
- Fusion preferences.

Use flat arrays and CSR edges.

## 28.2 Fusion grammar

RailSpec describes:

```text
must-adjacent pair
prefer-adjacent pair
zero-latency fusion
load-operation fusion
compare/test + branch
address update + memory operation
ARM64 load/store pair
move-elimination relationship
```

LLVM models macro-fusion through scheduler cluster edges that force eligible pairs adjacent. fileciteturn84file0L1-L7

## 28.3 Schedule candidates

### Source-stable

- Preserve producer order.
- Minimize movement.
- Minimize pressure.
- Keep semantic ordering clear.

### Latency/fusion

- Shorten critical path.
- Hide load latency.
- Balance processor resources.
- Interleave independent recurrences.
- Realize fusion.

### Pressure

- Delay definitions.
- Advance final uses.
- Shorten vector lifetimes.
- Avoid fixed-register overlap.
- Reduce spill risk.

Normal compilation selects one candidate.

Hot or difficult functions may evaluate two or three sequentially.

Only one full candidate exists in memory at once.

## 28.4 Post-RA scheduling

A bounded physical scheduler performs:

- Anti-dependency breaking.
- Compare/branch adjacency.
- Fusion repair.
- Reload movement.
- Independent recurrence interleaving.
- ARM64 pairing.
- Physical-register reassignment.
- Partial-register repair.

LLVM’s post-RA scheduler explicitly models hardware hazards and anti-dependency breaking after physical assignment. fileciteturn85file0L1-L7

---

# 29. Register allocation

## 29.1 Logical positions

Each instruction has:

```text
before
early use
normal use
definition
late clobber
after
```

This models:

- Tied operands.
- Destructive destinations.
- Early clobbers.
- Calls.
- Fixed-register operations.
- Nonoverlapping uses and definitions.

## 29.2 First allocator: `RALinearQ`

Implement first:

- Lifetime holes.
- Active/inactive sets.
- Fixed intervals.
- Call clobbers.
- Splitting.
- Rematerialization.
- Loop-weighted costs.
- Block-argument affinities.
- Canonical spillsets.
- Stack-slot reuse.
- Late SSA exit.

Roles:

- First complete allocator.
- Large-function fallback.
- Memory-constrained allocator.
- Correctness reference for greedy allocation.

## 29.3 Quality allocator: `RAGreedyP`

Progressive stages:

```text
0. Produce legal allocation
1. Apply affinities and cheap assignment
2. Evict cheaper interference
3. Split around loops and calls
4. Split cold uses
5. Rematerialize
6. Reposition spill regions
7. Bounded recoloring/repair
```

Typical stopping point:

| Objective | Stage |
|---|---:|
| Balanced | 3–4 |
| Speed | 5–6 |
| Max/profile-hot | 7 |
| Memory constrained | `RALinearQ` |

## 29.4 Spill cost

```text
dynamic store cost
+ dynamic reload cost
+ critical-path reload cost
+ broken affinity
+ frame growth
+ GC/root cost
− rematerialization value
```

## 29.5 Regional spill placement

Decide register versus stack residency across:

```text
loop
hot trace
cold branch
call region
EH region
safepoint region
```

Prefer:

```text
register in hot region
single boundary transition
rematerialize or spill in cold region
```

over pointwise local eviction.

LLVM’s spill-placement machinery similarly reasons over profile-weighted edge regions rather than treating each spill independently. fileciteturn97file0L1-L7

## 29.6 Experimental `RASSA`

```text
RailMach SSA
    ↓
SSA-aware spill-region placement
    ↓
insert split/reload SSA values
    ↓
physical assignment
    ↓
LateSSAExit
```

Promote only if it improves both:

- Allocator peak memory.
- Dynamic spill and move cost.

---

# 30. Late SSA exit

After allocation:

1. Remove identity transfers.
2. Group transfers per edge.
3. Emit acyclic moves whose destinations are not remaining sources.
4. Resolve cycles with:
   - Free scratch register.
   - Reserved temporary.
   - Target swap operation.
   - Temporary stack slot.
5. Place transfers on the least expensive legal edge side.
6. Run local physical copy motion.
7. Split critical edges only if unavoidable.

Placement preference:

```text
cold predecessor end
cold successor beginning
existing edge block
fallthrough edge
new block
```

---

# 31. Interprocedural register allocation

## Actual clobber masks

Compile acyclic direct callees before callers.

After allocating a callee, record its actual physical clobber mask.

Allocate callers using that mask rather than a conservative universal call mask.

LLVM implements this basic mechanism through clobber collection and call-site mask propagation. fileciteturn75file0L1-L7 fileciteturn74file0L1-L7

## Recursive SCCs

Initially:

- Use conservative ABI-class masks.

Later, for hot SCCs:

1. Compile conservatively.
2. Record actual masks.
3. Recompile SCC once with refined contracts.
4. Keep the better complete result.

## ABI classes

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

Actual clobber masks refine each class.

---

# 32. Allocator-managed callee saves

Represent incoming preserved registers as machine SSA values:

```text
incoming_r12 = IncomingPhysical(R12)
incoming_r13 = IncomingPhysical(R13)
```

If a register is not overwritten, emit no save.

If overwritten only on a cold path, place save/restore only around that path.

If using a different register is cheaper than preservation, allocation may avoid the callee-saved register.

This enables per-register shrink wrapping instead of one all-or-nothing prologue region.

---

# 33. Backend retry

After first scheduling and allocation, calculate:

```text
profile-weighted spills
profile-weighted reloads
reload critical-path cost
fixed-register shuffles
broken high-value affinities
frame growth
call preservation
code-size penalty
```

Retry once only if debt exceeds a threshold.

Retry may change:

- Schedule.
- Instruction cover.
- Destructive destination.
- Rematerialization policy.
- Split strategy.
- Allocator.
- One inline decision.
- ABI class.

Rules:

```text
maximum attempts = 2
no recursive retry
hot/high-debt only
one candidate live at a time
best complete candidate retained
```

---

# 34. Frame composition

Frame layout occurs after allocation and late SSA exit.

```go
type FrameRequirements struct {
    SpillSlots      []AbstractSlot
    RootSlots       []RootSlot
    CalleeSaves     []SaveRegion
    CallAreaBytes   uint32
    ResultAreaBytes uint32
    EHBytes         uint32
    RuntimeBytes    uint32
}
```

`FrameCompose` decides:

- Concrete offsets.
- Slot coloring.
- Save/restore regions.
- Short AMD64 displacements.
- ARM64 pairable slots.
- GC root identities.
- Call areas.
- Unwind metadata.

---

# 35. Post-RA target optimization

## AMD64

- LEA versus ADD/shift.
- Slow three-input LEA handling.
- Move elimination.
- Partial-register repair.
- False-dependency breaking.
- Macro-fusion repair.
- Register reassignment.
- Memory operand folding.
- Fixed-register divide/multiply repair.
- AVX width transitions.
- APX NDD/NF conversion.

LLVM’s x86 LEA repair varies decisions according to target-specific LEA latency, operand shape, port usage, and size policy. fileciteturn94file0L1-L7

## ARM64

- `LDP`/`STP` pair formation.
- Pre/post-index realization.
- Constant offset folding.
- Load-from-store promotion.
- Physical-register renaming to expose pairs.
- Lane/store folding.
- MOPS lowering.

LLVM’s ARM64 post-RA pass performs these classes of rewrites with explicit bounded scan limits. fileciteturn93file0L1-L7

Every search window must have a hard limit.

---

# 36. Target modes

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

- Baseline ISA.
- Generic CPU model.
- Portable artifact.
- Conservative vector width.
- Deterministic output.

## Native

- Exact host feature set.
- CPU-family tuning profile.
- CPU-specific scheduling.
- Target-specific instruction choices.
- Bulk-memory thresholds.
- Preferred vector width.
- Native cache identity.

## Explicit target

- User-specified triple.
- Feature set.
- CPU model.
- Cross-compilation.

## Fat native

- Compatibility body.
- One or two selected specialized clones.
- One-time entry-table resolution.
- No hot-loop feature tests.

Clone only when:

```text
hot function
+ real feature-specific opportunity
+ projected gain exceeds code-memory cost
```

---

# 37. Native hardware policies

## AMD64 APX

Treat APX as a distinct target:

- 32 GPRs.
- NDD three-operand forms.
- NF no-flags forms.
- Different private ABI.
- Different flags-lifetime policy.
- Different spill thresholds.
- Different code-size model.
- Different if-conversion opportunities.

Do not model APX as merely “more registers.”

## ARM64 MOPS

Choose among:

```text
tiny scalar inline
tiny NEON inline
small loop
MOPS
runtime helper
```

based on size, overlap, alignment, profile, and CPU.

## SVE2

Initial uses:

- Bulk scanning.
- v128 packet lifting.
- Predicated tails.
- Byte classification.
- Reductions.

Do not initially build a general scalable-vector semantic IR.

## Hardware bounds/sandbox modes

Future target policies may use:

- Guard pages.
- Shadow + guard.
- x86 segmentation.
- MPK.
- Arm MTE.
- Arm PAC.

These are runtime/compiler co-design modes and must be encoded in artifact identity.

---

# 38. Vector Packet Lifting

Experimental transformation:

```text
2 × independent v128 iterations
    → one AVX2 256-bit iteration

4 × independent v128 iterations
    → one AVX-512/AVX10 512-bit iteration

N × independent v128 iterations
    → one SVE2 strip-mined group
```

Required proof:

- Identical v128 operation graph.
- No cross-iteration dependency.
- Adjacent or disjoint memory.
- Correct combined bounds proof.
- Correct trap order.
- Supported alignment.
- Original v128 tail.
- No atomics.
- No reference-valued vector data.
- Exact FP semantics.

Likely workloads:

- UTF and JSON scanning.
- Base64/hex.
- Hashing.
- Byte classification.
- Checksums.
- Fixed-block transforms.
- Large comparisons.

This is not part of Dragline 1.0.

---

# 39. Profile system

Profiles are keyed to original Wasm:

```text
function index
Wasm instruction offset
structured edge
call site
allocation site
```

```go
type ModuleProfile struct {
    FunctionCounts []uint64
    EdgeCounts     []EdgeCount
    BackedgeCounts []EdgeCount

    CallTargets []TargetHistogram
    ValueRanges []ValueHistogram
    MemOpSizes  []ValueHistogram
    Allocations []SiteCount

    Generation uint64
    ModuleHash [32]byte
}
```

Profile sources:

1. Static hints.
2. Cheap Railshot counters.
3. Targeted instrumentation.
4. Hardware samples.

Distinguish:

- Startup.
- Warm steady state.
- Rare/shutdown phase.
- Railshot observations.
- Dragline observations.
- Instrumented observations.

---

# 40. Profile-guided layout

Use actual finalized or nearly finalized block sizes.

Cost includes:

```text
fallthrough
taken branches
forward/backward distance
hot code density
call distance
I-cache lines
page/iTLB footprint
alignment
cold separation
source-order stability
```

Start with function-local ExtTSP-like ordering.

Then:

- Hot/cold split.
- Function clustering.
- Adapter/stub/literal placement.

Later AOT experiment:

- Interprocedural block stitching.

---

# 41. Finalization

Railshot and Dragline own separate finalization policy.

Shared low-level primitives may include:

- Labels.
- Relaxable fragments.
- Relocation records.
- Metadata marks.
- Offset remapping.
- In-place compaction.
- Executable arena operations.

Dragline owns:

- Its block layout.
- Branch forms.
- Literal islands.
- Veneers.
- Hot/cold placement.
- Target-specific alignment.
- Metadata layout.

Dragline must not import `railshot/finalizer`.

---

# 42. Memory architecture

## Two substantial IRs only

```text
RailSSA
RailMach
```

No:

- SelectionDAG.
- Sea of Nodes.
- Third full MIR.
- Module-wide function IR.
- Simultaneous complete candidates.

## Arenas

```text
Arena A: RailSSA and CFG
Arena B: semantic analyses
Arena C: RailMach
Arena D: liveness and allocation
Arena E: layout/finalization
```

Release RailSSA as soon as RailMach and required source metadata are complete.

## Dense storage targets

| Structure | Initial design target |
|---|---:|
| RailSSA instruction | 16–24 bytes |
| RailMach instruction | 24–32 bytes |
| Value fact/remat entry | 4–12 bytes |
| Use entry | 4 bytes |
| Live segment | 12–16 bytes |
| CFG edge | 8–12 bytes |
| Block | 24–48 bytes |

## No per-node Go allocation

Prefer:

```text
[]Inst
[]Block
[]uint32
[]uint64
CSR arrays
open-addressed tables
```

Avoid:

```text
[]*Node
interfaces
linked users
maps for dense IDs
heap operand slices
```

## Candidate reuse

```text
generate candidate
allocate
score
retain compact best result
reset candidate arena
generate next
```

---

# 43. Memory budgets and downgrade sequence

```go
type CompileBudget struct {
    SummaryBytes  uint64
    SSABytes      uint64
    AnalysisBytes uint64
    MachBytes     uint64
    RABytes       uint64

    RewriteFuel   uint64
    NativeGrowth  uint64
}
```

Initial provisional targets:

```text
ordinary quality worker:
    target ≤32 MiB transient function workspace

large function:
    target ≤64 MiB per worker

serial large-module compile:
    target near or below 128 MiB total compiler RSS
```

These must be calibrated, not treated as guarantees.

Downgrade sequence:

1. Disable block versioning.
2. Disable optional inlining.
3. Reduce selector alternatives.
4. Use one schedule.
5. Disable retry.
6. Stop progressive allocator earlier.
7. Use `RALinearQ`.
8. Whole-module Railshot fallback if explicitly permitted.

---

# 44. Function artifacts and cache

## Cache identity

```text
function body hash
type dependency hash
runtime contract hash
private ABI revision
target triple
feature bitset
CPU tuning model
bounds mode
GC/runtime configuration
objective
profile hash
specialization hash
callee-contract digest
Dragline revision
```

## Artifact contents

- Relocatable machine bytes.
- Entry offsets.
- ABI class.
- Actual clobber mask.
- Required ISA.
- Relocations.
- Traps.
- Safepoints.
- Root maps.
- Source mappings.
- Constant/stub references.

Function-level caching enables:

- Callee-first compilation.
- Partial cache hits.
- Hot-function recompilation.
- Native variants.
- Cross-process reuse.
- Compiler daemon.
- Tiering.

---

# 45. Optional compiler daemon

Not required for Dragline 1.0.

```text
Wago process
    ├── Railshot
    ├── profile collection
    └── Dragline client
               │
               ▼
        draglined daemon
        ├── warm arenas
        ├── CPU models
        ├── artifact cache
        ├── compiler workers
        └── offline-derived policies
```

Benefits:

- Dragline memory does not inflate runtime RSS.
- Compiler crashes are isolated.
- Several processes share artifacts.
- Expensive target models stay resident.
- AOT and tiering share infrastructure.

The runtime independently validates every returned artifact.

---

# 46. Tiering

Initial tiered flow:

1. Compile module through Railshot.
2. Execute immediately.
3. Gather backend-neutral profile.
4. Select hot functions or call clusters.
5. Compile original Wasm through Dragline.
6. Install Dragline entry at call boundaries.

No OSR initially.

A running Railshot frame finishes as Railshot.

Hot call clusters should be compiled together where bridge overhead would otherwise dominate.

---

# 47. Verification

## RailSSA verifier

Checks:

- Types.
- Dominance.
- Block-argument arity.
- Edge arguments.
- Effects.
- Trap order.
- EH edges.
- Uses and definitions.
- Bounds certificates.
- Source mappings.

## RailMach verifier

Checks:

- Target legality.
- Register banks.
- Fixed constraints.
- Tied operands.
- Clobbers.
- Calls.
- Safepoints.
- Roots.
- Obligations.
- Scheduling dependencies.

## Post-RA verifier

Checks:

- No virtual registers.
- No overlapping physical assignments.
- All fixed/tied constraints.
- Correct split transitions.
- Correct parallel-copy resolution.
- Correct stack-slot interference.
- Exact root locations.
- Correct call preservation.

## Final-code verifier

Checks:

- Branches.
- Relocations.
- Function bounds.
- Trap offsets.
- Safepoint offsets.
- Entry points.
- Source maps.
- Artifact identity.

---

# 48. Rule verification

Every RailSpec rule records:

```text
semantic hash
Wasm semantic version
ISA semantic version
feature predicate
proof-tool version
proof status
```

Tests:

- Positive case.
- Boundary immediates.
- Feature absence.
- Alignment changes.
- Unknown upper bits.
- Intervening effects.
- Trap-order variants.
- Register-constraint conflicts.
- Single-precondition near misses.

Production contains generated Go matching and encoding only.

---

# 49. Fuzzing and chaos

Required modes:

- Random valid Wasm.
- Structured control stress.
- RailSSA mutation.
- RailMach mutation.
- Rule-guided lowering fuzzing.
- Liveness stress.
- Fixed-register stress.
- Parallel-copy cycles.
- Safepoints and roots.
- Random legal schedule ties.
- Random legal register choices.
- Random split decisions.
- Random block orders.
- Finalizer offset stress.
- Cross-engine differential testing.
- Artifact round trips.
- Concurrent compile/instantiate/close.

Every failure produces a single-function replay artifact.

---

# 50. Benchmark program

## Suites

1. Neutral arithmetic and control.
2. Pressure-heavy local code.
3. Calls and ABI.
4. Memory and bounds.
5. SIMD.
6. Crypto and hashing.
7. JSON and text.
8. Indirect calls.
9. Host imports.
10. WasmGC.
11. SQLite-like applications.
12. Large-function compile stress.
13. Short-lived end-to-end.
14. Long-lived sustained execution.
15. Native-code-size stress.

## Compiler metrics

```text
stage time
CPU time
B/op
allocations
peak live bytes
RailSSA instructions
RailMach instructions
rewrite count
proof queries
selection alternatives
schedule candidates
live segments
splits
spills
reloads
rematerializations
edge transfers
physical moves
frame bytes
native bytes
finalizer savings
```

## Execution metrics

```text
wall time
cycles
instructions
branches
branch misses
L1I/L1D misses
LLC misses
frontend stalls
backend stalls
loads/stores
uops/resource pressure
```

---

# 51. Implementation roadmap

## Phase 0 — sibling boundary

Deliver:

- `CompilerEngine`.
- Router.
- Shared input/output.
- Runtime ABI revision.
- Artifact engine identity.
- `--railshot`.
- `--dragline`.
- Strict Dragline failure.
- Dependency checks.

## Phase 1 — measurement

Deliver:

- Per-function stage metrics.
- Peak-live-memory accounting.
- Replay artifacts.
- Railshot quality-debt metrics.
- Cranelift/LLVM harness.
- Target fingerprint.
- Backend-neutral profile format.

## Phase 2 — RailSSA

Deliver:

- Dense storage.
- Structured prepass.
- Direct local SSA.
- Block arguments.
- Loop handling.
- Lazy sealing.
- Effects and traps.
- Verifier.
- Evaluator.
- Dumps.

## Phase 3 — RailMach echo backend

Deliver:

- Dense machine SSA.
- AMD64 scalar lowering.
- ARM64 scalar lowering.
- Target constraints.
- Source-stable scheduler.
- Initial `RALinearQ`.
- Dragline-private ABI.
- Finalizer.
- Whole-module strict mode.

## Phase 4 — late SSA exit

Deliver:

- Edge affinities.
- Block arguments through allocation.
- `LateSSAExit`.
- Parallel-copy resolver.
- Cycle breaking.
- Edge placement.
- Physical copy motion.
- Copy-debt metrics.

## Phase 5 — semantic optimizer and proof engine

Deliver:

- `SparseSimplify`.
- Known bits/ranges.
- Abstract heaps.
- Check simplification.
- Bounds certificates.
- Semantic obligations.
- `DemandProof`.
- Proof verifier.

## Phase 6 — pressure shaping

Deliver:

- Pressure estimation.
- Pressure-sensitive LICM.
- Cheap-operation sinking.
- Cold-use separation.
- Rematerialization.
- Induction placement.
- Block-argument reduction.
- Affine-rematerialization experiment.

## Phase 7 — quality allocation

Deliver:

- Progressive `RAGreedyP`.
- Eviction.
- Loop/call splitting.
- Cold-use splitting.
- Regional spill placement.
- Spillsets.
- Stack-slot coloring.
- Allocation diagnostics.

## Phase 8 — RailSpec and selection

Deliver:

- RailSpec schema.
- Generated core rules.
- `SelectOrder`.
- Address folding.
- Memory folding.
- Flags handling.
- Producer-linked combination.
- Rule verification.

## Phase 9 — scheduling and feedback

Deliver:

- Dependency DAG.
- Fusion grammar.
- Source-stable scheduler.
- Latency/resource scheduler.
- Pressure scheduler.
- Sequential candidates.
- Post-RA scheduler.
- One bounded retry.

## Phase 10 — private ABI and IPRA

Deliver:

- Call graph SCCs.
- Callee-first compilation.
- Actual clobber masks.
- ABI classes.
- Multi-result register ABI.
- Allocator-managed callee saves.
- Verifier-gated per-register shrink wrapping.
- `FrameCompose`.

## Phase 11 — target post-RA quality

AMD64:

- LEA repair.
- Move elimination.
- Partial-register repair.
- Fusion repair.
- Physical renaming.

ARM64:

- Pair formation.
- Pre/post-index.
- Constant-offset folding.
- Load/store promotion.
- Physical renaming.
- MOPS.

## Phase 12 — runtime specialization

Deliver:

- Same-instance/same-memory calls.
- Host effect contracts.
- Bounds preservation.
- Indirect-target specialization.
- WasmGC exact facts.
- Fresh-object barriers.
- Allocation elimination.
- Narrow root publication.

## Phase 13 — native/profile mode

Deliver:

- CPU models.
- Native feature identities.
- Target cost tables.
- Profile-guided function selection.
- ExtTSP-like layout.
- Bulk-memory calibration.
- Native clones.
- APX/SVE2 policy foundations.

## Phase 14 — production Dragline

Deliver:

- Complete required feature coverage.
- Stable artifacts.
- Compatibility mode.
- Native mode.
- Strict diagnostics.
- Whole-module fallback.
- Release benchmarks.

## Phase 15 — cache, daemon, tiering

Deliver:

- Function artifacts.
- Shared cache.
- Optional daemon.
- Railshot profile collection.
- Cross-tier bridge.
- Call-boundary installation.
- No OSR initially.

---

# 52. Initial PR sequence

1. Add sibling compiler router and engine identity.
2. Put Railshot behind the router unchanged.
3. Add strict empty Dragline engine.
4. Add compiler stats and replay format.
5. Add shared runtime/effect contracts.
6. Add RailSSA IDs and storage.
7. Add RailSSA CFG and block arguments.
8. Add Wasm-to-RailSSA builder.
9. Add RailSSA verifier/evaluator.
10. Add RailMach storage.
11. Add AMD64 scalar echo path.
12. Add ARM64 scalar echo path.
13. Add private Dragline ABI.
14. Add liveness and `RALinearQ`.
15. Add edge affinities and `LateSSAExit`.
16. Add parallel-copy resolver.
17. Add sparse semantic optimizer.
18. Add proof obligations and `DemandProof`.
19. Add pressure analysis/rematerialization.
20. Add `RAGreedyP`.
21. Add stack-slot coloring and `FrameCompose`.
22. Add RailSpec generator.
23. Add consumer-driven `SelectOrder`.
24. Add machine combination.
25. Add fusion/resource scheduling.
26. Add post-RA scheduling.
27. Add bounded retry.
28. Add exact clobber propagation.
29. Add Wago call/bounds specialization.
30. Add WasmGC specialization.
31. Add CPU-native profiles.
32. Add actual-size profile layout.

Do not prioritize broad inlining, block versioning, packet lifting, APX, exact solvers, or daemon work before steps 20–27 demonstrate a strong physical backend.

---

# 53. Experimental branches

| Experiment | Status | Promotion gate |
|---|---|---|
| Affine rematerialization | High-priority experiment | Fewer spills/live ranges without critical-path regression |
| Semantic block versioning | Experiment | Check/dispatch savings exceed code and I-cache cost |
| Indirect-target specialization | High-confidence experiment | Stable high target-hit rate |
| Vector Packet Lifting | Experiment | Real application gains, controlled tails and width penalties |
| APX private ABI | Native research | Beats merely exposing extra registers |
| SVE2 packet lifting | Native research | Wins on actual SVE2 hardware |
| Segmentation memory mode | Research | Base-register savings exceed prefix/platform cost |
| `RASSA` | Research | Beats greedy in both memory and spill quality |
| Trace allocation | Research | Boundary moves do not erase hot-trace gains |
| DAG/PBQP selection | Research | Repeated improvement over tree selection |
| Unison/SLOTHY oracle | Offline | Reveals repeatable heuristic debt |
| Interprocedural block stitching | AOT research | Frontend/I-cache gains justify metadata complexity |
| Compiler daemon | Deployment research | Cache reuse/RSS isolation justify operations |
| ML/evolved policies | Offline only | Distills to stable deterministic rules |

---

# 54. Explicit non-goals

Dragline 1.0 will not include:

- Railshot as a first pass.
- Shared Railshot/Dragline IR.
- A module-wide SSA graph.
- Sea of Nodes.
- SelectionDAG.
- A third full machine IR.
- General online egraph saturation.
- General source-language optimization.
- Broad loop vectorization.
- Polyhedral optimization.
- General software pipelining.
- Unbounded inlining.
- Unbounded block versioning.
- Online SMT, PBQP, or ILP.
- Runtime ML inference.
- OSR.
- JavaScript-style deoptimization.
- Copy-and-patch as the main backend.
- Simultaneous complete schedule candidates.
- Hidden Railshot fallback in strict Dragline mode.

---

# 55. Architectural kill criterion

After Dragline has:

- Dense RailSSA and RailMach.
- Pressure shaping.
- Consumer-driven selection.
- Target resource models.
- Fusion-aware scheduling.
- Progressive splitting allocation.
- Late SSA exit.
- Exact callee masks.
- Post-RA optimization.
- Native target policies.
- Wago call/bounds/GC specialization.

perform a formal comparison.

If neutral optimized-Wasm remains less than approximately 5% faster than Cranelift, determine which conclusion is supported:

1. Target cost models are inaccurate.
2. Scheduling and allocation interact poorly.
3. Spill placement remains weak.
4. Input Wasm leaves little generic backend opportunity.
5. Dragline’s advantage is primarily Wago-specific specialization.
6. External Cranelift or LLVM AOT should remain an optional quality backend.

Do not continue adding compiler complexity without crossing this gate.

---

# 56. First implementation milestone

The first meaningful Dragline should be deliberately narrow:

```text
validated scalar Wasm
    ↓
dense RailSSA
    ↓
constants, copies, DCE
    ↓
basic PressureShape
    ↓
RailMach SSA
    ↓
handwritten consumer-driven selection
    ↓
source-stable scheduler
    ↓
splitting RALinearQ
    ↓
LateSSAExit
    ↓
post-RA copy/reload cleanup
    ↓
Dragline finalizer
```

Support initially:

- `i32` and `i64`.
- Constants.
- Arithmetic and bitwise operations.
- Comparisons.
- Branches.
- Blocks and loops.
- Loads and stores.
- Returns.
- Basic direct calls after scalar control is stable.

Do not initially support:

- Inlining.
- SIMD.
- GC.
- EH.
- Atomics.
- Profiles.
- Native specialization.
- Multiple schedule candidates.
- Greedy allocation.
- Tiering.

The first prototype must answer:

1. Is RailSSA compact?
2. Is machine SSA practical?
3. Does late SSA exit reduce copies?
4. How much function-wide allocation helps over Railshot?
5. Where does compile memory go?
6. Which target patterns are needed to reach Railshot quality?
7. Is the architecture capable of progressing toward Cranelift?

---

# 57. Final definition

Railshot:

```text
validated Wasm
→ direct bounded lowering
→ immediate native code
```

Dragline:

```text
validated Wasm
→ compact semantic SSA
→ sparse optimization and demand-driven proofs
→ pressure shaping
→ machine SSA retained through allocation
→ integrated ordering and target selection
→ fusion/resource scheduling
→ progressive splitting allocation
→ late SSA exit
→ allocator-managed frame and callee saves
→ post-RA target optimization
→ profile/native layout
→ maximum-performance code
```

The final architectural thesis is:

> **Railshot makes Wago exceptionally fast to start. Dragline must make Wago exceptionally fast to run.**

Dragline should seek its advantage through physical backend quality, Wago-specific semantics, private ABI freedom, native CPU specialization, and bounded quality search—not by recreating LLVM’s general-purpose middle end.
