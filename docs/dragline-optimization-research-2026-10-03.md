# Dragline execution optimization research — 2026-10-03

This is a source investigation and experiment plan, not a performance result.
The inspected Wago starting revision is `34d1e4481ba7f93560c84a5deaef1fb71a862516`.
The active objective is now **50% faster execution**. A 50% throughput gain
corresponds to a paired execution-time ratio of **2/3**; always report time and
throughput separately. The user also identified the improved Railshot PR #780.
The user confirmed **50% higher throughput than PR #780**. Pin its measured
head `efa9aa22dfb3781284a55c465f7f57544eafade3`; the original Railshot comparison
is historical context. Earlier 30% targets and experiments
below retain their historical meaning; they do not define current completion.

## Choose AMD64 provisionally, then verify

The repository's [September 17 report](dragline-general-corpus-2026-09-17.md)
records complete AMD64 execution coverage and almost equal Railshot/Dragline
execution time. Its ARM64 Dragline result excludes two failing modules and is
substantially slower. That is a reason to start the native AMD64 investigation,
not a current performance claim. Establish fresh exact semantic coverage before
using timings. Keep an SSH ControlMaster connection to `hub@hub`, record its
CPU/toolchain configuration, and pin the two engines to the same available CPU.

## What already exists

Several earlier research recommendations are implemented in the starting tree:

| Mechanism | Current source evidence | Implication |
| --- | --- | --- |
| Scalar and vector machine types | [`railmach/mach.go`](../src/core/compiler/backend/dragline/railmach/mach.go), `TypeV128`, aligned two-unit spills, shared FPR bank | Do not repeat the old proposal to introduce a vector type. Inspect vector residency and actual backend admission instead. |
| Sparse integer facts, aliases, GVN | [`railssa/simplify.go`](../src/core/compiler/backend/dragline/railssa/simplify.go), `SparseSimplify`, `localPureGVN`, dominating equivalence checks | Measure missed transformations before replacing the middle end. |
| Pressure shaping and LICM | [`railssa/pressure.go`](../src/core/compiler/backend/dragline/railssa/pressure.go), `PressureShape`, `planPressureLICM` | Extend profitable legal cases through the existing planner. |
| Schedule alternatives and scoring | [`railmach/schedule.go`](../src/core/compiler/backend/dragline/railmach/schedule.go), [`schedule_score.go`](../src/core/compiler/backend/dragline/railmach/schedule_score.go) | Compare emitted spill/move costs, not just pre-allocation schedule scores. |
| Greedy allocation, segmented ranges, regional fragments | [`railmach/ragreedyp.go`](../src/core/compiler/backend/dragline/railmach/ragreedyp.go) | Allocation transitions and finalizer ordering are a concrete correctness and performance seam. |
| Address folding and machine operand forms | [`railmach/select.go`](../src/core/compiler/backend/dragline/railmach/select.go) | Improve existing selection and legality contracts. |
| Bounded self-recursive expansion | [`recursive_inline.go`](../src/core/compiler/backend/dragline/recursive_inline.go) | Assess broader call-graph inlining as a distinct opportunity. |

These are observations at the stated revision; they do not prove every path is
enabled or correct on the selected corpus.

## External compiler findings and concrete applications

### 1. Make allocation transitions explicit and independently check value flow

regalloc2 returns both per-operand allocations and ordered move edits. Its
checker propagates symbolic value sets through registers, spills, reloads, and
CFG merges to verify preservation of the original virtual-register dataflow.
These are separate from mere interval overlap checks.
Sources: [allocation output contract](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/doc/GENERAL.md),
[symbolic checker](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/src/checker.rs).

**Application:** represent all regional entry, victim preservation, fragment
exit, call preservation, and block-edge moves as a single ordered edit stream.
Emit these edits at their logical positions even when the corresponding
semantic instruction is removed by a post-allocation fold. This also permits
the checker to observe exactly the actions consumed by finalization.

At the starting revision, [`compile_amd64.go`](../src/core/compiler/backend/dragline/compile_amd64.go)
checks `PostRASkip` and other elimination masks before its fragment-start
reload loop, while victim restoration occurs earlier. This is a specific
ordering hazard to reproduce, not proof that every fragment is miscompiled.
A regression should force a fragment boundary at an eliminated producer and
verify live values on every successor, including calls and loop backedges.

This change enables more aggressive allocation safely; it is not itself a
prediction of a 30% speedup.

### 2. Split at actual conflicts and keep hot uses resident

regalloc2's Ion allocator uses exact block liveness, bundles of non-overlapping
ranges, weighted uses, and conflict-driven splitting. It can split at the first
conflict, requeue fragments with a register hint, and move unused range ends
into spill bundles. This keeps irrelevant lifetime area from dominating useful
register residency.
Source: [Ion design at `2fe490bc`](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/doc/ION.md).

**Application:** after the transition checker works, compare current regional
fragments with splitting around call clobbers and cold control paths. Start
with cases where emitted assembly shows repeated loads inside a hot loop.
Track dynamic-weighted reloads and edge moves, not only the number of spilled
vregs. Preserve scalar/vector width, caller masks, fixed registers, parallel
copy cycles, and scratch occupancy. A whole-function allocator rewrite is
justified if the existing fragment model cannot express these invariants.

### 3. Coordinate AMD64 operand folding with allocation

Cranelift's x64 ISLE lowering contains explicit sinkable-load alternatives for
integer add, and, or, and xor. regalloc2 models a destructive machine operation
as separate SSA inputs/output with a constraint that the output reuses one
input; copying is required when that input remains live.
Sources: [x64 lowering at `3be93457`](https://github.com/bytecodealliance/wasmtime/blob/3be934572ba647c8c925085480ae5670b6faa81f/cranelift/codegen/src/isa/x64/lower.isle),
[operand constraints](https://github.com/bytecodealliance/regalloc2/blob/2fe490bc9dda433f70c54f90f7633ed929f693d9/doc/GENERAL.md).

**Application:** inspect missed memory-source and destructive-destination
choices in Dragline's generated hot loops. Coalesce a dying input with the
result, commute operations when legal, and fold one load without creating an
extra scratch reload. Existing `OperandTied` and form machinery should carry
the constraint through selection, allocation, and verification. Never sink a
load across an aliasing store, grow/call invalidation, or an earlier observable
trap. Do not infer low spill cost from allocator counters alone: finalizer
scratch moves and call shuffles also matter.

### 4. Price loop transformations by machine costs and register pressure

LLVM's loop-strength-reduction pass chooses formulas containing bases, scaled
registers, and offsets. Its LICM implementation uses alias information and
MemorySSA; hoisting also needs execution/speculation safety. These provide
models for reasoning about addresses and effects, rather than a license to
hoist every syntactically invariant operation.
Sources: [LLVM 21.1.0 strength reduction](https://github.com/llvm/llvm-project/blob/llvmorg-21.1.0/llvm/lib/Transforms/Scalar/LoopStrengthReduce.cpp),
[LLVM 21.1.0 LICM](https://github.com/llvm/llvm-project/blob/llvmorg-21.1.0/llvm/lib/Transforms/Scalar/LICM.cpp).

**Application:** compare `base + i * stride` recomputation against pointer
recurrences and native addressing modes, counting added loop-carried registers
and boundary copies. Extend current pure-op LICM only where dynamic saved work
exceeds extra live-range cost. Guard zero-trip loops, Wasm i32 wrapping,
address zero-extension, effective-address overflow, memory growth, aliasing,
and exact trap order/source locations. Reassociation of floating-point
recurrences requires a semantic contract; ordinary Wasm arithmetic does not
grant unrestricted fast-math behavior.

### 5. Use bounded call-graph inlining to expose optimization

Wasmtime's inliner processes callees toward callers using call-graph ordering,
with cycles requiring separate handling. Its write-up emphasizes that inlining
also exposes information for subsequent optimization.
Source: [Wasmtime/Cranelift inliner](https://bytecodealliance.org/articles/inliner).

**Application:** distinguish call-transition overhead from lost constant and
alias information. Profile tiny hot direct callees, then prototype an SCC-aware
inliner with total growth and recursive-depth budgets. Rerun simplification,
dead-code removal, selection, and allocation after expansion. Evaluate code
size and instruction-cache effects as well as call counts. Preserve host-call
effect contracts, root/safepoint metadata, source mappings, and interrupt and
stack-growth behavior.

### 6. Delay a wholesale e-graph rewrite until the measured gap supports it

Cranelift's acyclic e-graph work combines pure-expression rewrites with a
side-effect skeleton and scoped elaboration; the design can subsume GVN and
LICM. This is a substantial architecture, not a prerequisite for obtaining
those individual optimizations.
Sources: [Cranelift 2022 report](https://bytecodealliance.org/articles/cranelift-progress-2022),
[design RFC](https://github.com/bytecodealliance/rfcs/blob/main/accepted/cranelift-egraph.md).

**Application:** first count profitable missed equivalent expressions after
existing simplification. If selection quality remains limited by early choices,
retain bounded alternatives until target-cost extraction. Include register
pressure, duplicate work, and code size in the objective. Effectful/trapping
operations require explicit ordering and legality proofs.

V8's Liftoff demonstrates that a baseline compiler can defer selected decisions
while emitting directly, whereas its optimizing tier has different goals.
That makes baseline code useful for simple sequence comparisons, without
setting Dragline's optimization ceiling.
Source: [V8 Liftoff](https://v8.dev/blog/liftoff).

## Lessons recovered from Wago knowledge

The inspected knowledge revision is `61bf11c1a00d083c87231c49270d066366d5993f`.
Its [execution research](https://github.com/wago-org/knowledge/blob/61bf11c1a00d083c87231c49270d066366d5993f/docs/singlepass-execution-performance-research-2026-08.md)
reports cases with zero ordinary spills but substantial call/region traffic,
and successful bounded pressure-aware tree ordering. Its
[Railshot plan](https://github.com/wago-org/knowledge/blob/61bf11c1a00d083c87231c49270d066366d5993f/docs/railshot-memory-compile-latency-code-quality-plan.md)
also records removal of experiments whose aggregate wins failed measurement.
These are historical observations. Reuse their diagnostic distinctions and
acceptance discipline; do not transfer old percentages to current Dragline.

Compare current Railshot's final machine sequences for the same hot functions:
operand evaluation order, direct address reuse, compare/branch fusion, private
call conventions, and call-local reloads. Every retained improvement should
depend on typed IR, liveness, effects, or machine constraints. Corpus/module
names, hashes, function indices, and complete algorithm recognition must never
select custom native implementations. The current
[Dragline research policy](dragline-cranelift-performance-research.md) states
this boundary explicitly.

## Measurement protocol and decision order

1. Run exact semantic gates on the selected target before recording a speedup.
   Include outputs, memory effects where available, and trap behavior. Unsupported
   or failed workloads stay visible and cannot silently leave the denominator.
2. Freeze a small screen spanning integer/hash, SIMD, compression, control/parse,
   floating point, and memory/call-heavy programs. Pick actual entries from the
   current catalog. Include troublesome correctness cases even if their timing
   is excluded until repaired. Keep this screen stable across iterations.
3. Warm each engine, alternate execution order in paired rounds, and use the same
   inputs, target features, bounds mode, runtime options, and native machine.
   Capture exact source/artifact hashes and full commands. Separate compilation
   and instantiation from steady execution.
4. After each coherent change, rerun correctness plus the small screen. Inspect
   assembly and hardware profiles for wins and regressions; fewer source lines,
   IR nodes, or allocated vregs alone are insufficient evidence.
5. At milestones, run the complete eligible corpus. Compute per-export paired
   ratios, fold exports equally within each module, then give modules equal
   geometric weight. Publish the distribution and worst regressions alongside
   the aggregate. Repeat long enough to distinguish the 0.70 target from noise.
6. Track compilation latency, native bytes, allocations, and peak memory as
   secondary costs. The user permits major compiler changes, but those costs
   remain relevant evidence when choosing among changes with equal execution.

Start with correctness and move accounting, then optimize the measured dominant
category: hot reloads/edge moves, machine operand quality, loop address work,
or calls. These source findings do not establish that any one category has
enough current cost to deliver the requested 30%; the native paired profile
must settle that question.

## Follow-up: score realized work after allocation

LLVM's generic machine scheduler treats register pressure, instruction latency,
physical-register dependencies, and execution resources as separate inputs to
candidate selection. It also distinguishes acyclic latency limits from critical
resource consumption. Source inspected October 3:
[MachineScheduler.cpp](https://llvm.org/doxygen/MachineScheduler_8cpp_source.html).

The Dragline experiment uses this as a design cue, not an LLVM-equivalent CPU
model: score surviving selected operations, operand reloads, spilled results,
regional transitions, and physical edge/fixed moves using block weights. Credit
verified adjacent heap-load folding and omit its dead intermediate. Keep the
existing debt comparison as a tie breaker. This estimate does not model port
contention or every finalizer peephole and must earn its place through native
paired measurements.

A six-round intervention on Heat-3D (`heat-fold-probe-01`) confirms a concrete
miss: source scheduling takes 0.8391 of automatic latency scheduling's time;
disabling heap-load folding in that same source schedule returns to 1.0013.
The resource estimate selects the fold-preserving source schedule without
recognizing a corpus name.

Broader schedule selection also exposed two finalizer scratch-contract errors:

- A pending vector result in XMM13 was overwritten when another spilled input
  reloaded into XMM13 before the consumer. Forwarding must check actual operand
  scratch assignments, including cold rematerialization.
- An edge-copy cycle saved a value in R10. A later copy to a spill slot was
  silently changed into constant rematerialization and reused R10. Rematerialized
  spill destinations must use the separate transfer temporary, R11/XMM14.

Both were reproduced with focused tests before repair and with corpus runtime
regressions. They reinforce the ordered-edit/checker recommendation above:
allocation verification cannot prove correctness of unmodeled emitter scratch
writes. The full timing experiment is recorded separately in the evidence
README; this research note makes no new aggregate speed claim.

## Next larger opportunity: independent loop work

LLVM's vectorizer uses runtime pointer-disjointness checks where static alias
proof is insufficient, keeps scalar remainder paths, and prices partial
unrolling against register pressure and code growth. Its documentation also
explains why ordinary floating-point reductions cannot simply be reassociated.
Source: [LLVM vectorizers](https://llvm.org/docs/Vectorizers.html), inspected
October 3.

A potential Dragline direction is to pack independent output recurrences across
an outer iteration while preserving the arithmetic order within each output.
That differs from splitting one floating-point sum into reordered partial
sums. Before implementation, count eligible loop nests in RailSSA and establish
which memory and trip-count facts can be proved. Runtime versioning would need
scalar fallback for possible aliasing, pointer wrap, and bounds/trap-order
cases. Existing selected vector instructions are not evidence of an automatic
loop-vectorization pass; the current source search found vector idiom lowering
and byte-cloned loop unrolling, but no general loop-vectorization implementation.
This remains an investigation direction, not an implemented optimization or a
performance projection.


## Leaf inlining experiment (2026-10-03)

Railshot's AMD64 `inline.go` already reserves callee locals and reinitializes them
per splice; parameter rebinding and a synthetic function-label block are useful
mechanics to reuse conceptually in Dragline before SSA. The initial Dragline
candidate admits scalar memory helpers so downstream load folding, constant
propagation, and register allocation can see across the former call boundary.

Wasmtime separates the inlining mechanism from embedder policy, tracks growing
caller size, and avoids unresolved mutually recursive callees. Its heuristic
comments identify call-site constants and single-use callees as future signals.
This supports explicit growth limits and a later call-site benefit policy; it
does not establish that Dragline's current numeric thresholds are optimal.
Sources: [Wasmtime compile/inlining](https://docs.wasmtime.dev/api/src/wasmtime/compile.rs.html),
[Cranelift inline infrastructure](https://docs.wasmtime.dev/api/cranelift_codegen/inline/index.html).

LLVM's inline-cost implementation distinguishes hot/cold call sites, models a
call penalty, and offers stack-size limits and a separate cost/benefit path.
Dragline's first experiment uses a deterministic leaf-size/site/growth budget;
after correctness and paired timing, loop weight and estimated simplification
benefit are candidates for selecting sites within that budget.
Source: [LLVM InlineCost.cpp](https://llvm.org/docs/doxygen/InlineCost_8cpp_source.html).

## Guarded vectorization: executable opportunity experiment

A CFG/SSA census now covers all 65 measured execution modules (823 functions,
935 self-loop blocks). The adjacent-store matcher finds 26 FP expression pairs
in eight modules; it does not prove alias freedom or dynamic hotness. A separate
screen finds 50 loops with FP load/store arithmetic, no calls, and no FP phi
recurrence; memory-carried recurrences remain possible.

LLVM's loop vectorizer versions loops with runtime pointer checks and keeps a
scalar path for overlap. Its SLP vectorizer instead discovers similar scalar
expression trees that can be packed. These suggest combining tree matching with
loop-level guards for Wasm, where linear-memory pointers are otherwise allowed
to alias. Source: [LLVM vectorizers](https://llvm.org/docs/Vectorizers.html),
rechecked 2026-10-03.

The concrete matmul experiment packs two independent outputs while retaining
per-output FP operation order. It checks complete touched ranges in i64 and
falls back to the original loop for aliasing, memory bounds, or address wrap.
Finite memory/trap and NaN semantic screens pass, but native timing **regresses**
from 88.63 us to 148.19 us. Omitting guards diagnostically still costs 131.16 us.
This is useful negative evidence: completing a vectorizer on this lowering would
not establish the desired speedup.

Assembly identifies additional integer address/edge work and missed branch
fusion. A scheduler overlay preferring compare/branch fusion over an induction
adjacency reservation lowers the guarded prototype to 138.19 us and the unsafe
no-guard control to 102.33 us. The full
native correctness gates pass, but the matched 65-module / 72-export comparison
is 0.999621 time ratio (0.0379% lower time), so the default scheduler is unchanged. Prototype
artifacts and exact measurement limits are in
[the performance log](performance/dragline-20261003/README.md).

The NaN screen initially compared all bits, exposing a permissible difference
between the existing scalar compilers. The corrected oracle checks canonical
versus arithmetic NaN results according to the coefficient class, bounds
normalization to destination elements, and preserves exact comparisons elsewhere.
Source: [Wasm numeric semantics](https://webassembly.github.io/spec/core/exec/numerics.html#nan-propagation).

## Machine-level unused values (follow-up)

LLVM's [DCE implementation](https://www.llvm.org/doxygen/DCE_8cpp_source.html)
removes instructions only after its trivially-dead predicate succeeds, detaches
operand uses, and revisits producers whose last use disappeared. It explicitly
salvages debug information and knowledge before deletion. The transferable idea
is to separate the absence of uses from the legality of removal and to propagate
liveness changes upstream.

The Dragline loop diagnostic contains unused comparisons and address arithmetic.
`BuildWithSimplify` applies safe aliases but does not directly consume semantic
`LiveInsts`. Applying that mask alone would be unsafe while machine CFG edges
and conservative transfers remain intact. The current diagnostic instead counts
actual machine operands, edge transfers, and results, then walks definitions in
reverse order. It removes only unused scalar/SIMD numeric definitions with no
metadata effects, reads, writes, traps, or obligations. Instruction/source IDs
remain stable. This is a probe, not a promoted production pass. Loop parameter
cycles and dead transfers remain conservative.

A second overlay traces actual machine liveness from effectful instructions,
control instructions, and function results through instruction operands and
block-parameter inputs. It then removes unmarked numeric definitions and edge
transfers into dead parameters. Reference values remain roots pending a separate
safepoint-liveness integration. This handles unused recurrence cycles that a
zero-use pass cannot remove. Local tests distinguish live/dead recurrence cycles
and retain trapping division, memory loads, calls, and function results.


## Delayed scalar memory operands

LLVM's [machine peephole optimizer](https://llvm.org/doxygen/PeepholeOptimizer_8cpp_source.html)
tracks foldable loads with one virtual-register user, asks the target to fold at
the consumer, and clears candidate loads at barriers. Its source explicitly
checks the single-user property when adding candidates to bound compile cost.
This suggests separating target operand eligibility from motion legality.

Dragline v74 applies that separation after allocation. The eight-instruction
window and strict address-location/live-segment proof permit a bounded change
without adding allocator uses. This remains more restrictive than doing the
fold before allocation: a dying address cannot be kept live by this pass, and
allocation still sees the load's original FP result. A future earlier machine
combiner could expose the address at the consumer and eliminate the temporary
FP interval, but it must represent effects and memory operands explicitly so
scheduling, allocation, ABI analysis, and emission share the same program.

The immediate motivation is local Railshot code, not a speculative throughput
model: archived native matmul disassembly folds the source multiply and
destination add, whereas v73 Dragline retains the destination load. Railshot's
`amd64/memory.go` defers eligible scalar FP loads and its commutative FP path
accepts a memory input from either operand. The new pass reproduces that
opportunity while proving the delayed address location remains valid.

The [WebAssembly threads memory model](https://webassembly.github.io/threads/core/exec/relaxed.html)
provides the concurrency context. The current implementation deliberately
limits its motion proof to unshared memory. Within that scope, crossing only
private reads and pure arithmetic preserves returned values and visible memory
writes; competing invalid reads retain the same trap class. This is a local
legality argument, not a claim of identical diagnostic source offsets or a
license to move loads across other traps. A division-barrier differential probe
found why that distinction matters: the Railshot signals-mode reference can
report division-by-zero before an earlier invalid read, so the oracle uses
Railshot explicit bounds checks.

## Follow-up: vector range checks belong outside repeated inner entries

The archived `vector-fold-probe-01`, `vector-schedule-probe-01`, and
`vector-once-probe-01` isolate three interacting costs: vector memory operands,
schedule choice, and placement of runtime range/alias checks. Full-width packed
float memory folds pass 156,816 native semantic cases. Combined with the earlier
graph DCE prototype, they reduce the manually vectorized matmul fixture from
about 123 us to 108 us; latency scheduling reduces it to about 81 us. A single
dominating frame-range guard reduces it further to about 75 us, versus about
82 us for the original scalar fixture. These are diagnostic transformed inputs,
not automatic-vectorizer or fixed-corpus results. See the performance README
and archived raw samples for the matched configurations and limitations.

LLVM's LoopVersioningLICM constructs a runtime memory check followed by the
original loop or a version with no-alias assumptions. Its feasibility checks
include a computable trip count and a bound on runtime-check count.
Source: [LLVM LoopVersioningLICM implementation](https://llvm.org/doxygen/LoopVersioningLICM_8cpp_source.html).

**Application inferred from the source and native experiment:** a general
Dragline transform needs an affine range certificate at a dominating loop
entry. It must prove nonwrapping memory32 address ranges, complete vector
access bounds, permitted inter-iteration aliasing, and an unchanged scalar
fallback for failed guards. In this fixture, a clamped 1..96 dimension and three
73728-byte frame regions provide that proof; successful dominating memory fills
also establish bounds evidence. The existing per-access bounds facts do not
by themselves encode this whole-loop certificate. Avoid embedding fixture
names, dimensions, or offsets into compiler matching. The current roughly 8%
matmul opportunity is too small to justify claiming the overall goal achieved.


## Updated baseline and bounded-loop candidate inventory

`pr780-compare-01` measures the exact PR #780 compiler/runtime source at
`efa9aa22dfb3781284a55c465f7f57544eafade3`, with the same Go version, CPU affinity,
fixed corpus, invocation helpers, and paired protocol as Dragline v74. Dragline's
module-balanced time ratio versus the PR is 1.0690508320; the 50% faster goal
remains unmet. The full report records three noisy non-Dragline series and all
raw samples. This materially changes the priority: numerical-loop lowering is
a larger deficit than the small dispatch and allocation-policy probes addressed.

A standalone adaptation of the PR's bounded region-loop analysis scans current
Dragline inputs and finds 33 structural loop candidates in 21 modules, including
18 independent-lane or adjacent-output candidates. Exact function offsets,
affine recurrence metadata, original analysis source pins, and the scanner are
in `performance/dragline-20261003/loop-opportunities-01`. This analysis does not
establish complete vectorization legality. Next implementation must preserve
source FP order, all scalar exit locals, memory32 wrap and widened immediates,
full access-range and alias guards, checked tails, and original scalar fallback.
It must use Dragline's compilation path; routing these functions to Railshot
would not implement the requested compiler improvement.


## Automatic loop transform: correctness achieved, performance regressed

An overlay now implements the adjacent-output subset in Dragline itself, with
runtime range/alias/trip guards and an unchanged scalar fallback. Exact exit
locals, memory, traps, finite FP values, allowed NaN behavior, source offsets,
and corpus semantics have dedicated checks. The initial source-offset ordering
failure and correction are archived. Target-qualified AVX2 admission and static
stream-guard deduplication are in a revised overlay.

The first all-65-module comparison regresses 1.7043% alone and 0.9232% with
vector folding/DCE. No production promotion occurred. Matmul and GEMM lose
substantially despite packed arithmetic; this provides a concrete diagnosis
loop for guard, instruction schedule, and register-allocation overhead. The
next screen compares four schedule policies after identical correctness gates.
See the performance README for raw evidence, noisy series, and exact ratios.


## Loop code size and branch layout interact with vectorization

The simplified automatic loop dropped below Dragline's 64-byte native unroll
minimum. Its generated backedge then used an out-of-line register-move block.
Lowering only that profitability minimum to 32 bytes restores unrolling while
retaining the existing safety checks. In the focused screen this removes the
matmul regression; a separate inline-backedge experiment also improves the
vector loop, but less than unrolling. Neither observation justifies changing
all loop policy without broad measurement.

The completed four-round 300 ms comparison over all 65 modules / 72 exports
shows the combined adjacent transform, vector folding/DCE, simpler guards,
invariant splats, and 32-byte unroll policy at **0.9949824694 times v74** and
**1.0662710373 times PR #780**. No series exceeds 20% spread. The confirmed gain
is only half a percent over v74, and the requested 50% throughput gain over the
PR is not achieved. The overlay remains unpromoted. Independent scalar-iteration
pairing is the next bounded extension; its native validation is pending.


## Independent iterations and the remaining scalar gap

Independent pairing passes dedicated exact exit-state and NaN tests, improves
Jacobi-2D in the focused screen, and initially regresses Heat-3D. A static
short-trip profitability restriction recovers Heat-3D. Its four-round 300 ms
full-corpus result is 0.9936780705 versus v74 and 1.0619044798 versus PR #780;
three PR series exceed 20% spread, while neither Dragline series does. The goal
remains unmet and the implementation is still an overlay.

Restricting short-loop unrolling to packed FP does not remove the scalar memory
and fastfloat regressions. Native dumps reveal identical memory function bytes,
and the restricted policy restores identical fastfloat function bytes too.
Diagnostic-only rebuild controls shift timings without removing the memory gap.
Further diagnosis must include runtime admission/metadata and placement; emitted
instruction counts alone cannot explain these measurements. PR #780 has newer
runtime gate machinery that this branch lacks, so its fast-call patches need a
lifecycle-preserving adaptation rather than a wholesale file copy.


## Runtime lifetime admission is measurable but insufficient

A narrow adaptation of PR #780's uncontended private lease reduces tiny and
many-function times by about 10% relative to v74 in a six-round native screen.
Correctness gates retain busy/borrowed/closed/managed fallback and close-time
finalization. PR #780 remains substantially faster on these calls. A separate
release-helper inlining revision and cached-gate experiments are underway.

The loop overlay's scalar memory regression disappears when combined with the
lease adaptation, while emitted compiler code is unchanged. Entry flags and
modulo-64 alignment diagnostics had been identical across compiler variants.
This is evidence to investigate runtime and placement effects, not proof of a
particular cause. Full-corpus validation is required before promotion.


## Width is a correctness contract before it is an optimization

The cached-gate adaptation plus existing loop overlay measures 0.9756003510
against v74 and 1.0477979910 against PR #780 across 65 modules, with the noisy
series listed in the evidence README. This is progress, but the target remains
unmet. All adaptations still preserve deferred gate-before-lifetime release.

The wider-vector investigation uncovered a concrete v128 cycle-save defect in
both native finalizers. A scalar save of an FPR temporary loses the other vector
lanes. Encoding tests and a Wasm register-swap/full-memory oracle reproduce it;
native AMD64 packages and semantic gates pass with full-width saves. The overlay
bumps the function artifact revision. ARM64 broad validation is pending.

Extending `MachineType` alone would be insufficient for a future v256 design:
`takeFreeSpillUnits` and `reserveSpillUnits` currently specialize one/two-unit
homes, and many finalizer paths distinguish only scalar versus v128. A native
loop-region alternative would need explicit local and live-through bindings:
the binding diagnostic finds nonempty operand-stack prefixes for most candidate
loops. Neither interface has been implemented. The next design must keep width,
allocation, memory footprints, exit values, and target qualification explicit.


The v128 temporary fix is now production v75 locally. Focused ARM64 and AMD64
checks pass; native AMD64 full packages and semantic coverage pass. Broader
ARM64 failures reproduce on v74 and need separate repair. Disabling native
backedge fusion does not resolve them. A fresh v75 performance run stopped at
lifecycle gates before timing, while isolated baseline/candidate controls pass.
Repeated whole-package gates and a fresh full comparison are underway; no v75
speed claim is made yet. See the evidence README for exact qualification.


## v77 correctness and measured v75 comparison

The completed `runtime-full-03` comparison verifies 65 modules and 72 exports.
The best runtime-plus-loop overlay takes 0.9766518426 times v75 execution time
and 1.0452482256 times pinned PR #780 time. Doitgen and SYRK are noisy in that
candidate; v75 and PR are stable within the recorded 20% threshold. The 50%
throughput target remains unmet and the performance adaptations remain overlays.

Local v77 repairs two ARM64 correctness defects exposed by broad validation:
the synthetic exit now emits its epilogue where the block layout places it, and
predicated copies reject folded constant sources whose physical registers were
never populated. The ordinary edge emitter already rematerializes those values.
A stale prepared-call assertion now checks `directIsolated`. The combined change
passes the full relevant ARM64 packages three times; exact evidence and source
hashes are archived in `arm64-correctness-01`. These repairs do not establish a
new performance result, and the wider native loop interface remains future work.


## Scalar memory recurrences close two large gaps

A six-round native width control isolates the source of the slowdowns: wide
loops matter in the Jacobi kernels, GEMM, and matmul, but not materially in BiCG
or GESUMMV. A guarded scalar invariant-cell transform now reduces those latter
two kernels by 41.4% and 31.1% versus the previous Dragline candidate. They still
take 1.1047 and 1.0249 times PR #780 time, respectively; the goal remains unmet.

The prototype retains the original FP order, uses whole-loop range and strict
alias guards, and publishes exact integer/FP locals and delayed memory cells.
It does not require extending the machine vector width or allocator. Native
packages, semantic checks, and 65,280 inputs in each bounds mode pass. The full
v77-rebased 65-module comparison now takes 1.0311185780 times PR #780 time
(`scalar-recurrence-full-02`), improving 1.69% over the previous candidate.
Doitgen, both QOI exports, and tiny.add have more than 20% max/min timing spread.
All four paired rounds were restarted after a host reboot, with exact binary
hashes checked and semantic gates rerun. The wider-loop interface is still
needed to address other gaps; no new performance transform is promoted yet.


## Four-lane native region bridge

The prototype now emits guarded YMM f64 loops directly from captured typed
regions, preserving stack-prefix values, integer/FP exits, and original
fallthrough behavior. Native differential fixtures exercise both paths and
pass in both bounds modes. In a six-kernel paired screen, Jacobi 2D takes
0.6421 times v77 time, but GEMM regresses to 1.1950 times v77. A path diagnostic
is being used to distinguish successful wide execution from guard overhead.
The prototype excludes fragmented functions and has incomplete source mapping.
Full runtime validation also encounters a cancellation hang reproduced on
unchanged v77. Exact evidence and qualification are in the performance README.


## Native wide loops with scalar remainders

GEMM initially always failed the divisibility guard. Executing complete vector
groups before the original scalar remainder exposed a live-output bug: a dead
address temporary reused the loop counter's physical register. Publishing only
SSA values live at the loop edge repairs the original failing corpus case.
Instruction-local allocation fragments are also supported through their
authoritative spill homes, enabling matmul and Jacobi 1D.

The verified eight-kernel screen improves GEMM by 18.5%, Jacobi 1D by 31.0%,
Jacobi 2D by 35.7%, and FDTD 2D by 23.6% versus v77. Heat 3D regresses 14.3%.
All six-round spreads stay within 20%. Most of these kernels remain slower than
PR #780, and no new complete-corpus result or target completion is claimed.
The prototype and its handoff regression remain archived overlays.


## Combined candidate, complete corpus

The combined runtime/recurrence/vector candidate completes all 65 modules and
72 exports in four paired native AMD64 rounds. It takes 1.0221888251 times pinned
PR #780 execution time, with no max/min spreads over 20% in either variant. The
raw samples, aggregation, archives, and effective source hash are independently
verified. The 50% goal remains unmet, and nothing from the experimental
performance overlays is promoted. A separate single-use packed memory-operand
prototype is in native validation.


## Native wide register preservation

Packed memory operands and invariant FP hoisting pass the scoped native gates,
including five differential fixtures and full-memory equality for ten modules
in both bounds modes. Preserving only the FPRs used by the wide body, inputs,
and rematerialization scratch removes a larger cost: the paired wide-08 screen
cuts matmul time by 10.5% and Jacobi times by 6.5–7.8% versus wide-07 hoisting.
All screen spreads are within 20%. Matmul remains 26.8% slower than PR #780;
most screened kernels are still slower. Full-corpus status above is unchanged.
See `native-wide-screen-04` for raw samples and independent verification.


## Register save reductions, combined full corpus

Wide-09 retains only GPR scratch/stream clobbers and entry register inputs in its
snapshot. Native differential, semantic, scoped runtime, and full-memory gates
pass. Its focused paired screen lowers matmul time another 6.1% and GEMM 3.6%
versus wide-08, with no spreads over 20%.

The combined candidate completes four fresh paired rounds across all 65 modules
and 72 exports: time / PR #780 = **1.0061183382716679**, throughput / PR =
**0.9939188681500647**. Candidate Doitgen and PR `tiny.add` are noisy (max/min
1.376 and 1.410), so this is not a stable parity result. The 50% target remains
unmet. Raw samples and independently reconstructed source identity are verified
in `native-wide-combined-full-02`. Production sources remain unchanged.


A further spill-input snapshot probe (wide-10) removes 224 generated bytes from
SYMM and leaves seven other admitted corpus functions byte-identical. Its native
gates pass, but paired timing finds no speedup (SYMM / wide-09 = 1.0004057291).
It remains separate from the combined candidate. The largest full-02 deficits
are now kissfft (1.279× PR time), yyjson (1.219×), memory (1.197×), PCRE2
(1.178×), and LZ4 (1.165×); further work needs broader hot-path improvements.


## Toolchain audit correction

PR profiles exposed an automatic Go toolchain switch: the PR benchmark module
requests Go 1.27.1, while Dragline binaries used Go 1.22.2. Embedded binary
metadata confirms the mismatch. Earlier PR ratios, including combined full-02,
are historical mixed-toolchain observations, not matched acceptance evidence.
`pr780-baseline-02` rebuilds the same source and harness with Go 1.22.2 and passes
the semantic gate. Future comparisons pin GOTOOLCHAIN and inspect every binary's
embedded Go version. Corrected full-corpus measurement is pending.

Kissfft native samples concentrate 94.8% in function 1. Its inner address loop
has repeated spill traffic; an isolated GPR-density policy for large recursive
AMD64 functions retains conservative scalar-FP allocation and unchanged regional
admission. Compiler, focused runtime, semantic, recurrence, and full-memory gates
pass; matched-toolchain focused timing is underway.


## Corrected full corpus after recursive GPR allocation change

The matched Go 1.22.2 focused screen cuts kissfft time by 25.0% versus combined-02
and 3.8% versus PR #780. Other screened candidate/control ratios are essentially
unchanged; every spread is within 20%.

`matched-full-01` now checks embedded toolchains and all execution results for
both binaries. Across all 65 modules / 72 exports, four paired 300 ms rounds give
candidate / PR time **1.0025388651576999**. Candidate Doitgen is noisy (1.387
max/min); the PR has no spread over 20%. Raw samples and independently
reconstructed source identity verify. The target remains unmet. A separate
SIMD i64-sum prototype is entering native differential validation; it is not
part of this full-corpus candidate.


## Integer reduction dependency chains

A bounded, guarded SIMD prefix now handles exact i64-add countdown reductions.
Prototype 01 exposed unordered source-map offsets; prototype 02 places a single
original scalar block after the prefix, serving both fallback and remainder.
Native gates pass, including 5,280 independently checked integer cases in each
of two bounds modes. Corpus admission is limited to memory.sum by the actual
loop shape, without module-name selection.

The matched Go 1.22.2 screen measures memory.sum at **0.6374045982 × previous
candidate time** and **0.7040964778 × PR #780 time**. All spreads remain within
20%. ARM64 compiler/runtime checks pass. Completed `matched-full-02` covers
all 65 modules / 72 exports with both result gates passing. Its module-balanced
time / PR is **0.9931789314157942**, or **1.0068679151041617 × PR throughput**.
Neither variant has any spread above 20%. Raw samples and independently
reconstructed source identity verify. The full 50% target remains unmet, and
the native runtime cancellation hang remains unresolved.


## Cancellation qualification: validated experimental fix

The baseline v77 managed-table cancellation hang reproduces again. Keeping the
authenticated signal request valid through a 50-microsecond scheduler sleep,
after unacknowledged Gosched polling, passes 100 repeats and full native runtime
suites. Composed with integer-sum-02, it passes 1,000 reentry repeats, ten rounds
of cancellation tests, full native compiler/runtime suites, and eight-CPU
cancellation stress. This supports a request-lifetime/delivery-timing hypothesis;
the exact failing order is not directly instrumented. See cancel-delivery-01/02.
The change remains an overlay and is not included in matched-full-02 timing.


## Giant linear allocation with regional reuse

Existing giant functions skip regional fragments. The new isolated probe keeps
their linear allocator, call-live promotion, source schedule, and unsafe cyclic
call exclusion, then rebuilds exact physical occupants and runs the verified
regional planner. Native compiler/full public runtime, semantic and differential
fixtures, full-memory gates, and ARM64 compiler/runtime checks pass.

Static metrics find new fragments in PCRE2 function 60 and YYJSON function 18.
Two matched-toolchain screens give YYJSON paired median time / integer-sum-02
of 0.9725682503 and 0.9738404772. PCRE2 medians are 0.9945161236 and 0.9911403211.
These are focused measurements; individual paired ratios still vary.

A second variant excludes fragments limited to one instruction position. It
loses the YYJSON gain and has a noisy PCRE2 series (1.342 max/min), so that filter
is rejected. This also shows that reuse across separate machine instructions
alone is an inadequate profitability rule for the existing emitter. Revision
01 proceeds to full-corpus evaluation. Production v77 remains unchanged.


## Regional candidate full corpus and multi-destination SIMD

Matched-full-03 verifies both result gates for 65 modules / 72 exports and four
paired timing rounds. Time / PR is 0.9912150619239573 (1.0088627972006308 ×
throughput). Candidate Doitgen has max/min spread 1.3803029267; the result is
qualified and the 50% target remains unmet. The measured candidate includes the
validated cancellation-delivery overlay.

The next prototype extends native independent-lane SIMD to up to four destination
streams. Every destination is checked against every stream for disjoint ranges
or exactly equal stride/width/start; scalar fallback preserves aliasing and trap
order. Scratch layout now reserves twelve stream entries. Native compiler/full
public runtime, semantic, prior fixture and twelve-module full-memory checks
pass. Two new fixtures each pass 1,728 cases per bounds mode against an independent
scalar oracle, also cross-checked with Railshot. ARM64 compiler/runtime checks
pass. Instrumentation proves successful and fallback execution for both fixtures
and 2,500 two-destination entries for correlation. ADI does not enter the path.
Six paired matched-toolchain rounds give correlation time / fast-regional01
0.9848710944, with no series above 20% spread. This is a focused result only.


## SIMD entry snapshots

Matched native profiles show both engines already use AVX2 in matrix inner loops.
Dragline additionally saves input registers, reloads them, then stores separate
input snapshots. Direct snapshots remove the middle step; taking all register
snapshots before spill/rematerialization reads preserves scratch dependencies.
Revision 02 additionally omits backups for input registers outside the bridge's
clobber set. Native and ARM64 correctness gates pass for both revisions.

The first focused screen measures matmul time / multi-store01 = 0.9888914909.
The second gives revision02 / revision01 = 0.9827490215 for matmul and
0.9826588815 for GEMM, alongside roughly 2% regressions for ATAX and Heat.
All binaries use Go 1.22.2; no timing series exceeds 20% max/min spread.
These results need follow-up and do not update the full-corpus aggregate.
Revision03 separately removes backups of reserved GPR emission scratch, which
cannot carry allocator values at the loop entry edge. It passes native and ARM64 validation. The focused screen measures matmul
time / multi-store01 = 0.9481751465 and GEMM = 0.9698700857, with correlation
slightly slower at 1.0092339146 and no series above 20% spread. Full-corpus
evaluation completed as matched-full-04: time / PR 0.9876689610, or 1.25% higher
throughput, with PR-side noise in Doitgen and Zstd decompression. Both engines
pass all 65-module / 72-export result gates. The target remains unmet. No prototype has been promoted
to production v77.


## ARM64 validation exposed an interrupt-header fault

Snapshot04's AMD64 floating-backup pruning passes native gates and has small,
mixed focused timing changes; a longer replication repeats GEMM and Heat improvements. Local ARM64
runtime testing crashes in requestDarwinInterrupt at header address
0xffffffffffffff99 (X26=15). The prior snapshot03 reproduces it. A generated
PC therefore does not establish that X26 may be dereferenced as a memory base.

An isolated registry fix checks JobMemory identity and excludes concurrent
unmapping during the interrupt scan. The classic reproducer passes 100 repeats
and full ARM64 suites pass. Review found a guarded-allocation hook gap; a
regression fails before registering Darwin guarded mappings, and full guarded
suites pass with that addition. Extended validation passes 1,000 classic and 100 guarded repetitions, totaling
110,000 concurrent cases. Full classic/guarded suites pass. The first attempt
exceeded its three-minute duration limit without reproducing the fault. This fix is independent of the measured AMD64 compiler candidates.


## LZ4 bounded-division gap

Native-profile-05 maps substantial LZ4 samples to a full-range unsigned remainder
sequence after an eight-bit mask. The matched PR baseline uses bounded reciprocal
lowering. Bounded-div01 ports the proof to a verified SSA AND producer and uses
reserved scratch registers without changing allocation or trapping behavior.
Local arithmetic/encoding and native compiler/runtime/corpus gates pass.
The six-round paired screen gives LZ4 compression / snapshot04 = 0.8760597200
and decompression = 1.0282856840, while both remain slower than PR780. Candidate
Doitgen is noisy. Unsupported cases retain ordinary constant division. Matched-full-05 completes at time / PR 0.9847840986, or 1.55% higher
throughput. PR UTF SIMD is noisy; the candidate has no series above 20% spread.
Both engines pass all result gates, and the full target remains unmet.


## Generic arithmetic immediate selection

The immediate combination pass runs before target selection, but its arithmetic
classifier matched selected AMD64 opcodes only. Generic-immediate01 uses semantic
opcodes with the same encodability limits and use-count checks. A regression
fails before the change and passes afterward. Native compiler/full public
runtime, semantic, fixture and full-memory gates pass. ARM64 suites pass with
the separate Darwin registry02 fix. Production v77 remains unchanged.

Six focused paired rounds give zlib time / bounded-div01 = 0.9665199703 and
zstd = 0.9733488093. Doitgen is noisy. Matched-full-06 completes at time / PR 0.9738918715, or 2.68% higher throughput.
PR QOI encode/decode samples are noisy. All 65-module / 72-export gates pass.
A separate plan census finds repeated FNV constants are distinct single-use
values, ruling out regional spill reuse alone as a solution to those loads.


## Simplifier budget exhaustion

The same census shows YYJSON and PCRE2 both use all 4096 rewrites, leaving no
budget for common-expression elimination. Scaled-fuel01 raises the AMD64 budget
to four times the instruction count, bounded between 4096 and 65536. The two
functions use 15359 and 24574 rewrites and merge repeated FNV constants. Local
compiler, guarded ARM64, and native compiler/runtime/corpus validation pass;
matched compile/execution measurement is pending. This is an experimental overlay.


Scaled-fuel01 passes all native correctness gates. Its six-round focused screen
measures time / generic-immediate01 of 0.9291656511 for PCRE2, 0.9732491791 for
YYJSON and 0.9579137723 for zstd. LZ4 decompression is 1.0105734131. Doitgen is
noisy in both Dragline variants. Compile cost is still under measurement; this
screen does not update the full-corpus aggregate.


The four-round scaled-fuel compile-cost comparison completes without a timing
series above 20% spread. PCRE2 time / previous is 0.9621081672 and zstd is
0.9696768569; other selected compile times are nearly unchanged. Most whole-process
RSS medians rise 2.7–6.1%, while PCRE2 falls 1.5%. RSS includes untimed setup and
default-engine code sizing. Timed allocation bytes stay within 0.36% of previous.
The full execution comparison is running as matched-full-07, followed by native
profiles of recursion, many-function dispatch, UTF SIMD, and BLAKE3.


Matched-full-07 completes at time / PR 0.9731969039, or 2.75% higher throughput,
with PR QOI/Doitgen noise and complete 65-module / 72-export result gates.
The next isolated native-wide51201 prototype doubles independent f64 lanes
from four to eight on AVX512F targets. It preserves per-lane arithmetic order,
all range/alias guards, scalar tails and the AVX2 fallback. The completed trip
counter moves beyond the 64-byte final-lane scratch. Local compiler/encoding
and guarded ARM64 suites pass; native execution validation is pending.


Native-wide51201 passes native compiler/full public runtime, all prior oracles,
twelve-module full-memory checks, and six extra fixtures covering every tail
length (2160 cases per fixture in both bounds modes, with AVX512 required).
The eight-module matched screen has no spread above 20%, but results are mixed:
matmul time / scaled-fuel01 is 0.9400383256, GEMM 1.0454219948, Heat 1.0391347477,
and Jacobi1D 1.1024923855. It remains experimental. Instrumented trip-count
collection is investigating whether extra scalar tails explain the regressions.


Trip diagnostics confirm GEMM has 35 paired iterations and Jacobi1D has 59;
both leave six scalar lanes after an eight-lane loop, versus two after AVX2.
Matmul has 32 paired iterations and no remainder. Heat has 18 ordinary
iterations and the same two-scalar tail at either width, so its regression
needs a separate explanation. All diagnostic fixtures exercise both success
and fallback paths while retaining exact oracle results.

Native-wide51202 adds an existing four-lane body after the eight-lane body when
needed, and handles initially short four-to-seven-lane regions directly.
It preserves stream starts and exact loop-carried exit locals across the width
transition. Local compiler checks pass; native and ARM64 validation are running.


Revision02's native differential gate catches incorrect memory for the short
invariant-load fixture (src=0, dst=0, n=4). Its local, ARM64 and native public
runtime suites had passed; those were insufficient to qualify the transform.
The cause is stale compile-time stream register assignments from the main body:
the short runtime path bypasses their initialization. Revision03 resets the
remainder stream descriptors so early invariant loads use stack bases. The
failing revision is archived, and the corrected revision is under validation.


Mixed-width revision03 passes all native and guarded ARM64 correctness checks,
but its matched screen regresses GEMM and Jacobi1D further, with no series above
20% spread. It is not promoted. Revision04 instead chooses eight lanes only
for complete groups, and otherwise runs the existing four-lane body for the
whole loop. This avoids copying loop state between vector widths. Local
compiler checks pass; native validation is running.


Revision04 passes all native and guarded ARM64 gates. Its matched eight-module
screen measures matmul time / AVX2 0.9327786005, GEMM 1.0201225290, Jacobi1D
1.0047476185, Heat 1.0012101277, and SYMM 1.0265868185. The mixed-remainder
regressions are mostly removed, but some smaller regressions remain. Full-corpus
measurement is active as matched-full-08, with an expanded runner/verifier that
also pins and checks the six new AVX512 tail fixtures and their oracle source.
The latest complete aggregate remains full07, 2.75% higher throughput with
PR-side noise. No prototype has been promoted to production v77.


Matched-full-08 completes at time / PR 0.9723980261, or 2.84% higher throughput.
All 65 modules / 72 exports and the expanded correctness verifier pass. Candidate
many_funcs.run and PR Doitgen have max/min spreads 1.2395 and 1.4398. The target
remains unmet. No prototype is promoted.

The next isolated experiment stores frequently read integer spill slots in unused
XMM registers in helper-free integer leaf functions. The ABI audit restricts homes
to XMM0,1,2,6,7, volatile under both scalar and vector register orders, and publishes
the complete volatile FPR clobber mask. Functions with captured wide regions are
excluded because their generated SIMD bodies also use XMM registers. Native
correctness and performance qualification are pending.


Integer-spill-fpr01 passes all standard native and guarded ARM64 correctness
checks, plus 12288 independently checked calls across I32 pressure and I64 loop
fixtures with scalar/vector callers. Cache and parallel compilation preserve the
new clobber contract. However, six paired native rounds regress BLAKE3 by about
28%, Monocypher by 65%, and zstd by 29% versus scaled-fuel01, with no series above
20% spread. The transfer-based prototype is rejected and archived. It does not
change the latest full-corpus result: full08, 2.84% higher throughput than PR #780.
A future cross-bank approach would need to keep arithmetic in XMM as well; the
current evidence does not demonstrate that doing so would be faster.


Integer-spill-fpr02 adds direct low-lane XMM add/subtract/bitwise operations,
avoiding transfers when enough operands/results occupy XMM homes. All prior
correctness gates plus 12288 independent pressure calls pass. A static census
finds only 14 eligible BLAKE3 operations and 19 Monocypher operations. Six paired
rounds recover some performance versus storage-only (BLAKE3 ~4%, Monocypher ~21%),
but remain 22%, 29%, and 24% slower than scaled-fuel01 on BLAKE3, Monocypher and
zstd. No series exceeds 20% spread. Revision02 is also rejected.

A separate fp-constant-licm01 experiment starts from native-wide51204. It admits
F32/F64 literal loads to the existing bounded FPR loop-hoisting reserve and counts
them against the same eight-register limit as vector constants. It preserves
innermost-loop scope, permits conditional literal speculation, and does not admit
FP arithmetic or trapping conversions. Local and native compiler, guarded ARM64
compiler/core/public runtime, all existing differential oracles and six AVX512
extra fixtures pass. A census finds scalar literal hoists in nine of ten selected
workloads. Focused timing and independent floating-point edge-case checks follow.


FP-constant-licm01 also passes 4608 independent FP literal calls, including
signed zero, subnormals, infinities, permitted NaNs and zero-trip exact bits.
Its nine-module six-round screen has time / native-wide51204 geomean
1.0032708546: ADI improves about 1.5%, Deriche regresses about 3.7%, N-body about
1.5%, and the rest are nearly unchanged. Candidate and previous timings have no
series above 20% spread; PR Doitgen is noisy at 1.409. The experiment is not
retained, and no new full-corpus benchmark is warranted. The latest complete
aggregate remains matched-full08, 2.84% higher throughput than PR #780, with
previously recorded noise. Production v77 and the active 50% objective remain
unchanged.


Scalar-spill-fold01 folds canonical frame spills into integer ALU/multiply memory
operands. It passes native/ARM64 gates, pressure oracles and extended AVX512
fixtures, but six paired rounds regress BLAKE3 by ~52%, Monocypher ~15%, and
libtommath ~5%; no timing series exceeds 20% spread. A static Linux-target code
dump shows BLAKE3 function4 shrinking from 6544 to 5952 bytes while replacing
160 LEAs and increasing register-copy count from 188 to 330. Fewer bytes and
fewer separate loads do not establish faster execution.

Revision03 restores the normal 64-bit copy form, passes all gates, and still
regresses BLAKE3 by ~53%; copy width alone is not the cause. Revision04 admits
folding only when the destination reuses the non-memory operand's register.
It is under native validation and timing. Revision02's FP extension remains a
local-only prototype pending resolution of the parent folding policy.


Scalar-spill-fold04 passes all native/ARM64 and extended oracle checks. Its
static BLAKE3 function4 has 41 folded stack reads, unchanged 188 register copies,
and 6432 bytes versus 6544 baseline. Six paired native rounds still regress
BLAKE3 by 11–12%, Monocypher by 4.6%, and libtommath by 1.5%; zstd improves 0.9%.
No series exceeds 20% spread. Revision04 is rejected. Broad memory-operand
folding and avoiding added copies both fail to improve these native workloads;
the microarchitectural explanation remains unproven. The latest full-corpus
result remains matched-full08, 2.84% higher throughput than PR #780 with its
recorded noisy samples. The 50% target remains unmet.

Current-source schedule-policy-probe02 compares automatic, source, latency and
pressure schedules using one Go 1.22.2 binary over all 65 modules / 72 exports,
four paired 300 ms rounds. All policies pass semantic/export checks; forced
policies pass wide-loop, multi-store and integer-sum differential oracles.
Time / automatic is source 1.0049716731, latency 1.0048444129 and pressure
1.0284696593. No series exceeds 20% spread. No forced policy is retained.
The runner's final self-copy raises SameFileError after saving every result;
the independent archive verifier confirms complete raw coverage and aggregates.

Latency improves memory.sum to 0.8398705200, LZ4 decompression to 0.9275048002,
CoreMark to 0.9429486416 and QOI encoding to 0.9438283158, but regresses SIMD
BLAKE to 1.3363387120. Pressure improves GESUMMV to 0.9207879564 and regresses
KissFFT to 1.3050188509. These are diagnostic policy comparisons, not a new PR
comparison. Even a retrospective best-policy-per-module selection gives only
0.9878725525 and cannot be treated as an implemented or validated compiler gain.

Static metrics03 records eight workloads under all four policies and archives
memory's generated bytes/disassembly. Automatic and pressure memory code are
identical, and their paired timing ratios stay near 1. The latency loop uses
CMP/JAE instead of a materialized SETAE/MOVZX/TEST branch condition and changes
load scheduling. Automatic selection chooses resource cost 199 / estimated
cycles 224 / one broken fusion over latency's 207 / 208 / zero broken fusions,
with equal zero spill debt and ten copies. This is a concrete follow-up for
isolating finalizer and scoring effects; it does not prove the cause yet.
Production v77 remains unchanged. Full08 remains the latest PR comparison.

Late-flags01 tests the materialized-condition explanation while retaining the
automatic schedule. Its finalizer proves flags survive only across audited
constants/LEAs after memory and immediate rewrites are fixed, preserving the
machine-level rejection of an unproven planned LEA. Local regression and negative
tests, native compiler/runtime, all semantic/export gates, 12288 pressure calls,
and inherited differential loop oracles pass. Static census finds 15 unique sites
in five modules. Memory code shrinks 637 to 629 bytes, but six paired native
rounds regress memory time to 1.2174514927 of native-wide51204; other affected
workloads are near unchanged. No series exceeds 20% spread. Revision01 is rejected.

Late-flags02 moves the comparison next to the branch only after proving its
physical operands survive the intervening instructions. It rejects overwritten
inputs, regional fragments, shrink-wrapped saves and edge-result renames. The
actual memory test fails before the adjacent comparison/branch change and passes
afterward, including its input-overwrite rejection. Native correctness gates
pass again. Static census finds 14 sites; one zstd site fails the stricter proof.
The two 629-byte memory modules differ only at six bytes: CMP/LEA becomes LEA/CMP.

Six paired native rounds compare revision02, revision01, native-wide51204 and
matched-Go PR780. Memory time / previous is 0.9731084556 and time / revision01 is
0.8029398911. The other affected exports and unchanged SHA256 control are nearly
unchanged; no series exceeds 20% spread. This recovers the first regression but
does not explain the full 16% gain from forced latency scheduling. Revision02 is
a focused diagnostic lead, with no full-corpus or compile-cost qualification and
no promotion. Full08 remains the latest complete PR comparison, 2.84% throughput
above PR780. Production v77 and the 50% target remain unchanged.

ARM64 validation exposed another intermittent baseline issue:
TestDarwinARM64InterruptStressAcrossHostTransitionsAndGC occasionally returns nil
instead of context cancellation. Revision01 fails its first public runtime suite
and one of 100 focused repetitions. An initial unchanged-baseline 100-repeat run
passes; a subsequent baseline 1000-repeat run reproduces seven failures. Both use
Darwin registry02. Revision02's fresh compiler/core/public runtime suite passes;
that pass does not resolve the intermittent failure. Logs remain archived.

Next scoring lead: selected flags-form comparisons lose their assumed realization
when scheduling separates them from branches. ScoreAllocatedEmission currently
does not explicitly charge the resulting SETcc/MOVZX/TEST sequence. A targeted
resource-accounting test can establish that omission before changing scheduler
preferences, while retaining actual finalizer and repeated native evidence.

Fusion-cost01 implements that accounting experiment on native-wide51204, without
late-flags01/02 emission changes. Its focused test fails with resource cost 24
instead of 48 when a flags-form comparison materializes SETcc/MOVZX/TEST, then
passes after those operations are charged at their execution block weights.
Verified flag reuse avoids the charge; float comparisons and ARM64 scoring stay
unchanged. Local and native compiler, native public runtime, 21 semantic cases,
all 65-module/72-export gates and inherited differential oracles pass. A fresh
guarded ARM64 compiler/core/public runtime run passes, without resolving the
known intermittent baseline cancellation failure.

Matched static census changes code in seven modules and leaves 58 module code
hashes identical. Memory sum now selects latency scheduling because pressure's
resource estimate becomes 223 versus latency's 207. The completed native four-round
three-variant full-corpus comparison records time / previous 0.9998344765 and
time / PR780 0.9723704733 (2.84146% higher throughput). There is no meaningful
aggregate gain, and the experiment is rejected. Memory improves 4.55%, but
libtommath regresses 7.63%; changed-code modules average 1.03% slower. PR utf-as-simd
and candidate Doitgen exceed 20% spread. All raw samples remain archived.

The four-round native compile-cost check covers seven changed modules and two
controls. Allocation bytes stay within 0.07%; memory compile timing is noisy
(max/min 1.2690), so its 1.0443 ratio is not a stable regression claim. Process
RSS includes untimed setup and default-engine sizing. Compile and execution
verifiers pass, including source/binary crosslinks and matched Go 1.22.2.

Libtommath function 28 now selects a resource estimate of 19821 over 20311, while
spill debt rises from 39004 to 96375, physical copies from 546 to 573, and estimated
cycles from 25679 to 25769. The >2% resource preference overrides these dimensions.
A general rule for such tradeoffs is an untested follow-up, not an implemented
fix or a demonstrated cause. Retained candidate remains native-wide51204;
production v77 is unchanged and the 50% objective remains unmet.


Fusion-cost02 tests a general resource tradeoff guard: a lower resource estimate
cannot buy higher spill debt when neither estimated cycles nor physical copies
improves (both cycle estimates must be known). The score-policy test reproduces
the libtommath selection and goes green with the guard. Native compiler, full
public runtime, 21 semantics, all 65-module/72-export result gates and inherited
independent oracles pass; the fresh guarded ARM64 suite also passes. Static code
changes in 15 modules versus native-wide51204. Libtommath function 28 returns to
its previous pressure schedule and exact 19197-byte size.

Six paired native 300 ms rounds across those 15 modules and unchanged SHA256
control record candidate / previous 1.0015458407 and candidate / cost01
1.0009445245. No series exceeds 20% spread. Libtommath's regression recovers,
but raytrace regresses 6.7%; the general guard is rejected. Fastfloat's emitted
code is identical to cost01 despite a timing difference, so that observed change
cannot be attributed to different instructions. Retained reference remains full08.

A separate byte audit sharpens the memory lead: forced-latency metrics03 and
fusion-cost02 emit identical complete 446-byte sum functions, including wrappers.
The sum function shifts by 32 bytes because the preceding function changes size.
The corpus times only sum and has no init hook. This suggests placement rather
than further instruction-selection changes; the evidence itself does not prove
causation. The completed single-binary four-offset placement diagnostic confirms a local
placement effect. Local and native compiler checks prove the complete memory
module after its prefix remains byte-identical. Each shift passes varied-input
integer-sum checks before timing. Eight paired native 500 ms rounds record
shift32 / shift0 = 0.8697163204 and shift48 / shift0 = 0.8673765001. Shift16 is
1.0034378030. No series exceeds 20% spread; the zero-shift rebuilt control is
1.0015066426 times original cost01. Thus placement alone recovers roughly 13%
for this exact loop. This is not a general padding policy or a full-corpus gain.

The next general hypothesis is consistent absolute alignment for compact hot
loops: function-local alignment must agree with final function placement.
The two faster memory placements put its hot loop at module-relative offsets
0/16 modulo 64; slower placements use 32/48. The diagnostic did not log absolute
native addresses, so these static offsets are not themselves a native address
audit. A candidate must
preserve metadata, calls, cache and parallel compilation and then pass full
correctness and repeated corpus timing before retention. Production v77 remains
unchanged; native-wide51204 remains retained and the 50% objective is unmet.


Loop-align64-01 tests that placement hypothesis on retained native-wide51204,
without the rejected flags-cost changes. AMD64 speed loop headers and rotated
zero-test latches align locally to 64 bytes, and module assembly aligns functions
containing Wasm loops to the same boundary in sequential and parallel paths.
The call graph retains its existing loop-presence calculation. The first variant
aligned every function; compact selection then failed its size contract (87/87
bytes). Restricting function padding to loop-containing functions restores that
contract. The initial placement test fails on sequential, parallel and both cache
paths before the change, then passes. Encoder tests cover all 128 offsets and
idempotent alignment, and a finalizer test checks actual loop-header offsets.

All local and native compiler checks, full native public runtime, 21 semantic
cases, all 65-module/72-export result gates and inherited independent differential
oracles pass. Fresh guarded ARM64 compiler/core/public runtime checks also pass;
the baseline intermittent cancellation issue remains unresolved. Static census
changes code in 61 modules and grows module-balanced native bytes by 6.31%
(memory 637 to 861). The completed four-round matched-Go1.22.2 comparison against
native-wide51204 and pinned PR780 records candidate / previous 1.0006459469 and
candidate / PR780 0.9738443090 (2.68582% higher throughput). There is no aggregate
gain, and the broad alignment rule is rejected. Memory_tree regresses 5.19% and
yyjson 4.70%; memory improves only 1.15%. PR utf-as-simd and previous Doitgen exceed
20% spread, while no candidate series does. Total native code grows from 1960866
to 2032778 bytes (3.67%); the module-balanced growth is 6.31%.

The separate native address audit confirms page-aligned mapping and 64-byte
function entries with both sequential and parallel compilation. Static memory
disassembly shows its 57-byte pressure-scheduled vector loop starts at module
relative 0x200, so the alignment is realized. This differs from the earlier
latency-scheduled placement experiment: moving those identical instructions
helped much more. These comparisons do not isolate which instruction accounts
for the difference, and they do not justify universal padding. Any further
alignment rule needs a demonstrated profitability condition that controls code
growth. Retained candidate remains native-wide51204 and matched-full08 remains
its reference; production v77 is unchanged and the 50% objective is unmet.


## Native-wide coverage and strided gather rejection

Four static probes inspect all 65 execution modules, preserving identical
machine-code hashes. The native-wide capture pass sees 2,891 loop attempts:
2,803 body-shape rejections, 55 inspection rejections, 19 lane-independence
rejections and 14 captured regions. This does not count earlier bytecode SIMD or
source SIMD. Raising the operation cap from 64 to 96 only moves one BICG loop to
inspection rejection. A diagnostic relaxation of non-power-of-two counters moves
12 loops to lane rejection and captures none. General limit increases alone do
not expand emitted native-wide coverage for this corpus.

The lane diagnostics identify two loops with contiguous stores, strided loads
and no carried floating-point locals: ADI function 1 / stack index 394 and SYR2K
function 1 / index 450. Strided-wide01 extends only native-wide eligibility and
gathers scalar raw bits into private scratch before vector arithmetic. It keeps
whole-region bounds/alias guards and excludes strided memory operand folding.
Four/eight-lane capture/emission and bounds tests pass after an initial red test.

All native compiler/public runtime checks, 21 semantic cases and the full
65-module/72-export result corpus pass. Four strided fixtures each pass 1,672
input cases in explicit and signal bounds modes against Railshot, comparing
results, traps and full memory with permitted NaN payload normalization. Exact
ADI/SYR2K full-memory checks pass after three repeated calls in both modes, as do
all inherited oracles. Fresh guarded ARM compiler/core/public runtime checks pass
with registry02; the known intermittent baseline cancellation issue is unresolved.

Only ADI and SYR2K generated code changes: 11,250 to 15,314 bytes and 3,970 to
6,546 bytes respectively. Six native CPU0 paired 300 ms rounds against retained
native-wide51204 show time ratios 1.0023508502 and 3.4481638462. SYMM, memory and
SHA256 unchanged controls are 1.0000746111, 0.9965728019 and 1.0031532184. No series
exceeds 20% spread. This complete transform is rejected. The screen does not
separate scalar gather overhead from entry guard costs; further gather work would
need to isolate that cause before pursuing another implementation. There is no
full-corpus performance or compile-cost claim for the rejected candidate.

Evidence: `docs/performance/dragline-20261003/wide-coverage-01`,
`strided-wide-01`, `strided-wide-census-01`, and `strided-wide-screen-01`.
All source/log/corpus/count/sample verification scripts pass. The retained
reference remains native-wide51204 / matched-full08, production v77 is unchanged,
and the 50% throughput objective remains unmet.


## Direct register gathering recovers SYR2K

The native strided-paths01 diagnostic records 46,200 successful SYR2K vector
entries versus 1,800 short-trip fallbacks (96.25% success). Trip counts 4 through
80 each have 600 successes across benchmark setup, correctness, warmup and timed
calls. ADI records no hits or failures. A separate static preflight-error probe
prints no errors and preserves all 65 code hashes, so ADI's lack of execution is
not explained by those explicit setup/body failure paths. Instrumented timing is
not used as performance evidence.

Strided-wide02 replaces scalar stores followed by a wide stack reload with
VMOVSD/VMOVHPD pairs, VINSERTI128 and VINSERTI64X4. Registers 14/15 are reserved by
the existing bounded allocator, and preflight tracks clobbers for the original
save/restore bridge. Guard logic, width selection and eligibility are unchanged.
Encoder bytes match independent GNU assembler output. Local and native compiler
checks, full native public runtime, all inherited independent oracles, four new
strided differential fixtures (1,672 inputs each in two bounds modes), exact
ADI/SYR2K memory/results after repeated calls, all 65/72 result checks, and fresh
ARM compiler/core/public runtime checks pass. The known baseline intermittent
ARM cancellation issue remains unresolved.

Six native paired 300 ms rounds record register / stack SYR2K time 0.2639146850,
and register / retained native-wide51204 time 0.8998788219. ADI and three unchanged
controls remain near 1.0, with no >20% spread series. This isolates the gather
implementation as the primary source of the large regression; no hardware-counter
claim about a particular stall is made.

The complete four-round CPU0 matched-Go1.22.2 comparison records candidate / PR780
0.9716241514, previous / PR780 0.9753555647 and candidate / previous 0.9967454146.
Candidate throughput is 1.0292045526 times PR (2.92% higher), far short of 50%.
SYR2K time / previous is 0.9050882669, corroborating the focused gain. ADI is
1.0024815565. The remaining 63 generated code hashes are identical, but timings
vary: Doitgen 0.86154, drwav 0.94071 and many_funcs 1.10216. These cannot be
attributed to the compiler transform. PR utf-as-simd and previous Doitgen exceed
20% spread; no candidate series does. The two changed modules' time geometric
mean is 0.9525409674, versus 0.9981818338 for unchanged code; these diagnostic
splits do not replace the full score.

Total generated code grows from 1,960,866 to 1,967,138 bytes (0.32%), with 1.19%
module-balanced growth. The completed compilation-cost run covers four paired
rounds of three end-to-end compiles for the two changed modules and nine controls.
ADI and SYR2K time ratios are 1.0063568373 and 1.0076041984; allocation-byte
ratios are 1.0300894511 and 1.0240158793. No compile-time series exceeds 20% spread.
Given repeatable SYR2K execution improvement, full correctness and these measured
costs, strided-wide02 becomes the retained experimental reference. This is a
local optimization gain, not achievement of the 50% full-corpus objective. All code stays
in isolated overlays; production v77, commits and remotes are unchanged.

Archives: `strided-paths-01`, `strided-wide-02`, `strided-wide-census-02`,
`strided-wide-screen-02`, `strided-wide-full-02`, and `strided-wide-compile-02` under
`docs/performance/dragline-20261003`. Their independent verifiers pass.


## Literal dependencies limit pure integer loop hoisting

Three static census probes inspect the retained strided-wide02 compiler. All
65 generated code hashes remain identical. Merely extending the operation list
exposes few direct opportunities; known constants defined inside a loop prevent
many existing integer expressions from meeting the original invariant test.
The raw occurrence logs are not unique hot operations or a profitability claim.

Licm-literals01 prototypes bounded dependency groups under the existing pure-op,
source-region and pressure rules: at most four extended roots per loop, literal
producers before consumers, and earlier same-loop moves available to later
roots. A new producer-order unit test fails on the baseline and passes afterward.
Small independent integer fixtures pass, but static CoreMark function 6 and four
native BLAKE3 runtime cases fail schedule verification. A diagnostic identifies
an original constant instruction that remains a fusion dependency even though
the consumer's machine operand uses its canonical equivalent. Moving only the
canonical definition breaks the selection DAG. Revision01 is disqualified and
has no performance measurement.

Licm-literals02 preserves original and canonical literal dependencies. Its
focused alias regression fails against revision01 and passes after the repair.
All 65 static corpus compilations, local AMD64 compiler/encoder checks, native
compiler/full public runtime, 21 semantic cases, 65-module/72-export result gates,
all inherited independent oracles and four new integer fixtures (4,032 modular
arithmetic calls across two compilers and bounds modes) pass. Fresh guarded ARM
compiler/core/public runtime checks pass with registry02; the known intermittent
baseline cancellation issue is unresolved.

Nine modules change code: CoreMark, fannkuch, json-as, kissfft, libtommath,
monocypher, PCRE2, Deriche and Floyd-Warshall. Total bytes grow 1,967,138 to
1,967,496, while module-balanced size is 0.9995501371 times baseline. Six native
paired 300 ms rounds for those nine and three unchanged controls record PCRE2
0.9741892763 and monocypher 0.9878232666 times retained strided-wide02 time.
Other changed modules remain near unchanged. Changed-module geometric mean is
0.9949159566, all twelve 0.9964930646; no series exceeds 20% spread. These are
focused results, not substitutes for the full corpus target.

The full four-round 65-module comparison against strided-wide02 and pinned PR780
is complete: candidate / previous time is 1.0005064407, effectively tied.
Candidate / PR time is 0.9667216940, but PR dispatch and QOI plus both Dragline
Doitgen series are noisy. The 3.44% absolute throughput figure is not proof of
a gain over the retained compiler. PCRE2 improves 1.96% again, and monocypher
0.57%; the nine changed modules have mean 0.9964741288 and unchanged code
1.0011560110. Full-corpus improvement remains unproven. Four paired compile-cost
rounds pass independent verification, with no >20% spread: most time ratios are
near 1.0, Deriche is 0.87557, Floyd-Warshall 1.02554, and allocation bytes stay
within 0.42% above baseline. The prototype remains unretained. Production v77
and remote branches are unchanged. Archives are `licm-census-01`, `licm-literals-01`, `licm-literals-02`,
`licm-literals-census-02`, and `licm-literals-screen-02` beneath
`docs/performance/dragline-20261003`; the completed archive verifiers pass.


## Alias-aware immediate selection follow-up

A selection-only census on retained strided-wide02 records 9,898 occurrences in
62 modules where the immediate combination's original producer differs from the
alias-resolved producer. Of these, 8,661 are I32Const pairs, 1,231 I64Const pairs,
and six I64Mul pairs. All 65 emitted code hashes are unchanged in this diagnostic.
It establishes breadth, not profitability or unique hot-instruction counts.

Canonical-immediate01 independently normalizes only integer-literal immediate
producers, preserving nonliteral producers, address combinations and compare /
branch combinations. It is based on strided-wide02, without the unretained
LICM groups. A focused equal/distinct i32/i64 literal test fails on the original
selector and passes with the change. Local AMD64 compiler/encoder and fresh
ARM compiler/core/public runtime checks pass. All 65 modules compile in the
static census; 47 change code, with total size 1,967,138 to 1,966,555 bytes and
module-balanced size ratio 0.9985318474. Full native correctness, all 65/72
result gates, and 7,056 independent literal-oracle calls pass. The focused
six-round 14-module time ratio is 1.0014033950, effectively tied, with no series
above 20% spread. NanoSVG improves 3.62%; drwav regresses 3.40% and PCRE2 1.54%.
An order audit finds candidate always ran first: the two-variant rotation and
reversal cancel. Small differences therefore carry an additional order bias
limitation. The prototype is unretained, and no full-corpus speed claim follows.
Archive: canonical-immediate-screen-01. Retained strided-wide02 is unchanged.


## Larger scalar leaves

Read-only admission analysis across the 65-module corpus finds five newly
eligible one-layer call-chain leaves (13 static call sites in three modules),
no additional SIMD leaves at the existing 384-byte cap, and 60 additional scalar
leaves (312 static call sites in 43 modules) at a 1,536-byte cap. The latter has
broader coverage and is tested first; counts do not establish dynamic hotness.

Large-leaf01 changes only the scalar body cap and cache epoch on strided-wide02.
The 4,096-byte caller growth budget and other restrictions remain. A medium-body
test fails on baseline and passes on candidate. Full native correctness, 65/72
results, 7,056 independent i32/i64 calls and fresh ARM compiler/runtime pass.
Code changes in 41 modules; total bytes rise 1,967,138 → 2,287,300 (+16.28%).
Many common utility expansions more than double PolyBench module sizes, so
execution benefit must justify the cost. The completed six-round 18-module screen measures 0.9742154224 times retained
time (2.58% lower), with audited alternating pair order. Monocypher improves
26.51%, zlib 13.43%, UTF SIMD 4.02%, and JSON SIMD 2.39%; NanoSVG regresses
2.38% and 2mm 2.98%. Previous yyjson is noisy (1.28245 max/min), while candidate
series stay below 20% spread. A full six-round 65/72 comparison against retained
and pinned PR780 is running; no retention is established.
Archives: leaf-chain-census-01, leaf-admission-census-01, large-leaf-01 and
large-leaf-census-01. Production and retained strided-wide02 remain unchanged.

The lexical-loop follow-up (leaf-hot-sites-census-01) finds 49 newly eligible
large leaves with 148 loop-nested static call sites across 39 modules, including
common utility/setup routines. A loop-site restriction alone therefore cannot
be assumed to remove all cold expansion. This probe does not measure hotness.


Large-leaf01's full six-round 65-module / 72-export comparison is complete and
independently reconstructed: time / retained is 1.0012344501, effectively tied,
and time / PR780 is 0.9738075304. Monocypher improves 26.71% and zlib 13.04%,
but expanded Nussinov regresses 28.00%, Floyd-Warshall 8.12%, and SYMM 6.32%.
Tiny also moves 6.68% despite identical code; noise is recorded in the full
archive and timing shifts are not all attributable to the transform. Revision01
is unretained. Its compilation-cost run follows the execution run on CPU0.

A branch-table census finds 32 newly admitted callees at 209 sites in 31 modules
above a 32-entry aggregate budget. Revision02 applies that budget only to bodies
above 384 bytes. Local AMD64 and guarded ARM tests pass; 4,096 independent ARM
branch-arithmetic calls cover both sides of the 32/33-entry boundary. All 65
modules compile: revision02 changes 13 versus retained, reduces added native
bytes from 320,162 to 129,627, and keeps monocypher/zlib code byte-identical to
revision01. Native AMD64 correctness and timing are pending. Source and evidence
are archived under large-leaf-02, leaf-control-census-01 and large-leaf-census-02.

Revision01 compilation costs are complete and verified, with no series above
20% spread: sampled numerical modules take 5.11–6.24× compile time and
2.30–2.60× allocation bytes; zlib nearly doubles and monocypher rises 34.79%.
Revision02 now passes all native compiler/public runtime, semantic and 65/72
result gates, inherited independent oracles, 7,056 larger-body arithmetic calls
and 4,096 branch-table calls. Its 17-module native screen is running with six
balanced permutations of revision02/revision01/retained. Production remains v77,
retained remains strided-wide02, and the 50% throughput target remains unmet.


The completed large-leaf02 focused screen measures 0.9744161418 times retained
time and 0.9819430632 times revision01 time across 17 modules, with no >20%
spread. Monocypher improves 26.62% and zlib 12.62%; Nussinov/Floyd-Warshall
return near retained time. NanoSVG and PCRE2 regress 3.83% / 2.20%. The full
six-round 65/72 comparison is active as large-leaf-full-02.

A separate allocator census on retained strided-wide02 records 88 visits and
11 machine fingerprints across six modules for call-free all-integer AMD64
loop functions of 128–239 instructions. All 65 emitted hashes remain unchanged
in the logging-only probe. Medium-density01 tests squared-use GPR cost for that
class without changing regional admission or call rules, and without the
larger-leaf changes. Local AMD64/ARM suites pass; 4,032 ARM independent modular
recurrence calls pass, and both fixtures compile to 164 AMD64 instructions.
Five corpus modules change code. Weighted spill-debt units also change, so
old/new score reductions cannot prove fewer spills or better execution. Native
AMD64 validation and timing are pending. Archives: medium-density-census-01,
medium-density-01 and medium-density-code-census-01. No production promotion.


Large-leaf02's complete six-round 65-module / 72-export comparison is now
verified, including all six executed permutations, paired medians, raw rows,
corpus and binary pins. Time / retained is 0.9934198102 (0.658% less time),
and time / PR780 is 0.9658279986 (3.54% higher throughput). The 13 changed
modules average 0.9672360704; 52 unchanged-code modules average 1.0000757562.
Monocypher uses 26.27% less time, zlib 13.14%, and UTF SIMD 3.50%. NanoSVG
regresses 3.28%, PCRE2 1.75%, and CoreMark 2.32%. Candidate and retained have
no series above 20% spread; PR780 QOI and zstd series do, limiting the absolute
PR comparison. No samples were discarded. See large-leaf-full-02.
Compilation costs are running before retention is decided. Production v77 and
retained strided-wide02 remain unchanged; the 50% throughput target is unmet.
The medium-density prototype's native qualification follows compilation costs
on CPU0; its static allocator metrics are not execution-performance evidence.


Large-leaf02 compilation costs are complete and independently reconstructed.

Compilation costs are complete and verified in ../large-leaf-compile-02.
Numerical compile-time ratios are 0.99365–1.00511, removing revision01's
5–6× regressions. Costs remain substantial elsewhere: zlib 2.00474×,
monocypher 1.34780×, NanoSVG 1.43171×, and CoreMark 1.32786×. No measured
compile series exceeds 20% spread. Zlib allocation bytes rise 40.74% and
monocypher 20.62%; RSS includes untimed setup and is not isolated compiler RSS.

Decision: retain large-leaf02 as the experimental execution reference because
its focused and complete comparisons repeat Monocypher/zlib gains and the
full aggregate uses 0.658% less time with unchanged-code modules near parity.
This accepts recorded compilation and code-size costs for the execution
objective; it is not a production promotion. NanoSVG, PCRE2 and CoreMark
regressions remain visible in the full report. The 50% target remains unmet.

The medium-density01 native oracle and compiler checks pass; the rest of its
full native qualification is running. It remains a separate strided-wide02-based
prototype until measured against both execution references.


Medium-density01 native qualification is complete: compiler/public runtime,
21 semantic cases, all 65 modules / 72 exact export results, inherited memory,
pressure, recurrence and wide-loop oracles, and the independent 4,032-call
recurrence oracle pass. Both fixtures confirm 164 AMD64 machine instructions,
inside the policy range. The source remains b4bc9da98ce83af1a94e32fa2de1c5a1382849b8707a5276533e3ffc0df45b28;
the Go1.22.2 native binary is 216c0aa92ba4a4da5131dac4f6573a6126060afc7bcbc208121d0c2f1517a1f9.
The nine-module screen against strided-wide02 and retained large-leaf02 is running.
No allocator runtime gain is claimed yet. A logging-only census of smaller
64–127-instruction integer loops is running locally to assess broader coverage.


The smaller-loop diagnostic is complete in small-density-census-01: 107 raw
allocator visits, 17 module/fingerprint entries (16 globally distinct structures) across six modules (json-as,
json-as-simd, yyjson, libtommath, NanoSVG and PCRE2), for call-free all-integer
loops with 64–127 machine instructions. All 65 complete native hashes and input
hashes match strided-wide02, proving the logging probe changes no emitted code.
These counts indicate static coverage, not hotness or runtime gain. The current
128–239-instruction density prototype's native focused screen is still running.


Medium-density01's focused comparison is complete and verified: 0.9607954646
times strided-wide02 execution time across nine modules, 1.0016584778 times
large-leaf02, no series above 20% spread. Monocypher improves 28.06% versus
strided-wide02 and NanoSVG 2.43%; zlib loses large-leaf02's gain. This motivates
combining the independently qualified policies rather than assuming additive
performance. Source and raw evidence are in medium-density-screen-01.

The combined leaf-density01 policy passes local AMD64 compiler/encoder, fresh
guarded ARM compiler/core/public runtime, all three ARM independent oracles,
and the full native qualification: compiler/public runtime, 21 semantic cases,
65 modules / 72 exact result exports, all inherited memory/pressure/recurrence/
wide-loop oracles, 7,056 large-leaf arithmetic calls, 4,096 branch-table calls,
and 4,032 density arithmetic calls. Both native density fixtures admit at 164
machine instructions. Effective source is 493a6c804b323a73d0dfff95c9c99b16bb76e2b4afe3769cdb80c75d6a1c6d9e.
The static census changes five modules from large-leaf02 and shrinks total bytes
by 1,296 to 2,095,469; every per-module delta matches standalone density. The
17-module native interaction screen is running. No combined performance result
is claimed. Production v77 remains unchanged; retained reference is large-leaf02.


Leaf-density01's seventeen-module native interaction screen is complete and
verified: time / large-leaf02 is 0.9947245791 and time / standalone density is
0.9906320368. The five changed modules average 0.9853787934 versus large-leaf02;
twelve unchanged modules average 0.9986447695. No series exceeds 20% spread,
but unchanged yyjson moves -1.55% and UTF8proc +0.92%. Monocypher improves 1.43%,
NanoSVG 3.61%, and PCRE2 2.03% versus large-leaf02; zlib retains its prior gain.
Combined Monocypher is 0.80% slower than standalone density, so improvements
are not simply additive. The full six-round 65/72 comparison against retained
large-leaf02 and pinned PR780 is now running. See leaf-density-screen-01 and
leaf-density-full-01. Production remains unchanged and the 50% target is unmet.

Small-density01 extends the combined policy's lower instruction threshold from
128 to 64, with other eligibility, regional and call rules unchanged. New 64/127
boundary tests fail before the change and pass afterward. Local AMD64 compiler/
encoder and guarded ARM compiler/core/public runtime pass. The independent ARM
oracle passes 4,032 calls over twelve modular recurrences; both i32/i64 fixtures
have 124 AMD64 machine instructions. All 65 static compilations succeed; code
changes in json-as, json-as-simd, NanoSVG, PCRE2 and yyjson, adding 128 total
native bytes. Effective source is 68bc9a8318489f14dcbf1ca2d83d29d95e7ecdf979c3c660956c338dc52fa47c.
Native correctness and timing are pending. An initial metrics command used the
ARM compiler build and was rejected; the corrected AMD64 static build passes.
Archives retain both logs. See small-density-01 and small-density-code-census-01.


Mixed scalar-FP GPR-density coverage is now documented in
mixed-gpr-density-census-01. The logging-only strided-wide02 probe keeps all
65 native code hashes unchanged. It counts 139 allocator visits and 32 module/
fingerprint pairs but only five globally distinct structures: Matmul's 205-
instruction loop, NanoSVG's 201-instruction loop, and three structural variants
of the shared 143-instruction utility across thirty PolyBench modules. The
apparent breadth mostly repeats utility code; there is no new optimization or
runtime gain from this probe. The small-density census wording is also corrected:
17 module/fingerprint entries represent 16 globally distinct structures.


Leaf-density01's full six-round 65-module / 72-export comparison is complete
and independently reconstructed: time / large-leaf02 is 0.9965019624, time /
PR780 is 0.9612940105, throughput / PR780 is 1.0402644654. The five changed
modules average 0.9838671595 and sixty unchanged modules 0.9975621582. Changed
Monocypher improves 1.95%, NanoSVG 3.30%, PCRE2 2.30%, zstd 0.65%; json-as
regresses 0.17%. No series exceeds 20% max/min spread, yet unchanged many_funcs
improves 8.81%, Matmul 2.00%, dispatch 1.86%, fastfloat 1.70%, tiny 1.68%.
These shifts contribute to the small aggregate gain and prevent attributing
all of it to allocation. All raw samples remain in leaf-density-full-01.
Compilation costs are now running, followed serially by small-density01 native
qualification. Retention remains undecided and production is unchanged. The
50% throughput target is unmet.


Leaf-density01 compilation costs are complete and verified.

Compilation costs are complete and verified in ../leaf-density-compile-01.
Four paired rounds over five changed modules and two controls show changed
compile-time ratios of 0.97977–1.00243 versus large-leaf02. Allocation bytes
remain near or below baseline, and no series exceeds 20% spread. Unchanged
memory compiles 5.89% faster, illustrating that small compile-time differences
need caution too. RSS includes untimed process setup.

Decision: retain leaf-density01 as the next experimental execution reference.
Monocypher, NanoSVG and PCRE2 gains repeat across focused and full comparisons,
with 1,296 fewer native bytes and similar measured compile costs. The complete
aggregate improves only 0.350%, partly from unchanged-code timing shifts;
absolute measured throughput / PR780 is 1.0402644654. This is not a production
promotion or achievement of the 50% target. Earlier large-leaf compile costs
relative to strided-wide02 still apply and remain archived.

Small-density01 native arithmetic oracles (including 4,032 new calls) and compiler
checks pass; its full runtime and corpus gates are still running. No small-loop
performance result is established.


Small-density01 native qualification is complete and independently checked:
compiler/public runtime, 21 semantic cases, all 65 modules / 72 exact export
results, inherited full-memory/pressure/recurrence/wide-loop oracles, and all
four independent arithmetic/table oracle programs pass. The new small-loop
oracle executes 4,032 calls and confirms both fixtures at 124 machine
instructions. The nine-module six-round comparison against retained
leaf-density01 and prior large-leaf02 is running. No new runtime gain is
claimed yet. Production v77 remains unchanged.


Small-density01's six-round nine-module native screen is complete and verified:
time / retained leaf-density01 is 1.0015681198, slightly slower. NanoSVG regresses
2.26%; PCRE2/yyjson improve about 0.5%. Candidate JSON serializeN spread is
1.2632598939; other series stay below 20%. The extension is unretained and no
full timing run follows. See small-density-screen-01.

Linear-gpr-density01 changes the cost formula, not admission: AMD64 GPR intervals
already using density get one power of function-size/range-length rather than
two. Preserve FP/ARM/area costs and the long-range floor. All three allocation,
preservation and weighted-debt consumers use the same helper. Unit tests fail
with old costs 88/107 versus new 53/72 and pass with the change. Debt units and
schedule ranking change together; raw debt values cannot be physical spill
counts. Effective source is a7ada32bdd9cc0031a191e5d7527c11f53b0c55f970f570c56cafecebdbea589.
All 65 static compilations pass; 51 native code hashes change and total bytes
shrink 6,156 to 2,089,313. Local AMD64, fresh guarded ARM, independent ARM oracles
and the complete native qualification pass, including compiler/public runtime,
21 semantic cases, 65 modules / 72 exact exports and inherited independent
memory/arithmetic oracles. The nineteen-module native screen is running. No
runtime improvement is claimed yet. Retained leaf-density01 and production v77
remain unchanged; the 50% goal is unmet.


Linear-gpr-density01's nineteen-module screen is complete and verified:
module-balanced time / retained leaf-density01 is 1.0058877054, with no >20%
spread. Monocypher regresses 6.25%, zlib 5.41%, LZ4 3.54%, NanoSVG 2.88%, PCRE2
1.39%; yyjson improves 3.39%, BLAKE3 2.16%, JSON SIMD 1.71%. It is unretained
and no full comparison follows this broad formula change.

Revision02 isolates allocation priority from schedule-debt scoring. Both actual
allocator paths fail a new pressure-fixture integration test on revision01:
reported debt 86,258 versus retained-unit sum 7,383,449. Restoring the final
metric formula makes the test pass while allocation priorities remain linear.
Effective source is c1f3ce743cf5b25e0975d4153664af1c6d7c231538882c9e5e00dcd8b3c909db.
All 65 static compilations succeed: 15 code hashes and 27 selected schedules
change versus revision01; 51 hashes differ from retained. Total code is
2,088,965 bytes. Monocypher, zlib and LZ4 remain byte-identical to the first
regressing prototype, weakening the schedule-score explanation for their losses.
Local AMD64, fresh guarded ARM and complete native correctness pass, including
65/72 exact results and independent arithmetic/memory oracles. The twenty-module
three-variant screen is running over all revision02 changes, the three earlier
regressions and two controls. No new runtime gain is claimed. Retained
leaf-density01 and production v77 remain unchanged; the 50% target is unmet.


## Linear priority revision02: verified rejection

The six-permutation twenty-module native screen completes with revision02 time
/ retained leaf-density01 **1.0152248185**, and time / revision01 **0.9948685961**.
No variant/export exceeds 20% sample spread. NanoSVG and PCRE2 recover relative
to revision01, but KissFFT regresses 30.61%, Monocypher 6.53%, zlib 5.38% and LZ4
2.41% versus retained. KissFFT is nearly identical between both linear-priority
revisions. BLAKE3 improves 2.12% and yyjson 4.15%, insufficient for an aggregate win.

All raw paired samples, actual permutations, exact export checks and source/binary/
corpus hashes verify. Full candidate correctness remains complete. Reject revision02
without a full performance run. The separation between priority and schedule-debt
units is now isolated, but it does not justify changing retained allocation policy.
Per-function schedule changes are independently reconstructed in the static census.
Leaf-density01 remains the experimental reference at 4.03% higher full-corpus
throughput than PR #780; production v77 is unchanged and the 50% target is unmet.


## Segmented lifetime-gap allocation: lower AMD64 trial threshold

The retained loop trial threshold is64 weighted-debt units, while actual
acceptance requires a reduction of8 with no increases in slots, preservation,
copies/cycles, fixed repairs or broken fusions. The isolated segmented-threshold01
overlay lowers only AMD64 trial admission to8. ARM, giant-function dispatch,
allocation mechanics and acceptance guards remain unchanged. A boundary test
fails at8/9/63 before the change and passes after it.

The full static census adds28 trials in11 modules and accepts3 functions:
LZ4 function3 (54→8 debt,9→6 slots,89→79 copies), PCRE2 function41 (40→28 debt,
11 slots unchanged,81→72 copies), UTF-AS-SIMD function5 (12→2 debt,3→2 slots,
36 copies unchanged). Only these3 modules change native hashes; total -207 bytes.
Remaining rejected trials are archived too. Full native compiler/runtime,
21semantic cases,65/72exact results and independent memory/arithmetic gates
pass; fresh local AMD64 and guarded ARM checks pass. Six paired native rounds
on all3 changes plus3 unchanged controls are running; no performance claim yet.


## Segmented threshold01 timing and copy-guard revision02

Revision01 six-module screen completes and verifies at 0.9962184453 times retained
time, without any max/min spread above20%. LZ4 decompression improves8.12%; its
module-balanced ratio is0.95182249. PCRE2 ratio0.99411861. UTF-AS-SIMD validator
regresses7.87%, giving module ratio1.03308633. Three unchanged controls stay near1.
Smaller static spill debt and code did not prevent this validator regression.

A generated-block oracle validates154 LZ4 streams with varied literal lengths,
match lengths and offsets including overlapping back-references, repeated3times
in2bounds modes:924 calls each on native AMD64 and ARM. Expected decoded bytes
are independently constructed; Railshot supplies a complete-memory crosscheck.
An initial harness bug retained borrowed Invoke result storage across another
call; copying scalar pointers immediately fixes it. The failed harness log is
retained and is not attributed to compiler behavior.

Revision02 additionally requires fewer physical copies when accepting a newly
tried low-debt loop (minimum reduction8 and baseline debt below64). Existing
acyclic/high-debt behavior is tested unchanged. Red/green tests pass. Across all 65
inputs, LZ4/PCRE2 remain identical torevision01 and UTF-AS-SIMD returns exactly to
retained code; only2modules change versus retained,175bytes smaller. Native
revision02 qualification awaits the currently running revision01 compile-cost
job; local AMD64 and guarded ARM suites plus independent ARM oracles pass.


Revision01 compilation cost is complete and verified: LZ4 time1.02221,
PCRE2 time1.00418, UTF-AS-SIMD time1.03484 versus retained. Allocation bytes rise
at most1.27%, allocation count at most3.32%; no time-series spread exceeds 20%.
The unchanged memory control measures0.97901, showing measurement variation.
Revision02 native qualification also completes: full compiler/runtime,21semantic,
65/72exact results, inherited memory/arithmetic gates and924varied LZ4 calls.
A six-module three-way native screen is now running; no new full result exists.


## Segmented copy guard: focused result and full comparison

Revision02 screen verifies at **0.9938368168** times retained time across six
modules. LZ4 decompression is **0.9180260448** times retained, repeating the
previous 8% gain; LZ4 module ratio is 0.9622099474 because compression varies.
PCRE2 is 0.9961946300. Restored UTF-AS-SIMD code measures 1.0016185017 while
unguarded revision01 again regresses at 1.0404287345. Two unguarded BLAKE3 series
have max/min spread 1.20344/1.20527; candidate/retained have none above 1.2.

Reject revision01. Revision02 merits the full six-permutation 65-module/72-export
comparison against retained leaf-density01 and matched Go 1.22.2 PR780; that run
is now live. No full aggregate or retention decision exists yet. Revision02
compilation overhead remains to be measured separately. The full-corpus target
remains 50% higher throughput; the last retained full result remains 4.03%.


## Segmented threshold full result: no demonstrated aggregate gain

The six-round full comparison verifies time / retained 1.0011498869 and time /
PR780 0.9604700757 for revision02. Retained leaf-density01 measures 0.9587454267
versus PR (4.30% higher throughput), a repeat measurement of the same candidate.
LZ4 improves 5.41%, PCRE2 regresses 0.42%. Unchanged-code mean is 1.0020050475;
many_funcs shifts +9.50%. Candidate/PR QOI, PR Doitgen, and retained Blake series
have >20% spread. No samples are excluded. Diagnostic unchanged-ratios-to-one
sensitivity is 0.9992081319, excluding-many_funcs is 0.9997488787; neither replaces
the complete result. Threshold02 is not retained; its compile follow-up is not run.

## Bounded shared liveness: broader allocation opportunity

Four static diagnostics preserve all 65 input/native hashes. Of 241 module/shape
pairs (240 global shapes), 78 visits across 26 modules exhaust the scalar budget,
completing 4,426 of 15,067 eligible values in those visits. Exact full reachability
finds 4,821 ranges with holes versus 2,222 under the current budget. Full bitsets,
block-delta and changed-word solvers all match every independent scalar reference
membership bit. The changed-word ring queue uses 273,752 predecessor-word updates
and 209,679 word visits, versus 618,024 counted operations in the incomplete
budgeted scalar walk. Units differ; this is not a measured speedup. Bitmap
storage peaks at 1,006,848 bytes before queues/indexes/output, also archived.

The isolated segment-bitset01 prototype uses the changed-word algorithm on AMD64,
capped at 65,536 bitmap cells and 3 times existing propagation work. Any incomplete
solve uses the original scalar walk; partial liveness is never consumed. ARM
keeps the existing walk, and all new scratch buffers are counted. Fifteen
synthetic graph/register configurations compare 23,902 membership bits, with
zero-work, partial-work, matrix-cap and ARM fallback checks. Local AMD64 suites,
guarded ARM suites and independent ARM oracles pass.

All 65 static inputs compile: 9 modules change, 2,380 bytes disappear, 8 additional
allocations are admitted and none lost. Full native qualification passes all compiler/runtime, semantic, 65/72 exact
results and independent memory/arithmetic gates. A twelve-module native screen
is running against the parent threshold02 and retained leaf-density01.
The prototype is based on unretained threshold02, so performance must also be
compared with retained leaf-density01. No bitset runtime gain is established.


## Shared liveness: separate-binary timing and layout control

Bitset01's twelve-module screen verifies at 1.0114427754 times retained time
and 1.0154991821 times parent threshold02. Memory is 20.33% slower despite
identical native code; no series exceeds 20% sample spread. Changed results
are mixed: NanoSVG +4.16%, utf8proc +1.71%, yyjson -3.44%, PCRE2 -1.40%,
raytrace -1.58%, zstd -1.08% versus retained. All samples are archived. A stable
cross-binary difference in unchanged code prevents clean attribution to policy.

A diagnostic executable now includes candidate, parent and retained policies,
selected by A82A_SEGMENT_POLICY at process start and salted into the artifact
revision. All 195 static source/code comparisons match the original references.
Native 65-module/72-export and 21-semantic gates pass separately in all three
modes. Default candidate compiler/runtime and independent arithmetic/memory
checks pass; fresh local AMD64 and guarded ARM tests and ARM oracles pass.
The completed twelve-module six-permutation run measures 0.9942475784 times
retained time, with no series above 20% spread. Unchanged memory is now
0.9985765782 times retained; its previous 20.33% regression disappears with one
executable. NanoSVG still regresses 4.08%. This focused result does not establish
a full-corpus gain; bitset01 is not retained and its full comparisons have not run.

Separately, cloned diagnostic allocation completes the two giant liveness graphs:
PCRE2 function60 has 32,385 instructions, 9,731 blocks and 1,206 ranges with gaps;
yyjson function18 has 21,903 instructions, 6,070 blocks and 1,066 such ranges.
Bitmap plus queue scratch is 14,168,336 and 9,177,840 bytes, excluding indexes,
interval copies and output. Diagnostic caps are 1,048,576 cells / 48 times the
scalar traversal budget. All 65 emitted code/input hashes remain unchanged;
no giant execution policy is implemented or validated by this observation.


## Giant exact liveness with the fast linear allocator

The isolated giant-bitset01 prototype applies the completed liveness solution
only to AMD64 functions with at least 8,192 instructions, while retaining the
fast linear allocation and promotion/regional steps. This differs from the old
giant probes that switched to full greedy allocation. Ordinary functions remain
on retained leaf-density01; ARM stays unchanged. The matrix/work limits are
1,048,576 cells and 48 times the scalar budget, with complete fallback on failure.

Fresh local AMD64 compiler/encoder and guarded ARM compiler/runtime tests pass;
ARM arithmetic, table, medium-density and LZ4 oracles pass. Native compiler and
runtime, all 65/72 result and 21 semantic gates, and independent memory and
arithmetic checks pass. The completed four-module native screen measures
1.0440365550 times retained time: PCRE2 1.0272200829, yyjson 0.9659359937,
unchanged memory 1.1865665955 and Monocypher 1.0091572644. Memory has 1.2883
max/min spread. Opposing changed-module results and unchanged-code noise do
not establish a reliable gain. The prototype is unretained; no full run follows.
The static census matches all 65 inputs and changes only PCRE2
function60 and yyjson function18. PCRE2 grows 25,355 bytes, increases slots from
212 to 1,656, and grows its frame from 1,888 to 13,440 bytes. yyjson shrinks 5,245
bytes but slots rise from 340 to 515 and its frame from 2,848 to 4,240 bytes.
Exact liveness therefore does not imply fewer stack locations under this
allocator. No native speedup is established. Archive: giant-bitset-code-census-01.


Potential follow-up, not implemented: the fast allocator's physicalFree closure
still compares full interval spans and a promotedThrough endpoint when promoting
spilled call-crossing values. With segmented first-fit placement, a new occupant
in an otherwise dead hole can conservatively prevent such promotion. Giant01
reduces PCRE2 promotions from 146 to 75 and yyjson from 144 to 85. A separate
probe could compare exact ranges against both original occupants and earlier
promotions while retaining the span path for ordinary allocation. This is a
source-based hypothesis; the timing and frame changes do not prove causality.


## Exact promotion conflict follow-up

Giant-bitset02 implements the previous hypothesis in an isolated overlay. For
giant AMD64 functions with segmented ranges, the promotion pass tests exact
ranges against all original register occupants and separately stored previous
promotions. Ordinary functions and ARM keep conservative span checks. The new
helper passes 5,958 independent pointwise cases; a conservative-helper overlay
fails the hole case. This red overlay reproduces the old conflict rule rather
than running the original parent binary.

The complete 65-module static census changes only PCRE2 and yyjson. Against
giant01, promotions recover 75 to 121 / 85 to 123, and code shrinks 2,457 / 1,799
bytes. Slots are 1,665 / 491, still above retained 212 / 340. Native compiler,
runtime, 65/72 results, 21 semantic cases and independent memory/arithmetic
checks pass. Fresh local AMD64, guarded ARM and all four ARM oracles pass.

A diagnostic one-executable runner selects giant02, giant01 and retained policies
at process start and salts the artifact revision. All 195 input/native-code
hashes match their individual references. Native qualification passes for all
three modes. The completed six-permutation screen rejects giant02: time /
retained is 1.0124339833 and time / giant01 is 1.0099076058. PCRE2 / retained is
1.0418486597 and yyjson 0.9935580043; both are slower than giant01. Unchanged
memory is 1.0014560032, Monocypher 1.0135311919. No series exceeds 20% spread.
Giant01 / retained is 1.0011824804, with PCRE2 regression and yyjson improvement
still opposed. Neither is retained; no full comparison follows. The promotion
hypothesis is supported statically but fails to produce faster native execution.


Next broader lead, not implemented: the latest retained-versus-PR780 full run
has the largest time ratios in fib_rec (1.15895), yyjson (1.15432) and LZ4
(1.12149). See segmented-threshold-full-02/results.json, pr780/previous module
ratios; these retain that run's noise and placement limitations. The recursive
inliner currently expands one level of a small, pure integer self-recursive
function. A bounded second expansion could remove additional calls while
preserving the existing purity and type restrictions. The helper currently uses
one body both as the scan source and inline template; separate those roles before
attempting multiple levels, and allocate another parameter-local group per level.
This is a source-based lead, not a performance result or a special case for the
Fibonacci corpus artifact. Any implementation needs varied integer/multi-parameter
recurrence oracles and native code/compile-cost measurements.


## Second bounded recursive expansion

Recursive-depth01 implements the next lead on retained leaf-density01. AMD64
requests two expansion levels through the existing target-aware frontend; ARM
and targetless callers keep one. No eligibility restrictions are relaxed. The
scan body remains the original while the inline template expands; each level
has a distinct parameter-local group. Depth is restricted to 1 or 2, expanded
bytecode to 1,024 bytes. A one-level red overlay fails the two-level shape test;
the new shape has eight remaining call sites, three local groups and unchanged
source data. This is a general pure-integer transformation, not a corpus match.

All 65 static inputs agree with retained. Only fib_rec changes: 206 to 373
native bytes, 31 to 71 machine instructions, frame 48 to 64, zero spills. Fresh
local AMD64/ARM suites and full native compiler/runtime, 65/72 results, 21
semantic cases and independent memory/arithmetic gates pass. New independent
recurrence oracles each pass 24,552 calls on local AMD64, native AMD64 and ARM,
covering eight fixtures, mixed/four parameters, mutation, nested return,
branching recursion, void results, repeat calls, two bounds and workers 1/4.

A diagnostic executable fixes A82A_RECURSIVE_POLICY at startup and salts the
artifact revision. Both modes match the separate references in all 130 native
code/input comparisons and pass complete native qualification. Six alternating
paired rounds on Fibonacci, tiny, dispatch and memory complete with Fibonacci
at 0.7503852019 times retained time. The three unchanged controls are within
0.13%; no series exceeds 20% spread. The four-module aggregate is 0.9302269960,
which does not describe the complete corpus. Four paired compile rounds find
Fibonacci time 1.5692072778, bytes 1.3879939806 and allocations 1.1266968326
times retained. Median raw time is 0.309 to 0.484 ms; the retained dispatch
compile control has 20.11% spread. The full 65/72 PR780 comparison is running,
with candidate and retained sharing one executable and PR separately pinned.
The prototype remains
unretained pending that evidence. Production v77 remains unchanged.


## Third recursive level and bounded fallback

Recursive-depth02 requests three levels on AMD64. A completed-level counter
ensures a 1,024-byte cap hit retains the last complete expansion and declares
only its parameter locals. The 64-byte four-parameter/two-call cap fixture would
produce 1,072 bytes at level three; it keeps level two with twelve locals. The
new three-level shape test expects sixteen call sites and four local groups;
a two-level red overlay fails that check. Local AMD64 compiler/encoder and
ARM compiler checks pass. Nine independent recurrences including the cap case
pass 29,304 calls on local AMD64 and ARM. Only fib_rec changes across all 65
static inputs: 206 to 768 bytes, 31 to 151 machine instructions, zero to one
spill, frame 48 to 64, copies 2 to 14. Depth two was 373 bytes with no spill.
Native qualification has not started while recursive01's full run owns CPU0.

The first ARM full suite fails TestDarwinARM64InterruptStressAcrossHostTransitionsAndGC
at iteration 24: nil instead of context cancellation. This test uses MustCompile's
default Railshot (api.go/config.go/core compiler EngineRailshot=iota), outside the
modified recursive Dragline path. A retained-baseline 100 repetition probe passes.
Then 1,000 repetitions each reproduce 5 baseline and 4 candidate failed tests, all
with the same error. One fresh full candidate run passes. All initial/probe logs
and the source-pinned baseline overlay inputs are retained; the intermittent
runtime failure remains unresolved. These are failed test repetitions, not
failure counts out of a claimed complete 32,000 invocation set.


## Two-level full comparison complete

The full six-permutation 65-module/72-export run verifies candidate/previous
leaf-density01 time 0.9968223073, candidate/PR780 time 0.9614939472, and throughput
1.0400481490 times PR780 (4.00% higher). Fibonacci repeats at 0.7483693222 times
previous time; unchanged 64 geomean is 1.0012974000 and many_funcs 1.0009253547.
No candidate series exceeds 20% spread. PR QOI encode/decode and UTF-AS-SIMD
validation, plus previous JSON-AS-SIMD serialize/deserialize, exceed 20%; all
samples remain. The all 65 aggregate is authoritative, not the unchanged-code
sensitivity 0.9955505639. Candidate/previous share one executable; PR is separate.

Recursive-depth01 is now the retained experimental execution reference. The
0.318% paired aggregate gain and repeatable 25.16% Fibonacci gain justify its
bounded code/compile cost here: +167 native bytes, median compile 0.309 to 0.484 ms,
allocated bytes +38.8%, allocations +12.7%. Older 4.03%/4.30% leaf-density results
are different measurements, not current claims. The requested 50% target is
still unmet. No production promotion, commit or push occurred. The three-level
follow-up has started native qualification after the full timing job completed.


Three-level follow-up qualification is now complete: native compiler/runtime,
all 65/72 result gates, 21 semantic cases, independent memory/arithmetic gates
and the expanded 29,304-call recursive oracle pass. The initial ARM failure,
matched baseline/candidate reproductions and successful full rerun are preserved
and checked by the archive verifier. No native timing or compile-cost result
exists for recursive-depth02; recursive-depth01 remains retained.


## Recursive depth02: one-executable qualification and focused measurement

The three-level policy and retained two-level policy now share one diagnostic
executable. All 130 generated input/code hashes match the canonical references.
Both modes pass all 65/72 native result gates, 21 semantic cases and 29,304
independent recursive calls; inherited full-memory oracles and fresh local
AMD64/ARM checks pass. Production source SHA is unchanged.

Six alternating paired focused rounds give three/two time ratios Fibonacci
0.9254830630, tiny 0.9988729208, dispatch 0.9832729188 and memory 0.9989714964.
No timing series exceeds 20% spread. Dispatch is unchanged code and its movement
is not attributed to compiler improvement. Four paired compile rounds show
Fibonacci 0.493 to 1.296 ms (paired ratio 2.6419334649), 60.6% more allocated
bytes and 36.3% more allocations. Native code is 373 to 768 bytes with one
spill slot. Whole-process RSS is not an isolated compiler-memory measurement.

The full six-permutation 65-module comparison against the retained two levels
and pinned Go 1.22.2 PR780 is running. Three levels remain unretained and the
latest completed retained headline remains 4.00% higher throughput than PR780.
The 50% target remains unmet. See recursive-policy-single-binary-02,
recursive-policy-single-binary-screen-02, recursive-policy-compile-02 and
recursive-policy-full-02 in the performance archive.


## Three-level full result and bounded byte-copy follow-up

Recursive-policy-full02 completed all six variant permutations and all 65
modules / 72 exports. Independent verification recomputes paired ratios,
module balancing, actual order, exact output gates and source/input/binary pins.
Three-level time / PR780 is 0.9583632378 (4.35% higher throughput), two-level /
PR780 0.9593965785, and three/two 0.9984949841. Fibonacci three/two is
0.9199194531; unchanged64 is 0.9997745479. Neither Dragline has a series above
20% spread. PR QOI encode/decode, UTF-AS-SIMD validation and Zstd decompression
are noisy; all samples are retained. The shared-executable control applies only
to the Dragline comparison. Recursive-depth02 becomes the experimental reference
with the documented 2.64x compile-time and +395-byte Fibonacci code cost. No
production promotion or commit occurred. The full 50% target remains unmet.

Two new static body-rejection probes preserve all 65 generated code hashes.
The complete current census records 3,007 body rejections and 870 short
single-block candidates. Of those, 119 across seven modules lack only byte
load/store support relative to the existing parser. A new bounded affine parser
admits 55 copy loops (52 four-byte and 3 one-byte iterations) across CoreMark,
BLAKE3, QOI and zlib. Static counts are not runtime hit rates.

Byte-copy-native01 implements AVX2 32-byte transfers using the existing region
entry/exit bridge. Whole-range guards preserve scalar fallback for possible
traps, wrapping trip counts and overlapping ranges except identical starts.
Exit locals are evaluated at the last completed scalar iteration; scalar tails
use the original loop. Only the four admitted modules change generated code.
Fresh local AMD64 compiler/encoder and guarded ARM suites pass. Six fixture
variants pass 10,152 independent sequential-byte oracle calls, comparing full
memory, trap outcomes and three exit locals, with overlaps, short/tail counts,
large offsets, memory boundaries and repeated calls in two engines/two bounds
modes. Native qualification is running; no timing claim exists. The prototype
was based on recursive-depth01 before the three-level retention decision and
must be combined explicitly if it later proves useful.


Byte-copy-native01 native qualification has now completed successfully. All
compiler/runtime, semantic, complete corpus and inherited independent oracles
pass. A stricter second byte oracle verifies exact TrapLinMemOutOfBounds codes
as well as full memory and exit locals: 10,152 calls on native AMD64, local
AMD64 and ARM. Six static fixture checks prove admission and nonempty native
emission for one/two/four-byte iterations and large memory immediates. Its new
virtual Go file is included in the effective-source hash alongside physical
sources. The archive verifier passes. No execution timing exists yet; the next
step is one diagnostic executable selecting byte-copy or retained depth01 policy,
with exact static code equivalence and both modes qualified before timing.


## Byte-copy policy timing, costs and path evidence

Byte-copy-policy-single-binary01 fixes A82A_BYTE_COPY_POLICY at process startup,
salts the artifact revision and matches both canonical policies in all 130
source/code hashes. Both policies pass full 65/72 outputs and 21 semantic cases,
plus the exact 10,152-call byte oracle. Candidate native compiler/runtime and
all inherited oracles pass; local AMD64/ARM and six fixture admission tests pass.

Six alternating paired 300 ms rounds give copy/scalar time ratios CoreMark
1.0001938450, BLAKE3 0.9943535112, QOI 0.8727871891, zlib 1.0038156318, unchanged
tiny 0.9982459787 and memory 0.9979626926. QOI encode/decode ratios are
0.9291471720 and 0.8198458764. Candidate zlib inflate has 1.20926 max/min spread;
all other series stay below 20%. The focused six-module mean 0.9766895260 is
not a full-corpus claim. Four compile rounds show time near baseline; BLAKE3
allocated bytes rise 14.86%, allocations 13.94%, and native code grows 30,760 B.
CoreMark/QOI/zlib add 672/1,904/1,856 native bytes. Whole-process RSS is not
isolated compiler memory.

A separate instrumented native run records CoreMark 0 successes/264 short-loop
fallbacks, BLAKE3 62,106 successes/330 short fallbacks, QOI 3,415 successes/0
fallbacks and zlib no entries. These include harness setup/warmup/correctness
calls and are not per-guest-call counts. BLAKE3 successful trip counts are
8/11/15/16 scalar iterations; QOI has 1,228 at708 and 2,187 in a capped1024-or-more
bucket. Instrumented timings are excluded from performance evidence. The path
verifier recomputes totals and histogram agreement from raw output.

The combined byte-copy-policy-single-binary02 now uses three recursive levels
in both modes. Retained matches recursive-depth02 on all65; candidate matches
the copy prototype on64 and the three-level reference on Fibonacci. Fresh local
checks pass and native qualification is in progress before a full comparison.
Byte copying remains unretained; the current retained headline is still 4.35%
higher throughput than PR780, far short of 50%. Production code is unchanged.


Combined native qualification is now complete. Both modes pass all65/72 exact
outputs,21 semantic cases and the10,152-call exact copy oracle. Candidate full
compiler/runtime and inherited gates pass;29,304 independent recurrence calls
pass on native AMD64, local AMD64 and ARM. All archive verifiers pass. The
six-permutation full65/72 comparison is running in byte-copy-policy-full02;
no retention decision or new overall claim has been made.


## Compact byte-copy guard follow-up and store-only probe

While byte-copy-policy-full02 owns native CPU 0, compact-policy01 shortens only
copy-region range setup. Equal stride and width give length = count × width.
For a zero memory immediate, actual memory32 length is already at most 2^32,
so its end check also proves the address does not wrap. Nonzero immediates retain
both checks. Alias and emitted-loop eligibility rules are unchanged;
non-power-of-two widths fail preflight. Candidate, previous original-copy and
retained scalar policies are fixed at process startup and share three recursive
inline levels.

All 130 previous/retained code hashes match the combined reference. Candidate
changes only four modules and removes 2,840 native bytes: BLAKE3 2,488; CoreMark
40; QOI 152; zlib 160. Six emitted fixtures get smaller, and disabling compact
guards makes the size assertion fail as expected. Local AMD64 compiler/encoder
and guarded ARM suites pass. Expanded byte cases add low addresses exercising
the 65,528-byte immediate: 19,872 independent calls pass for each local AMD64
mode and on ARM, checking full memory, exact memory trap codes and exit locals.
Inherited ARM oracles and local AMD64 recursion pass. Native qualification has
not started; the existing full run continues on its original process handle.

A separate store-only planner probe allows zero-load plans through the final
finish check. All 65 generated code hashes remain identical to recursive-depth02,
so that condition alone yields no emitted corpus opportunity. It is unretained
and has no runtime or performance claim. Both new archives have passing static
or local verification scripts. Production source remains unchanged.


## Original byte-copy bridge retained after complete comparison

The six-permutation full run byte-copy-policy-full02 is complete and verified.
Copy/scalar time is 0.9989673378 across all 65 modules / 72 exports; copy/PR780
is 0.9558032564, or 4.62% higher throughput. QOI repeats a 12.98% time reduction
(encode 7.09%, decode 18.49%). CoreMark/BLAKE3/zlib ratios are
1.0034456359 / 0.9965816385 / 1.0050795002; unchanged 61 average 1.0010954944.
Candidate zlib and PR UTF-AS-SIMD exceed 20% sample spread. All samples remain;
the 0.10% full-corpus increment is small relative to variation.

Retain the original copy bridge experimentally based on repeatable QOI benefit
and complete correctness coverage, with costs explicitly recorded. Combined
compile02 shows changed-module time near baseline; BLAKE3 allocated bytes
increase 14.87%, allocations 13.98%, and native code adds 30,760 B. The current
source reference is combined single-binary02, candidate policy, SHA
96b6518a9e1a0a9934e21ebc7757cda29372fa991955812ca3e6a740aaeb7c21.
Production v77 remains unchanged. The 50% target is not met. Compact-guard
native qualification is now running; no compact timing or retention claim yet.


Compact-policy01 native qualification now passes and its archive verifier passes.
All three policies pass 65/72 results, 21 semantic cases and 19,872 expanded
copy-oracle calls each. Candidate compiler/runtime and inherited checks pass,
including 29,304 recurrence calls. The three-policy six-module focused screen
is running with six balanced permutations in one executable.


Compact-screen01 is complete and verified. Compact/original-copy time ratios
are CoreMark 1.0002694464, BLAKE3 0.9995129921, QOI 0.9901218349, zlib
0.9906928098, unchanged tiny 1.0009206035 and memory 1.0013985905. Six-module
mean is 0.9971410989; compact/scalar is 0.9745778554. QOI encode/decode ratios
are 0.9852672284 / 0.9950003610. Original-copy zlib inflate has 1.20042 spread;
no other series exceeds 20%. Earlier path instrumentation saw no zlib copy
entries, so this is not evidence of a zlib runtime-path improvement. Compilation
costs are running before a full comparison; compact remains unretained.


Compact-compile01 is complete and verified: four paired rounds of three compile
iterations for six modules. Changed-module compact/original compile ratios are
0.999503–1.001923; BLAKE3 allocated bytes and allocations decrease 0.658% and
0.528%. No compile-time series exceeds 20% spread. RSS remains whole-process.
Compact-full01 is running with six balanced permutations over 65 modules /
72 exports against original copying and matched PR780. Compact is unretained;
the original copy bridge remains the 4.62% experimental reference.


## Compact guards retained; wide exit maintenance follow-up

Compact-full01 is complete and verified. Compact/original-copy time is
0.9991393674 and compact/PR780 time is 0.9573916587 (4.45% higher throughput).
No timing series exceeds 20% spread. Changed BLAKE3/CoreMark/QOI/zlib ratios
are 0.9974529383 / 0.9995253070 / 0.9957089444 / 0.9951987473; unchanged61
average 0.9992817829. Retain the compact variant experimentally for its repeated
small QOI gain, 2,840 fewer native bytes and near-baseline compile cost. Earlier
4.62% and current 4.45% headlines are separate runs, not a paired regression.
Production v77 remains unchanged; 50% is unmet.

Wide-exit-census01 keeps all 65 code/input hashes identical to original copying.
It records 136 emission visits, 408 calculated exit values, 146 published values
and 146 needed step updates; 14 of 15 visited modules have unused exit values.
Visits can repeat and are not runtime hotness. Wide-exit-policy01 prunes only
exit-slot calculations using the existing publication predicate. Input snapshots,
step updates and fallback/bounds/alias rules are unchanged. All 30 focused copy
fixtures pass; baseline has 24 expected unused-result failures and six controls.

After local process handles and temporary files disappeared, pinned archived
sources and complete compiler/runtime logs were recovered. Partially archived
oracle/census logs are preserved; missing evidence is being rerun. The restored
shell inherited a Go1.27 GOROOT, causing immediate Go1.22 build failures before
execution. Those logs are retained; removing inherited GOROOT restores the
verified Go1.22.2 toolchain. Native qualification on hub is running separately.
Exit pruning remains unretained and currently uses original, not compact, guards.


Wide-exit local recovery is complete. Both local AMD64 modes and ARM each pass
99,360 independent cases. Candidate changes exactly 14 modules and saves4,976
native bytes; retained matches all65 original-copy code hashes and all130 inputs
agree. Native qualification passes both65/72 result gates,21 semantic cases
and99,360 copy cases per mode, plus inherited memory/arithmetic and29,304
recurrence cases. The16-module screen is running with all14 changed modules
and two controls, six alternating paired rounds in one executable. Both modes
use original copy guards; combination with compact guards is not yet measured.


Wide-exit-screen01 is complete and verified: 16-module time / original is
0.9924043290, with no timing series above20% spread. Heat-3D improves2.72%,
Jacobi-1D2.05%, Symm2.02%; ATAX regresses0.39%. Controls tiny/memory ratios
are0.9990710182 /1.0007023946. This isolates exit pruning against original copy
guards, not compact guards, and is not a full-corpus claim. Compilation costs
are running before combining changes and full-corpus confirmation.


Wide-exit-compile01 is complete and verified: changed-module paired time ratios
range0.98450–1.01256 with no>20%spread; allocation costs remain near baseline.
Wide-exit-policy02 composes exit pruning with retained compact guards. Local
AMD64/ARM qualification and both-mode65-module code comparisons are running.
The retained control must exactly match compact-policy01, and candidate changes
must be explained by the separately validated exit pruning. No combined timing
claim, native qualification or retention decision exists yet.


Combined policy02 qualification is complete and verified. Retained matches all65
compact-policy01 native hashes; candidate changes14 modules and removes5,040
additional bytes. Non-copy modules match separately qualified exit pruning.
Both native policies pass65/72 results,21 semantic cases and99,360 independent
copy calls each, plus inherited memory/arithmetic and29,304 recurrence calls.
Local AMD64 modes and ARM each pass99,360 copy calls. The initial local suite
failed only because temporary fixtures were missing; that log is preserved,
pinned fixture restoration makes the complete compiler/encoder suite pass.
All30 combined focused fixtures pass. Wide-exit-full02 is now running with six
balanced permutations versus compact-only and PR780. No combined retention or
new full-corpus claim yet; current retained result remains4.45%, target50%unmet.


Wide-constant-census01 is complete and verified with all65 native hashes and
input hashes unchanged versus combined policy02. Across136 emission visits,
530 I32 inputs include384 immutable locals but only8 qualifying constant locals,
all BLAKE3 and value65,536. Counts may repeat and are not runtime hotness. No
new constant-input optimization was added; this does not show broad opportunity.
The combined full comparison is still running on its original handle. Combined
compile-cost runner/verifier are prepared as wide-exit-compile02 for after that
native CPU0 job terminates.


Prepared native-profile07 refresh for qualified combined policy02 and pinned
PR780: YYJSON, LZ4, Blake-AS, raytrace, matmul and UTF-AS-SIMD. Earlier diagnostic
profiles predate later optimizations. The new profile job has not started and
must follow full02 and compile02 on native CPU0. Runtime PC retention and sealed
image mapping remain diagnostic overlays; instrumented timings are excluded
from performance acceptance. Full02 continues on its original live handle.


## Combined exit pruning retained after full measurement

Wide-exit-full02 is complete and verified. Candidate/compact-only time is
0.9981021002 and candidate/PR780 time is0.9546362014, or4.75% higher throughput.
Changed14 average0.9919246210; unchanged51 average0.9998045986. Numerical gains
repeat, with small ADI/ATAX/correlation regressions retained. No candidate series
exceeds20% spread; control JSON-AS-SIMD/Doitgen and PR Doitgen are noisy. All
samples remain. The full gain is small and the50% target remains unmet.

Combined compile02 is verified with changed time ratios0.97374–1.00194 and no
>20% spread; BLAKE3 allocated bytes decrease1.529%. Retain combined policy02
experimentally for the repeated numerical gains, modest full benefit and5,040
fewer code bytes. Sourcee43d5330b9ac94bb4f9361ff5633311b83bb5da947d98f2a688963f338d18876,
binary2e13586895c3fb272ed3871ad71e0a967d8b4bf15d33b58842c9e487338459a8.
Production v77 is unchanged. Native-profile07 is now running for seven modules
(including BLAKE3) after both acceptance jobs terminated. Instrumented timings
will not be used for speed claims.


## Fresh profile07 completed; SIMD branch follow-up

All fourteen profiles verify against exact sealed images and source/input pins.
UTF-AS-SIMD exposes a VPTEST/SETNE/MOVZX/TEST/JE sequence. An isolated candidate
removes only the materialization tail when its sole reader is the branch and
byte/source-map positions prove no intervening emission. Focused emission and
escape-rejection checks pass; corpus qualification and timing remain pending.
Profiles are diagnostic, not acceptance measurements. Production is unchanged.


## SIMD branch tail candidate not retained

Candidate source2ffac5b548b4ee3f44d02a9b8cdca9888f0dffe339dfcff66e2d3ec8bd672c59
passes native compiler/encoder/public-runtime, both policies'65-module/72-export
and21-semantic coverage, and20,800 varied branch-oracle calls per policy. Local
ARM64 checks pass. The emitted-code census matches the retained reference in all
65 controls; candidate changes only JSON-AS-SIMD and UTF-AS-SIMD, minus32bytes each.
A disabled-transform test fails as intended; the enabled candidate passes.

Six paired500ms rounds give changed-module ratios0.997818281 and0.997887543,
comparable to unchanged control movement. Six paired1s rounds give1.002118298
and0.999272244. No series exceeds20% spread. Compile ratios are1.001922904 and
0.998045262, with allocations near baseline. Keep all unfavorable measurements;
these screens do not establish an execution gain. Candidate remains unretained.
The accepted full-corpus figure remains4.75% higher throughput than PR780;50%
is still unmet. Production v77, HEAD and the worktree's existing edits are unchanged.

Executed-image follow-up confirms the intervention in the exact profiled UTF-AS-SIMD
image: retained0x3c7f VPTEST/SETNE/MOVZX/TEST/JE becomes candidate0x3c5f VPTEST/JE.
The separately hot16-byte vector-count loop remains byte-identical after relocation.
All jobs terminated; profile, candidate, timing, compile and image archives verify.


## Affine conversion loops: qualified candidate and full comparison

A code-preserving census finds three initialization loops excluded by i32-to-f64
conversion: matmul and Jacobi1D/2D. The isolated candidate keeps four modular i32
lanes in registers and uses packed signed conversion. Unsigned inputs require a
whole-loop widened range proof at most INT32_MAX; other inputs take the original
scalar loop. FP trees keep operand and operation order. All previous memory and
alias guards remain. All other62 modules retain identical emitted code.

Source87c7a3961277abef4c10ba3c190aee9d2d7598773cded39a8fcd45aabe5d0d03 passes
native compiler/encoder/public-runtime, both policies'65/72 and21-semantic gates,
30,240 independent conversion-oracle calls, and exact full-memory comparisons
for ten numerical modules. Local ARM64 checks pass. The code increase is4384bytes.
Six paired500ms rounds measure candidate/control0.9032934144 for matmul,
0.9476776177 for Jacobi1D and0.9680113279 for Jacobi2D, with unchanged controls
near1 and no>20% spread. Full01 is now running with six balanced permutations
against its control and pinned PR780. Compile cost follows on the same native
CPU0 after full measurement terminates. Production v77 is unchanged.


## Broader conversion admission probes

Code-preserving shape/rem-census01/02 compare all65 native hashes to conversion01.
The shape census records85 loop visits in29 modules;27 straight remainder loops
span14 modules. Adding rem_u reconstruction/parsing yields zero additional native
regions:26 bodies encode, only ATAX and one Doitgen parse, and neither meets
independent-lane admission. 2MM WAT inspection shows runtime-invariant induction
increments and reversed adjacent store order. These need explicit new proofs.
A remainder emitter by itself cannot unlock the broad set. The full conversion01
comparison continues on its original process, without overlapping native jobs.


## Conversion01 retained after complete execution and compile comparison

Full01 is complete: six balanced permutations, all 65 modules / 72 exports,
matched Go 1.22.2, CPU0. Candidate/control time is 0.9968308058; candidate/PR780
time is 0.9527559721, or 4.96% higher throughput. Matmul improves 10.81%,
Jacobi1D 5.08% and Jacobi2D 3.60%; unchanged62 average 0.9999493701. Candidate
yyjson, control JSON-AS serialize/Doitgen and PR UTF-AS-SIMD exceed 20% sample
spread. All raw samples remain. Prior 4.75% is a separate-run headline.

Four alternating compile pairs put changed-module times at 0.9932–1.0262 times
control, with 4.36–5.22% more allocated bytes and 2.15–3.21% more allocations.
No compile series exceeds 20% spread. Native code grows 4,384 bytes. An extra
138 varied matmul oracle calls per policy match exact results and full memory.
Retain conversion01 experimentally for repeated gains and modest compile cost.
Source: 87c7a3961277abef4c10ba3c190aee9d2d7598773cded39a8fcd45aabe5d0d03.
Binary: fe9e5f0df89203bfcf26cac8e8bcd72dcffbe676f1ed4294288370d74ff76b13.

Candidate, full comparison, compile and admission-probe archives verify. All
jobs are terminal. Production v77 remains unchanged, uncommitted and unpushed;
the 50% target remains unmet. Next investigate paired affine conversion outputs
with explicit ownership and lane-order proofs before native emission.


## Paired conversion probes and isolated descending-store prototype

Both paired shape probes preserve all 65 native code/input hashes. Equal-stride
conversion matching alone adds zero shapes. Normalizing descending adjacent
stores on a diagnostic copy finds one: Seidel2D function1, start82. ATAX/BiCG
carry FP recurrences and remain outside the proof. Both 85-visit probes verify.

An isolated paired-conversion prototype now reconstructs low/high affine integer
lanes explicitly and normalizes descending stores transactionally. Full bounds
checks justify the paired fast-path store; failed guards retain original scalar
order and partial-write behavior. Twelve admission fixtures pass, as do 72,576
native independent-oracle calls per policy. Native compiler/runtime and local
ARM checks pass. Emission changes only Seidel2D, adding 960 bytes; all retained
hashes match conversion01. Complete corpus gates are in progress, timing pending.
The prototype is unretained; production and the 4.96% full result are unchanged.


## Paired conversion qualification complete; prototype unretained

Both policies pass all65/72 and21 semantic gates, 72,576 independent calls each,
and exact full-memory comparisons for11 numerical modules. Native compiler/runtime,
local ARM and12 paired-fixture admission checks pass. Source is
c6f3e7ea47ef84628ee8408f5d851bf8543fe05c8388ec5cefea89ae62799d5e; binary is
cffa640f6aa52c18ebb1788c1d60de2f8bba43e766c6d0e143b380edae85b6d8.

Six paired1s rounds give Seidel time0.9938783606; six paired2s rounds give
0.9969977163. Unchanged controls move by comparable amounts (first matmul1.011592,
Jacobi2D0.995708; second matmul0.997576, Jacobi2D1.003722). No series exceeds20%
spread. Four compile pairs give Seidel time1.013649, bytes1.017931 and allocations
1.020539; no compile series exceeds20%. Same-executable policy comparisons do
not capture common implementation overhead. Native code grows960bytes.

Keep all results, but leave paired conversion01 unretained: focused benefit is
small and comparable to control movement, and no full-corpus gain is established.
The admission, qualification, both timing and compile archives verify; all jobs
are terminal. Physical production Go hash remains
41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.
No promotion, commit or push. The retained full result remains4.96% higher PR780
throughput and the50% target remains unmet. Runtime-invariant induction remains
the next prerequisite for broader remainder/conversion coverage.


## Dynamic induction and unsigned remainder: qualified broad candidate

A code-preserving diagnostic finds24 additional loop shapes in13 modules after
allowing affine increments formed only from invariant locals. Constant trip-counter
and memory-stride proofs remain. The candidate combines this with paired conversion
and exact unsigned remainder via guarded binary64 division/floor/multiply/subtract.
Whole-loop dividends must stay withinINT32_MAX and divisors be nonzero/invariant;
failed guards retain scalar trap and partial-write order. FP trees keep their order.

Thirty admission fixtures, six rejection/transactionality fixtures, one million
independent arithmetic-model cases and151,200 native oracle calls per policy pass.
Both policies pass all65/72 and21 semantic gates, native compiler/runtime, localARM,
and exact full memory for20 numerical modules. The old GEMM binding test assumed
all regions were the original kernel; it now explicitly checks that kernel remains,
while new initialization exits are covered by returned-state/full-memory oracles.
The initial test failure is preserved. Source6b3975d3495292a5442b311fae39bbbd589263f405e0930ca790b451df7312c5;
binary88c2473336f2055dd1cbdbbfb439c59503fac8f19e62a63f45ee72762d4a2432.

Census changes14 modules (including paired Seidel), adding29,072bytes; other51
are identical and all65 control hashes match conversion01. Six paired500ms rounds
measure ATAX0.790494, BiCG0.799590, Gemver0.885902, Gesummv0.697552, MVT0.801821.
Doitgen1.004478 regresses; its control spread is1.396839. All samples remain.
Three unchanged controls are near1. Four compile pairs compare both same-executable
control and the prior matched Go1.22.2 binary. Changed14 versus prior average
time1.0038803203, bytes1.0340357466, allocations1.0307606818, without>20%spread.
Unchanged memory is1.057586 times prior compile time. RSS is whole process.

The full65/72 six-permutation comparison started after all other native jobs
terminated, usingCPU0,300ms/export and pinned PR780. It is live on the original
handle; do not restart on observation timeout. Candidate retention awaits completion.
The retained headline stays4.96% higher throughput,50%unmet. Production unchanged;
no promotion, commit or push. Completed qualification, census, screen and compile
archives verify. This turn made progress through implementation and new evidence.


## Dynamic01 retained after verified full comparison

Full01 completed all six balanced permutations and verifies against raw samples,
source/binary/input/toolchain pins, complete 65/72 coverage and qualification gates.
Candidate/control time is 0.9791169968; candidate/PR780 time is 0.9330165086,
or 7.18% higher throughput. Changed14 average 0.9097549632; unchanged51 average
0.9990661402. ATAX/BiCG/Gesummv/MVT gains repeat. Doitgen retains a small regression.
No candidate series exceeds20% spread; control dispatch/Doitgen and PR Doitgen do.
All raw results remain. The unchanged-code sensitivity is 0.9798350144.

Retain dynamic01 experimentally for the repeated gains and 2.09% full time decrease,
with modest compile-time cost, 3.4% average additional allocated bytes and 29,072
additional native bytes. Prior4.96% is a separate-run headline. The50% target
remains unmet; production is unchanged, uncommitted and unpushed.

While full measurement ran, a code-preserving census found24 active constant
remainder conversions across11 modules. A new packed-integer prototype proves
round-up reciprocal quotient equality on guarded x<=INT32_MAX, then uses exact
integer remainder before f64 conversion. The independent arithmetic model passes
2,948,709 cases; the Go helper passes1,573,199. Initial independent-stream emission
exceeded the register budget; sharing bit-identical immutable FP constants fixes
that without removing guards. All92 local constant fixtures now emit. All65 control
hashes match dynamic01; candidate changes only11 expected modules. Native
qualification has started after full01 terminated. No follow-up timing claim yet.


## Constant vector remainder qualified; full comparison started

Source0a39897be10307ca99cc49783b694bcdec8306e26decd1da1878e47494e2a973 and
binary12f77e555dd05f294bee7d3eb616472ff7bc94ef350852ffa90aa24e4604797e pass both
policies’65/72,21 semantic and463,680 independent oracle calls, plus native
compiler/runtime, localARM and20-module full-memory gates. The candidate changes
11 modules and adds1,336bytes; all65 control images match retained dynamic01.

Six paired500ms rounds give ATAX0.832277, BiCG0.839457, Gemver0.881826,
Gesummv0.789337 and MVT0.871608. All11 changed modules improve; controls remain
near1. Candidate/control Doitgen spreads1.385869/1.380580 are preserved. Four
compile pairs versus prior give changed11 time0.9973069121, bytes1.0011764351
and allocations1.0000698401; no compile series exceeds20%spread. RSS is whole
process, not isolated compiler memory. Completed archives independently verify.

The full65/72 six-permutation comparison started only after qualification, screen
and compile jobs terminated. It is the only live native job, on its original
handle. Retention awaits completion; the current retained score remains7.18%
higher PR780 throughput and the50% target remains unmet. Production Go source
is unchanged; no promotion, commit or push.

## Constant vector remainder retained; vector rotates under qualification

The full original job completed and independent verification passes all65/72,
six balanced permutations, matched Go1.22.2, source/binary pins and raw samples.
Candidate/control0.9848653866, candidate/PR7800.9213376029, throughput/PR780
1.0853784724 (8.54% higher). Changed11 average0.9162084651; unchanged54
0.9994696732. All11 changed modules improve. Noise>20%: candidate drwav1.22985;
PR Doitgen1.37089 and UTF SIMD1.34154; control none. All samples remain.
Retained experimentally based on repeat gains, correctness and near-baseline
compile costs. Production unchanged, no promotion/commit/push;50% remains unmet.

The logging-only vector-rotate-census-01 finds224 eligible common-source,
single-use complementary i32x4 shift/or pairs in Blake SIMD functions7/8,112each.
All65 emitted-code/input hashes match the retained parent. The isolated
vector-rotate-01 prototype uses an explicit AMD64 rotate operation before
scheduling/allocation, gated on AVX512VL, and preserves emitted ISA requirements
through serial, cached and parallel compilation. Local selector/rejection,
encoding and feature/cache tests pass; Clang independently matches the encoding.
Candidate/control census changes only Blake SIMD,35811 versus38019bytes.
Native oracle/compiler/runtime/corpus qualification started after the full job
terminated. It is the only native CPU0 job. No timing or retention claim yet.

Vector-rotate-01 native qualification completed:88,704 independent full-memory/trap
calls per policy, both policies65/72+21 semantic coverage, native compiler/runtime
and local guardedARM64 pass. Final source d88f40b1a68bfe14383b36ab49b71aa0e0fe5fe7db2f3aee98e518fe03a7daa2,
binary f0f137dd61bb970067ff4a74941f2a57e702fedea01f0cfbdf6079fc2824384f.
The archive verifier checks source, native gates, local feature/cache tests, all65
code/input hashes, Clang encoding and archive hashes. The focused six-round1s
paired screen started after native qualification terminated. Production physical
Go hash remains41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.

Vector-rotate-screen-01 completed and independently verifies: six alternating1s
pairs, Blake SIMD0.9622421691 (3.78% less time), Blake scalar1.0009721217,
Blake3 0.9982222255, UTF SIMD1.0036201960, memory1.0003289834. No series exceeds
20% spread. Compile-cost measurement started only after this screen terminated;
it is the only native CPU0 job. The full retained PR780 score remains8.54% higher
throughput; this focused result does not replace that score.

Vector-rotate-compile-01 completed and independently verifies four rounds of three
compilations across one changed module/four controls. Blake SIMD candidate/prior
time0.8023710586, bytes1.0073767729, allocations1.0015906681; same-executable
time0.8009233135. No time series>20% spread. Whole-process RSS is not isolated
compiler memory. The full65/72 six-permutation matched-Go1.22.2 comparison then
started on its original job and is now the only live CPU0 job. It remains
unretained pending that result. The verified retained score remains8.54% higher
PR780 throughput;50% is unmet.

## SIMD Boolean tree prototype while rotate full comparison runs

Logging-only vector-boolean-census-01 preserves all65 code/input hashes and finds
85 possible single-use Boolean trees:JSON SIMD4,Blake SIMD8,UTF SIMD73. Counts
overlap and do not establish hotness. The follow-up vector-boolean-01 is based on
qualified rotate01; its full performance decision remains pending. It admits51
actual contractions (JSON2,UTF49); rotate selection already consumes Blake's8.
The candidate changes only JSON SIMD/UTF SIMD, with UTF96bytes smaller and JSON
unchanged in size. All65 control images match rotate01.

A selected opaque ternary opcode preserves arbitrary Boolean truth tables and
noncommutative andnot input order. Shared producers are rejected. Destructive
destination aliasing is handled by permuting the truth table, exhaustively checked
for256tables/two swaps/eight inputs.128Wasm fixtures pass synthetic-target ISA,
cache and parallel compilation checks. Local ARM64 oracle passes286,720 exact
full-memory/trap calls and guarded compiler/runtime gates pass. This validates
fixtures; actual AMD64 sequences still require native qualification after the
sole live rotate full job finishes. Frozen source
dc0092b02951fbce5607be769c2e68ae3b4c89cc3a6d24d37302aecf571842a3.
No timing or retention claim yet. Production Go hash remains
41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.

## Vector rotates retained after full comparison

The full original process terminated successfully; independent verification
checks all65/72,six balanced permutations,raw ratios and source/binary/toolchain
pins. Time/control0.9993674535 (0.063% less),time/PR7800.9189740075,throughput/PR
1.0881700591 (8.82% higher). Changed Blake SIMD0.9625786703 repeats the focused
3.74% reduction; unchanged64 average0.9999532972. Noise>20%:candidate JSON
serialize1.27922,JSON SIMD serialize1.34737;control Doitgen1.39415;PR none.
All samples remain. Diagnostic unchanged=>1 ratio0.9994134109. Earlier8.54%
headline is a separate run,not the paired benefit from this narrow intervention.
Retained experimentally based on repeat execution gain,complete correctness,
2208bytes less code and19.8% less compile time with0.74% more allocated bytes.
50% remains unmet. Boolean native qualification started only after the full job
terminated; it is the sole live native CPU0 job. Production unchanged.

Boolean native qualification completed and all gates pass:286,720 exact
full-memory/trap calls per policy,both policies65/72+21semantic cases,compiler/
encoder/runtime and localARM. Sourcedc0092b02951fbce5607be769c2e68ae3b4c89cc3a6d24d37302aecf571842a3,
binary2fd233ae827981340c31ef0d6f2f35ba3a4a5f0c026329c451f07c4baa95ebfa,Go1.22.2. The original qualification job terminated;
vector-boolean-screen-01 then started six alternating1s pairs on CPU0 across
JSON SIMD,UTF SIMD and JSON scalar/Blake SIMD/Memory controls. It is the only
live native job. No performance result or retention claim yet.

## Boolean regression rejected; constant-operand qualification underway

The six-round Boolean screen verifies UTF validation time/control 1.0792275004
and UTF module 1.0386771628. JSON SIMD is neutral at 1.0006146519; controls remain
near one. No candidate series exceeds 20% spread; retained JSON SIMD deserializeN
has spread 1.5126997812. All samples remain. The optimization is not retained;
its precise microarchitectural regression cause is unproven. No full or compile
run was started. Retained rotate01 remains 8.82% above PR780 throughput.

The two logging-only constant censuses preserve all 65 code/input hashes. The
first finds 76 same-block, nonzero single-use constants; the second identifies
28 planned hoists (JSON11, Blake2, UTF15), leaving 48 UTF opportunities. The new
vector-constant-01 excludes every planned hoist and folds the remaining constant
into an existing RIP-memory instruction. Only UTF changes, by -224 bytes.

Source c96f25f88e223bd994e253809b9695f693f8e37c64eb2725bb053da990ae019b
passes 120 local fixtures (44 folds), rejection cases, cache/parallel checks,
guarded ARM64 tests and 268,800 independent oracle calls. Both native policies'
268,800 oracle calls and compiler/encoder/runtime tests also pass. Corpus gates
are still running on the original native job; no performance claim yet.

Constant-operand native qualification completed successfully. Both policies pass
268,800 independent full-memory/trap calls, all 65 modules/72 exports and 21
semantic cases, plus compiler/encoder/runtime tests. Frozen source
c96f25f88e223bd994e253809b9695f693f8e37c64eb2725bb053da990ae019b,
binary 400287554c49a194cc3b6cb9aa3b688db99c35e43b6bf1de38b701249d0e2945,
Go1.22.2. The archive verifier checks source, exact raw gates, 65 unchanged
control images, code changes and local feature/cache/rejection coverage.
The six-round 1s paired screen started after qualification terminated. UTF SIMD
is the changed module; JSON SIMD, Blake SIMD, UTF scalar and Memory are controls.
It is the only live native CPU0 job. The retained score remains 8.82% above PR780,
and the 50% target is unmet. Production remains unchanged.


## Constant-operand regression rejected; validation profiling

The original screen job terminated successfully. Six alternating paired 1s rounds
produce UTF SIMD time ratio 1.0377504017, validation 1.0767115266, conversion
1.0001990967. JSON SIMD 0.9988154183, Blake SIMD 1.0009745527, UTF scalar
1.0005675415 and Memory 1.0007004360 are unchanged controls. Candidate has no
series above 20% spread; retained Blake SIMD has max/min 1.2457797919. All samples
remain. Constant01 is rejected, and no full or compile-cost run is started.
Both Boolean01 and Constant01 regress validation while preserving conversion.
Their common cause is not established; inspect executed native instructions before
another change. Retained rotate01 remains 8.82% above PR780, target unmet.


## Validation profiles and aligned literal prototype

Native-profile08 completed and independently verifies exact qualified compiler
image prefixes for rotate01, Boolean01 and Constant01. Their validation function6
accounts for 915/918, 976/980 and 983/987 native samples; all native PCs map to
instruction boundaries with +1 correction. These diagnostic timings are excluded
from acceptance. Instruction/data placement and opcode changes remain confounded;
no causal regression claim is made. Literal data addresses are unaligned.

Vector-literal-align01 places each unique 16-byte SIMD literal at a 16-byte
boundary after executable function code. Function entry alignment supplies the
absolute guarantee. Deduplication, instructions, schedules and allocation remain.
Source 63dcd4c1938facafad785ed9ca90ae822adc25f98963f1fe058f7b758bbe74e7.
Only Blake SIMD, JSON SIMD and UTF SIMD images change; total code grows13 bytes.
All65 retained images match rotate01. Local unit/cache/parallel/ARM gates and
268,800 oracle calls pass. The first new test's overstrict byte equality assumption
fails on the unchanged serial/parallel control; the corrected test checks both
outputs' actual literal alignment, with original failure evidence preserved.
Native qualification is running; no performance claim or retention decision.


Literal-align01 qualification terminated successfully: both native policies pass
all65/72 exports,21 semantic cases and268,800 oracle calls. Binary
 a80bdaa5924c48fc6a561baa4d147054916ad13b2311a627460acd83c837f150 (Go1.22.2).
The focused six-round1s screen verifies UTF SIMD0.9835446125 (validate0.9629252095,
convert1.0046055449), JSON SIMD0.9976696098, Blake SIMD0.9975554063,
UTF scalar0.9997410074 and Memory1.0005416706. No series has >20% spread.
All raw samples remain. Full65/72 six balanced permutations versus same-binary
control and pinned PR780 is running on CPU0; no aggregate or retention claim yet.


The local UTF planning diagnostic is separately archived as utf-validation-plan01.
It preserves the full module hash and matches the chosen5,693-byte function6 body
to the actual profile08 image. A5,712-byte trial did not match and is not used as
execution evidence. The four hot loaded vectors have stack base locations, but
regional fragments mean this alone is not proof of a register-allocation defect.
Replaying the old SIMD-branch matcher with rewriting disabled matches VReg39;
that existing rejected optimization already covers this hot branch. No new
branch change is justified by this diagnostic.


While full measurement runs, a possible higher-impact direction is numeric
compute loops with independent outputs. The pinned GEMM Wasm has an inner loop
that updates two adjacent f64 outputs using a shared scalar load and separate
multiply/add operations, then advances16 bytes. Existing capture only accepts
single-block loop bodies and can pack adjacent outputs, so first census whether
this compute loop is already admitted and, if not, its actual rejection reason.
This is exploratory, not a claimed optimization opportunity or implementation.
Any future vectorization must preserve each output's FP operation order (no FMA
or reassociation) and guard the entire memory footprint/alias relation before
reordering stores, with original scalar fallback for failing guards.


## Aligned literal data retained after completed full and compile measurements

The original full job and subsequent compile job both terminated successfully.
Six balanced65/72 rounds verify candidate/control0.9994473342, candidate/PR780
0.9195803910, throughput/PR1.0874525053 (8.75% higher). Changed UTF SIMD0.9833984208,
validation0.9648778014, conversion1.0022745395; Blake SIMD0.9972804317,
JSON SIMD1.0000453042. Changed3 geomean0.9935479380; unchanged62 geomean0.9997336756.
Unchanged->1 sensitivity0.9997012923; exclude many_funcs0.9994145852;
many_funcs1.0015455004. Aggregate time gain is only0.055%; do not overstate it.
Prior8.82% headline is a separate run, not a paired regression.

Noisy raw series retained: PR Doitgen1.4366821267 and LU1.9966690708;
candidate Jacobi2D2.0718871085, LU1.5651013262 and LUdcmp1.6123565357;
control Doitgen1.3841233573, Jacobi2D1.8675972320, LU1.9745621426 and
LUdcmp1.4430004386. All are unchanged-code modules.

Four paired3-iteration compile rounds show changed-module time/previous:
UTF SIMD1.0009492674, JSON SIMD1.0024622878, Blake SIMD0.9973104838;
allocated bytes/previous0.9999121639,0.9999520221,0.9999993993.
Unchanged Memory time is1.0792206754 vs previous and1.0305609118 vs same-binary
control. No compile-time series exceeds20% spread. RSS is whole-process peak.

Retain literal-align01 experimentally for its repeated changed-code gain,
complete qualification and13 additional native bytes, with near-neutral changed
compile costs. Source63dcd4c1938facafad785ed9ca90ae822adc25f98963f1fe058f7b758bbe74e7;
binary a80bdaa5924c48fc6a561baa4d147054916ad13b2311a627460acd83c837f150.
The50% goal remains active/unmet. Physical production Go source remains
41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.
No promotion, commit or push occurred. No native measurement job remains live.


## Scalar local-carried reduction investigation

A code-preserving compute-loop census (compute-loop-census-01) matches all65
retained literal-align01 code/input hashes. GEMM already selects its compute loop.
Native-profile-09 confirms YMM arithmetic/store execution:918 of921 samples in
native code,643 in function1 and275 in function0; six PCs remain unaligned after
the+1 adjustment. These profiling timings are diagnostic only.

The new scalar-local01 overlay delays repeated stores of local-carried FP
reductions to one invariant cell under whole-loop range/alias guards, preserving
all FP node/operand order. Its source is
3907246133a7fcba7c6ceacc0a34f20f9adcb18bffe3e98c68e5bebe5928b6a2;
binary d2e2e40d70fffec49b46c9a281956f90d72e6592ccc7c1db79d9f608c7cbabf7.
Both native policies pass407952 independent oracle calls,65/72 corpus exports,
21 semantic cases and compiler/runtime gates. Local ARM oracle407952 and
compiler/runtime pass. 24 positive and6 rejection fixtures cover admission,
original fallback, cache and4-worker compilation. Retained-policy65 code hashes
match literal-align01; six candidate kernels change by10768 additional bytes.
Performance qualification is pending in scalar-local-screen-01. Production
source is unchanged; literal-align01 remains the retained reference until the
new measurements support a decision.


### Scalar-local01 rejected after paired native measurements

Six alternating1s rounds complete. Candidate/control execution ratios:
Cholesky1.0841441591, LU1.0837174037, Trisolv1.0989308760,
2mm1.2781071075,3mm1.2894012912,Atax1.0560771254.
Unchanged GEMM1.0016052680 and Memory1.0003224246 are near neutral.
No series exceeds20% max/min spread; all samples retained.
No full-suite acceptance run was started for this regressing prototype.

Four paired3-iteration compile rounds complete. Changed-module time/control:
Cholesky2.0106332250,LU1.9091973200,Trisolv1.9669046264,
2mm2.5570718915,3mm2.1230432666,Atax1.8918178708.
Allocated bytes/control range1.3918–1.6764. Prior-reference executable comparisons
agree. No compile-time series exceeds20% spread. RSS is whole-process peak.

Reject scalar-local01; retain literal-align01. This establishes regression but
not its mechanism: guards, larger generated bodies and register allocation
remain hypotheses. Future work should inspect generated hot-loop code before
revisiting a similar transform. Both measurement sessions terminated0; archive
verifiers recompute exact coverage/order and all ratios from raw logs.
Production all-Go SHA rechecked unchanged:
41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.
No promotion, commit or push. No native job remains live. The50% goal remains
active/unmet; last retained full-suite result remains8.75% higher throughput
than PR780, not updated by this focused screen.


## Direct native scalar-local01 follow-up

Native-profile-10 verifies exact retained/rejected 2mm code-image prefixes and
raw PC attribution. Rejected scalar-local01 executes its store-free transformed
FP loop; repeated address generation and expanded control remain visible. The
profile does not prove causality. Native counts980/883 (retained/rejected),
with4/2 unalignedPCs retained, diagnostic only.

A direct native scalar loop prototype (native-scalar-local01) avoids the Wasm
rewrite, retains every FP node in its own XMM register and keeps input streams
in GPRs. It reuses whole-loop validity checks and strictly rejects destination
aliasing; original machine loop remains fallback. FP operand/order semantics are
unchanged. Capacity<=14 FP nodes; unsupported shapes fall back. Same six modules
change,14776 extra native bytes; all65 retained-policy code images match
literal-align01. Source01101626a4330ba7e13080328591774c97aa6c61149643a9d47428ebc9d0b600.
Local24-positive/6-rejection admission/cache/parallel tests pass. Both native
policies and localARM pass407952 independent ordered-FP oracle calls each.
Native compiler and localARM compiler/runtime pass. Native runtime/corpus gates
and performance qualification still pending at this checkpoint. Production
remains unchanged; no retention claim.

Direct native scalar-local01 qualification completed: both native policies pass
65/72 corpus and21 semantic gates; full compiler/runtime pass. Frozen binary
be0e773aadc4ef3ebc5ce093a529f134608c308805b842a4f82464132678eba4.
Archive verification passes source/code/toolchain/coverage and pins. Focused
six-round alternating1s benchmark started; no measured result yet.


### Direct native scalar-local01 rejected

Six paired1s rounds: Cholesky1.0876829651,LU1.0855782989,
Trisolv1.1282844325,2mm1.3008879238,3mm1.2454049561,Atax1.0777227883.
Unchanged GEMM0.9993807871,Memory0.9997870936. No>20%spread; all samples
retained. All six changed modules regress; no full acceptance run started.

Four paired3-iteration compile rounds: changed time/control0.99725–1.01236,
time/previous0.99952–1.01379; bytes/control1.01988–1.05455. No>20%compiletime
spread. Direct emission avoids the earlier rewrite's large compile cost but
still provides no execution gain. Reject, retain literal-align01. Measurements
do not isolate the cause; do not assume removed stores were the bottleneck.

Both native sessions completed0 and all archive verifiers pass. No native job
remains live. Source/binary archived and rejected; production unchanged, no
commit/push. The50% target remains active/unmet; last retained full65/72 result
is8.75% higher throughput vs PR780. Next investigate parser/integer-heavy gaps
(yyjson1.16092,LZ4 1.10227 retained time/PR in the prior full run), checking prior
experiments before implementing another hypothesis.


## Odd constant divisibility01

The integer-profile-reuse01 check matches current retained code prefixes for
YYJSON,LZ4,Blake-AS andBLAKE3 to archived native-profile07 images. LZ4's hot
function2 computes a bounded remainder by3 only to test zero. A new isolated
finalizer replaces odd-constant unsigned remainder with a modular-inverse
multiply/compare predicate only for one Boolean consumer and no SSA/result
escape. All numeric/multiple-use/even/zero divisors preserve prior lowering;
64-bit scratch conflicts also fall back before emitting any code.

Source694fbef26fd2491b8425ad694a53bed09b1f23874a559f847cf77e949d320952.
Pure inverse/predicate, consumer rejection and scratch-alias tests pass.
Both native policies and ARM pass9561888 independent integer oracle calls each,
including99360 divide-by-zero traps across300 functions, both engines/bounds
and1/4 workers. Native compiler/runtime and ARM suites pass; native corpus
qualification still finishing. Both65-module censuses complete: retained hashes
all match literal-align01, candidate changes onlyLZ4 (5174->5174) and zlib
(38160->38141). Performance pending; production source unchanged.

Divisibility01 native qualification completed under both policies, including
65/72 and21 semantic cases. Qualified binary
f5b1dda3caa2480a79ba9e4a1f52a171bae9ea3726e372d25b87f50c3ca949ec.
Source archive verification passes. Focused six-round paired screen is running
on LZ4,zlib and unchangedYYJSON/Memory, all prior retained policies fixed.


Divisibility-screen01 completed0 and independently verifies six paired1s rounds:
LZ4 module0.9517570732, compression0.9090231455 (9.10% less time),
decompression0.9964999582; zlib0.9969446735. UnchangedYYJSON1.0039400236,
Memory0.9981595112. No>20%spread; all raw samples retained. Compile costs are
running before a full65/72 comparison; no retention or new aggregate claim yet.
Live gh PR780 check confirms MERGED with unchanged head efa9aa22dfb3781284a55c465f7f57544eafade3.


Divisibility-compile01 completed0 and independently verifies four paired
3-iteration rounds. Time/control:LZ4 0.9957428739,zlib1.0021508468;
allocatedbytes/control0.9998921680/0.9999965431. UnchangedYYJSON0.9990755142,
Memory1.0007873630. No>20%compiletime spread; RSS whole-process only.

Historical running checkpoint (superseded by completion below): divisibility-full01 was live on ORIGINAL exec session56056. Last poll reports
round1 through memory_tree and confirms the handle is still running. Six balanced
PR780/candidate/same-binary-retained permutations,65 modules/72 exports,300ms.
Do not start another nativeCPU0 measurement or restart this job on timeout.
Remote/home/hub/wago-dragline-a82a-20261003/divisibility-full-01;
runner/tmp/a82a-divisibility-full-01.py. Local analysis/verifier/README and a
running-checkpoint.json are prepared; no full results or retention yet.
All completed source/screen/compile/profile-reuse verifiers pass. Physical
production Go SHA remains41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.
Goal remains active/unmet; retained literal-align01 full result8.75% throughput
abovePR780 remains the last accepted aggregate. No commit/push/promotion.


## Even-divisor extension while full01 runs

Divisibility02 factors d=odd*2^k and rotates the modular-inverse product right
byk before comparing with floor(UINTMAX/d). Pure powers of2 keep the old mask
path. Policyodd reproducesdivisibility01; retained reproducesliteral-align01.
Source9d57821474026ddc67eecd17e4c6e8c60702695179890fe237d15a415221cfe2.
Local AMD64 underRosetta and nativeARM oracles each pass15278832 calls,
99360traps across474functions. Odd/even algebra, Boolean consumer, scratch,
rotate encoding and edge-escape unit tests pass; ARM compiler/runtime passes.
All195 census code/input checks pass. Candidate changes10 additionalmodules
versusodd, adding86bytes; everyodd image equalsdivisibility01, everyretained
image equalsliteral-align01. Native AMD64 qualification/timing remain pending
and must follow the original livefull01 session56056.

Code-preserving divisibility-census02 records full65 matchingcode/inputhashes.
Unsigned checks: LZ4 divisor3,zlib31,UTF8proc28,nine numericmodules20.
One signedcheck is Nussinov function1 divisor20. The initial prose omitted that
signed tuple; the raw-log verifier exposed it and the report was corrected.
The first diagnostic invocation also lackedA82A_ROOT; its error is preserved,
and the corrected runner passed. Emission visits are not runtime hotness.
No signed optimization is implemented. The unsigned extension remains local
only; no native performance or retention claim is made.


## Divisibility01 full completion and retention

Original session56056 completed0; all six balanced rounds,65 modules/72 exports
are downloaded and independently verified. Candidate/control time0.9992687891
(0.073% less); candidate/PR7800.9152816704, throughput1.0925598450 (9.26% higher).
Changed2 geomean0.9824728081; unchanged630.9998066716. LZ4module0.9601574117;
zlib1.0053068454 regresses0.531% and remains included. Noisy PRQOI encode1.809895,
decode1.382265,UTF SIMDvalidation1.277333 and controlDoitgen1.372795 are retained.
Unchanged-code-neutral sensitivity0.9994560676; excludingmany_funcs0.9992573682.
Retain divisibility01 experimentally based on repeated LZ4 improvement, complete
correctness,19 fewer nativebytes and near-neutral compilation. Aggregate gain is
tiny. Prior8.75% is a separate run; separate PR executable layout/hostvariation
limit absolute claims. Goal active/unmet; no promotion,commit,push.

Native divisibility02 qualification started after full01 completed, original
exec session48819. Three policies(candidate,odd,retained),15,278,832 independent
calls each then compiler/runtime and65/72+21semantic gates. No timing yet.


Native divisibility02 original session48819 completed0. All three policies pass
15,278,832 oracle calls/99,360 traps,65modules/72exports,21semantic cases; native
compiler/runtime pass. Binary41fc3047bcf3d4b91daaff0c71e632c81365867e6cf7ca3bd18307a4e2cbf493.
Native/local independent archive verifiers pass. Screen02 is now running original
session58025, six alternating1s pairs,10changed modules+LZ4/zlib/YYJSON/Memory
controls, same binary candidate vsodd. Source remains unretained pending timing.


## Even-divisor screen and compile complete; full02 live

Screen02 original session58025 exited0. Independent verifier confirms six
alternating1s pairs. UTF8proc0.9647026183; changed10geomean0.9962749862,
unchanged4geomean0.9975697486. No>20%spread. Nine PolyBench modules range
0.995554–1.003283. UTF8proc changed checks concern Hangul; fixed corpus input
has no Hangul, so direct execution of those checks is not an established cause.
Code layout remains a possible explanation. All favorable/unfavorable samples stay.

Compile02 original session29107 exited0; four alternating3-iteration rounds,
ten changed modules plus YYJSON/Memory controls. Changed time/control0.9922–1.0138;
allocation bytes near neutral. UTF8proc0.994945/control,1.001635/prior executable.
Unchanged Memory0.939747/control limits broader attribution; no>20%time spread.
Whole-process RSS is not isolated compiler memory. Archive verifier passes.

Full02 original session52057 is live, six balanced3variant permutations,
65modules/72exports,300ms, nativeCPU0. Candidate and odd-only control share binary
41fc3047bcf3d4b91daaff0c71e632c81365867e6cf7ca3bd18307a4e2cbf493; PR780 pinned unchanged.
No second native CPU0 job may overlap it; resume original handle, never restart
on observation timeout. Local analyze/verify scripts and checkpoint are prepared.
Retained result remains divisibility01,9.26% throughput overPR780;50%target unmet.

## Native profile11 and qword memory-fill lead

Profile11 ran before full02 and completed0 on session47267. Retained divisibility01
source, diagnostic native-PC and mapping overlays,8s per module. Independent
verifier matches qualified code image prefixes and inputs. PCRE2 has944native
samples (one unaligned retained),function60 45.66%,function8 27.54%. UTF8proc849
native samples,all aligned; function0 51.94%,function7 20.26%,function2 18.14%.
NanoSVG878native samples,all aligned; function56 23.35%,REP STOSB infunction53
87samples (9.91%). These are diagnostic samples,not acceptance timings.

Bulk-fill01 local prototype on retaineddivisibility01 replaces fills>=64bytes with
REP STOSQ and exact byte tail after unchanged full range check. Broadcast lowerAL;
reservedR10/R11 only,no vector clobber. Policy A82A_BULK_FILL_POLICY participates in
artifact revision. Source1a6f920a327399afe7437d99d65829ed20ef7816a7f26c5aa39b7ab4a80395b6.
AMD64/Rosetta candidate/control and nativeARM each pass376832 independent calls,
74368expected traps,all256bytevalues,boundaries,fullmemory and live-argument returns.
Encoding/broadcast unit and ARMcompiler/runtime pass. A first build passed wrong
EmitBytes argument form; preserved diagnostic,corrected beforequalification.
All130census code/input checks pass; retained65matchdivisibility01,candidate25change
(+4076bytes). Local archive verifierpasses. Native source and runner uploaded,
qualification NOT started whilefull02 occupiesCPU0. No timing/retentionclaim.
Production source remains41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.
No commit,push,promotion. Goal active/unmet; work continues.


## Divisibility02 full comparison complete

Original session52057 completed0. Raw samples, six balanced permutations,
65modules/72exports, policies/source/code sensitivities independently verify.
Time/control0.9996433814 (0.036%less), time/PR7800.9159781825,
throughput1.0917290598 (9.17%higher). UTF8proc0.9646526385 repeats3.53%less time.
Changed10geomean0.9931983654; unchanged551.0008196879. GEMVER/Jacobi1D/LUdcmp
regress0.163%/0.096%/0.150%, included. No>20%spread. Unchanged-code-neutral
sensitivity0.9989505709; excludingmany_funcs0.9996837654. Retain experimentally,
with86extra bytes and near-neutral compile costs. Gain is tiny and the changed
Hangul checks' execution is not established as the cause of UTF8proc improvement.
Prior9.26% is separate-run evidence,not a paired regression.50%target still unmet.

Bulk-fill01 native qualification started only afterfull02 finished; original
session48374 completed0. Both policies pass376832calls/74368traps,65modules/72exports,
21semantic cases,nativecompiler/runtime. Binarye82dd1e04cfc1fba969b937c8735fff7f0c3e4acbd711ad16315ce3837c7b0b3.
Native/local archive verifiers pass. Focused screen now live originalsession8581,
25changed modules+Memory/Fannkuch controls,six alternating1s pairs onCPU0.
This independent prototype is based on odd-onlydivisibility01,not the newly
retained even02; a successful combined candidate will require separatequalification.

## Fill distribution and separate ARM baseline failure

AMD64/Rosetta instrumentation replaces all3staticNanoSVG fills with a counter
helper that preserves the original fill. Both engines and bounds,5calls each,
match original result and fullmemory. Per invocation8fills,total40860bytes:
4below64,3at64–255,1at4096–65535,noneelse. This establishes path eligibility,
not timing or cause. Source/input/diagnostic/verifier archives are fill-distribution01.

The ARM diagnostic traps in the original uninstrumented module underDragline.
A separate standalone single-instance run with physical production sources and
no compiler overlay reproduces the trap in both explicit and signalbounds;
Railshot succeeds both with4789067689214705683. Explicittrap:func24,Wasmpc0x19.
This baseline failure is independent of instrumentation and the AMD64-only fill
prototype; its cause is not diagnosed. No ARM whole-corpus correctness claim.
Physical Go hash remains41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.


## Short memory-fill extension02, local qualification complete

Bulk-fill02 adds bounded overlapping head/tail stores for lengths1–63 after the
unchanged whole-range bounds check; zero length skipsstores,>=64retainsqwordREP
and exactbyte tails. Restore end-pointer/count convention,no vectorclobber.
Source3d02d5ea88d0ea289cdee014180b3046f42e7e49e74bdf9c775589d9d4e1f778.
Policiescandidate/qword/retained; artifactrevisionincludespolicy. ThreeAMD64/Rosetta
policies and nativeARM pass376832calls/74368traps each. Encoding/control and
compiled-fixture opcodechecks pass; ARMcompiler/runtime passes. All195census
images verify:qword65equalsbulk-fill01,retained65equalsdivisibility01. Candidate
changes25modules and adds12484bytes overqword-only. No native timing/retention.
All localjobs terminal0:unit42595,oracles12736/73461,census53441,ARM92481,paths4482.
Localverifier passes. Source and native runner uploaded; nativequalification
NOT started while originalbulk-fill-screen01session8581 remainslive. Source is
still based onodd-only,not newlyretainedeven02; combiningneedsnewqualification.


## Qword-only fill rejected; short-fill native screen live

Original qword screen8581 completed0 and independently verifies all6paired rounds,
25changed modules+2controls. Changed25time/control1.0037749923; unchanged2
1.0004644487; focused27 1.0035293916. NanoSVG0.9965104262 nearneutral; SHA256
regresses3.36%,Monocypher1.99%,libtommath1.88%,JSON1.58%. FastFloat0.9745678366
andPCRE2 0.9839490135 isolatedgains do not supportretention. RetainedYYJSONspread
1.2630164595; allsamplesincluded. Rejectbulk-fill01, no compile/fullrun.

Bulk-fill02 nativequalification started only afterqword screenfinished,original
session83113 completed0. Three policies each pass376832calls/74368traps,
65modules/72exports,21semantic cases; compiler/runtimepass. Binary451a14ea2e528896825645d47da5d3411154d8ee2569dd327c88a198792b4071.
Local/nativeverifierspass. Originalscreen10758 nowlive onCPU0: sixbalanced
candidate/qword/retained permutations,1s perexport,25changed+Memory/Fannkuch.
Candidate must beatretainedodd-only aswellasrejectedqwordparent. No newPRscore.

Code-preserving fill-constant-census01 completed0(original72196) and verifies
all65code/inputhashes equalbulk-fill02candidate.184emissionvisits,92groups,
25modules;168visitsknownbyte (0,8,9,255),98knownlength (64–129600). NanoSVGfn18
haszero/248known;fn48/53 havezero withdynamiclength. Visits are notruntimehotness.
Source9029c8c07bf8dbbf86a6ca0b926882a45dcf899c902d9285003dbb432101c84b.
Next possible follow-up: pass existing nativeIntegerConstant proofs tobulk helper
to omit known-zero broadcast setup and constant-size threshold/tail machinery,
while preserving whole-range boundschecks and adding independentconstantfixtures.
This is not implemented or measured. Retained fullscore stays9.17%abovePR780;
50%goal active/unmet. ProductionGo unchanged; no commit,push,promotion.


Constant-fill03 implements the census lead as an overlay. Source
5cc950f8ec6b0c69681306ee076c891cf382157392b58d589617feb6edf4d598.
Optional direct-path SSA hints preserve all existing range checks. Known bytes
avoid broadcast; known lengths omit dispatch and dynamic tail setup. Unknown
operands retain short02 behavior; the short and retained policies reproduce all65
parent code/input images exactly. Candidate changes25modules, -8849bytes vsshort.
Three AMD64/Rosetta policies each pass327960constant calls/157176traps across799
functions and376832dynamic calls/74368traps. ARM equivalents and compiler/runtime
suites pass. Unit/emission checks and195code-image checks pass; archive verifier
passes. Initial MovImm32 type error fixed before qualification and retained.
Native qualification pending CPU0 availability after short02screen10758. No source
promotion or new execution claim. Retained matched fullscore remains9.17%.


Short-fill02screen10758 completed0 and independently verifies486raw runs, six
balanced permutations. Changed25candidate/retained1.0054751557, unchanged2
0.9990544471, focused27 1.0049981358. Candidate/qword changed25 1.0017456630.
No series exceeds20%spread. NanoSVG1.0283240676,YYJSON1.0282364235,
SHA2561.0265842741,libtommath1.0204285983. Rejectshort02; no compile/fullrun.
All samples retained; source decision, screen analysis and independent verifiers
updated. Native constant-fill03qualification started only after10758completed,
original8406: both oracles underallthreepolicies and compiler/runtime now pass;
65/72corpus and21semantic qualification in progress. ProductionGoSHA rechecked
41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.


Constant-fill03native8406completed0. Three policies each pass376832dynamic calls/
74368expected traps and327960constant calls/157176traps across799functions,
65modules/72exports,21semantic cases,compiler/runtime suites. Native binary
86eb565cc84e2f6a3145b247649213bcb2d9f30373daa1f37709c48c5758718f.
Local/native archive verifiers pass. Focused screen23080 nowlive, nativeCPU0,
sixbalanced candidate/short/retained permutations,25changed+Memory/Fannkuch,
1s/export. Candidate must beat retained odd-only, not merely rejectedshort02.
Source and screen checkpoint archives updated. Fullretained9.17%unchanged;
50%goal active/unmet; productionGo unchanged, no promotion/commit/push.


## Constant scale multiplication01

Profile11 UTF8proc contains IMUL24 before a dependent table load; the sampled
sequence motivates exact modular integer strength reduction, not a per-instruction
latency claim. New overlay uses retained even-divisibility02, separately fromfill.
Positive(1,3,5,9)*2^k constants emit atmosttwo instructions: LEA/move then shift.
Three policies candidate/LEA+shift,lea/3,5,9only,retained/original. Direct and
oversized structured immediate paths are covered; unsupported constants fall back.
No register allocation or FP semantics change. Source
823f1a561109113a11d707a5004299026332751bab95d86b82ac90185c1c006c.
All3AMD64/Rosetta policies and nativeARM each pass3126256independentcalls,
542functions,721inputs, including bothoperandorders andliveoriginaloperands.
Admission/direct+stack emissionchecks andARMcompiler/runtime pass.195codeimages
verify all65retained exactly matcheven02; candidate changes40modules,+1428bytes;
lea changes11,+16bytes. Localarchiveverifier passes. Nativepending afterlive
constant-fill-screen03original23080. No native speed or retention claim.

Scale-mul01profile-sequence-proof.json verifies UTF8proc's original-profile hot
bytes0x78..0xb3 exactly match retained even02 despite complete-image differences.
At0x9b, IMULr8d,r8d,24 becomes LEAr8d,[r8+r8*2];SHLr8d,3. Localverifier checks
allthreeimagehashes andexactrange/replacementbytes. This establishes emission at
the motivating sequence, not latency or performance. Source uploaded for native
qualification afterfill-screen03; the upload completed0. ProductionGohash still
41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.


## Constant-fill03 rejected; scale-mul native qualification

Originalfill-screen03session23080completed0;486rawruns verify sixbalanced
permutations. Candidate/retained changed25 1.0012647280;unchanged2 0.9996898436;
focused27 1.0011479849. Candidate/shortchanged25 0.9975582770 only improves a
rejectedparent. YYJSON+2.53%,libtommath+1.75%,Miniz+1.24%;NanoSVGnearneutral.
CandidateBlake spread1.3160272051 retained;otherpolicies no>20%series.
Rejectfill03,no compile/fullrun. Source/screen decision,README,pins/verifierspass.
Native scale-mul01qualification started after23080ended,original76627; allthree
3126256-call oracles andcompiler/runtime passed;corpus qualification ongoing.

## Nested-loop syntax census01

New logging-only census on even-divisibility02,source
2f7c9cb17469ec8a942a4d2f6ecdfa7024eeb72095d8cdd39c4401808a86bdae.
Original83425completed0;all65code/inputimages match even02.87records include
30straight-line syntax groups across17modules (2mm2,3mm3,andothers). Scope is
captureNativeWideRegions input: one nested loop,FPops/stores,two backedges,
allowedlocal/integer/FPops. No cross-iteration dependence,range,alias ortrip-count
proof is claimed;dependentSeidel also appears.2mm-source.wat preserves the actual
compute nest, whose inner local16sum carries while outeroutputs advance. Current
nativeWideLoopBody rejectsnestedcontrol. Packing independent outeroutputs with
per-outputFPorder preserved remainsunimplemented;needs guardedfallback foralias,
wrap,bounds andtails pluscorrectexitstate. Archiveindependentverifier passes.

Scale-mul01nativequalification76627completed0. Allthree policies pass3126256calls,
65modules/72exports,21semantic cases andcompiler/runtime. Binary
acb481bb32138091893fc4903e1ca0440a9fc4c5a5fa18075b0397c97d62e863.
Local/nativearchiveverifierspass. Nativefocused screen65108 nowlive, sixbalanced
candidate/lea/retained permutations,40changed+Memory/Fannkuch,1s/export. It uses
current even-divisibility02 control. Lastpollround1libtommath. ProductionGoSHA
41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f unchanged.
No retention,commit,push; fullretained9.17%abovePR780,50%targetunmet.


## Nested-loop affine analyzer and scalar guard model01

Implemented new isolated nested_loop.go analysis on even-divisibility02;source
4d2ab453de198e41d38dff41405d3fcc62e687208aef220630cd6600c8f1bb27.
One nestedloop,affineI32 expressions,constant-step recurrences,staticinnertrips,
storeouterstride8/innerstride0,loadouterstride0or8. FPouterrecurrences andunsupported
control/effects reject. Widened memoryimmediates remainseparate frommod32addresses.
Scalar guard model requirespositive/divisible/evenoutertrips,fullbounds andstrict
input/outputdisjointness exceptownoutputread. No emittedruntimeguard/vectorcode.
Originalcensus63412completed0:272records,7plans across2mm2/3mm3/GEMVER1/MVT1;
all65nativecode/inputimagesexacteven02. BothAMD64/Rosetta andnativeARMtests pass
12288synthetictraces/4165acceptedguardcases/13admissionfixtures and1792tracesonall7
corpusplans. Independent scalarinterpreter compares everymemoryaddress andwritten
integerexit. FPvaluesaredummyforaddress-onlyproof; noexecution/FPresultclaim.
Archiveverifierpasses. FutureemittershouldintegrateinsideboundedAdjacentLoopModule
scan to preserveother priorlooptransforms, useaddedlocals,originalbytefallback,
lastlaneFP/exactintegerexitpublication,sourceoffsets,andfullindependentoracles.
Scale-mul-screen01original65108remainslive;no newretentionorPR780score.


## Guarded nested-loop emission01; scale-mul screen complete

Scale-mul-screen01 session65108 completed0 and all756raw runs verify. Candidate
42-module time/control0.9983238533, changed40ratio0.9981825281; LEA-only changed11
ratio1.0014689739. Candidate linked_list+5.29%, PCRE2+1.23%. Control spreads above20%
for Doitgen1.40950, Gesummv1.20552 and YYJSON1.33047. Deferred, not retained.

Nested-loop01 emits guarded f64x2 independent outer outputs, preserving FP order,
scalar fallback and exact integer/last-lane FP exits. Source3c3943a4efeb1378ed3753dc67f1d7e9d0e9a1e1631882d04ff945b18981b1cb;
binaryf38f6630fe9f66d9725161d29d77c3afcaea0f437abe47a1a299905d313efb3b.
Four modules change (+6279bytes); all65 retained images exactly match even02.
Initial source-offset ordering failure repaired by attributing checked fast code
to the original outer header, retaining exact fallback offsets. Forced-AVX2
compilation proves all288fixture transforms. Rosetta has no native AVX2 and its
execution passes cover fallback only; nativeAMD64 qualification explicitly
requires AVX2. Both native policies pass27648calls/12544traps,65/72corpus,
21semantic andcompiler/runtime. NativeARM fallback oracle andsuites pass.
Original90470completed0; focused screen74018 nowrunning onCPU0.

An initial Railshot signals oracle failure is a preexisting constant-i64 store
partial mutation, reproduced without experimental overlay on Rosetta/nativeARM
and with physical-v77 overlay on nativeAMD64. Both Railshot backends split it
into two32-bit stores: the first modifies memory before the second faults.
Dragline passes; Railshotexplicit passes. Raw failure, independent tiny fixture,
all168calls and backend source snapshots are archived in cross-page-store-01.
The nested oracle retains exact trap-memory checks and accepts only quiet NaN
payload variation for independently predicted arithmetic NaNs. No baseline fix.

No production source promotion, commit or push. Retained full score remains9.17%
higher throughput than PR780, short of50%.


Nested-loop-screen01session74018 completed0; all72raw runs verified. Candidate
2mmratio0.6659741,3mm0.6170276,GEMVER0.8909099,MVT0.8473978;
changed4geomean0.7463126,controlsnear1. No>20%spread. Compilecost38482completed0:
changed modules1.61–2.42xtime,1.56–1.81xallocatedbytes,1.20–1.27xallocationcount;
whole-processRSS+1.2–2.1%againstsameexecutable. No>20%compiletime spread.
Fullcomparison54313running, notretained. Scratch-compaction02source
 d26f8e850b1531c941630b3c0dad6edd88b19f31968b5449686b3638cbaea3a1
preserves all65candidate/rawimages againstnested01 andall65retainedimages, plus
all288syntheticnativefixturecodeimages. LocalARMoracle/suitespass. Rosettalocal
compile diagnostic overlappedcensus andcannotbeclaimedasnative; allocatedbytes
fall11–15%butcounts+1–2%,timeonlymodestlybetter. Nativequalificationandcompilecosts
queuedin26077afterfull54313. ProductionGoSHAunchanged41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f.


## Nested-loop full01 verified; grouped guards03 qualified

Full01original54313completed0, all1170rawruns/sixpermutations verified.
Time/control0.9816436075;time/PR0.9008229951,11.0096%higherthroughput.
Changed4ratio0.7490045022,unchanged61ratio0.9992099619. Noisyseries retained:
PRDoitgen1.4168,QOIencode1.8363,QOIdecode1.3421,UTFSIMD1.5471;
candidateYYJSON1.2272,controlYYJSON1.2045. Experimental,notretainedduecompilecost.

Source02originalsequential26077completed0: threepoliciespass27648calls/12544traps,
65/72corpus,21semantic,compiler/runtime. Binary0bf4853cd275460bf0f08b9b445664171a12e059c33a38c4c858b5da7f1b025b.
Compilecosts02reduce1.4–5.5%time,10.7–14.2%bytes versusraw01, butcounts+0.9–2.4%
andtimeversuseven02still1.57–2.35x. No>20%time-spreadseries. Codeimagesidentical.

Groupedguards03source8d2fd5292abb401c7bbb9a40f01378a5423bf7e3aa2009cd4b4c33eee1aec657,
binary2c877c9febaedb453dea9cce85d160b12e0188e491b76855191de5d8f787af4c.
Combinesequalaffine-coefficient/outer-stridesourcestreamsintoconservativewidened
ranges, dropsduplicateoutputandown-outputreadchecks;sevenplans66checksbecome21.
65536generatedplans/16905acceptedcasesperarchproveoriginalguardimplicationand
independentmodularaccess/aliasbounds. Source03changesfourmodules,-822bytesversus02,
but3mm+815bytes. All195controlimagesmatchparents. All288fixtureemissionschecked.
ARMfallbackoracleandsuitespass. Native03threepolicieseachpass101376calls/41824traps
withAVX2required,65/72corpus,21semantic,compiler/runtime. Originalsequence92114
nowrunsfocusedthreepolicyscreen;compilecostsfollow. No retention or full03claim.

Positive-stride diagnostic f35dedb54e5798ef7aef90bbce731f57962d9dde0dd95da18702710a6115ac19
completedlocal85035:272records,9plans. OnlynewplansGEMVERfn1outer864 andMVTfn1outer411,
sourceouterstride960/inner24,contiguousoutputs. All65nativeimagesunchangedeven02.
Otherpreviousload-stridefailuresnextrejectnonneighboringoutputs. No gatheremitter
ornewmodulecoverageclaim. Retainedscore9.17%;50%targetunmet.


## Paired scalar source loads04 qualified

Original03sequence92114completed0. Screen03candidate/compact changed4ratio
1.002909373;3mm-1.95%,GEMVER+2.88%,othersnearneutral. No>20%spread. Compile03
candidate/compact time0.806–0.878,bytes0.882–0.922,counts0.938–0.991;vs even02
time1.317–2.070,bytes1.215–1.425. No>20%compiletime spread. Experimentalparent,
notretained;allrawrunsandarchiveverifierspass.

Paired-load04source44dbf4ff02952373d0e1de060fdd248179bfe7f51a2b8133508474f68bf639d4,
binaryc38e564b5b8c72cdcf347beaff8cbb40a3ee5cd98d7ec6097a94050a78cd5c63.
Positive signed32 sourceouterstrides use two scalarF64loads thenreplace_lane1;
guard-validI32scratch is reused aftertheguard. Contiguousoutputs,wholebounds,
aliasfallback,orderedFPandexactexitsremain. Groupedcontrolmatches03all65images;
retainedmatcheseven02. OnlyGEMVER/MVTchangeversus03,+1334bytes;fourversuseven02,
+6791bytes. ForcedAVX2compilationproves1152fixtures,864pairedloads(strides16/24/960)
and288contiguous. ARMfallback405504calls/167520trapsandsuitespass. Nativeoriginal
37206completed0: threepoliciescandidate/grouped/retainedeachpass405504calls,
167520traps,65modules/72exports,21semantic cases,compiler/runtime. Source04screen
andcompilecostsrunsequentiallyin81582. Full04scriptsarepreparedin/tmp,notstarted;
awaitscreen/costs beforechoosingafullrun. No newretentionscoreorproductionchange.


Source04sequence81582completed0;108rawscreenrunsverify. GEMVERtime/grouped03
0.894216982,MVT0.869379115,changed2geomean0.881710592. Otherfourcodeidenticalmodules
nearneutral. Fourtransformed/even02ratio0.705030711, six-module0.792093074.
No>20%screenseries. Compile04GEMVER/MVTtime/grouped1.24793/1.33515,
bytes1.12774/1.16615,counts1.04328/1.07856. Transformed/even02compiletime1.64743–2.07704,
bytes1.37063–1.49835. No productionpromotionorretention. Full04nowrunning original
16151:6balancedcandidate/retained/PR780permutations,65modules/72exports,300ms/export,
1170rawruns,CPU0,Go1.22.2. Experimentalfull01score11.01%;retained9.17%;50%unmet.

Compile04grouped-controlMVTspread1.2659729181 is retained; candidate/previous have
no>20%compile-time series. IncrementalMVTcompile-costprecision is limited.


## Continuation: completed nested04 process and call-tree local gates

- Original full04 session16151 exited0. `hub` is now offline and two SSH/download attempts timed out. The process summary is captured as unverified evidence in `nested-loop-full-04/completion-checkpoint.json`; raw1170 samples remain remote. No new verified performance or retention claim.
- Bounded call-tree01, effective source `6e59fa4d929ca0c52aaf644ea4f0dbfcb8c655f4636f3d79775d089701d2b294`, extends eligible small wrappers by depth1/2 and then reapplies ordinary leaf admission. Imports and cyclic/residual calls reject; original budgets remain enforced. ARM keeps depth0.
- All65 retained code images match nested04. Six AMD64 modules change; code grows28,465bytes at depth2 or17,137 at depth1. This is coverage evidence, not performance evidence.
- Integer/F64 depth and compiler-path checks, whole-module source preservation and source offsets pass both architectures. Four local independent oracle runs pass160,000calls/87,760traps each: three AMD64 Rosetta policies plus ARM64 fallback. Full-memory/global/ordered-FP checks include trap aftermath.
- ARM compiler/runtime suites pass. Rosetta compiler suites pass. Full Rosetta runtime fails the same343 tests/subtests under both candidate and retained with SIMD disabled, then panics in a v128 host-signature case. Failures are archived, not suppressed; native AMD64 suite remains required.
- Native qualification, eight-module three-policy execution screen and compilation-cost runners are prepared. They have not run. `call-tree-01/verify-local.py` passes. No production source promotion, commit or push. Retained9.17%, previously verified experimental11.01%, target50% unmet.


## Continuation: recovered noisy full04 and native call-tree qualification

`hub` returned online. The full04 download completed and analyze/verify passed all1170 raw records, exact65/72 coverage, source/binary/artifact pins and six balanced permutations. Calculated time/PR7800.896473251855 (11.548% higher throughput), time/even020.974591650669. Changed4ratio0.694189904836;unchanged610.996516774997. These calculations are not accepted as a new performance claim:215 of216 export series exceed20% spread, maxima4.24x/5.44x/4.31x. All samples remain. Separate compilation cost1.65–2.08x still prevents retention.

Current host inspection finds other CPU-heavy adapter-wasmtime, adapter-wavm and Node jobs, including CPU0; no unrelated process was stopped. Historical causality is not proven. A quiet benchmark window was requested. Native call-tree qualification started in original session72509; this correctness work can proceed during unrelated load, but new timing must wait. Goal remains50% higher throughput than PR780.


Native call-tree01 qualification completed original72509 with exit0. Downloaded evidence passes both local and native archive verifiers: three policies each160,000calls/87,760traps,65modules/72exports,21semantic cases; integer/F64 compiler-path unit checks and compiler/runtime suites. Binary `4c09e697367f8fdcc5ab1da67b610a362ccb29427c98307b6f45735d7090c2a0`, effective source unchanged. Runtime SIMD failures under Rosetta do not reproduce in native AMD64 qualification. Eight-module screen and compile-cost runners now record host-load snapshots and enforce all native gates. Timing has not started because unrelated CPU-heavy benchmark jobs remain active. No new throughput claim or retention.


Quiet-host follow-up: a20-second per-CPU `/proc/stat` probe foundCPU0 98.75% busy;CPU1/6/7 100%;CPU2 74.07%;CPU3/4/5 average14.80%/17.77%/22.28%, with61%/23.71%/41.24% one-second peaks. Pinned competing benchmarks occupy0/1/6/7 and unpinned desktop/build work occupies the remainder. There is no demonstrated quiet alternate CPU. Live candidate and PR780 binary SHA-256 match the pinned artifacts; no own native benchmark is running. Raw availability evidence and the two-turn timing blocker audit are archived in call-tree01. Timing awaits the requested quiet window; no unrelated process affinity or lifecycle was changed.


Third consecutive timing-blocker audit: another20-second probe showsCPU0 98.80% busy andCPU1/6/7 effectively100%, with competing benchmarks still pinned to those CPUs. The least busy alternate averages10.87% busy and reaches19.39%; other alternates have larger bursts. A quiet matched run is unavailable. Native correctness is complete; the pending screen and compile measurements cannot establish a reliable result under this interference. Blocked audit threshold is satisfied. Resume when a quiet benchmark window is available; preserve the full50% target and all evidence. No performance completion claim.


## Resumed: matched call-tree measurement on CPU7

The resumed goal has a fresh blocked audit. A new20-second probe findsCPU0/1 still saturated butCPU7 now mean2.17%/max4% busy;CPUs3–6 similarly lightly used. CPU7 has no sibling hardware thread in the reported topology. All three qualified policies are now pinned toCPU7 in original screen session32571, six balanced orders,1s/export, six changed modules plus Memory/Fannkuch controls. The source and binary remain unchanged. Other CPUs are loaded, so all sample spreads and controls still gate interpretation; no timing claim yet. No unrelated process was changed.

Static function metrics show depth2 affects7/7/3/1/2/3 functions in JSON/JSON-SIMD/LibTomMath/Monocypher/NanoSVG/PCRE2. JSON adds31 spill slots and208 frame bytes summed across changed functions, while call relocation count falls9. These are static metrics, not dynamic work or speed measurements. `function-analysis.json` preserves both depth policies and all deltas.


Call-tree screen32571 completed0 onCPU7. Raw144runs/180export measurements, six balanced policy permutations, exact source/toolchain/input/policy pins and paired aggregation verify. Depth2 /original leaf changed6ratio0.989740569390;unchanged2ratio0.999316673326. Depth1 changed6ratio0.993585548319. Depth2JSON0.9634695,JSON-SIMD0.9867393,LibTomMath1.0007567,Monocypher1.0063623,NanoSVG0.9938970,PCRE2 0.9877876. All spreads<20% (maxima2.66%/4.63%/2.34%). Depth2 /depth1 additional2ratio0.996457 but identical6ratio0.997608 limits small-depth attribution.

Exhaustive46,656 paired whole-round bootstrap resamples give a conditional95% interval0.985604–0.993288 for the changed6 ratio; controls0.993553–1.000941. This covers this six-round screen only, not unobserved interference or full-corpus performance. Compile-cost session5144 is now running onCPU7, four alternating3-iteration rounds; no overlapping own native benchmark. No retention or full65/PR780 claim yet.


Compile5144 completed0 and verified96raw runs onCPU7. No>20% time spread. Depth2 versus original leaf policy: JSON/JSON-SIMD time1.606046/1.579297, bytes1.764808/2.130347, allocation counts2.108814/2.352786. Monocypher time1.156108,LibTomMath1.054546,NanoSVG1.012993,PCRE2 1.001870; controls near neutral. Depth2 versusdepth1 JSON time1.336032/1.324031. Broad call-tree01 is unretained; no full run follows the modest mixed screen and large JSON compilation regression.

Direct-wrapper admission census01 completed locally with all65 input hashes matched. Allowing residual scalar direct local calls in <=384byte wrappers produces58 potential additional wrappers across11modules. It does not model caller budgets or existing recursive expansion, and does not prove safety, emitted coverage or performance. Archives and verifier preserve all counts and limitations. Next candidate can investigate retaining residual direct calls rather than recursively expanding the entire tree; independent reentry/call-effects tests are required.


Direct-wrapper01 is implemented as an unqualified overlay, source92192387063c0e56cb6b8263fed13b96e7519f50a41fbb1540baca8394708b91. It expands one<=384byte wrapper but retains its local scalar direct calls, with existing caller budgets. Tree/retained policies match all130 qualified parent images; candidate changes11modules,+76,439bytes versus original leaf policy,+47,974versustree. The corpus census21294, three Rosetta existing-oracle policies13253, ARM fallback87057 and residual-call/source-map units65600 all exited0. Existing oracle coverage is160,000calls/87,760traps perpolicy; it is not a substitute for dedicated residual-call effects/reentry tests. Native suites and65/72/21gates remain pending. No benchmark is currently running; CPU7 was suitable for the completed matched call-tree screen/cost run. Goal50% remains active and unmet; no physical source promotion, commit or push.

## Direct-wrapper02: correctness fixes and native qualification

The dedicated reentry test exposed two existing structured AMD64 failures.
The integer failure minimizes to a call-free function: destination equals the
right operand, which the helper overwrites before use. The original emitter
fails 2,613 of 14,872 V8 cases per bounds mode; preserving RHS in RCX passes all
29,744 checks. A separate FP failure remains after that fix: a structured caller
interprets an abstract FPR clobber mask as hardware XMM preservation. Restoring
all saved structured FP/vector locals and constants fixes the original reentry
scenario. Red/green logs and discarded minimizations are in integer-alias-01.

Direct-wrapper02 source is
`6fc96619cc6a02eb31ad351cf81d20cddaf63acee7db6c003a538d189d432f22`;
native Go1.22.2 binary is
`479702f757781950280ad3a006485bcee550e8dc65bca21ed0b82c2d3e20b07a`.
Native session86326 completed successfully. All three policies pass the integer
oracle, 2,304 reentry calls per policy, 160,000-call / 87,760-trap existing oracle,
65 modules / 72 exports, 21 semantic cases, compiler/runtime suites, and
residual-call/source-map checks. ARM fallback suites/reentry and four local
existing-oracle policies also pass. Both archive verifiers pass.

The corrected census74256 contains 195 images, all matching direct-wrapper01
under the same policy. Wrapper inlining changes 11 modules and adds 76,439
bytes versus the fixed original-leaf control. The first census harness ran no
matching tests; its logs are retained and excluded from coverage. A fresh host
probe found CPU7 mean6.96% busy/max18.18%, so timing has not started. All fixes
remain overlays; physical Go SHA41a6a8c99c7a1546cab3916bc250ced8159a770c259357d417c55b64ee13ec3f
is unchanged. The 50% target versus PR780 remains active and unmet.


Direct-wrapper02 screen66099 and compile5858 completed0 on quiet CPU7 after a
second probe found mean2.07%/max3.96% background activity. All234 screen runs /
306 export samples and156 compile runs verify. Candidate/leaf changed11 time
ratio0.988117914; unchanged controls0.999924097. No>20% series spread in either
experiment. JSON/JSON-SIMD execution ratios0.939336/0.937589 accompany compile
ratios1.282251/1.253129; LibTomMath compile1.398432 and Memory Tree2.902453
(about1.12ms extra). Broad wrapper02 is unretained; no full65/PR780 timing claim.
Correctness fixes are preserved in the qualified overlay and correctness-fixes.diff.
YYJSON is a narrower follow-up lead: execution0.962566, compile0.999184, without
a module-specific admission rule. No native job remains running. Goal50% stays
active and unmet; production Go sources are unchanged, with no commit or push.


## Quotient coverage and guarded word-copy follow-up

The guarded unsigned quotient-to-F64 prototype is locally correct but changes
none of the 65 corpus code images. It is unretained for lack of target-corpus
coverage; no native timing claim is made. The archive retains the failed
admission test before implementation, 24 successful AVX2/AVX512 emissions,
one million guarded arithmetic cases, and three 211,680-call execution oracles.
See vector-quotient-01.

A separate logging-only word-copy census preserves all 65 code hashes. Of 108
body-shape observations, two plans in Monocypher admit ordered four-word I64
copies. Word-copy01 extends the existing guarded AVX2 copy path to 16-, 32-,
and 64-bit elements. Its census changes only Monocypher (+1,120 native bytes).
All 65 retained images match divisibility02. Eighteen fixtures pass 36 forced
emissions; independent whole-word load/store oracles pass 30,456 calls per
policy on local ARM64 candidate and Rosetta AMD64 candidate/retained.
Native CPU7 qualification is running as session96990; its two word-copy
oracles, unit checks, and compiler suite have passed. Runtime and full corpus
checks remain pending. Unrelated host builds preclude a timing claim at this
checkpoint. Archives: word-copy-census-01 and word-copy-01. Production Go
source remains unchanged; no commit or push; the 50% target remains unmet.


Word-copy01 native session96990 completed with exit0: two 30,456-call memory
oracles, 36 forced emissions, compiler/runtime suites, 65 modules / 72 exports
per policy, and 21 semantic cases per policy pass. Local and native archive
verifiers pass. No timing was taken on the busy host; the prototype remains
unretained. The older native-profile05 shows only one of 705 Monocypher native
samples in function5, a historical prioritization clue rather than a current
performance claim. Both quotient01 and word-copy01 evidence are preserved.
No native job remains running. The 50% target is active and unmet.


## Constant vector-loop setup: broad qualification completed

The earlier immutable-local constant probe missed loop-entry values carried by
SSA edges. A new logging-only census preserves all65 input/code images and
finds94 of96 distinct regions across25 modules with known I32 entry inputs;
79 have known limits and counters. Raw192 visits and the first failed harness
compilation are preserved in wide-constant-census-02.

Wide-setup01 resolves those incoming values with bounded constant/phi analysis,
folds affine arithmetic only during setup, and replaces trip guards only when
their exact modulo-32 conditions are proven true. Body and exit arithmetic
continue using updated snapshots; memory/alias/conversion guards remain.
The census changes25 modules, removes5712 native bytes, and keeps all65 retained
images identical to divisibility02. It includes neither word-copy nor quotient
experiments. Production Go is unchanged.

Local tests pass90000 arithmetic cases,1024 trip-guard cases,edge/phi/cycle/type/
budget rejection checks,66 forced emissions and55836 independent byte-memory
calls per policy. Native sessions41801 and9342 complete0: both policies pass
65 modules/72 exports,21 semantic cases,55836 memory calls,compiler/runtime
suites,and20 FP fixtures with2160 cases each in2 Dragline bounds modes against
explicit Railshot. Fourteen static-trip FP fixtures also pass local Rosetta.
Source/code/input/raw-output/pin verifiers pass. No timing or compilation-cost
measurement exists yet; no retention or 50% claim. No native job is running.
Archive: wide-setup-01.


## Wide-setup measurement blocker

The qualified wide-setup01 source and matched Go1.22.2 candidate/retained/PR780
binaries pass measurement preflight. Both ten-second host activity gates fail;
native sessions50293 and72762 exit75 with zero benchmark measurements. The
first probe records CPU7 mean26.285%,peak59.596%,and5.471 busy background cores.
Evidence and independent gate verifiers are archived in wide-setup-screen-01
and wide-setup-full-preflight-01. A complete pinned runner is saved as
wide-setup-01/measure.py for focused execution,all65 compilation cost,and full
65-module/72-export comparison. Native contention has persisted across three
consecutive goal turns. No native job is running; a quiet host is required to
choose retention and substantiate progress toward50%. Production Go remains
unchanged; no commit,push,retention or performance claim.
