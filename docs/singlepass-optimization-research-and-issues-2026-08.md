# Research and upstream audit for a stronger Railshot optimizer

Research date: 2026-08-10

Local checkout: `5cb8284f2d2c27390b5a7dc1fd1d0a29bcea7d3e`

Current `origin/main` audited: `ba518483` (three commits ahead of the checkout)

Live issue query: `gh issue list --repo wago-org/wago --state open --limit 200`

This is a companion to
[`singlepass-execution-performance-research-2026-08.md`](singlepass-execution-performance-research-2026-08.md).
That report contains the measured Wago/wazero baseline and the detailed current
Railshot audit. This document adds primary-source compiler research, a current
upstream implementation survey, and the open-issue inventory as of the date
above.

The repository Graphify index was checked first. It reported the graph at
`f70d3704`, 14 commits behind this checkout. `graphify update .` re-extracted
the changed code but reported no topology change and left the graph snapshot
unchanged, so Graphify was used for broad seam discovery and every concrete
claim below was checked against source search. The three newer `origin/main`
commits were inspected from a read-only archive so the user's checkout was not
changed; they add shared GC resolver stubs/reuse and invocation/instantiation
work that is included below.

## Executive recommendation

Railshot should spend some of its compilation-speed lead on **more choice at a
bounded sink**, not on a conventional whole-function optimizer by default.
There are four complementary projects here:

1. Build an architecture-neutral, fixed-capacity **Valent region optimizer**.
   Extend the current tree cover from associative trees to a small DAG with
   value numbering, known-bit/range facts, effects, target-independent
   canonicalization, and explicit fuel. Keep at most a configured number of
   nodes and fall back to current lowering when full.
2. Give each target a generated or table-driven **costed tree/DAG selector**.
   Label alternatives by latency, throughput, bytes, register need, fixed-register
   constraints, flags behavior, and whether they fold memory/immediates. Select
   a whole cover at the sink instead of committing to each instruction in
   isolation.
3. Add an allocation-bounded **machine combiner/finalizer** between target
   lowering and final byte publication. Initially retain symbolic instruction
   records only for the current basic block or bounded region; run target
   combines, copy/extension cleanup, local scheduling, and then exact branch and
   frame relaxation. This is much safer and more capable than decoding arbitrary
   finalized bytes.
4. Create an **offline optimizer laboratory** that harvests missed local
   sequences from the corpus, searches or synthesizes better AMD64/ARM64
   sequences, proves integer/bitvector rules, and emits cheap online matchers.
   Expensive search stays out of production compilation.

These projects can share one rule semantics and test corpus. A rule should state
its value semantics, trap/effect ordering, required facts, target features,
clobbers, cost, and code-size delta. That unifies handwritten peepholes,
superoptimizer discoveries, and target selection without forcing one giant
optimizer pass.

A genuinely massive alternative is a per-basic-block SSA/DAG like WAMR Fast JIT.
It would enable broader CSE, scheduling, and local register allocation, while
remaining much smaller than Wazevo/Cranelift whole-function SSA. It is viable
before v0.1, but it conflicts with the current no-IR architecture decision in
[`no-ir-plan.md`](no-ir-plan.md). Treat that as an explicit architecture choice,
not a refactor that sneaks in through accumulating special cases.

## Current optimization inventory and audit

This inventory is based on the current `origin/main` source, not only on
`OPTIMIZATIONS.md`. The public catalog contains **42 named optimization
definitions**, while AMD64/ARM64 codegen emits **94 distinct peephole/explain
event names**. Neither number alone is “the number of optimizations”: some
events are candidates or telemetry, and several important production paths are
controlled by uncatalogued environment switches. The grouped inventory below is
the useful semantic list.

| Area | What exists now | Audit |
|---|---|---|
| Module analysis and compile path | Fused requirement/fact walks; function hints; local scores, last-use hints, call effects, immutable-table facts, module-global scoring/pinning, sparse hints, adaptive parallel compilation, worker scratch reuse, dense `br_table` scratch, precomputed GC layouts, reusable assembler/function scratch. | Strong allocation-conscious base. Analysis products are fragmented across feature summaries and backend hint structures, and new local optimizers still add byte-reader lookahead or their own scratch. Define one compact effect/fact summary and one worker-owned arena contract. |
| Deferred values / Valent blocks | Constants, registers, slots, local/global references, deferred loads, and bounded expression trees survive until a sink. Loads fold into consumers; constants become immediates; the allocator can condense or spill late. | This is Wago's main advantage over simpler baseline compilers. It is tree-shaped, so repeated values cannot be shared, and rules are distributed across `stack.go`, `emit.go`, `driver.go`, `fuse.go`, SIMD/SWAR files, and reader lookahead. Turn it into a fixed-capacity typed DAG or at least a shared labeled tree interface. |
| Scalar algebra | Integer constant folding; same-operand and ALU identities; extension/wrap elimination; zero/sign-extension folds; strength reduction; signed/unsigned magic division; high-half multiply; AMD64 three-operand immediate multiply; ARM64 multiply-add/subtract; byte-mask XOR; packed mask tests. | Good breadth, but facts are rediscovered by shape. The previously tried recursive known-bits walk imposed broad no-hit cost. Compute known-zero/known-one, range, alignment, and affine facts incrementally when a deferred node is created, so consumers get O(1) facts without recursive probing. |
| Tree scheduling and covering | AMD64 Sethi-Ullman-style register-need ordering, safe commutation, an eight-leaf associative accumulator cover, scaled-index and affine LEA folding. ARM64 has three-operand/old-destination sinks and UXTW/address folds. | The AMD64 cover is a successful prototype but uses fixed yes/no heuristics, recursively recomputes register need, caps association at eight leaves, and has no competing costed alternatives. Store labels once, admit covers by fuel rather than one hard leaf count, and include bytes, latency, register pressure, fixed registers, flags, memory folds, and destination reuse. Port semantic selection across architectures instead of cloning x86 shapes. |
| Locals and register residency | Static integer/FP/V128 local pins, entry-argument pins, extended FP pools, module-global pins, final-use transfer, local sinks (`tee`, unary, float, V128, select, three-operand), register merge across joins, AMD64 straight-line interval regions, ARM64 experimental loop-region pins, and call next-use dead-store suppression. | There are many overlapping pin allocators and heuristic cutoffs. They can reserve registers that a hot expression needs, then fall back to an unpinned recompilation. Replace independent pin policies with a bounded regional allocator using next-use, rematerialization cost, call constraints, and eviction cost. Keep the current allocator as the zero-budget fallback. |
| Flags, compares, selects, branches | Compare-to-branch, float-compare-to-branch, `eqz` inversion, compare/tee/branch, flags-backed select, `cmov`/`csel`, compare materialization, simple-if local sinking, and post-assembly conditional-plus-jump folding. | Flags are still a short-lived special state rather than a first-class constrained result. A symbolic machine window can preserve flag dependencies, form carry chains, compare/select/branch alternatives, and respect clobbers. The AMD64 branch fold is deliberately size-preserving and leaves a five-byte NOP; #326 should replace it with true relaxation and offset remapping. |
| Bounds and linear memory | Signal/guard-page check elision; explicit inline checks; one or multiple straight-line bounds certificates; invariant-base loop precheck/versioning; folded memarg offsets/addressing; deferred loads; alias-aware load forcing; immediate stores; exact straight-line store-to-load forwarding; small constant `memory.fill`/`memory.copy` unrolling; tiered backward-overlap copy. | Straight-line certificates are cheap and valuable. Loop versioning only recognizes direct `local.get` bases, allocates scan maps, excludes useful affine induction shapes/SIMD, and duplicates the body. Reuse dense scratch, derive affine induction/range summaries, and cost versioning by dynamic check count and code growth. Extend forwarding to exact byte ranges, narrow/wide overlap, fresh allocations, and non-aliasing memory epochs. |
| Calls and inlining | Internal register ABI, mixed GP/FP staging, multi-result handling, direct/tail calls, call-result/local-set fusion, preserved/reloaded pins, call-free inline hints, bounded leaf inlining, immutable-table and monomorphic indirect-call specialization, redundant type-check elision, direct-only host-adapter elision, prepared/private/isolated/direct integer entry, and current-main scalar `Invoke` private-entry reuse. | The ABI work is a major execution win. Inlining is still driven mainly by encoded body bytes, leaf status, signature class, and fixed cutoffs; it lacks net native-byte cost, caller pressure, hotness, and propagated facts. Build a call-site cost model from compact effect summaries and #327's objective. Generalize direct entry by an explicit state/ownership contract rather than another list of booleans. |
| SIMD and SWAR | V128 constant pools/caches, V128 local pins/sinks/forwarding, memory-operand folding, immediate shifts, native shuffle families, rotate/shuffle recognition, `and+any_true`, `not+and`, `vptest`, tee/store cleanup, packed-byte widen/pack/parse, mask test, and high multiply idioms. | Exact bounded rules work, but broad mask/SIMD patterns have better independent-workload evidence than `widen4`/`pack4`/`parse4`-style fixture-concentrated matchers. Move rules into one semantics catalog, require independent-producer hits, and use offline synthesis/proof. Add bounded SLP only after the region DAG can prove adjacency, aliasing, trap order, and profitability. |
| WasmGC codegen | Recursive dead-constructor removal, exact/non-null local facts, cast/access fusion and cast elision, repeated length/get/set opportunity tracking, checked native scalar struct/array access, native barrier fast paths, existing-card updates, transactional batched struct allocation, precomputed layouts, and current-main bounded handle-resolution reuse plus shared resolver stubs. | This is the clearest evidence that delayed facts pay. Facts are AMD64-heavy and are cleared conservatively at boundaries; several load-forwarding experiments fired statically but did not improve runtime. Merge facts by intersection at structured joins, preserve unaffected facts with effect summaries, track freshness/publication, and lower barriers only after facts stabilize. Port the semantic layer once, then select AMD64/ARM64 sequences separately. |
| Layout, pools, frames, and cold code | Function/internal-entry alignment, V128 trailing pools, grouped trap stubs, register-only frame elision, compact ARM64 frame adjustments, AMD64 small-frame elision, adapter/stub sharing in selected paths, and code-size telemetry. | Decisions are made by separate heuristics and no final layout owner can trade branches, padding, pools, veneers, stubs, and hot/cold placement together. Implement #326/#333 under one finalizer and make choices consume #327's speed/balanced/size/embedded objective. |
| Runtime boundary optimizations | Direct native-context rebinding, prepared-call caches, private/isolated leases, direct integer entry, scalar invoke caching, host transition specialization, zero-allocation steady-state invocation, and selected instance/structural-identity caching on current main. | These are not peepholes but explain several wazero comparison rows. Keep them separate from codegen claims. Continue specializing from immutable compiled/instance facts, with concurrency/isolation invariants expressed as a small entry-mode state machine and tested under race/attachment changes. |

### The most important problems in the current optimizer itself

1. **Per-compile configuration is global mutable state.**
   [`optimization.Bindings.Apply`](../src/core/compiler/optimization/catalog.go)
   locks a backend `Bindings`, mutates package booleans, and holds the lock until
   the whole module compile returns. Both backends call it even when no override
   is supplied. This makes rules harder to audit and can serialize independent
   same-architecture module compilations. Replace it with an immutable
   per-compile `OptConfig` copied into module/function state. Keep environment
   variables only as construction-time defaults.
2. **The public catalog is incomplete.** The 42 definitions omit meaningful
   switches for GC facts/allocation/resolver reuse, interval regions, SWAR/SIMD
   superoptimization, float-compare fusion, memory commutation, magic division,
   and several runtime entry paths. Conversely, `stack-fence` and `stack-reg`
   are operational invariants presented beside code-quality choices. One typed
   catalog should distinguish semantic safety, target capability, optimization
   objective, experimental rule, and diagnostic A/B.
3. **High-level rule logic is duplicated by target.** AMD64 and ARM64 each own
   copies of constant folding, stack semantics, bounds facts, inlining scans,
   SIMD/SWAR matchers, call staging, and GC pattern logic. Encoding must remain
   target-specific; semantic matching, effect facts, and proof tests should not.
4. **Lookahead is becoming an accidental IR.** Call next-use scans up to 64
   operations, loop prechecks rescan bodies and allocate maps, dead-GC and SIMD
   needles peek through the reader, and hints perform separate whole-body work.
   A bounded region representation would make that state explicit, reusable,
   allocation-bounded, and easier to invalidate correctly.
5. **Early byte emission destroys optimization information.** The current
   post-assembly branch peephole can only do a size-preserving rewrite. Copies,
   extensions, flags dependencies, address alternatives, macro-fusion, and
   scheduling are much easier before bytes and PCs become authoritative. Retain
   a tiny symbolic machine window, then encode once.
6. **Fixed heuristics do not implement a coherent objective.** Examples include
   the eight-leaf associative cap, 64-op call lookahead, 160-byte inline ceiling,
   fixed alignments, pool choices, loop-precheck density, and local pin counts.
   Hard resource caps are good; profitability thresholds should flow from a
   measured target cost model and #327 objective.
7. **Static hit counts are not hotness.** The reverted GC load caches are a good
   warning: thousands of static sites can be dynamically cold. Keep static
   counters for coverage, but rank work using executed sampling or representative
   end-to-end A/Bs.

### How to strengthen the optimizations already present

| Current mechanism | Stronger next form |
|---|---|
| Valent condensation | Competing costed covers over a labeled bounded DAG, with current direct condensation as fallback. |
| Recursive Sethi-Ullman need | Store register-need/cost labels incrementally; include fixed registers and destination constraints. |
| Eight-leaf associative accumulator | Fuel-bounded n-ary cover with target-aware ordering, immediate/memory leaves, multi-use rejection, and spill-cost comparison. |
| Shape-only scalar folds | Incremental known-bit/range/alignment/affine lattice; no recursive no-hit tree walks. |
| Short flags window | Symbolic condition values and explicit flag clobbers through a bounded machine window; enable wide-arithmetic carry chains. |
| Static pin families | One regional next-use allocator with rematerialization and pressure-aware admission. |
| Direct-base loop precheck | Affine induction/trip-range proof, dense scratch, SIMD/multi-memory sizing, and objective-aware versioning. |
| Exact store/load forwarding | Byte-range memory epochs, partial overlap/narrowing, fresh-object alias classes, and store combining. |
| Fixed leaf inliner | Call-site net cost using removed setup/stubs, caller pressure, propagated constants/facts, hotness, and code objective. |
| Handwritten SIMD/SWAR needles | Generated semantic rules, proof provenance, near-miss generation, independent-producer evidence, and optional bounded SLP. |
| Conservative GC fact clearing | Structured-join intersection, loop-modified-local masks, freshness/publication state, bounded resolved-address reuse, and late barrier lowering. |
| Size-preserving branch fold | Real branch/frame relaxation plus hot/cold block layout and exact metadata remapping. |
| Per-function linear constant pools | Fixed-value keys, reusable site storage, costed local/register/module-island alternatives under one layout owner. |

### Public optimizer catalog on current main

The 43 registered definitions are:

```text
bounds-facts, st-flags, store8-flags, reg-merge, tee-sink, unary-sink,
three-op-sink, olddest-rhs-sink, branch-fold, store-load-fwd, uxtw-add,
entry-arg-pins, x8-pin, ext-fp-pins, call-next-use,
affine-lea, tree-order, assoc-tree, bmi2-rorx, leaf-scratch-pins,
vex-float-mem, multi-bounds-cert, immutable-table, immutable-table-type,
inline-callfree, store-forward, frame-elide, frame-elide-reghomed,
small-frame, v128-const-cache, v128-pins, v128-sink, reg-abi, inline,
loop-precheck, loop-region-pins,
immutable-poly-fastpath, legacy-fp-pins, legacy-gp-pins, stack-fence,
stack-reg
```

### Complete current peephole/explain event vocabulary

These 95 names are the union emitted by current AMD64 and ARM64 source. Candidate
and bookkeeping events are included deliberately so the audit trail is complete:

```text
affine-lea-cover, alias-load-kept, all-calls-inlined, alu-identity,
assoc-tree, assoc-tree-candidate, bmi2-rorx, br-pair-fold, br-table-jump,
call-dead-local-store, call-local-reload, call-local-reload-fp,
call-local-reload-gp, call-local-store, call-localset-fuse, call-result-x0,
cmp-branch-fuse, cmp-tee-branch-fuse, commute-mem-left, compare-setcc,
const-fold, deep-fp-local-pin, div-by-const, eh-root-clear, eh-root-init,
entry-arg-local-pin, eqz-fold, ext-elim, extend-wrap-elim,
fcmp-branch-fuse, fcmp-value-fallback, fcommute_mem,
final-cast-array-len-fuse, final-cast-struct-get-fuse, float-local-sink,
float-minmax-local-sink, frame-adjust-elide, gc-array-len-repeat,
gc-dead-new, gc-known-array-len, gc-known-struct-get, gc-ref-cast-elide,
gc-resolve-reuse, gc-shared-resolve-call, gc-struct-get-repeat,
gc-struct-set-get, if-local-sink, immutable-local-call-indirect,
immutable-table-type-check-elide, interval-region, interval-region-evict,
interval-region-reactivate, lea-scaled-index, linear-store-load-fwd,
local-3op-sink, loop-precheck, memcopy-unroll, memfill-unroll,
mixed-call-reg-arg, monomorphic-call-indirect, mul-add-fuse, mul-high-u64,
mul3-imm, old-dest-rhs-sink, pure-drop, same-operand, select-cmov,
select-flags, select-local-sink, simd-and-anytrue, simd-anytrue-vptest,
simd-local-forward, simd-mem-fold, simd-not-and, simd-rotr-imm,
simd-shift-imm, simd-shuffle-native, simd-shuffle-rotr,
simd-shuffle-same, simd-shuffle-zip, simd-tee-store-elide,
small-frame-adjust, store-imm, store-load-fwd, store8-flags, strength-reduce,
swar-mask-test, swar-pack4, swar-parse4, swar-widen4, tree-order,
tree-order-candidate, uxtw-add, v128-local-sink, xor-byte-mask
```

## Research papers and what to take from them

### 1. Costed expression-tree selection is the closest fit

| Primary source | Mechanism | Direct Railshot application | Cost and risk |
|---|---|---|---|
| Aho and Johnson, [Optimal Code Generation for Expression Trees](https://doi.org/10.1145/800116.803770) (1975/1976) | Dynamic programming finds an optimal cover for an expression tree in time linear in the number of tree vertices for the paper's machine model. | Railshot already owns bounded Valent trees. Label each node with several target alternatives and select a whole cover at `condense`, including address modes, immediates, destructive vs. three-operand forms, flags results, destination coalescing, and rematerialization. | Exact optimality does not transfer to aliasing, traps, shared DAG values, modern port pressure, or fixed-register constraints. The useful part is delayed whole-tree choice, not the theorem's machine assumptions. |
| Fraser, Hanson, and Proebsting, [Engineering a Simple, Efficient Code-Generator Generator](https://drhanson.s3.amazonaws.com/storage/documents/iburg.pdf) (1992) | A small BURG-style generator compiles tree rewrite rules into fast bottom-up matchers; dynamic programming chooses a minimum-cost cover. | Generate target selectors from declarative rules rather than duplicating nested Go switches. Wago can add rule validation and emit compact Go tables or direct code at build time. | Rule languages become opaque if they also encode effects, allocation, and ABI details. Start with pure integer/bitwise/compare/address trees and leave calls, loads with uncertain trap behavior, and GC effects on direct paths. |
| Sethi and Ullman, [The Generation of Optimal Code for Arithmetic Expressions](https://doi.org/10.1145/321607.321620) (1970) | Orders expression evaluation to minimize register/storage traffic under a simple register-machine model. | Wago already has a Sethi-Ullman-style `treeRegisterNeed` in `amd64/treeorder.go`. Generalize its labels to include target fixed-register needs and alternative covers, then port the facility to ARM64. | Reordering may change Wasm trap order. Only reorder nodes whose effect summary proves non-trapping and side-effect-free; loads in guard mode remain trapping. |

Current Wago seams:

- [`amd64/treeorder.go`](../src/core/compiler/backend/railshot/amd64/treeorder.go)
  computes register need but is not a general selector.
- [`amd64/treecover.go`](../src/core/compiler/backend/railshot/amd64/treecover.go)
  flattens bounded associative integer trees into one accumulator. This is a
  useful first cover, not yet a costed competing-cover framework.
- Both backends build deferred trees in `stack.go` and commit target choices in
  `emit.go`. A shared label/effect layer should sit between those two seams.

### 2. Superoptimization should be offline, with tiny online matchers

| Primary source | Mechanism | Direct Railshot application | Cost and risk |
|---|---|---|---|
| Massalin, [Superoptimizer: A Look at the Smallest Program](https://doi.org/10.1145/36177.36194) (1987) | Searches instruction sequences and uses testing/pruning to find very small implementations. | Search bounded, hot Wago semantic kernels: packed masks, rotations, comparisons, saturating idioms, carry chains, SIMD reductions, and address calculations. | Raw exhaustive search scales badly and randomized tests are not proofs. Do not put it in normal compilation and do not accept a candidate from tests alone. |
| Bansal and Aiken, [Binary Translation Using Peephole Superoptimizers](https://theory.stanford.edu/~sbansal/pubs/osdi08_html/index.html) (OSDI 2008) | Harvests sequences, searches offline, verifies candidates with SAT, and applies learned rules online with ordinary matching; live-register information makes more replacements legal. | Harvest actual Railshot output and compare it with LLVM/GCC/Cranelift output for the same small pure kernels. Include live flags/registers and target features in the rule key. Generate direct matchers for the best proven rules. | Machine-state modeling must include flags, partial registers, NaNs where relevant, traps, memory aliasing, and target feature availability. Start with register-only integer windows. |
| Sasnauskas et al., [Souper: A Synthesizing Superoptimizer](https://research.google/pubs/souper-a-synthesizing-superoptimizer/) (2017) | Uses synthesis over LLVM-like pure SSA expressions to discover missing peephole optimizations; discoveries were integrated manually into production compilers. | Translate bounded, pure Valent trees into a small bitvector expression language, use synthesis offline, then generalize and hand/audit the resulting rewrite. This is particularly attractive for `i32/i64` algebra and SIMD lanes. | Souper assumes an SSA-like, mostly pure value domain. Memory, Wasm traps, floats, and GC effects need separate semantics. The original project is now archived, so copy the method, not the dependency. |
| Lopes et al., [Provably Correct Peephole Optimizations with Alive](https://web.ist.utl.pt/nuno.lopes/pubs.php?id=alive-pldi15) (PLDI 2015), and current [Alive2](https://github.com/AliveToolkit/alive2) | Specifies local rewrites declaratively and proves refinement or produces counterexamples. Translating hundreds of LLVM rules exposed real wrong optimizations. | Make proof or exhaustive bitvector checking part of the rule-generation gate. Wago's integers have simpler semantics than LLVM poison/undef, but the model must include Wasm wraparound and trap/effect ordering. | Alive2 verifies LLVM IR, not Wago directly. A small Wago semantics or translation is engineering work. Keep runtime code generation independent of the prover. |

The practical first milestone is not “build a universal superoptimizer.” It is:

1. define pure `i32/i64` Valent semantics and target sequence semantics;
2. harvest the top 100 repeated hot shapes from corpus profiles;
3. compare Wago, LLVM, GCC, and search-generated sequences;
4. prove candidate equivalence for all integer inputs; and
5. emit a generated Go matcher plus positive, near-miss, and differential tests.

### 3. Equality saturation is useful inside a hard box, not as the default model

Tate et al., [Equality Saturation: A New Approach to
Optimization](https://www.cs.cornell.edu/~lerner/papers/popl09.html) (POPL
2009), retain many equivalent forms in an e-graph and select a profitable result
after saturation. This directly attacks phase-ordering failures: for example,
canonicalizing an expression one way should not destroy a later target fold.

Cranelift is a current production reference: its
[e-graph implementation](https://github.com/bytecodealliance/wasmtime/tree/42b3376636fc32d648719a0863fe112465152f7f/cranelift/codegen/src/egraph)
uses a whole-function SSA IR, while its public project description explicitly
calls out e-graphs, rewrite DSLs, fuzzing, and translation validation as core
techniques ([Cranelift project](https://cranelift.dev/)).

Do **not** copy whole-function equality saturation into default Railshot. Even a
small rewrite set can grow an e-graph aggressively, and effects/aliases/traps
complicate congruence. Two bounded uses are credible:

- offline: saturate harvested pure expression shapes and export only winning
  rules; or
- online: a fixed-size “micro e-graph” per Valent sink, with node, iteration,
  time/fuel, and extraction budgets. On exhaustion, extract the best known
  expression or use the original lowering.

The online version should be attempted only after a simpler dynamic-programming
tree selector demonstrates a real phase-ordering ceiling.

### 4. Better register allocation needs bounded next-use, not global intervals first

Traub, Holloway, and Smith, [Quality and Speed in Linear-scan Register
Allocation](https://dash.harvard.edu/bitstreams/7312037d-c641-6bd4-e053-0100007fdf3b/download)
(PLDI 1998), shows how a linear scan over live intervals obtains code quality
near graph coloring while compiling much faster. It is important evidence for a
speed/quality middle ground, but classic linear scan requires intervals over a
function and is therefore not a drop-in algorithm for Railshot's direct
allocator.

The transferable pieces are:

- split/reload at well-defined control boundaries;
- rematerialize constants and cheap pure expressions instead of spilling;
- prefer registers based on future uses and instruction constraints; and
- separate allocation policy from instruction selection costs.

For Wago, use the existing summary scan or a bounded region scan to record the
next few local uses and fixed-register constraints. Replace “deepest resident
operand” only where counters show ordinary spills or call reloads. A full
live-interval allocator is a different architecture and should be benchmarked as
such.

### 5. Demand-driven bounds reasoning is promising but SSA-dependent in its full form

Bodík, Gupta, and Sarkar, [ABCD: Eliminating Array Bounds Checks on
Demand](https://research.ibm.com/publications/abcd-eliminating-array-bounds-checks-on-demand)
(PLDI 2000), adds constraints to an SSA value graph and answers bounds-check
queries with sparse demand-driven traversals. The reported mechanism is
attractive for a JIT because work is focused on checks that matter, but its value
graph and loop reasoning are not available in direct Railshot.

Portable subset:

- enrich the existing bounded facts with affine forms `base + index*scale + c`;
- derive loop-header induction summaries during the existing body scan;
- query only repeated/hot checks;
- version a loop only when one precheck proves the complete accessed range; and
- cap proof steps and code growth.

Full ABCD, PRE, general LICM, and cross-block range propagation require SSA/CFG
infrastructure. They belong either in an explicit regional/basic-block IR
overhaul or outside the default path.

## Current upstream compiler and runtime survey

All source links in this section are pinned to the upstream head inspected on
2026-08-10.

### Baseline/direct Wasm compilers

#### Wasmtime Winch

Winch is the clean lower bound for complexity. Its
[register allocator](https://github.com/bytecodealliance/wasmtime/blob/42b3376636fc32d648719a0863fe112465152f7f/winch/codegen/src/regalloc.rs)
describes itself as single-pass, uses a bitset free list, and spills stack values
on demand. Its
[shadow stack](https://github.com/bytecodealliance/wasmtime/blob/42b3376636fc32d648719a0863fe112465152f7f/winch/codegen/src/stack.rs)
retains constants and locations, enabling immediate folds without an IR.

Wago already exceeds this model with deferred expression trees, hot-value
pinning, local sinks, exact needles, and more target folding. The useful Winch
lesson is negative: do not trade Wago's compile-speed lead merely for another
simple baseline allocator. New cost should buy whole-tree selection, scoped
facts, or a machine combiner.

#### V8 Liftoff

Liftoff maintains a virtual stack/cache state with constants, registers, stack
slots, use counts, merge states, and spill history in its current
[assembler state](https://github.com/v8/v8/blob/c635f0d160b6e988b5ea5a907511a2929beb5d5e/src/wasm/baseline/liftoff-assembler.h).
Its current
[compiler](https://github.com/v8/v8/blob/c635f0d160b6e988b5ea5a907511a2929beb5d5e/src/wasm/baseline/liftoff-compiler.cc)
also contains selective sequence detection where a large known idiom justifies
lookahead.

Portable ideas:

- make merge cache states explicit enough to avoid unnecessary canonical-slot
  round trips;
- preserve constants/rematerializable values through more consumers;
- add long lookahead only for measured, high-value sequences; and
- use last-spilled/use-count information to avoid repeatedly evicting the same
  registers.

Liftoff is not evidence for broad local CSE or global scheduling; those remain
the job of V8's optimizing Wasm tier.

### Small regional-IR compilers and interpreters

#### WAMR Fast JIT

WAMR Fast JIT currently builds basic-block JIT IR, lowers it, performs a reverse
per-basic-block allocation, and emits code. The inspected pass order is visible
in [`jit_compiler.c`](https://github.com/bytecodealliance/wasm-micro-runtime/blob/ba0377af0107d5ac9376b25896c8243ddd618e19/core/iwasm/fast-jit/jit_compiler.c),
the IR and instruction hashing in
[`jit_ir.h`](https://github.com/bytecodealliance/wasm-micro-runtime/blob/ba0377af0107d5ac9376b25896c8243ddd618e19/core/iwasm/fast-jit/jit_ir.h),
and allocation in
[`jit_regalloc.c`](https://github.com/bytecodealliance/wasm-micro-runtime/blob/ba0377af0107d5ac9376b25896c8243ddd618e19/core/iwasm/fast-jit/jit_regalloc.c).

This is the most relevant precedent for a massive Wago overhaul: retain one
basic block, hash-cons pure expressions, schedule with exact local next-use, and
discard the IR after emission. It enables stronger local CSE and scheduling than
Valent trees because shared values form a DAG. The price is clear: more
transient nodes, a lowering phase, virtual registers, and a second traversal.

If pursued, improve on WAMR's shape rather than cloning it:

- fixed-capacity, typed arenas owned by a compiler worker;
- no pointer-rich per-node allocations;
- effects and traps encoded explicitly;
- target cost selection before physical allocation;
- hard region/fuel limits with direct-lowering fallback; and
- region lifetime no longer than a basic block or structured unique-predecessor
  region.

#### Wasm3

Wasm3's current
[compiler](https://github.com/wasm3/wasm3/blob/d77cd814aa0bc68cb1df917580a6304d34cfb30b/source/m3_compile.c)
specializes threaded operations by operand location, retains one integer and one
floating cached register, and uses copy-on-write local/slot behavior; its
[operation machinery](https://github.com/wasm3/wasm3/blob/d77cd814aa0bc68cb1df917580a6304d34cfb30b/source/m3_exec_defs.h)
is the source of its superinstruction-like specialization.

The reusable principle is to make value location part of rule selection so
copies disappear. Wago should apply that to native covers and ABI/merge moves,
not copy interpreter dispatch machinery.

### Whole-function optimizing compilers: mine rules, do not copy architecture blindly

#### wazero Wazevo

Current wazero uses a whole-function SSA pipeline. Its
[pass driver](https://github.com/tetratelabs/wazero/blob/3ab421731a94caa7f407973ee689385707f7af81/internal/engine/wazevo/ssa/pass.go)
runs dead-block elimination, dominators, redundant-phi elimination, no-op
elimination, DCE, block layout, and loop/dominator construction. The source
still lists constant folding, CSE, arithmetic simplification, block coalescing,
copy propagation, tail duplication, and loop unrolling as future work. The
[backend](https://github.com/tetratelabs/wazero/blob/3ab421731a94caa7f407973ee689385707f7af81/internal/engine/wazevo/backend/compiler.go)
then lowers SSA values to virtual registers and performs register allocation.

Consequences for Wago:

- wazero's current execution advantage on a workload is not proof that a
  sophisticated SSA mid-end caused it; inspect disassembly and runtime/ABI paths;
- its AMD64/ARM64 lowering files are useful sources of target idioms and ABI
  choices; and
- its CFG/global passes are unsuitable for bounded Railshot unless Wago makes an
  explicit whole-function-IR decision.

#### LLVM and Clang

LLVM separates transformations by abstraction and cost:

- [InstCombine](https://llvm.org/docs/InstCombineContributorGuide.html) is a
  target-independent canonicalizer. Its contributor guide explicitly routes
  constant folding, value tracking, no-new-instruction simplification,
  expensive combines, and target-costed vector combines to different places.
- [GlobalISel combiners](https://llvm.org/docs/GlobalISel/Pipeline.html) rewrite
  Machine IR before/after legalization with target-specific rules and iteration
  limits.
- [MachineCombiner](https://github.com/llvm/llvm-project/blob/1fb48d1418cf55f398024c60af13436b806561f9/llvm/lib/CodeGen/MachineCombiner.cpp)
  evaluates target patterns against critical-path latency and resource depth;
  its own comments use multiply-add as the prototype and avoid combines that
  lengthen the critical path or increase resource pressure.
- [PeepholeOptimizer](https://github.com/llvm/llvm-project/blob/1fb48d1418cf55f398024c60af13436b806561f9/llvm/lib/CodeGen/PeepholeOptimizer.cpp)
  performs compare, select, conditional-branch, extension, load, recurrence, and
  copy/source rewrites around Machine IR and def-use information.

Portable ideas for Railshot:

- keep target-independent canonicalization separate from target-costed combines;
- test negative, commuted, multi-use, vector, and flag cases for every rule;
- compare latency/dependency depth and bytes, not only instruction count;
- track fixed-register and register-pressure effects in a candidate cover; and
- run pre-allocation combines while symbolic operands are available, then a
  narrower post-allocation cleanup.

LLVM's full InstCombine, MachineTraceMetrics, GlobalISel worklists, and def-use
chains assume IR/MIR and a function CFG. Copying the pass names without those
invariants would produce brittle peepholes.

#### GCC

GCC's current
[`combine.cc`](https://github.com/gcc-mirror/gcc/blob/baa4e4ddf800602d818470ef493d3508c4e1e32a/gcc/combine.cc)
substitutes definitions into uses and recognizes valid replacements over pairs,
triplets, and a small number of quadruplets. It uses logical links, costs,
nonzero/sign-bit facts, and refuses unsafe crossings such as calls. GCC also
runs machine-specific peepholes before and after allocation; the official
[optimization options](https://gcc.gnu.org/onlinedocs/gcc/Optimize-Options.html)
document forward propagation, instruction combination, if-conversion, DCE/DSE,
store merging, rematerialization, and the different speed/size optimization
profiles.

The best bounded Wago analogue is a small producer-to-use combiner over symbolic
machine instructions, capped at two to four definitions and stopped by calls,
traps, unknown memory, merge targets, and effect barriers. GCC's general dataflow
and loop passes require the global representations Wago intentionally lacks.

## Current open Wago issues with optimization leverage

Every issue in this table was `OPEN` in the live GitHub query on 2026-08-10.
The status is a snapshot, not a promise that the issue remains open later.

| Issue | Optimization idea already captured | Relationship to this research |
|---|---|---|
| [#297 — Fix severe runtime performance gaps](https://github.com/wago-org/wago/issues/297) | Concrete losing rows: backward overlapping copy, BLAKE, branch classifier, and tiny calls. | These should be the first profile/disassembly harvest set. Backward copy and tiny calls are runtime/ABI tracks; BLAKE and classifier are strong local selector/combiner targets. |
| [#314 — Structured GC facts, load forwarding, and dead-allocation elimination](https://github.com/wago-org/wago/issues/314) | Bounded reference facts, exact-type/cast folding, constructor/field forwarding, fresh-object non-aliasing, bounded caches, dead allocation trees, selective superinstructions. | Use the same region fact/effect substrate as scalar optimization. Do not build a parallel GC-only optimizer framework. This is the clearest open issue for bounded DAG/value-numbering work. |
| [#315 — Late GC barrier lowering and reference bulk operations](https://github.com/wago-org/wago/issues/315) | Select barriers only after type/freshness/generation/card facts are known; compact native bulk paths. | Strong evidence for delayed whole-cover selection. Barrier state is an effectful target lowering, not an early decoder choice. |
| [#316 — Reuse arenas and buffers across Valent-block compilation](https://github.com/wago-org/wago/issues/316) | Typed transient arenas, dense/epoch tables, reusable bounded assembler/metadata buffers, call-effect summaries, and richer flush/spill counters. | Prerequisite for spending more optimizer work without losing Wago's memory advantage. The proposed region optimizer should allocate only from these worker-owned arenas. |
| [#317 — ARM64 WasmGC native-fast-path parity](https://github.com/wago-org/wago/issues/317) | Port semantic fast paths while selecting native AArch64 addressing and conditional forms rather than mechanically translating x86. | A shared semantic rule plus per-target cover is the right seam. Avoid another backend-by-backend copy of high-level pattern logic. |
| [#322 — WasmGC/compiler/JIT-size roadmap](https://github.com/wago-org/wago/issues/322) | Tracks #314–#317 and requires A/Bs, compile cost, generated bytes, runtime, memory, and architecture parity. | Use these acceptance gates for all new optimizer work. It explicitly keeps whole-function SSA out of the current plan. |
| [#326 — AMD64 final code relaxation](https://github.com/wago-org/wago/issues/326) | Fixed-point `rel8`/`rel32` branch relaxation, short stack adjustments, and complete PC metadata remapping. | High-confidence project. It improves I-cache/code size and establishes the finalizer/offset-map infrastructure needed by a stronger post-lowering machine pass. |
| [#327 — Speed, balanced, size, and embedded objectives](https://github.com/wago-org/wago/issues/327) | Coherent optimization objectives for alignment, inlining, pools, adapters, worker policy, and artifact identity. | Make this the optimizer cost-model interface. Internal rules should consume objective weights; public API should not expose dozens of peephole toggles. |
| [#330 — Emit serial code into a checked final executable arena](https://github.com/wago-org/wago/issues/330) | Remove full native-image copies while preserving rewind, metadata patching, W^X, and deterministic artifacts. | Primarily compilation/memory work, but it can repay some of the compilation budget spent by smarter optimization. Coordinate its in-place layout with #326 and literal placement. |
| [#333 — Literal islands and allocation-free constant pooling](https://github.com/wago-org/wago/issues/333) | Fixed-size constant keys, reusable site storage, measured per-function vs. shared islands, target range constraints, objective-aware choice. | Add literal materialization/pool/island alternatives to the costed selector. Pooling and code relaxation need one layout owner. |
| [#339 — WebAssembly Wide Arithmetic](https://github.com/wago-org/wago/issues/339) | Preserve multi-result add/sub/multiply identity through lowering to carry chains and high-half multiply instructions. | Excellent stress test for flags-resident values, fixed-register constraints, multi-result covers, and target-specific selection. Do not expand to generic scalar ops too early. |
| [#340 — Acquire-Release Atomics](https://github.com/wago-org/wago/issues/340) | Preserve memory order through decode and target lowering; constrain CSE, DCE, forwarding, and scheduling around atomic effects. | The new effect model must encode ordering barriers before it is trusted for memory combines. This issue supplies hard negative tests. |

Issue-level ordering suggested by the dependencies:

1. #316 optimizer scratch/counters and #327 objective seam;
2. disassembly/profile attribution for #297;
3. generalized Valent cover and offline rule laboratory;
4. #326 finalizer/offset map, coordinated with #333;
5. #314/#315 facts and late effectful lowering on the shared substrate;
6. #317 parity as a semantic-rule/per-target-cover test; and
7. #339/#340 as next-generation flags/effect stress tests.

## New project ideas

### Project A: Railrule, one semantic rule catalog

Introduce a small internal rule description with:

```text
match shape
value type and semantics
required known bits/ranges/alignment/features
effects and possible traps
result alternatives
register classes, fixed registers, flags, and clobbers
latency / reciprocal throughput / byte cost
objective and code-growth guards
proof/test provenance
```

Generate three consumers:

- a Valent-tree matcher before materialization;
- a symbolic-machine combiner after lowering; and
- exhaustive/differential rule tests.

This is a breaking cleanup worth doing before v0.1: high-level rewrite semantics
should not be duplicated in AMD64 and ARM64 packages. Target encodings and costs
remain target-local.

### Project B: Fixed-capacity Valent DAG

Within one basic block or structured unique-predecessor region:

- hash-cons pure non-trapping nodes;
- maintain a fixed open-addressed value-number table;
- attach known-zero/known-one bits, small ranges, alignment, and reference facts;
- give loads/stores/calls explicit memory/effect epochs;
- forward only under exact alias/freshness proof;
- stop at merges, calls with unknown effects, atomic barriers, or budget exhaustion;
- extract each sink through the costed target selector; and
- release the arena immediately after the region.

This makes common subexpressions and facts shareable without retaining a
whole-function graph. It is more powerful than the current tree-only model and
less invasive than WAMR Fast JIT.

### Project C: Symbolic machine window and scheduler

Retain the last small machine-instruction window before encoding. Track defs,
uses, flags, memory class, trap sites, branch targets, and estimated target
latency. Apply:

- producer-to-use folding over two to four instructions;
- compare/test/branch and select/cmov/csel formation;
- extension and partial-register cleanup;
- copy propagation and store-to-load forwarding;
- multiply-add and address-mode formation;
- independent instruction reordering to shorten dependency depth; and
- target-aware macro-fusion adjacency on AMD64.

Flush the window at an effect barrier, incoming branch target, unknown alias,
call, trap-order boundary, or capacity limit. This borrows the useful local
parts of GCC combine and LLVM MachineCombiner without building their CFG/MIR.

### Project D: Differential missed-optimization miner

For each hot function or bounded pure region:

1. emit Wago disassembly and counters;
2. compile an equivalent C/Rust kernel through current Clang/LLVM and GCC;
3. optionally compare Cranelift/wazero target output;
4. normalize register names and extract dependency/byte costs;
5. identify repeated Wago-only extra moves, loads, branches, or instructions;
6. reduce to a minimal Wasm reproducer; and
7. feed the shape into Railrule/superoptimization.

The output is a ranked backlog backed by dynamic samples and occurrence counts,
not a grab bag of classic transforms.

### Project E: Two-dimensional compile budget

Optimization effort should be constrained by both work and output:

```text
analysis fuel       = visited nodes + rewrite attempts + proof/fact steps
code-growth budget  = emitted bytes + duplicated cold paths + pools/veneers
```

Allocate the budget hierarchically by module, function hotness/size, and region.
A giant cold generated function should not consume unbounded compile time; a hot
small kernel can receive a larger local search budget. This implements #327
without exposing every internal knob.

### Project F: Compiler-oracle mode

Because the project is pre-v0.1, add a deliberately heavy developer-only mode
that compiles each function two ways:

- normal bounded Railshot; and
- experimental regional IR / exhaustive local selection.

Compare semantics, generated code, counters, and runtime. Never ship this mode
in minimal products. It provides a quality ceiling and lets experiments prove
that complexity earns its place before migrating a bounded subset into the
default compiler.

## Techniques that do and do not fit the current architecture

| Technique | Bounded/direct fit | Needs explicit regional or whole-function IR |
|---|---|---|
| Costed tree covering, commutation, address/immediate folding | Yes | No |
| Offline-superoptimized local rules with online matching | Yes | No |
| Fixed-size known-bit/range/alignment facts | Yes | No |
| Bounded next-use and rematerialization | Yes | No, if limited to a region or summary lookahead |
| Two-to-four producer machine combine | Yes | No, with symbolic window and hard barriers |
| Exact post-layout relaxation | Yes | No, but needs offset remapping |
| Bounded basic-block DAG/value numbering | With an intentional regional-IR change | Regional IR |
| General CSE/GVN/PRE | No | Whole-function SSA/CFG |
| General LICM/induction-variable rewriting | Only exact pre-scanned loop shapes | SSA/loop forest or equivalent |
| Trace scheduling and global block placement | No | CFG, frequencies, liveness |
| General scalar replacement / escape analysis | Only tiny fresh structured objects (#314) | Whole-function alias/escape analysis |
| Equality saturation over complete functions | No | Whole-function e-graph/SSA and substantial memory |
| Classic linear-scan intervals | No | Whole-function intervals/liveness |

The pre-v0.1 freedom makes either column possible. The important decision is to
name the architecture honestly, budget it, and measure compile latency,
allocations/RSS, generated bytes, and execution together. A bounded regional IR
can be an excellent design; an unbounded one assembled accidentally from maps,
lookahead caches, and special cases cannot.
