# Downloads paper review: expanding Railshot's Valent trees

Date: 2026-08-10
Repository baseline for the source audit:
`ad4fa1e407c12a8acb974634083b585cf1002213`
Companion survey: [singlepass-execution-performance-research-2026-08.md](singlepass-execution-performance-research-2026-08.md)

## Decision

The papers in `~/Downloads` strengthen the case for making Railshot's existing
Valent tree into a **bounded costed instruction selector**, rather than merely
raising `maxDeferDepth` or adding more root-local peepholes.

The most valuable next project is a breaking redesign with three pieces:

1. a compact effect-typed tree node with a stable arena index;
2. bottom-up labels for several result states, not just one scalar register-need
   number; and
3. a generated, target-specific rule catalog whose costs account for copies,
   spills, immediates, memory operands, fixed registers, code bytes, and the
   requested destination.

The first implementation should stay bounded and single pass: label only the
existing deferred tree at a sink, cap the prototype at 16 nodes, retain direct
lowering as fallback, and allocate no heap memory on a near miss. This is a
substantial increase in optimization power without introducing whole-function
SSA or an unbounded IR.

Sethi--Ullman supplies the scheduling foundation, iburg supplies the practical
minimum-cost covering model, and Souper/Minotaur supply an offline route for
discovering and proving rules. The two SWAR papers are useful sources of exact
integer rule families, but not reasons to put search or large fact lattices on
the online compilation path. Valent-Blocks confirms the architecture while also
making clear that its evaluation does not establish the best modern tree policy.

## Selected implementation slice

The first slice started alongside this review, intentionally below the full
multi-state selector:

- AMD64 deferred nodes now cache their Sethi--Ullman register-need label when
  their tree shape is created. One `labelDeferredNode` seam also maintains the
  existing height label and is the insertion point for later effect and cover
  states.
- The associative accumulator no longer copies at most eight leaves into a
  scratch array. Two allocation-free walks validate the tree, choose its
  maximum-need seed, and emit every remaining leaf. The existing height-six
  Valent bound is the fuel, so all currently representable associative trees
  are eligible (at most 64 leaves) without a second arbitrary cutoff.
- A sixteen-leaf balanced regression proves that one whole-tree cover is chosen;
  the old implementation could not cover that root.
- Destination-hinted trees are now covered when their stored register need is at
  least four. If exactly one flattened leaf reads the old destination, that leaf
  becomes the seed; repeated destination reads retain the ordinary alias-safe
  lowering. This makes requested destination and register pressure actual cover
  costs rather than a blanket rejection.

The final current-corpus result is **517 destination covers** in explicit mode:
native code falls by **4,169 bytes**, spills by **117** (5,461 to 5,344), and
reloads by three. Guard mode selects 471 destination covers, removes **864
bytes**, 12 spills (3,973 to 3,961), and one reload. The changed modules are
independent large producers: QuickJS/script, SQLite, and Ruby. Every runnable
corpus module remains byte-identical, so this is a generated-code/register-
pressure result rather than a claimed execution-time win.

The need-four threshold is measured policy, not folklore. An initial need-three
version selected 1,470 explicit-mode sites and removed more code, but it changed
small hot JSON/UTF functions without reducing their spills; 14-sample reversed-
order measurements regressed the six causally affected execution rows by 0.35%
geomean. That version was rejected. Need four retains 117 of its 129 removed
spills while eliminating every runnable-corpus code change.

On the affected large compilers, 20 base/new samples on a pinned Ryzen 7 7800X3D
leave ordinary SQLite/Ruby codegen and full compilation statistically flat.
Forced-worker Ruby codegen is the most sensitive compile-time trade at +2.25% to
+3.42%; allocation bytes and counts decrease slightly. The complete AMD64
backend suite, AMD64 vet, and the guard-enabled corpus differential against
explicit bounds and wazero pass.

## Corpus inventory

I recursively inspected all 21 PDFs in `/Users/work/Downloads`: 18 are top-level
files and three are AltTab application assets. Eleven filenames are
research-paper copies. SHA-256 deduplication leaves eight distinct PDF files
representing seven distinct publications: the scanned `vntblk.pdf` and the
text-bearing Valent-Blocks PDF are different files containing the same paper.
No additional research paper in DOC, DOCX, HTML, EPUB, PS, or TeX form was
present.

### Included papers

| Publication | Local files | SHA-256 | Pages | Identity |
|---|---|---:|---:|---|
| Sethi and Ullman, “The Generation of Optimal Code for Arithmetic Expressions” | `321607.321620.pdf` | `9dd12be60227bd9c48f26092b1adcaa164e3fa381968d51df94321c79cf71b4b` | 14 | JACM 17(4), 1970; [DOI 10.1145/321607.321620](https://doi.org/10.1145/321607.321620) |
| Fraser, Hanson, and Proebsting, “Engineering a Simple, Efficient Code-Generator Generator” | `iburg.pdf`, `iburg-1.pdf` | `9dfcea540d2dbef0a4245e1157467c4fb0e2f170cd0d9454fe8f460dddeb47bc` | 13 | LOPLAS 1(3), 1992; [DOI 10.1145/151640.151642](https://doi.org/10.1145/151640.151642) |
| Scheidl, “Valent-Blocks: Scalable High-Performance Compilation of WebAssembly Bytecode For Embedded Systems” | `Valent-Blocks_Scalable_High-Performance_Compilation_of_WebAssembly_Bytecode_For_Embedded_Systems.pdf`, `-1.pdf`, `-2.pdf` | `95c63b945398fcf9b72ef5eea18b3e8f2a3b0595e01b11fc1e2799e838ff9f9c` | 6 | iCCECE 2020; [DOI 10.1109/iCCECE49321.2020.9231154](https://doi.org/10.1109/iCCECE49321.2020.9231154) |
| Same Scheidl paper, image-only scan | `vntblk.pdf` | `9bf720e5a034695b9fb7a78ec9b6e9dc9c929e70eb19ec40cb359b02da48c076` | 6 | Same publication and pagination; different PDF representation |
| Sasnauskas et al., “Souper: A Synthesizing Superoptimizer” | `1711.04422v2.pdf` | `b01f936a799c64e049f88028a49cb23a6396cfbc0e52d28eed5b3b817ae0febc` | 14 | [arXiv:1711.04422v2](https://arxiv.org/abs/1711.04422) |
| Liu, Mada, and Regehr, “Minotaur: A SIMD-Oriented Synthesizing Superoptimizer” | `2306.00229v3.pdf` | `e82b353ace9e9816deae033f15815c80b16d636ad06d0c55abe5ebb797ac5136` | 25 | OOPSLA 2024; [DOI 10.1145/3689766](https://doi.org/10.1145/3689766); [arXiv:2306.00229v3](https://arxiv.org/abs/2306.00229) |
| Vigna, “Broadword Implementation of Rank/Select Queries” | `Broadword Implementation of Rank_Select Queries.pdf` | `ece11f3d946ca1ac31fb873f531c8fec770cfd2da45148365d42abeb8bf8f03b` | 16 | WEA 2008; [DOI 10.1007/978-3-540-68552-4_12](https://doi.org/10.1007/978-3-540-68552-4_12); local copy revised 2023-01-31 |
| Lamport, “Multiple Byte Processing with Full-Word Instructions” | `Multiple-Byte-Processing-with-Full-Word-Instructions.pdf` | `37463e990521b88aa4d3b775afee5891fa46b444679f62079126cc5cc41ea24b` | 5 | CACM 18(8), 1975; [DOI 10.1145/360933.360994](https://doi.org/10.1145/360933.360994) |

Exact duplicate groups are therefore:

- the three text-bearing Valent-Blocks filenames; and
- `iburg.pdf` plus `iburg-1.pdf`.

### Excluded PDFs

The remaining PDFs were opened or identified from their text and metadata and
excluded as textbooks, coursework, project/brand documents, receipts, or
application assets. Their private filenames and metadata are intentionally not
recorded in the repository. No additional research source was present in PS or
TeX form.

## Paper-by-paper matrix

| Paper | Main contribution and algorithm | Assumptions | Evaluation | Important limitations | Railshot relevance |
|---|---|---|---|---|---|
| Sethi--Ullman | Bottom-up labels give the register need of an expression tree; a top-down schedule minimizes instructions/storage references for its machine model. It extends this to commutative flips and associative-commutative clusters ordered by label. | Tree, distinct leaves, specified algebraic laws only, homogeneous general registers, simple load/store/binary-op machine and fixed register count. | Formal optimality proofs and worked examples, not an empirical modern-CPU study. | No DAG sharing, target encodings, immediates, flags, memory operands, fixed-register instructions, instruction latency, or precise traps. Integer AC transformations map cleanly to Wasm; floating-point reassociation does not. | Directly improves `treeRegisterNeed` and the associative cover. Most useful idea: turn a same-op binary tree into an n-ary cluster, sort descendants by a target-aware label, then choose the best accumulator shape. |
| iburg | A tree grammar defines target patterns, nonterminals, and costs. A bottom-up dynamic-programming label pass records the cheapest rule for every goal state; a top-down reduction emits the selected cover. It hard-codes readable matchers and closes chain rules. | Subject is a tree; each operator has fixed arity; costs are non-negative; iburg can use compile-time/contextual costs, while BURS precomputes only constant costs. | In SPEC-era `lcc`, iburg matching consumed 8.5–12.4% of compile time versus burg's 1.1–2.0%; burg's matcher was roughly 6–12x faster. Early iburg was up to 25x faster than Twig on cited modules. | Tree-only covering duplicates shared DAG work; a naive state record per Railshot node would bloat compile memory; constant BURS costs cannot express register pressure or destination/context without operator splitting or state expansion. | Best template for a generated “Railrule” catalog. Railshot needs iburg-like dynamic costs over a tiny bounded tree, not a general compiler-generator runtime or a huge table automaton. |
| Valent-Blocks | Classifies Wasm operations by valence and non-stack effects, records pure expression trees on a virtual stack, and recursively emits them when a side-effecting instruction triggers resolution. It also proposes concurrent validation and an offboard local-hotness reorder. | Input Wasm was already aggressively optimized; simple numeric workloads; quasi-single-pass recursive resolution; a fixed scratch-register reserve. | Seven kernels. On x86-64, reported mean execution was 1.21x Clang, versus Liftoff 2.66x and Wasmer Singlepass 3.61x. Thumb-2 averaged 1.21x GCC. Ten runs, relative SD under 1%. | It did not measure compile time or compile memory, disabled ARM bounds checks, selected fastest flash alignment, used simple optimized inputs, and left correctness proof and richer workloads to future work. Its claimed “guaranteed side-effect free” sandbox language is imprecise. | Validates Railshot's architecture, but Wago already exceeds the paper with deferred loads, target fusions, facts, and richer allocation. The next gains come from smarter resolution and softer sinks, not merely copying the paper. |
| Souper | Extracts pure loop-free LLVM integer DAGs, adds path/merged-control facts, canonicalizes them, enumerates/synthesizes cheaper RHSs, proves equivalence with SMT, and caches results. | Functional bitvector DAG; originally scalar integer subset; no memory, floats, vectors, or loops in the modeled fragment. | Building LLVM found about 7,900 distinct rewrites used about 85,000 times. Souper reduced a Clang binary by 2.9 MB/4.4%, but that binary ran about 2% slower. Cold compile was commonly 5–25x slower; a warm cached LLVM build was roughly 9 versus 8 minutes. SPEC results were mixed. | Expensive online search; static code-size/instruction cost interacts badly with inlining and backend decisions; specific discoveries need generalization; unresolved block-path/undefined-behavior concerns appeared in the evaluation. | Keep synthesis offline. Extract bounded Railshot tree shapes and have a proof pipeline discover exact, bitwidth-polymorphic rewrite rules; check generated rules into a small target catalog. Do not run SMT in normal compilation. |
| Minotaur | Extracts bounded loop-free cuts across data/control/memory dependencies, enumerates concrete operations with symbolic constants, verifies refinement with Alive2, ranks with LLVM cost models and llvm-mca, then caches and applies rewrites. It formalizes 165 x86 SIMD intrinsics. | Relies on upstream unrolling/vectorization to expose local cuts; finite instruction/time bounds; accuracy depends on LLVM/Alive2/llvm-mca models and supported targets. | Cascade Lake: GMP average 7.3%, max 13%; SPEC CPU 2017 average 1.5%, max 4.5%. libYUV average 2.2%, max 1.64x. Warm cached SPEC builds were about 3% slower than Clang `-O3`; cold synthesis can be hundreds of times slower. | Still misses inter-iteration transformations; search and proof are far too expensive for Railshot's online path; target cost predictions can be wrong; vector/intrinsic semantics and cache infrastructure are substantial maintenance. | Strong evidence for target-aware offline rule mining, symbolic literal synthesis, flexible bit reinterpretation, and translation validation. It also argues that Wago's rule costs should use measured target sequence cost rather than instruction count alone. |
| Vigna broadword | Branchless SWAR rank/select: sideways addition, packed k-bit comparisons, multiplication-based lane aggregation, hierarchical rank/select inventories, and Elias--Fano variants. Includes an eight-operator unsigned packed comparison formula and select-in-word construction. | Unit-cost word RAM with fast add/sub/logic/multiply; primarily 64-bit or wider words; carefully bounded lane fields prevent cross-lane carry. | Compares rank/select space and nanosecond timings across sizes/distributions on its contemporary 64-bit systems; rank9 uses 25% auxiliary space and at most two cache misses. | Results are data-structure-specific and hardware-aged; several sequences now lose to native `POPCNT`, SIMD, or target-specific idioms; formulas require strict lane/range preconditions. | Feed its exact formulas to the offline rule miner and known-bits preconditions. Potential covers include packed comparisons, nonzero-byte masks, cumulative byte sums, and select stages. Require corpus hits and native-instruction comparison before retaining any rule. |
| Lamport SWAR | Introduces full-word processing of packed fields using per-lane test functions, a mask-expansion step, and masked selection, including equality, unsigned/signed comparison, overflow, and conditional packed updates. | Packed n-bit values generally have one guard bit per lane; full-word arithmetic/logic/shift and sometimes multiplication; profitability depends on fields per word and machine instruction costs. | Analytical instruction-count break-even examples: about 2.7 fields for the simple string case and generally around 3–4 fields; no modern hardware benchmark. | Extra guard bits reduce density; without guard bits only equality/inequality stays practical in its construction; instruction-count estimates do not model modern pipelines or SIMD. | Useful semantic decomposition for synthesized SWAR rules: predicate bits -> lane mask -> masked merge. It suggests cover states such as `lane-high-bits` and `lane-mask`, but is not itself a tree scheduler. |

## Current Railshot tree audit

The graph was refreshed and queried, then the returned paths were checked against
the source because the graph also indexes the repository's untracked
`.codex-patch-worktree/`.

### What exists now

- `amd64/stack.go` stores explicit `arg0`/`arg1` links in deferred `elem` nodes.
  `pushBinOp` folds constants, applies constant and same-operand identities,
  recognizes an exact SWAR shape, and caps the deferred height at six. Crossing
  the cap materializes the deeper child immediately.
- `amd64/treeorder.go` recursively computes one Sethi--Ullman-style integer label.
  Every concrete value costs one register. Equal child needs add one; otherwise
  the maximum wins. `treeReorderSafe` separately rejects deferred loads and
  other potentially trapping work.
- `amd64/emit.go:condenseBinary` applies that label only at a commutative binary
  root, only when the left child is deferred and strictly more expensive, and
  only when interval registers are absent. It then has separate handwritten
  choices for constants, memory-left commuting, scaled/affine LEA, small
  constant multiply, three-operand `imul`, and final materialization.
- `amd64/treecover.go` flattens at most eight leaves of `add`, `and`, `or`, or
  `xor`, chooses a maximum-register-need leaf as the accumulator seed, and
  consumes every remaining leaf in original collection order. It rejects a
  destination hint and uses a separate conservative safety predicate.
- `amd64/emit.go:applyALU` already exposes valuable result states implicitly:
  a right operand can be an immediate, register, borrowed local/global register,
  spill slot, local slot, or folded deferred memory reference. These choices are
  not represented in the current tree label.
- The target-specific knobs `tree-order` and `assoc-tree` are bound in
  `amd64/knobs.go`; focused tests and explain counters provide differential
  oracles.

### Gaps in the existing decisions

1. **One label conflates unlike leaves.** An `imm32` or foldable r/m right operand
   can require zero additional registers on AMD64; a large constant, owned
   register, borrowed register, fixed-register division, and deferred load all
   have different costs and hazards. They all currently label as one.
2. **The choice is root-local.** Handwritten fusions run in a fixed order. There
   is no comparison between overlapping covers such as affine LEA versus a
   commuted memory fold versus an accumulator cover.
3. **The associative cover only partially uses its information.** It chooses one
   maximum-need seed but does not sort all leaves, score their operand forms, or
   account for destination aliasing and future fixed-register hazards.
4. **Safety is duplicated.** `treeReorderSafe`, `treeAccumulatorSafe`, deferred
   load rules, division/shift hazards, and drop purity encode overlapping effect
   facts separately. Adding more covers will make drift likely.
5. **Height is a proxy for pressure.** A six-deep immediate-heavy tree may be
   cheap, while a shallower balanced tree with fixed-register operations may be
   expensive. Raising the cap alone already measured poorly in the companion
   report because the selector could not exploit the extra structure.
6. **There is no explicit cover provenance.** Explain mode can count a chosen
   peephole, but cannot report competing covers, their costs, or why a candidate
   was rejected.

## Proposed project: bounded Valent cover engine

This is intentionally a breaking redesign. Pre-v0.1 is the right time to replace
the current accretion of tree-specific helpers with one auditable selection seam.

### 1. Make tree semantics explicit

Replace the loose combination of `kind`, `op`, `typ`, `deferDepth`, and several
recursive safety predicates with compact generated metadata:

```text
node = { op, type, left, right, effect, arenaIndex, height }

effect = pure
       | readsImmutable
       | readsLocalVersion
       | mayTrapLoad
       | mayTrapArithmetic
       | writesState
       | control
```

The exact representation can remain smaller than this pseudocode. The key is
that reorderability, discardability, duplication, and legal covering derive from
one effect table. A local/global read also carries a version identity when future
regional value numbering is enabled. A deferred memory load remains ordered
even when its explicit bounds check has already happened, because guard mode can
still trap at the native load.

A stable arena index is worth the breaking change. It enables dense sidecar label
arrays without adding a large state record to every `elem`, makes cover traces
readable, and prepares bounded DAG sharing later. Use 32 bits unless a proven
function-size bound justifies 16.

### 2. Label multiple result states

At condensation, walk a tree of at most 16 nodes bottom-up and compute the best
candidate for a deliberately small state set:

```text
GPR              value in any general register
DEST             value in the requested destination without a final copy
IMM32            signed immediate accepted by the consumer
RM                foldable memory operand with required trap order
FLAGS            EFLAGS condition with a known predicate
ADDR              AMD64 base + index*scale + disp32 address form
RAX_RDX           fixed pair for divide or high multiply
RCX               fixed count form when BMI2 is unavailable
```

ARM64 should use its own states rather than pretending these are portable:
`GPR`, `DEST`, logical/arith immediate classes, shifted register, extended
register, address, and `NZCV` are more natural there. Share semantic rule names,
not target constraints.

Each candidate records a rule ID, child states, orientation, and a compact cost.
The label scratch is fixed-capacity and lives on the compile worker, so a near
miss is allocation-free.

### 3. Use a lexicographic cost, not one magic integer

The first prototype should optimize:

```text
(mandatory spills, mandatory copies, estimated uops, bytes, live-register peak)
```

This deliberately makes spill/copy avoidance dominate small throughput guesses.
Add target-tuned latency/throughput only after disassembly and benchmark evidence
show that the simpler tuple chooses poorly. Costs must be context-sensitive:

- requested destination and whether it aliases an operand;
- currently pinned/reserved/fixed registers;
- whether a child is already in an owned or borrowed register;
- immediate encoding class and memory-fold legality;
- hot/cold size policy from immutable per-compile configuration; and
- target feature set such as BMI2.

iburg shows why a grammar is useful, but its constant BURS cost model is not
enough for these contexts. Generate readable Go decision code in the iburg style
and evaluate small dynamic cost functions at compile time.

### 4. Start by expressing existing covers

The first rule catalog should produce byte-identical or better code for rules
Railshot already trusts:

```text
GPR(add GPR IMM32)             -> add-ri
GPR(add GPR RM)                -> add-rm
ADDR(add GPR (shl GPR {1..3})) -> base-index-scale
ADDR(add ADDR IMM32)           -> affine-disp
GPR(mul GPR {3,5,9})           -> lea-self-scale
GPR(mul BORROWED IMM32)        -> imul-rri
FLAGS(cmp GPR IMM32)           -> cmp-ri
DEST(add DEST X)               -> in-place accumulator
```

Only after parity should the selector add competing covers. This makes it
possible to delete `tryLeaScaledAdd`, `tryLeaMul`, `tryAssociativeTree`, and the
root-level commute chain incrementally rather than keeping two authorities.

### 5. Generalize associative trees properly

For modulo integer `add`, `mul`, `and`, `or`, and `xor` where Wasm semantics and
effects permit reassociation:

1. flatten a same-op cluster into at most 16 leaves;
2. label every leaf for consumer forms (`IMM32`, `RM`, `GPR`, `DEST`) and
   register need;
3. choose the seed using destination reuse first, then owned-register reuse,
   then highest expensive-subtree need;
4. sort the remaining nontrivial leaves by descending dynamic need, while
   scheduling immediates and legal r/m operands where they avoid materialization;
5. compare left-deep accumulation with a balanced subcover when latency or
   multiple three-operand instructions make it cheaper; and
6. preserve original order for any leaf whose effect class is not freely
   reorderable.

This is Sethi--Ullman Algorithm 3 adapted to a two-address target and real operand
forms. Include integer multiply in the candidate set; the current associative
cover omits it even though Wasm integer multiplication is associative modulo
2^N. Never reassociate scalar floating-point operations without an explicit
nonstandard relaxed-FP mode.

### 6. Add offline synthesis and proof, not online search

Build a separate developer tool that:

1. exports frequent missed or expensive Valent trees from explain/corpus runs;
2. canonicalizes commutative operands and bit widths;
3. enumerates short AMD64/ARM64 sequences with symbolic constants;
4. checks exact Wasm refinement, including traps and width behavior;
5. scores candidates with measured sequence throughput/latency and code bytes;
6. generalizes discoveries into typed, preconditioned rules; and
7. emits Go catalog entries plus exhaustive/differential tests.

Souper and Minotaur both show that solver search is powerful and compilation-time
expensive. Their best fit for Wago is an offline “rule foundry.” The checked-in
compiler should perform exact matching and cheap dynamic selection only. Proofs
should cover all i32 inputs exhaustively when feasible and use an SMT/Alive2-like
bitvector model for i64 and SIMD. The proof artifact belongs beside the generated
rule, so a reviewer can audit semantics separately from matcher code.

### 7. Defer DAG sharing until the tree selector earns it

Souper and Minotaur operate on DAGs, but Railshot currently has tree ownership
and release rules. Prematurely sharing nodes can lengthen live ranges and turn a
saved computation into extra spills. After the tree selector lands, instrument
duplicate pure subtrees inside a Valent region. Only if the corpus shows useful
frequency should a fixed-cap value-number table create shared nodes. The cover
engine would then compare recomputation against materialize-once, not assume
sharing always wins.

## Concrete new cover families suggested by the papers

### High-confidence tree work

1. **Full associative cluster scheduling.** Extend the existing eight-leaf
   accumulator to 16 leaves, include `mul`, sort all expensive leaves, and score
   immediates/memory/destination forms. This is the smallest useful vertical
   slice of the general selector.
2. **Address nonterminal.** Represent `base + index*scale + disp` as a result
   state instead of recognizing a few nested shapes. This naturally combines
   affine LEA, array offsets, object payload offsets, and constant displacement
   folding without persistent address IR.
3. **Flags nonterminal.** Let compares, `eqz`, selected masks, and boolean
   consumers cover to FLAGS/NZCV until a materialized boolean is actually needed.
4. **Conversion-through-consumer covers.** Fold wrap/extend into operand-width
   loads, compares, shifts, and three-operand forms where the target encoding
   already performs the extension. The tree grammar makes overlapping choices
   explicit.
5. **Fixed-register-aware scheduling.** Label division, remainder, high multiply,
   and variable shifts with their clobber sets so sibling order is chosen with
   RAX/RDX/RCX pressure in the cost rather than excluded by broad predicates.

### SWAR rule candidates

These need offline proof and corpus-hit gates:

- packed-byte equality/nonzero to lane-high-bit masks;
- expansion of lane predicate bits to all-ones lane masks;
- masked select `(a & mask) | (b & ~mask)` to target `andn`/blend forms;
- packed unsigned k-bit comparison with explicit guard-bit/known-range
  preconditions;
- sideways-add/popcount trees to scalar `POPCNT` or vector lane popcount where
  the result semantics match;
- repeated-lane constant formation and multiplication aggregation; and
- byte/word cumulative sums and select-in-word stages when the surrounding Wasm
  really implements rank/select.

Do not add these as unconditional online algebra. Wago already learned that a
broad known-bits mechanism can block better exact SWAR selections. Exact shape +
minimal precondition + target cost is the right admission rule.

## Delivery sequence

### Phase A: explain the lost choices

- Add opt-in tree dumps containing node IDs, effects, storage forms, current
  chosen order, spills/copies, and candidate cover names.
- Record histograms for node count, height, associative cluster size, register
  need, sink kind, and missed foldable operands.
- Run the committed corpus and representative real modules before changing code.

Exit: identify at least three frequent tree families where current root-local
selection emits an avoidable copy, spill, or instruction.

### Phase B: associative scheduler v2

- Introduce arena node indices and unified effect metadata.
- Implement n-ary integer clusters with all-leaf ordering and dynamic operand
  costs, still through existing emitters.
- Keep `tree-order` and `assoc-tree` fallbacks as differential oracles.

Exit: execution or code-size win on at least one broad corpus target, no
unexplained regression, bounded compile cost, and allocation-neutral near misses.

### Phase C: multi-state labels and generated rules

- Add the fixed-cap label scratch and the initial AMD64 states.
- Express existing ALU, immediate, r/m, LEA, multiply, and flags choices in the
  generated rule catalog.
- Compare generated selection byte-for-byte against current focused goldens,
  then delete superseded handwritten authority.

Exit: parity across focused tests and corpus, with explain output showing the
winning rule and rejected alternatives.

### Phase D: ARM64-native catalog

- Reuse semantic effect metadata and rule names.
- Define ARM64 immediate, shifted/extended-register, address, destination, and
  NZCV states independently.
- Prefer its three-operand forms rather than translating AMD64's two-address
  costs.

### Phase E: rule foundry

- Export real hot trees, synthesize and prove candidates offline, and check in a
  first small batch only when A/B execution measurements justify each family.
- Start with integer/SWAR and target instruction identities; leave floating point
  and memory-effect rewrites until the semantics model is mature.

## Measurement and rejection rules

For every phase measure, on the exact same commit and target:

- runtime for touched kernels and broad no-hit corpus cases;
- full and backend-only compile latency;
- allocations, peak RSS, and retained worker scratch;
- generated bytes, instruction/uop counts, spills, reloads, copies, and maximum
  live-register pressure; and
- candidate/hit/rejection counts by rule.

Use repeated uncontended runs and raw values before ratios. Use `hub@hub` for
AMD64 execution/disassembly when the local host is not AMD64. Reject a rule or
state that adds measurable compile/RSS cost without a repeatable execution or
code-size return. In particular:

- do not raise `maxDeferDepth` in isolation;
- do not add a heap map per function or per tree;
- do not run SMT during production compilation;
- do not reassociate floating point under standard Wasm semantics;
- do not reorder loads, trapping conversions, division, or other observable
  traps without an explicit proof that order is preserved; and
- do not make instruction count the sole target cost.

## Bottom line

The Downloads corpus does not argue for a traditional optimizing tier. It gives
Railshot a cleaner, more ambitious path that stays true to direct compilation:

> Turn each bounded Valent tree into a tiny target-specific dynamic-programming
> problem, with explicit effects and result states; discover sophisticated rules
> offline, prove them, and keep online selection exact and cheap.

The immediate implementation choice is **associative scheduler v2 followed by
the multi-state AMD64 cover engine**. It expands the tree work the user is most
interested in, subsumes current hand-written decisions instead of layering more
special cases on top, and creates a durable seam for both compiler research and
future ARM64 parity.
