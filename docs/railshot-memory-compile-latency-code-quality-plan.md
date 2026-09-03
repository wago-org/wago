# Railshot memory, compile-latency, and code-quality plan

I reviewed Wago’s current `main` at commit `b40f0305906928dd415ae677fdfba5f1a608f464`, including both Railshot backends, the byte-backed frontend, hint analysis, operand stack, register allocation, root analysis, parallel compilation, code-image ownership, the September 1 arena study, and the existing optimization research.

The central conclusion is:

> **Railshot does not need an IR, a more sophisticated whole-function allocator, or another pile of handwritten peepholes. Its next frontier is to make compiler state smaller, pointer-free, bounded, and shorter-lived—then combine instruction selection, allocation, and encoding within that bounded state.**

The work should happen in this order:

1. Compact retained function summaries.
2. Eliminate failed first compilation attempts.
3. Bound and discard per-worker high-water memory.
4. Make GC-root planning adaptive.
5. Replace the pointer-rich 112-byte operand node with compact IDs.
6. Add costed tree selection, rematerialization, and a tiny machine window.
7. Gradually retire the old representation behind whole-function eligibility.

This is evidence-backed and falsifiable. Nothing is “proven for Wago” until it passes the measurement gates below, but the first several stages are intentionally behavior-preserving and build directly on wins already measured in the repository.

---

## Current-main reconciliation — September 3, 2026

A follow-up audit at `779e5e65842359c1c7b169f1af299097853a71ad` found several additional general reductions and two places where current `main` is already ahead of this plan. The complete evidence and primary-source references are in [Railshot memory reduction follow-up](research/railshot-memory-reduction-2026-09-03.md).

- AMD64 already labels deferred trees with bounded register demand and uses it for semantics-safe evaluation ordering. Extend that analysis into pre-emission pressure prediction; do not build a second tree-labeling mechanism.
- Both backends already have a fixed 24-operation machine window for ABI shuffles. Generalize it only when counters identify missed machine-level combinations; do not add a parallel window.
- Compact pointer-rich `ctrlFrame` records before or alongside operand-node conversion. Ordinary control frames currently pay for cold GC, EH, loop, and merge state.
- Resolve module invariants once, narrow compiler-only indexes, flatten parallel metadata, and apply retention limits per scratch buffer.
- Retire default-off experiments and mature rollback switches that fail the normal qualification gates. Compiler mechanisms should replace old state, not accumulate beside it.

The implementation starts with two code-identical cuts from that audit:

1. Module hint scanning always retains exact touched-global records instead of a dense function-by-global matrix, and the fixed hint record drops from 200 to 152 bytes. On a synthetic 1,024-function/1,024-global shape with one touched global per function, this changed the ARM64 hint benchmark from approximately 5.47 MB and 0.64 ms per operation to 0.24 MB and 0.12 ms per operation. This is a targeted stress result, not a full-corpus claim.
2. Module-wide synchronous-host-call classification is computed once per module, and the bounded module-global pin list replaces a per-function `globals`-sized membership bitmap.

The policy boundary is explicit: production may select work from validated semantics, effects, bounded resource estimates, and target costs. It must never select an optimization from producer identity, module or function names, function indexes, benchmark membership, hashes, or memorized body bytes. A corpus may validate a general mechanism; it may not activate one.

---

## 1. What current Railshot has already solved

Several recommendations that would have been correct a month ago are now obsolete.

| Area | Current state | Consequence for this plan |
|---|---|---|
| Function-body AST removal | Production decoding already retains raw `BodyBytes` and does not materialize instruction trees. | Do **not** build another streaming or AST-removal frontend. |
| Serial native-code copying | Serial compilation now emits directly into the writable code image. This materially reduced heap use while preserving native bytes. | Do not spend more time on serial output ownership. |
| Basic compiler instrumentation | `CodegenStats` already measures code bytes, frame size, spills, reloads, flushes, bounds checks, calls, pins, peepholes, and unpinned retries. It is opt-in and nil-safe when disabled. | Extend the existing dashboard; do not invent a parallel telemetry system. |
| Initial arena sizing | The September 1 work already uses bounded body estimates for serial compilation and measured up to 18.3% lower backend bytes on `utf-as`. | The remaining issue is retained high-water and node representation, not merely initial capacity. |
| Regional local allocation | Both backends already have bounded interval-region pinning. Current admission is mostly call-free, bounded, straight-line code. | Extend regions only where counters prove it worthwhile. |
| Deferred-load alias handling | The cheap opportunity was measured as nearly dead across the real corpus. | Do not prioritize a general alias-analysis project. |
| Direct parallel image assembly | Multiple implementations reduced Go heap but regressed parallel latency, so the heap-backed join was deliberately retained. | Fix scheduling and scratch coexistence instead of retrying the same mapping design. |

That leaves four current structural cliffs.

### The hot operand node is much too large and too scannable

The common `elem` is currently 112 bytes on both architectures. It contains four direct `*elem` links, while its embedded storage includes rare pointer-bearing fields such as a custom-type pointer and a register slice.

Because links are pointers, nodes need stable addresses, which in turn encourages chunked arenas whose high-water capacity is retained when the stack resets. The current reset rewinds the arena but does not discard its previously grown chunks.

Pointer-free backing matters independently of raw byte size: Go does not scan the backing storage of pointer-free slices, maps, or channels. The repository’s own controlled probe found that a 56 MB pointer-rich node backing contributed almost the same amount to `/gc/scan/heap`, while an equally sized pointer-free backing contributed almost none.

### The hint plane retains too much per-function structure

`funcHints` is currently 200 bytes per function, before accounting for its referenced local/global score and last-use arrays. Its fields include several slice headers and sidecars for local scores, global scores, last gets, global eligibility, sparse globals, and immutable table information.

The current module hint construction allocates local-score storage proportional to the sum of locals, and a dense global-score and eligibility matrix when `functions × globals <= 1<<20`. At the cutoff, those two global arrays alone occupy roughly:

```text
1,048,576 × 4 bytes  global scores
1,048,576 × 1 byte   eligibility
≈ 5 MiB
```

The full dense information is useful while scanning a function, but almost none of it needs to remain attached to every function until compilation ends.

### Register exhaustion can duplicate a whole function’s work

Current code can abandon a pinned compilation attempt after register exhaustion and compile the entire function again with local pinning disabled.

That is a bad p99 cliff even when it is rare:

```text
latency  = failed decode/lower/emit/finalize attempt
         + successful second attempt

memory   = high-water created by both attempts
```

The September 1 arena investigation explicitly identifies retry-cost counters and a bounded pressure hint as the next work.

### Parallelism is bounded by worker count, not by bytes in flight

Four workers produced strong backend speedups, but representative allocated bytes rose by roughly 43–93%, and an `esbuild` run increased peak RSS from about 147 MiB to 190 MiB. Higher worker counts showed even larger backend speedups, but the full-pipeline and memory results are why adaptive mode stops at four.

The next scheduler needs to understand that four tiny functions are different from four giant functions.

---

# 2. Target architecture

The intended end state should look like this:

```text
validated byte-backed module
        │
        ▼
single fused function-summary scan
        │
        ├── compact fixed FuncSummary[]
        ├── compact top-local/global side tables
        ├── root-plan estimates
        └── scratch/code-size/pressure estimates
        │
        ▼
memory-budgeted function scheduler
        │
        ▼
bounded worker state
        ├── pointer-free ValueNode arena
        ├── NodeID operand stack
        ├── exact near-future use within packet
        ├── small fact tables
        └── bounded machine window
        │
        ▼
costed selection + allocation + encoding
        │
        ▼
existing finalizer / relocations / stubs / code-image owner
```

The important architectural constraints are:

- No whole-function SSA.
- No permanent second public compilation tier.
- No per-instruction heap allocation.
- No optimizer structure that grows without a hard cap.
- No retry that recompiles an already emitted function.
- No extra body walk unless its measured benefit pays for it.
- No new compiler mechanism without deleting or replacing old state.
- No transformation whose bounds-check, address, type-width, trap-order, or clobber obligations cannot be stated explicitly.

V8 Liftoff demonstrates that direct one-pass compilation can still keep a virtual stack, defer constants, and snapshot merge state without building an IR. Winch similarly avoids an IR and complex register allocation, but its lower generated-code quality illustrates the ceiling of opcode-at-a-time selection. TPDE points to the useful middle ground: combine selection, allocation, and encoding in one bounded compilation pass instead of serializing those decisions into separate global phases.

---

# 3. Phase 0: turn the current stats system into a resource ledger

This must be the first PR because several later choices depend on distinguishing live memory from allocation traffic, and payload work from GC overhead.

## Extend, do not replace, `CodegenStats`

Add a module-level `CompileStats`, still behind a nil pointer or explicit diagnostics mode:

```go
type CompileStats struct {
    StageNanos       [compileStageCount]uint64
    BodyBytesWalked  [bodyPassCount]uint64
    FunctionAttempts uint64

    HintHeaderBytes   uint64
    HintSidecarBytes  uint64
    RootAnalysisBytes uint64

    WorkerScratchReserved  uint64
    WorkerScratchPeak      uint64
    WorkerScratchRetained  uint64
    WorkerScratchDiscarded uint64

    TransientCodeBytes uint64
    RetainedCodeBytes  uint64
    JoinBytes          uint64

    RetryFunctions      uint64
    RetryInputBytes     uint64
    RetryNodesAllocated uint64
    RetryCodeBytes      uint64
    RetryNanos          uint64

    MaxGPPressure    uint16
    MaxFPPressure    uint16
    MaxV128Pressure  uint16
    MaxFixedPressure uint16
}
```

Also extend per-function stats with:

- Flush reason: call, merge, alias, safepoint, tree cap, machine-window cap.
- Maximum live operand nodes.
- Maximum pending-tree register demand.
- Fixed-register conflict count.
- Pin relinquishments.
- Rematerialization candidates and hits.
- Move-cycle count at calls and merges.
- Selector candidates examined.
- Matcher and machine-window time.
- Root-plan mode and estimated versus actual bytes.

## Measure Go scanning, not only `B/op`

Record these official runtime metrics around complete compilation:

```text
/gc/scan/heap:bytes
/gc/scan/stack:bytes
/gc/heap/live:bytes
/gc/heap/goal:bytes
/cpu/classes/gc/mark/assist:cpu-seconds
/cpu/classes/gc/mark/dedicated:cpu-seconds
/cpu/classes/gc/mark/idle:cpu-seconds
```

Go exposes these specifically so heap scan and GC assist can be separated from ordinary execution CPU.

Run every memory experiment in three modes:

| Mode | Purpose |
|---|---|
| `GOGC=off` | Exposes allocator and compiler payload costs without concurrent GC noise. |
| Default GC | Measures real end-to-end latency and assist cost. |
| Forced GC after compile | Reveals retained and scannable high-water after transient work should be dead. |

## Required benchmark matrix

Use prebuilt binaries and interleaved A/B order. Capture p50, p90, p99, maximum, confidence interval, bytes/op, allocs/op, peak live heap, scan heap, and fresh-process RSS.

The matrix should include:

- `tiny`, `fib`, and one-function scalar modules.
- `many_funcs`.
- `json-as`, `utf-as`, and BLAKE/SIMD.
- Lua, SQLite, Ruby, and esbuild.
- One function with thousands of ALU nodes.
- One function with many locals.
- One module with many functions × many globals.
- Deep structured control.
- Multi-result calls.
- GC-reference-heavy functions.
- Exception handling.
- Plugin/custom-instruction lowering.
- A deliberately register-hostile function.
- A giant function followed by many tiny functions.
- The same giant function compiled repeatedly in one process.
- Workers `1`, `2`, `4`, and `8`.

The **giant-then-tiny** case is essential. Ordinary `B/op` will not reveal a worker that permanently retains a giant scannable arena.

---

# 4. Phase 1: compact the function-summary plane

This is the best low-risk next memory project. It changes representation but need not change generated code at all.

## 4.1 Split `funcHints` into a fixed header and contiguous side tables

Target a 48- or 64-byte fixed record:

```go
type FuncSummary struct {
    Flags uint32

    BodyBytes     uint32
    StackNodeHint uint32
    CodeByteHint  uint32
    LocalCount    uint32

    LocalTopStart  uint32
    GlobalTopStart uint32
    IntervalStart  uint32

    LocalTopCount   uint8
    GlobalTopCount  uint8
    MaxControlDepth uint8
    PressureClass   uint8

    MaxGPNeed       uint8
    MaxFPNeed       uint8
    MaxV128Need     uint8
    FixedReserve    uint8

    // Remaining compact scalar fields.
}
```

A 64-byte header would reduce the fixed record from 200 bytes by **68%** before sidecar savings.

Keep optional information in module-owned pointer-free arrays:

```go
type RankedLocal struct {
    Index uint32
    Score uint32
}

type RankedGlobal struct {
    Index uint32
    Score uint32
    Flags uint32
}

type IntervalLocal struct {
    Index   uint32
    LastGet uint32
    Score   uint32
}
```

Each function stores only an offset and count.

This removes multiple slice headers from each summary, improves locality, and lets all ordinary summaries occupy one non-scannable backing array.

## 4.2 Reuse dense scan scratch; retain only decisions

During the byte-backed hint scan, use one worker-independent scratch object:

```go
type HintScratch struct {
    LocalScore   []uint32
    LocalLastGet []uint32
    LocalEpoch   []uint32

    GlobalScore []uint32
    GlobalFlags []uint8
    GlobalEpoch []uint32

    Epoch uint32
}
```

Resize it only to the maximum local/global count encountered, not the sum across all functions. Use epoch tagging instead of clearing every entry between functions.

For a giant outlier:

- Allocate an ephemeral scratch backing.
- Produce the compact summary.
- Discard the outlier backing immediately.
- Keep the normal reusable backing at a configured cap.

The existing `GlobalHintAccumulator` already follows the right conceptual model: dense reusable scratch during scanning, sparse retained records afterward. Apply that model to locals as well.

## 4.3 Retain exact top-K candidates

Do not approximate pin decisions.

While the full score array is available, retain exactly the candidates that codegen could select:

```text
KGP = number of target GP pin registers + 2
KFP = number of target FP pin registers + 2
```

For small K, an insertion-sorted fixed array is likely cheaper than sorting all locals or maintaining a heap.

Retain full `score + lastGet` information only for functions that pass the existing interval-region admission. That path is already bounded to at most 256 locals and 16 KiB bodies.

## 4.4 Remove the dense function × global matrix

The current matrix can approach 5 MiB at its cutoff. Replace it with:

1. One reusable dense accumulator for the function currently being scanned.
2. Exact extraction of its top global candidates.
3. A compact module aggregate for module-global pin decisions.
4. Per-function sparse records only for globals that survive selection.

This should preserve pin choices exactly because full scores still exist at selection time; only dead information is discarded.

## Phase 1 acceptance gates

- Native code and serialized artifacts remain byte-identical.
- `FuncSummary` is at most 64 bytes.
- No per-function slice allocation for ordinary scalar functions.
- Dense function × global retained storage is gone.
- At least 50% lower summary/sidecar bytes on a many-functions fixture.
- At least 10% lower full-compile `B/op` on a 1,000+ function module.
- No compile-latency regression above the repository’s normal investigation threshold.
- The giant-local scratch is absent after forced GC.

---

# 5. Phase 2: eliminate whole-function retry

The goal is not merely to make retry faster. The goal is to make it disappear from production.

## 5.1 Measure the failed attempt

Use the existing `UnpinnedRetry` event, but preserve the failed attempt’s resource counters before resetting function stats:

```text
failed attempt nanoseconds
failed code bytes
failed node count
failed spill count
pins selected
maximum simultaneous GP/FP/vector demand
fixed-register operation at failure
Wasm byte offset of failure
```

Group failures by cause. Do not assume “too many pins” until the counters prove it.

## 5.2 Compute register demand during the existing hint scan

Add a bounded Sethi–Ullman-style pressure calculation to the byte scan. For each deferred scalar expression:

```text
need(leaf/rematerializable constant) = 0 or 1
need(noncommutative a op b)          = max(need(a), 1 + need(b))
need(commutative a op b)             = min(
                                          max(need(a), 1 + need(b)),
                                          max(need(b), 1 + need(a))
                                      )
```

Track GP, FP, and vector classes separately. Add reserves for:

- Fixed-register instructions.
- Call argument and result staging.
- Memory address temporaries.
- Multi-result values.
- GC helper lowering.
- Bulk-memory clobbers.
- Inline expansion.

This is not a live-interval analysis. It is a conservative register-demand summary of the bounded trees Railshot already constructs.

## 5.3 Choose the pin budget before emission

For each register class:

```text
available_for_pins =
    allocatable
  - predicted_expression_need
  - fixed_register_reserve
  - safety_margin
```

Then select only that many pins. A function with very high temporary demand should begin with fewer optional pins instead of discovering this after emitting most of its code.

## 5.4 Add controlled pin relinquishment

The second defense should be an in-place escape hatch:

1. Select the least valuable optional pin.
2. Write its current value to its canonical local home.
3. Change the local from pinned to frame-resident.
4. Return the register to the allocator.
5. Continue compilation.

This must initially exclude pinned values whose coherence rules are complicated by calls, globals, GC references, or pending borrowed addresses.

This converts a catastrophic full retry into one store and a local-state transition.

## 5.5 Keep retry only as an oracle

For one development cycle:

- Production uses predictive pin budgeting.
- Debug/CI can run the old retry-capable path.
- A test fails if production reaches retry on the corpus.
- Differential tests compare semantics and aggregate quality.

Remove the full second attempt once the pressure corpus and real corpus show zero retries.

## Phase 2 acceptance gates

- Zero production retries on the complete corpus and adversarial pressure suite.
- At least 95% agreement between predicted pressure class and observed maximum pressure.
- No more than 1% execution geomean regression from reduced pinning.
- No individual hot benchmark more than 2% slower unless compile p99 improves enough to justify it.
- Register-hostile compile p99 materially lower.
- Failed-attempt bytes and nanoseconds become zero.

---

# 6. Phase 3: control worker high-water and bytes in flight

Do not change the current 256-node parallel initial arena indiscriminately. The September 1 study deliberately kept that ceiling to avoid multiplying a giant hint across every worker.

The remaining work is lifecycle and scheduling.

## 6.1 Split retained and ephemeral arena chunks

Classify arena backing into:

```text
base chunks       retained across ordinary functions
overflow chunks   owned by one function and discarded at its end
```

A possible policy:

```text
retain up to:
    max(default base, rolling p90 ordinary-function demand)

discard:
    chunks above retain limit
    chunks allocated for one giant function
    chunks whose utilization stayed below 25%
```

Use hysteresis so a sequence of moderately large functions does not repeatedly allocate and release the same backing.

Because pointer stability is only needed during a function’s compilation, overflow chunks can be dropped immediately after that function completes.

## 6.2 Add a dedicated giant-function lane

Do not allow four workers to independently become giant workers.

Classify a function as giant when its estimate exceeds any of:

- Node scratch threshold.
- Root-analysis threshold.
- Predicted code bytes.
- Local/global scratch threshold.
- Configured fraction of compile-memory budget.

Only one giant function may compile at once. Ordinary workers continue processing small functions if the remaining budget permits.

This directly protects against permanent multiplication of high-water capacity.

## 6.3 Schedule expensive functions early

Within deterministic constraints, schedule functions approximately largest-first.

The output is still stored by source index, and errors are still reported according to source order. Scheduling order therefore need not affect artifact determinism.

Large-first reduces this overlap:

```text
already-retained output from many small functions
+ giant scratch
+ giant root graph
+ giant temporary output
```

The giant’s scratch is allocated before much output has accumulated, then discarded before the long tail of small functions.

## 6.4 Use a weighted memory semaphore

Estimate per-function transient cost:

```text
reservation(f) =
    fixed worker state
  + node scratch estimate
  + hint/locals scratch
  + root-analysis estimate
  + relocation/finalizer scratch
  + transient function output
```

Permit a function to start only when:

```text
sum(active reservations)
+ retained completed output
+ module fixed memory
<= compilation memory budget
```

Tokens have different lifetimes:

| Token | Release point |
|---|---|
| Operand/node scratch | End of function |
| Root-analysis scratch | Root plan finalized |
| Finalizer scratch | Function artifact finalized |
| Function output | Parallel join or module completion |
| Module summary storage | End of compilation |

If an estimate is exceeded, acquire additional tokens before growing. Underestimation must reduce parallelism, never reject a valid module.

## 6.5 Keep the current parallel heap join

The code-image work already tested direct mapping and disjoint population. Variants reduced heap from roughly 6.25 MB to 3.73 MB on the benchmark but regressed latency by 2.4–12.9%, so they were removed.

Do not revisit that unless another architectural change removes the page-touch or synchronization cost. The scheduler should count the current join’s bytes, not wish them away.

## Phase 3 acceptance gates

- Prediction error within 25% at p90.
- Giant functions never enlarge every worker.
- Forced-GC retained scratch after giant-then-tiny is near the ordinary baseline.
- At the same worker count: at least 20% lower peak RSS on one parallel large-module workload; **or**
- At the same memory budget: at least 15% better backend throughput.
- Serial compilation remains statistically unchanged.
- Serial and parallel native bytes remain identical.
- Error selection remains deterministic.

---

# 7. Phase 4: make GC-root planning adaptive

Current exact root analysis has a 64 MiB arena ceiling. Exact liveness is worthwhile for reference-heavy functions, but it is unnecessary overhead for many functions.

Use three primary modes.

## 7.1 `RootNone`

Select when the function has no references live across any safepoint.

Output:

- No liveness graph.
- No root bitmap.
- No reference-home initialization solely for root reporting.

This should cover most scalar functions.

## 7.2 `RootAllCanonical`

Select for small reference sets where conservative canonical roots are cheaper than constructing and solving a graph.

Requirements:

- Every root slot included in the bitmap contains either a valid reference or canonical null.
- Reference operands are canonicalized before a safepoint.
- No uninitialized frame bytes are interpreted as handles.
- The cost of extra root retention stays below a fixed threshold.

A simple model is:

```text
conservative cost =
    prologue initialization bytes
  + safepoints × bitmap words
  + estimated additional objects retained

exact cost =
    graph allocation
  + graph propagation
  + exact-map serialization
```

Choose conservative roots only when the former is clearly cheaper.

This mode may use one function-level local-root bitmap plus small exact stack-home maps where necessary.

## 7.3 `RootExactGraph`

Retain the existing exact analysis for:

- Many reference locals.
- Many safepoints.
- WasmGC-heavy functions.
- Loops where conservative roots would retain substantial object graphs.
- Exception-handling shapes with complex merges.
- Any function outside the simpler modes’ proof obligations.

## 7.4 Optional capped effect tape

Only after measuring graph setup as a dominant cost, prototype a pointer-free byte-backed effect tape:

```text
local-def
local-use
stack-ref-push
stack-ref-pop
safepoint
control-open
control-merge
```

The tape must have a byte cap. If exceeded, use the existing graph analysis. Do not permit it to become a second unbounded representation of the whole body.

## Phase 4 acceptance gates

- Exact root maps remain the differential oracle.
- Full WasmGC, EH, and malformed-module suites pass.
- No invalid or uninitialized slot can be reported as a root.
- Conservative mode’s retained guest-heap increase stays below its configured threshold.
- Root-analysis peak bytes decrease on mixed scalar/reference corpora.
- Ordinary scalar functions allocate no root-analysis graph.
- No runtime GC regression above 1.5% on reference-heavy benchmarks.

---

# 8. Phase 5: replace pointer-rich `elem` with a compact ID representation

This is the largest structural memory change and should be staged rather than rewritten all at once.

## 8.1 First split cold storage

Move rare fields out of the common node:

- Custom instruction/type pointers.
- Variable-length register bundles.
- Plugin-specific payloads.
- Rare GC/type metadata.
- Large immediates that do not fit inline.
- Debug-only source information.

Use a compact `ColdID`:

```go
type ColdID uint32

type ColdValue struct {
    Custom *coreplugins.CustomType
    VRegs  []Reg
    // Other rare fields.
}
```

Ordinary scalar nodes should never pay for pointer-bearing cold fields.

## 8.2 Replace pointers with IDs

A realistic 32-byte common node is:

```go
type NodeID uint32

type ValueNode struct {
    Op    uint16
    Type  uint8
    Flags uint8

    A NodeID
    B NodeID

    Imm uint64

    Aux  uint32
    Home uint32
}
```

That is 28 bytes of fields and will generally occupy 32 bytes after alignment.

Replace:

```go
prev, next, arg0, arg1 *elem
```

with:

```go
Prev, Next, A, B NodeID
```

Or, preferably, eliminate the intrusive list entirely and represent the operand stack as:

```go
stack []NodeID
```

A reduction from 112 bytes to 32 bytes is a **71.4% reduction in common node payload**. It does not imply a 71.4% reduction in total compiler memory, but it attacks one of the hottest structures directly.

## 8.3 Use one growable pointer-free backing

IDs remain valid if a slice backing moves. That means the current requirement for pointer-stable geometric chunks disappears.

Initially:

```go
nodes []ValueNode
```

can simply grow. The backing is pointer-free and therefore not scanned by Go.

Later, once equivalence is established:

- Add an inline small backing.
- Recycle dead nodes.
- Bound pending nodes per packet.
- Drop giant transient backing after the function.

Do not begin with a custom off-heap allocator. Plain pointer-free Go memory receives most of the GC benefit while preserving portability and tooling.

## 8.4 Introduce node generations only in debug mode

To catch stale IDs during migration:

```text
NodeID = generation | index
```

In release mode, retain a plain compact index unless measurements show the check is free.

## 8.5 Bridge through accessors

The first NodeID PR should preserve all current lowering behavior:

```go
func (f *fn) node(id NodeID) *ValueNode
func (f *fn) left(id NodeID) NodeID
func (f *fn) right(id NodeID) NodeID
```

Machine code should remain byte-identical.

Only after this bridge passes should the allocator and selector begin exploiting compact metadata.

## 8.6 Bound the pending packet

The eventual model should have:

```text
default live pending nodes: 32
hard live pending nodes:    64
```

When the cap is reached:

1. Pick a safe root using deterministic policy.
2. Materialize it to a register or canonical spill home.
3. Replace its subtree with a compact leaf.
4. Recycle unreachable node IDs.
5. Continue.

Cap exhaustion should produce canonical code, not allocate an arbitrarily larger optimizer structure.

## Phase 5 acceptance gates

- Common node at most 32 bytes.
- No pointers in the common node backing.
- At least 60% lower node backing bytes on expression-heavy functions.
- Large reduction in `/gc/scan/heap` attributable to Railshot scratch.
- No per-op heap allocation.
- No native-code differences in the bridge PR.
- No more than 2% compile-latency regression during the bridge.
- Hard packet cap demonstrated by an adversarial function.
- Cap exhaustion does not retry or fail compilation.

---

# 9. Generated-code quality lane

This can begin with smaller work while the NodeID migration is underway, but the larger selector should land on the compact representation to avoid rewriting it twice.

## 9.1 Ship restricted pending `local.set` and `local.tee`

This is already identified in Wago’s roadmap and remains a sound bounded optimization.

Initially admit only values that are:

- Pure.
- Non-trapping.
- Register-free.
- Made from constants, canonical slots, immutable globals, or safe local references.
- Free of borrowed memory addresses.
- Free of GC ownership or rooting complications.

A pending binding enables:

```text
local.set x; local.get x     → forward the pending value
local.set x; local.set x     → delete the first set
local.set x; return          → delete dead store when x is not observable
expr; local.tee x; consumer  → sink directly into consumer and x’s home
```

Flush pending bindings on:

- Control merges.
- Calls and host transitions.
- Safepoints.
- Potentially aliasing local or global effects.
- EH boundaries.
- Plugin/custom-instruction boundaries.
- Packet cap.

This is a better near-term bet than further deferred-load alias work, which existing counters found nearly absent.

## 9.2 Cost entire deferred trees at their sink

Railshot already pays to retain bounded expression trees. It should make one selection decision for the tree rather than a sequence of locally greedy decisions.

Label each root with possible result forms:

```text
immediate
GP register
FP/vector register
flags
memory operand
scaled address
canonical spill home
fixed-register result
```

Each candidate has hard constraints and a cost tuple:

```text
validity:
    type width
    effects
    aliasing
    trap order
    fixed registers
    clobbers
    feature set
    bounds proof

cost:
    temporary registers
    critical-path latency
    estimated uops
    code bytes
    moves
    spills/reloads
    compile effort
```

Selection should be lexicographic at first:

1. Semantically valid.
2. Fits register budget without avoidable spill.
3. Lowest estimated latency/uops.
4. Lowest code bytes.
5. Deterministic tie break.

Avoid one mysterious weighted score until the components have been validated independently.

## 9.3 Use Sethi–Ullman ordering only where semantics permit

For commutative, pure, non-trapping subtrees, choose the child order that minimizes temporary register demand.

Do **not** reorder:

- Potentially trapping loads.
- Calls.
- Atomic operations.
- GC allocation or barriers.
- Volatile/plugin effects.
- Expressions whose trap order is observable.
- Floating-point expressions where a transformation changes required semantics.

## 9.4 Add bounded rematerialization

Rematerialization is the principle that recomputing a cheap value can cost less than spilling and reloading it.

Start with an explicit whitelist:

- Integer constants.
- Zero values.
- Small add/sub constants.
- Shifted indices.
- Masks.
- Known linear-memory base plus small offset.
- Immutable global values.
- Simple extended/truncated local values when the source remains available.

Store a compact rematerialization recipe rather than an owned register. Include actual target cost:

```text
remat cost < spill store + later load + register-pressure penalty
```

Never rematerialize a trapping load or anything whose value can change.

## 9.5 Improve calls and merges before building a global allocator

Several measured slow functions had zero ordinary spills, indicating that generic spill policy is not the primary problem. The valuable traffic is often:

- Call-local reloads.
- Argument shuffles.
- Merge-state shuffles.
- Values pinned across regions where the register would be more useful temporarily.

Add a bounded parallel-move resolver for calls and control merges:

1. Eliminate identity moves.
2. Schedule acyclic moves.
3. Break cycles with one scratch register when available.
4. Otherwise use one scratch slot.
5. Prefer changing a flexible destination assignment over introducing a move.

Record cycles, scratch-register use, and scratch-slot use in `CodegenStats`.

## 9.6 Expand interval regions carefully

Current interval-region pins are bounded to call-free, mostly straight-line functions. Expand in this order:

1. Unique-predecessor structured blocks.
2. Simple `if` regions with identical local state at both exits.
3. Loops with no calls and a statically bounded set of modified locals.
4. Calls whose clobber set does not overlap the admitted cache registers.

Use versioned local-state logs:

```text
local index
old location
new location
definition epoch
```

Cap the log. On overflow, canonicalize the region and continue.

Do not construct whole-function live intervals.

## 9.7 Add a tiny post-selection machine window

Keep the last 8–16 symbolic machine operations before final encoding.

Optimize only exact local patterns:

- Redundant moves.
- Move chains.
- Extension followed by truncation.
- Compare materialization followed by branch.
- Spill immediately followed by reload.
- Duplicate address construction.
- Fixed-register shuffles.
- Load/store cancellation where aliasing is exact.
- Adjacent constant materialization.
- ARM64 address-mode and pair-load/store opportunities.
- AMD64 memory-source and three-operand opportunities.

Flush the window at:

- Labels and branch targets.
- Calls.
- Traps and safepoints.
- EH edges.
- Atomics.
- Plugin/custom effects.
- Relocation boundaries.
- Window cap.

This is a bounded machine peephole buffer, not a basic-block IR.

## 9.8 Generate selection matchers offline

QBE’s `mgen` is a useful model: patterns are numbered offline, candidate sets are represented compactly, and a small generated matcher program captures variables while handwritten policy chooses among valid candidates. Classic `wburg` work shows that useful tree grammars can be optimally parsed in one bottom-up pass and can even avoid an explicit full IR tree.

A Railshot rule should declare:

```text
root opcode
child classes
input/output types
result form
effects
trap behavior
alias class
fixed registers
clobbers
required CPU features
bounds-proof transformation
emitter
cost tuple
```

Runtime limits:

```text
maximum tree depth:       8
maximum bytecodes/root:  32
maximum candidates/root: 8
maximum alternatives:    2 per goal
online allocations:      0
```

Keep direct switch fast paths for singleton patterns. Invoke the matcher only where an opcode has multiple useful covers.

## 9.9 Verify rules offline

Use three levels:

1. Exhaustive small-domain testing for narrow integers and shifts.
2. Differential execution against current Railshot.
3. SMT verification for algebraic, width, and address transformations.

VeriISLE demonstrates the practical model: rule chains are checked offline using SMT and authoritative ISA semantics; no solver is needed during compilation.

---

# 10. Bounds checks and memory-address transformations need proof-carrying lowering

This should be treated as a security requirement, not just a testing detail.

In April 2026, Wasmtime disclosed:

- A Winch bug where upper bits in an address register were assumed clear, allowing out-of-sandbox memory access.
- A Cranelift AArch64 bug where the address used for checking diverged from the address used for the actual load.
- A Winch `table.size` width error that could expose host stack data.

Railshot’s selector should therefore never independently reconstruct “equivalent” checked and accessed addresses.

Use an explicit address identity:

```go
type MemAddrID uint32

type MemAccess struct {
    Addr        MemAddrID
    IndexWidth  MachineType
    AccessWidth uint8
    Offset      uint64
    Memory      uint32
}

type BoundsProof struct {
    Addr        MemAddrID
    AccessWidth uint8
    Kind        BoundsProofKind
}
```

The bounds check and the actual memory instruction must consume the same `MemAddrID`. A transformation that changes the address either:

- Transforms the proof and access together under a verified rule, or
- Invalidates the proof and emits a new check.

For declared-minimum elimination, only remove a check when all of these hold:

```text
same memory index
same effective-address expression
correct 32/64-bit wrapping semantics
offset + access width cannot overflow
effective range is below the declared minimum
memory cannot shrink
no selector rule later substitutes a wider load
```

The same width discipline applies to tables, multi-value ABI results, GC references, and vector memory instructions.

---

# 11. Compact-core migration strategy

Do not convert every Railshot feature simultaneously, and do not mix two compiler states halfway through a function.

## Whole-function eligibility

The initial compact path should accept functions containing:

- Scalar integer constants and arithmetic.
- Integer comparisons.
- Basic locals and globals.
- Scalar loads and stores.
- `select`.
- Basic blocks, loops, and `if`.
- Direct calls only after scalar call lowering is ready.

Initially exclude:

- SIMD.
- WasmGC.
- Exception handling.
- Atomics.
- Multi-memory and memory64.
- Typed function references.
- Plugin/custom instructions.
- Complex multi-result calls.

An unsupported function compiles entirely through current Railshot. There is no translation of partially built trees or allocator state.

This gives a clean safety boundary:

```text
function is compact-eligible
    → compact path from byte 0

function is not compact-eligible
    → current path from byte 0
```

## Never fall back by recompiling after emission

Feature admission must happen before compilation.

Resource-cap exhaustion inside the compact path must:

- Materialize pending state.
- Clear local optimizer facts.
- Continue with canonical scalar lowering.

It must not restart the function through the old backend.

## Shadow mode

In CI and benchmark builds:

1. Compile eligible functions with both paths.
2. Run differential inputs.
3. Compare traps, results, memory, tables, globals, and host effects.
4. Record native-code and compile-resource deltas.
5. Never expose dual compilation in production.

## Delete as coverage grows

The compact core must not become a permanent second implementation.

Set removal milestones:

```text
50% ordinary scalar function coverage
80% scalar corpus byte coverage
95% non-GC/non-SIMD corpus byte coverage
```

At each milestone, move shared semantics into the compact core and delete the corresponding old node/lowering machinery. The goal is convergence, not dual maintenance.

---

# 12. Recommended PR sequence

| PR | Change | Must remain identical | Primary gate |
|---:|---|---|---|
| 0 | Extend current stats with stage, scratch, scan, retry, and pressure metrics | Code and artifacts | Disabled overhead statistically zero |
| 1 | Split `funcHints` into fixed `FuncSummary` plus side tables | Native-code hash | Header ≤64 B |
| 2 | Reusable local/global scan scratch and exact top-K retention | Pin choices and code hash | Dense function × global matrix gone |
| 3 | Retry-cost capture and pressure estimator | Existing production policy | Predictor validated against observed pressure |
| 4 | Predictive pin budgeting | Semantics | Zero retries on corpus |
| 5 | Optional pin relinquishment | Semantics | No full retry on adversarial functions |
| 6 | Retained versus ephemeral arena chunks | Native-code hash | Giant-then-tiny retained memory collapses |
| 7 | Weighted memory scheduler and giant lane | Serial/parallel byte identity | Lower RSS at equal workers or faster at equal budget |
| 8 | Adaptive `RootNone` / conservative / exact plans | Root correctness | Lower root-analysis memory |
| 9 | Move cold `storage` fields to side tables | Native-code hash | Ordinary node no pointers from cold payload |
| 10 | Replace node links with `NodeID`; operand stack becomes IDs | Native-code hash | Pointer-free common backing |
| 11 | Bounded packet recycling/canonicalization | Semantics | Hard cap proven |
| 12 | Restricted pending locals and rematerialization | Semantics | Fewer local stores/reloads |
| 13 | Costed scalar tree covering | Semantics | Runtime win without compile/memory regression |
| 14 | Bounded call/merge resolver and expanded regions | Semantics | Fewer moves and call-local reloads |
| 15 | Tiny machine window | Semantics | Better code bytes/uops |
| 16 | Generated matcher plus offline verification | Semantics | Matcher ≤2% of scalar codegen time |
| 17 | Compact scalar path enabled by default | Public behavior | Passes all aggregate gates |
| 18 | Delete superseded old scalar machinery | Public behavior | Net source and binary complexity decrease |

PRs 1–2, 3–7, and 9–11 are the main memory/latency sequence. PRs 12–16 are the main generated-code-quality sequence.

---

# 13. Acceptance framework

## Correctness gates

Every PR touching lowering must pass:

- Official MVP, Release 2, and enabled Core 3 suites.
- Both explicit and signal-backed bounds configurations.
- AMD64 and ARM64.
- Malformed and invalid corpus.
- WasmGC and EH suites when the change can reach those paths.
- Differential execution against the previous path.
- Random instruction-sequence generation.
- Trap code and trap-order comparison.
- Host-call side-effect comparison.
- Serial/parallel deterministic artifact comparison.

For address, width, or selector changes, add:

- 32-bit and 64-bit address overflow cases.
- Maximum offsets.
- Narrow loads at the end of memory.
- Sign- and zero-extension combinations.
- Table32/table64 width differences.
- Multi-result upper-bit poisoning tests.
- Guard-page and explicit-check parity.

## Compile-latency gates

I would use these as default rejection thresholds:

| Metric | Gate |
|---|---:|
| Tiny-module p50 | No regression above 1.5% |
| Full-corpus compile geomean | No regression above 1.5% for memory-only changes |
| Large-module p50 | No regression above 2% |
| Large-module p99 | Must improve for retry/scheduler work |
| Metrics-disabled overhead | Below measurement noise |
| Matcher/window share | At most 2% of scalar backend time |
| Cap-exhaustion latency | Linear and bounded |

A code-quality change may spend compile latency only when its execution improvement passes a declared exchange rate. For example:

```text
+1% compile geomean allowed only for:
    ≥3% execution geomean improvement
    or ≥5% improvement in an explicitly targeted hot workload
```

## Memory gates

| Metric | Gate |
|---|---:|
| Summary header | ≤64 bytes/function |
| Common value node | ≤32 bytes |
| Common node backing | Pointer-free |
| Per-op allocations | Zero |
| Serial peak RSS | Never regress for memory-only changes |
| Parallel peak RSS | At least 20% lower at equal workers, or equivalent throughput gain at fixed budget |
| Post-giant retained scratch | Near ordinary-worker baseline |
| Root-analysis cap case | Correct fallback without unbounded growth |
| Optimizer packet | Hard 64-node limit |
| Machine window | Hard 16-op limit |

## Generated-code gates

| Metric | Gate |
|---|---:|
| Execution geomean | Positive and statistically supported |
| Individual regression | Investigate above 1.5%; reject above 3% without a clear trade |
| Native-code geomean | No more than 2% growth by default |
| Per-function growth | Must fit function and module byte budgets |
| Spills/reloads | Non-increasing on target workloads |
| Calls/merge moves | Non-increasing |
| Trap and safepoint metadata | Correct and bounded |
| Artifact determinism | Exact |

---

# 14. Work I would explicitly avoid

## Do not build whole-function SSA

The baseline compiler literature confirms the compile-time advantage of direct lowering, and Wago has already chosen Railshot as its sole execution backend. Liftoff and Winch validate this direction; TPDE shows that better local integration does not require splitting the compiler into global IR phases.

## Do not build an e-graph

An e-graph would add pointer-rich state, unpredictable saturation, large hash tables, and awkward trap/effect constraints. Offline superoptimization can discover rules; production should execute only bounded generated matchers.

## Do not add whole-function live intervals

The current problem is not a universal spill storm. Exact near-future use within a pending packet, local region information, and explicit call/merge handling offer most of the useful benefit with bounded memory.

## Do not start with `sync.Pool`

Pooling the current pointer-rich arena would retain high-water and make ownership less clear. First shrink the representation, make backing pointer-free, and define retention limits. Pool only small stable worker objects afterward.

## Do not revive a frontend rewrite

Production decoding is already byte-backed and avoids function-body instruction trees. The remaining frontend opportunity is to fuse compatible summaries and avoid repeated immediate decoding without disturbing validation error order.

## Do not retry direct parallel mmap assembly

That experiment already failed Wago’s balanced latency gate. Revisit only if a future code-image or page-population primitive materially changes the cost.

## Do not accumulate unranked peepholes

Wago already contains many strong exact peepholes. Every new pattern should come from one of:

- A hot disassembly.
- A high counter.
- An offline search result.
- A recurring missed cover in selector diagnostics.

Once a family has several overlapping cases, replace the handwritten cascade with a generated rule table.

## Do not expose dozens of resource knobs

Internally, Railshot may need separate limits for nodes, roots, facts, matcher fuel, machine window, and code growth. Publicly, expose at most a small policy seam such as:

```go
type CompileResources struct {
    MemoryBytes uint64
    Workers     int
}
```

Optimization internals should derive their caps from that policy.

---

# 15. Realistic target envelope

These are engineering targets, not promised results.

## After summary compaction, retry elimination, and worker lifecycle

Aim for:

- 10–20% lower full-compile allocated bytes on many-function modules.
- More substantial reductions on function/global-matrix stress cases.
- 15–25% lower parallel peak RSS at the same worker count.
- Materially lower p99 on register-hostile functions.
- No generated-code changes from the first several PRs.

These targets are consistent with current measured wins: the recent arena work already reduced `utf-as` backend allocation bytes by 18.3%, while function-result scratch reuse cut allocation counts by about 5.2% on Ruby and 5.4% on esbuild without changing native-code hashes.

## After pointer-free nodes

The common node payload target is an exact 112-to-32-byte reduction, or 71.4%. Total backend memory will fall by less because code, summaries, root structures, and side tables remain.

The more important secondary target is:

- A major decrease in `/gc/scan/heap`.
- Lower mark-assist CPU under parallel compilation.
- Better repeated-compile behavior after giant functions.
- Less allocator and cache traffic while walking deferred trees.

## After the quality lane

I would set:

- 5–10% execution geomean improvement across the currently losing compute kernels.
- 15% or better improvement on at least some targeted call-, merge-, or SIMD-heavy workloads.
- No broad compile-latency regression.
- No more than 2% native-code geomean growth.
- No regression in Wago’s strongest existing workloads.

The quality gains are most likely to come from:

1. Fewer call and merge moves.
2. Better child evaluation order.
3. Rematerialization instead of spill/reload.
4. Better memory and immediate folding chosen as a whole tree.
5. Bounded regional local allocation.
6. Exact machine-level cleanup.
7. Generated multi-operation SIMD/SWAR covers.

They are less likely to come from a generally “smarter” global spill allocator, because several known slow functions already report zero ordinary spills.

---

# Final recommendation

The first concrete implementation sequence should be:

```text
1. Extend existing stats with memory, retry, and pressure accounting.
2. Replace 200-byte funcHints with ≤64-byte FuncSummary records.
3. Reuse dense scan scratch and retain only exact top-K decisions.
4. Add Sethi–Ullman-style pressure estimates to the existing body scan.
5. Use those estimates to eliminate full unpinned recompilation.
6. Discard giant worker overflow chunks and add a byte-budget scheduler.
7. Make root analysis adaptive.
8. Move rare storage out of elem.
9. Convert elem pointers to NodeID and shrink the common node to 32 bytes.
10. Add restricted pending locals, rematerialization, and costed tree covering.
11. Add a 16-operation machine window and offline-generated matchers.
12. Delete old scalar machinery as compact-core coverage expands.
```

That sequence attacks the real current bottlenecks without undoing Railshot’s defining advantage. It preserves direct compilation, keeps optimizer state bounded, lowers Go GC involvement, eliminates repeated work, and raises machine-code quality through better **local integration** rather than a heavyweight global representation.
