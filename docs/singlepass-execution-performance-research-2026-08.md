# Raising Railshot execution speed without giving up direct compilation

Research date: 2026-08-05
Repository snapshot: `07950eb38ef343089580ce73e15f29dccebed567`

## Executive conclusion

The useful middle ground is not shunting yard, whole-function SSA, lazy code
motion, or classic trace scheduling. It is a **bounded region optimizer built on
the Valent stack that Railshot already has**:

1. cost and select whole deferred expression trees at their sink, rather than
   committing to each local instruction choice independently;
2. add scoped local value numbering for non-trapping pure values in a basic
   block or unique-predecessor structured region;
3. use bounded next-use information to make spill, rematerialization, and
   region-pin decisions;
4. strengthen explicit-bounds code with straight-line facts and carefully
   delimited loop prechecks; and
5. continue offline-discovered, online-bounded needle rules for important
   corpus idioms.

All five can preserve a single forward code-generation traversal. The first
three require delayed emission for a bounded region, so “single-pass” must mean
**one traversal of Wasm with bounded pending state**, not “machine bytes are
irrevocably emitted at every opcode.” That is also how serious baseline
compilers obtain their best local wins.

The first prototype should be **costed tree covering at a Valent sink**, not a
new general IR. Railshot already paid for the expression tree; a target-specific
cost label on its existing nodes can choose fused addressing, destructive vs.
three-operand forms, destination coalescing, rematerialization, and evaluation
order. This is the closest answer to “more powerful Valent blocks.” On the
current ARM64 corpus, this should initially optimize instruction sequences and
call/region moves rather than generic spilling: the measured slow functions
below report zero ordinary allocator spills.

## Embedded-first memory and scalability constraints

Execution speed is only one axis. Wago is intended to remain practical on small,
low-end, memory-constrained systems while scaling up cleanly on larger machines.
The optimizer therefore needs a **budgeted implementation**, not merely an
algorithm that is asymptotically reasonable.

The target memory model is:

```text
transient compiler memory
  = module summaries
  + worker count * bounded worker scratch
  + current bounded optimizer region

retained product memory
  = native code
  + required relocation/trap/GC/metadata tables
  + retained Wasm or segment data required by the product
```

Native code and required product metadata necessarily grow with the module. The
hard promise should be **bounded transient workspace plus output-sized retained
storage**, not constant total memory independent of input or output size. This
matches `docs/streaming-singlepass-plan.md`.

### Non-negotiable optimizer invariants

- Keep transient analysis at **O(locals + control depth + configured region
  budget)** per active worker. Do not retain a whole instruction graph, CFG,
  per-op live interval list, or unbounded expression table.
- Reuse worker-owned dense arrays, bitsets, bump storage, and open-addressed
  tables. Avoid pointer-rich maps and per-node heap objects in normal codegen.
- Put an explicit cap on Valent-tree labels, value-number entries, pending
  loads, next-use records, rewrite attempts, and optimizer fuel. Exhaustion must
  fall back to today's correct lowering, not allocate without limit.
- Treat **native-code growth as memory consumption**. Inlining, loop versioning,
  cold clones, and multi-versioned CPU paths need per-function and per-module
  byte budgets, not only latency-based profitability scores.
- Include I-cache pressure in the cost model. A sequence that is one cycle
  faster but much larger may be a loss on an embedded core and on large server
  workloads with many hot functions.
- Keep compile scratch separate from the compiled product. Oversized temporary
  chunks should be discardable after the function/module instead of permanently
  raising the reusable worker high-water mark.
- Do not add mandatory runtime profiling tables, duplicate hot-function code,
  or an unbounded tiering cache. Any future recompilation mode must be opt-in,
  byte-budgeted, and able to retire the replaced code safely.
- Preserve a serial, lowest-memory compilation policy. Parallel function
  compilation multiplies worker scratch and must be limited by both CPU count
  and a memory budget.

### Scale through policy, not separate architectures

Expose one small resource-policy seam and keep the implementation choices behind
it. Conceptually:

```text
compiler policy
  transient-byte budget
  native-code growth budget
  maximum workers
  optimization effort
```

The public interface should not expose every tree/window/table knob. A compact
policy can select internal limits:

| Policy shape | Intended system | Internal behavior |
|---|---|---|
| Minimal | MCU-class or severely constrained host | Serial compile, current small Valent trees, no code duplication, dense fixed scratch, exact needles only |
| Balanced | Ordinary embedded/edge and default server use | Bounded costed tree covering, scoped facts, conservative region analysis, measured loop versioning |
| Throughput | Memory-rich multicore host | More compiler workers and a larger—but still capped—region/fuel budget; same fallback and product format |

These are policy shapes, not necessarily three public enum values. The important
property is monotonic resource use: a larger budget may improve compilation
parallelism or code quality, while a smaller budget still compiles the same valid
module correctly with the direct fallback.

This is a deep module seam: callers provide resource intent; the compiler owns
the interaction among worker count, analysis effort, scratch layout, code growth,
and fallback. Leaking individual optimizer internals into runtime configuration
would create a shallow interface and make embedded tuning fragile.

### Memory acceptance gate

Every experiment in this document must report, at minimum:

- peak live heap and process RSS during full compilation;
- compiler allocations and worker-scratch high-water bytes;
- native-code bytes and retained compiler/runtime metadata bytes;
- compiled artifact size and per-instance retained footprint where affected;
- execution latency and allocations; and
- full compile latency at one worker and at the selected parallel worker count.

Test at least one small module, one large module, and one pathological function
that reaches the configured optimizer cap. The cap case must demonstrate bounded
memory and correct fallback. A runtime win that materially increases peak memory
or native code without an explicit policy decision is not a balanced win.

## Current Wago baseline and source audit

The repository is already well past a normal baseline compiler:

- `hints.go` performs one allocation-conscious body pre-scan for loop-weighted
  local/global hotness, calls, memory/control shape, inline candidacy, and stack
  arena sizing. Ordinary code generation is one emitting traversal, but the
  total backend is already intentionally **summary pre-scan + direct emission**,
  with bounded reader rewinds for exact needles and versioned-loop bodies.
- `stack.go` constructs explicit Valent expression trees from Wasm's postfix
  stream and caps deferred-tree height at six. A tree-cover prototype can
  therefore operate on bounded state that already exists.
- `emit.go` already performs target hints, operand commutation, constant and
  memory folding, scaled-index fusion, strength reduction, and local-result
  sinking. A selector must compare alternative *combinations* of these forms,
  not merely replace the existing switch with a rule table.
- `regalloc.go` spills the deepest resident operand as an approximation of
  farthest future use. Pinned locals/globals, lazy call spills, register merges,
  register ABI calls, frame elision, and architecture-specific register pools
  are already present.
- Exact bounded needles already handle compare/branch, SIMD/SWAR shapes,
  `call; local.set`, GC constructor/drop trees, and more. Post-assembly branch
  folding exists on amd64, and ARM64 has additional branch and store/load
  rewrites.
- Straight-line bounds certificates and hybrid fast/slow loop versioning are
  implemented in both backends. The open bounds opportunity is affine induction
  and indexed/running-pointer loops, not the original invariant-base precheck.
- Stores still call alias-blind `materializePendingLoads`, so a conservative
  disjoint pending-load window remains a real local seam.

### Measurements on the current checkout

Host: Apple M4 Max, Darwin/arm64, commit
`07950eb38ef343089580ce73e15f29dccebed567`. These are research-direction
measurements, not publication-grade cross-machine claims.

The matched small-module full-compile benchmark (decode + validate + native
codegen, 300 ms, five samples) produced these medians:

| Engine | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Wago | 6,881 | 44,576 | 67 |
| wazero | 40,994 | 302,927 | 265 |

Wago is **5.96x faster** on latency and uses **6.80x less allocated bytes** in
this fixture. One-iteration full-corpus samples are noisier, but the large-module
direction is consistent: Wago/wazero medians were about 69.0/220.2 ms for
regexmatch, 27.2/66.9 ms for Lua, 93.9/282.4 ms for SQLite, 1.146/4.377 s for
Ruby, and 0.760/2.569 s for esbuild. This leaves a meaningful compile budget,
but not an unlimited one: some small and SIMD modules are already close.

The matched prepared-function corpus execution sweep (150 ms, three samples)
shows that the gap is clustered:

| Export | Wago median | wazero median | Result |
|---|---:|---:|---:|
| `spectralnorm.run` | 404,742 ns | 685,537 ns | Wago 1.69x faster |
| `matmul.run` | 156,313 ns | 226,321 ns | Wago 1.45x faster |
| `sha256.hashN` | 29,246 ns | 44,714 ns | Wago 1.53x faster |
| `fannkuch.run` | 1,254,579 ns | 1,004,084 ns | wazero 1.25x faster |
| `nbody.step` | 163,425 ns | 146,582 ns | wazero 1.11x faster |
| `raytrace.render` | 275,136 ns | 236,282 ns | wazero 1.16x faster |
| `blake-as-simd.hashN` | 587,664 ns | 435,612 ns | wazero 1.35x faster |
| `utf-as-simd.convertN` | 142,329 ns | 98,470 ns | wazero 1.45x faster |
| `utf-as-simd.validateN` | 343,912 ns | 164,158 ns | wazero 2.09x faster |
| `tiny.add` | 65.24 ns | 20.20 ns | wazero 3.23x faster |

All Wago execution rows remained at 0 B/op and 0 allocs/op. `tiny.add` and
similar very short exports are dominated by the public native-entry path, not
the guest ALU sequence, and need to stay a separate runtime/ABI track.

Current explain counters further narrow the compiler work:

- `fannkuch` reports 15 pinned locals, 85 condensations, 64 flushes, and zero
  ordinary spills/reloads. Its problem is not a conventional spill storm.
- `raytrace` reports zero ordinary spills but nine call-local reloads in its hot
  helper, making call-aware liveness more relevant than generic victim choice.
- the hot BLAKE SIMD functions report 24/28 pinned locals, roughly 450 vector
  local sinks each, and zero ordinary spills, yet generate about 8.8/9.3 KiB of
  native code. The likely prize is better multi-op NEON selection, scheduling,
  and code shape rather than simply pinning more values.

Therefore the first implementation gate is disassembly/profile attribution on
these losing rows. A general algorithm earns its place only if it removes the
specific extra native instructions, dependency chains, branches, or call moves
found there.

## What “single-pass” means in the compared systems

The term is used too loosely. These are materially different designs:

| System | Actual pipeline | Useful lesson for Wago |
|---|---|---|
| Wasmtime Winch | Visits Wasm operators and emits through a macro assembler without an IR. Its allocator is a bitset free-list that spills on demand. | The clean reference for true baseline/direct codegen, but not the execution-quality target. |
| V8 Liftoff | Decodes and emits in one pass, while retaining a virtual operand stack, constants, register/slot locations, merge snapshots, and a little lookahead. | Strongest direct precedent for deferred values and bounded opcode fusion. |
| SpiderMonkey Wasm baseline | Fast Wasm-to-machine translation; its optimizing Wasm tier lowers through MIR/LIR. | Confirms the baseline/top-tier split, but public docs expose fewer reusable baseline details than Liftoff. |
| WAMR Fast JIT | Despite its name, it constructs basic-block JIT IR, normalizes/hash-conses it, lowers, runs a reverse local allocator, then emits. | A useful middle-quality design, but **not** evidence that those optimizations are free in a direct one-pass compiler. |
| Wasm3 | Compiles Wasm to threaded interpreter operations, with one integer and one FP cached register, copy-on-write locals, constant slots, and operand-location-specialized operations. | Useful storage/superinstruction ideas; not a native-code execution comparison. |
| Cranelift / TurboFan / SpiderMonkey Ion | Whole-function SSA/graph IR, optimization passes, sophisticated allocation, then codegen. | Shows the performance ceiling and which transformations need global information; it is deliberately outside Railshot's architecture. |
| LuaJIT | Records hot traces into IR, optimizes them, then performs reverse assembly/allocation; DynASM is the assembler generator, not the optimizer. | Trace specialization is powerful, but LuaJIT is not a one-pass/no-IR precedent. |

Primary sources:

- [Winch architecture](https://bytecodealliance.org/articles/winch-aarch64-support)
  and its [single-pass register allocator](https://docs.wasmtime.dev/api/src/winch_codegen/regalloc.rs.html).
- V8's [Liftoff design](https://v8.dev/blog/liftoff), current
  [Liftoff source](https://github.com/v8/v8/blob/24ca3c92423e223d6ce9b39835382a550a07148b/src/wasm/baseline/liftoff-compiler.cc),
  and [tiering pipeline](https://v8.dev/docs/wasm-compilation-pipeline).
- Mozilla's [SpiderMonkey architecture](https://firefox-source-docs.mozilla.org/js/).
- WAMR's current
  [pass pipeline](https://github.com/bytecodealliance/wasm-micro-runtime/blob/70f4cd383f1a474d6759e3185b4eca6f6ddde4d4/core/iwasm/fast-jit/jit_compiler.c),
  [normalizing IR constructor](https://github.com/bytecodealliance/wasm-micro-runtime/blob/70f4cd383f1a474d6759e3185b4eca6f6ddde4d4/core/iwasm/fast-jit/jit_ir.h), and
  [per-basic-block reverse allocation](https://github.com/bytecodealliance/wasm-micro-runtime/blob/70f4cd383f1a474d6759e3185b4eca6f6ddde4d4/core/iwasm/fast-jit/jit_regalloc.c).
- Wasm3's current
  [compiler](https://github.com/wasm3/wasm3/blob/d77cd814aa0bc68cb1df917580a6304d34cfb30b/source/m3_compile.c) and
  [threaded-operation machinery](https://github.com/wasm3/wasm3/blob/d77cd814aa0bc68cb1df917580a6304d34cfb30b/source/m3_exec_defs.h).
- LuaJIT's
  [trace recorder](https://github.com/LuaJIT/LuaJIT/blob/1edc3e52b67eaf6ce5f809be8e17d6862594b8bc/src/lj_record.c),
  [fold optimizer](https://github.com/LuaJIT/LuaJIT/blob/1edc3e52b67eaf6ce5f809be8e17d6862594b8bc/src/lj_opt_fold.c),
  [assembler](https://github.com/LuaJIT/LuaJIT/blob/1edc3e52b67eaf6ce5f809be8e17d6862594b8bc/src/lj_asm.c), and
  [DynASM documentation](https://luajit.org/dynasm.html).

## Engine findings worth copying

### V8 Liftoff: direct codegen can still defer decisions

Liftoff keeps constants as virtual-stack metadata and materializes or folds them
only at the consumer. More importantly, current Liftoff looks ahead one opcode:
when an `i32` comparison or `i32.eqz` is followed by `if` or `br_if`, it records
an outstanding comparison and later emits a conditional branch directly. It
does not create the intermediate `0/1` value. Its source also contains a much
larger exact-sequence detector for one specialized runtime pattern, proving that
bounded “needle” recognition is compatible with the same decoder-driven
architecture.

Railshot already implements the broad equivalents: compare-to-branch fusion,
`eqz` folding, deferred constants and loads, bounded SIMD/SWAR needles, and
direct local sinks. The lesson is therefore architectural, not a missing
one-token rule: **keep a decision reversible until its immediate consumer is
known**. Extend that principle at measured sinks rather than growing a generic
peephole framework.

Liftoff also snapshots and reconciles its virtual stack at structured merges.
This validates Railshot's use of structured control as a bounded state boundary,
but it does not imply global allocation quality. V8 explicitly says Liftoff
cannot perform redundant-load elimination, strength reduction, inlining, or
better register allocation as effectively as TurboFan because it emits each
Wasm instruction independently.

### Winch: a useful lower bound, not the target design

Winch intentionally avoids IR and complex allocation. Its allocator chooses an
available register from a per-class bitset and invokes a spill callback if none
is available. This is genuinely simple and fast, but it has no future-use
knowledge. Wago already goes beyond it with deferred trees, local/global hints,
pinning, lazy spills, and target-specific selection.

The useful incremental idea is to keep Winch's constant-time free-list mechanics
while improving only the **victim policy** using bounded next-use data. There is
no need to replace Railshot's allocator wholesale.

### WAMR Fast JIT: a block-local middle tier, but with an IR

WAMR runs `frontend -> lower_cg -> regalloc -> codegen`. Its normalized IR
constructor performs constant folding/simplification and may reuse an equivalent
hashed instruction. Its allocator collects all virtual-register occurrences in
a basic block and walks the block backward, with a special preference that
helps two-operand instructions.

This is relevant because it isolates two likely sources of better execution:
local common-subexpression elimination and knowledge of future uses. It is also
a warning: copying WAMR literally would add retained basic-block IR and a later
allocation pass. Wago can take a smaller subset with scoped value numbers and a
capped pending region.

WAMR's own user guide says LLVM JIT execution is roughly twice Fast JIT while
starting more slowly. Treat that figure as WAMR's qualitative tiering statement,
not a transferable Wago prediction: [WAMR running modes](https://github.com/bytecodealliance/wasm-micro-runtime/blob/70f4cd383f1a474d6759e3185b4eca6f6ddde4d4/gitbook/tutorial/running-modes/README.md).

### Wasm3 and superinstructions

Wasm3 uses threaded operations, caches at most an integer and FP value in host
registers, preserves them into slots at control boundaries, reuses constant
slots, and avoids copying a `local.get` until mutation requires it. It selects
specialized operations for register/slot operand combinations.

Those are sound direct-compilation ideas, but classic superinstructions mostly
remove interpreter dispatch. A native JIT already removed that dispatch. For
Railshot, “superinstruction” should mean **a multi-Wasm-op machine template that
reduces actual native instructions, spills, branches, or checks**, not merely a
combined compiler handler. The Bytecode Alliance's Pulley RFC makes the same
distinction while describing superinstructions for an interpreter:
[Pulley RFC](https://github.com/bytecodealliance/rfcs/blob/main/accepted/pulley.md).

### Bounds checks are large enough to deserve a separate track

Wasmtime reports explicit checks causing a 1.2x–1.8x slowdown in its tested
configurations and normally elides them with virtual-memory guard pages:
[Wasmtime fast-execution guide](https://docs.wasmtime.dev/examples-fast-execution.html#configure-wasmtime-to-elide-explicit-bounds-checks).
Wago already has guard-page mode, so analysis work matters primarily for
explicit-bounds targets and for eliminating repeated address work that remains
even under guard pages.

## Algorithm survey and fit

### 1. Bounded tree covering: highest fit

Wasm is already a typed postfix stack language. **Shunting yard solves infix
parsing and precedence**, so it adds no useful information here; Dijkstra's
original context was ALGOL translation, not code optimization
([primary archive](https://ir.cwi.nl/pub/9251)). The relevant family is
expression-tree scheduling and instruction selection:

- Sethi and Ullman label expression subtrees with register requirements and
  choose an evaluation order that minimizes temporary storage:
  [The Generation of Optimal Code for Arithmetic Expressions](https://doi.org/10.1145/321607.321620).
- Aho and Johnson give a linear-time dynamic-programming algorithm for optimal
  code generation on expression trees:
  [Optimal Code Generation for Expression Trees](https://doi.org/10.1145/800116.803770).
- BURG/BURS systems turn target patterns and costs into fast bottom-up tree
  matchers: [BURG paper](https://www.complang.tuwien.ac.at/ublu/tools/doc/burg.pdf)
  and [BURS theory](https://doi.org/10.1145/73560.73586).

Railshot's deferred Valent blocks are already expression trees. At condensation,
label each capped tree with a small set of result states such as:

- result in any GP/FP register;
- result in a particular fixed register;
- flags only;
- folded immediate or memory operand;
- direct pinned-local destination; and
- rematerializable value.

Each target rule returns a simple static cost: estimated instructions, bytes,
and register pressure. Emit the cheapest legal cover. This generalizes the
current curated fusions without constructing SSA or a whole-function IR.

Cost and risk:

- compile cost is linear in nodes times a small state/rule count; cap nodes and
  fall back to today's condense path;
- it may increase code size if the cost model values latency too aggressively;
- only reorder nodes proven non-trapping and effect-free. Wasm traps are
  observable enough that loads, division, conversions, and GC checks must remain
  ordered unless a proof says otherwise;
- DAG-shaped common values require explicit sharing policy; do not silently
  duplicate expensive or trapping nodes.

### 2. Scoped local value numbering: high fit, uncertain corpus payoff

Local value numbering assigns a canonical number to `(op, operands, type)` and
reuses an earlier equivalent computation while its inputs remain valid. Briggs,
Cooper, and Simpson extend it from a basic block into extended basic blocks by
carrying tables through unique-predecessor blocks and using scoped state for
shared prefixes: [Value Numbering](https://www.cs.tufts.edu/~nr/cs257/archive/keith-cooper/value-numbering.pdf).

For Railshot, scope it to the existing straight-line/structured state:

- values from constants, immutable globals, locals at a known version, and
  non-trapping integer operations are initially eligible;
- key local reads by a per-local version incremented on `local.set`;
- either exclude loads initially or key them by memory plus a conservative
  memory epoch invalidated by stores/calls/grow;
- snapshot/rollback tables at structured forks; propagate only through a
  unique-predecessor continuation;
- release a value number when retaining it would force a spill, or rematerialize
  cheap constants/identities instead.

This can perform local CSE and expose a real DAG to the Valent machinery. It may
also lose: production Wasm generators already run optimizers, and preserving a
shared value may cost more register pressure than recomputing it. Instrument
duplicate pure expressions first and require a corpus signal before building it.

### 3. Delayed-emission peepholes / needles: strong fit, already productive

Classic peephole optimization replaces a short adjacent machine sequence with a
cheaper equivalent. Davidson and Fraser describe a retargetable optimizer that
simulates pairs and replaces them with one instruction:
[paper](https://doi.org/10.1145/357121.357129). McKeeman's original peephole
paper is [here](https://doi.org/10.1145/364995.365000).

Railshot's better variant is bytecode-side matching before native emission. It
already does this for SWAR, SIMD, compare/branch, `call; local.set`, scaled
addresses, and other forms. Continue the **offline discovery / online exact
matcher** model:

1. mine hot disassemblies and `CodegenStats` for repeated expensive sequences;
2. derive patterns offline, using superoptimization tools if useful;
3. ship only small, auditable matchers with exact type, alias, trap, liveness,
   and feature predicates; and
4. retain kill switches and differential tests.

Good candidate families are store narrowing, target-specific bitfield/extract
forms, address-update/load sequences, multi-result sinks, float call-result
sinks, and repeated explicit-bounds address shapes. A generic rewrite engine is
unlikely to beat curated rules on compile cost or auditability.

### 4. Bounded next-use allocation: high fit

Poletto and Sarkar's linear scan walks precomputed live intervals once and is
much cheaper than graph coloring while producing good code:
[Linear Scan Register Allocation](https://doi.org/10.1145/330249.330250).
It is not strict streaming: constructing accurate whole-function intervals
requires prior knowledge and CFG edge handling.

Wago already scans each body for summary hints. Extend that scan with bounded
facts instead of adding full intervals:

- for a straight-line block or simple loop, record next/last use of locals;
- when spilling, prefer the resident value whose next use is farthest, is dead,
  or is cheapest to rematerialize;
- avoid permanently pinning a local whose hot region is small, and give that
  register to operand evaluation outside the region;
- retain the existing canonical slot at merges and fall back for complex
  `br_table`, exception, nested-loop, or call shapes.

This directly complements the region-pin design already outlined in
`docs/valent-blocks-expansion-plan.md`. The important experiment is not “add
linear scan”; it is “does next-use information reduce measured call reloads,
merge moves, or region traffic enough to repay the scan?” The current slow ARM64
rows have zero ordinary spills, so spill-victim improvement alone is presently
measurement-gated rather than a top-ranked project.

### 5. Bounds facts and loop prechecks: high fit for explicit mode

ABCD eliminates bounds checks on demand using a constraint graph over SSA:
[ABCD paper](https://research.ibm.com/publications/abcd-eliminating-array-bounds-checks-on-demand).
The whole algorithm is not a no-IR fit. A bounded subset is:

- retain facts that one checked address/range covers nearby constant offsets;
- merge adjacent fixed-width accesses into one range proof;
- recognize a simple induction local and invariant upper bound in a single
  natural loop;
- emit one preheader check only when overflow, final extent, memory growth, and
  every loop exit are proven safe; otherwise keep existing checks.

This matches Railshot's existing straight-line certificate and hybrid
loop-precheck implementation. The next extension is the already-measured missing
shape: a running pointer advanced by a constant stride, or an invariant base plus
a bounded induction index. Keep proof state per memory and invalidate it at
calls, stores when alias-sensitive facts are involved, `memory.grow`, and control
merges. Validate trap order on failing inputs; eliminating a later bounds trap
must not expose an earlier operation that should not have executed.

### 6. Superoperators / superinstructions: medium fit only as native templates

Superinstructions reduce the indirect-branch cost of interpreters by combining
common bytecodes. The research reports dispatch as a dominant interpreter cost:
[Ertl and Gregg](https://jilp.org/vol5/v5paper12.pdf), and
discusses static/dynamic combinations and their compile-time/code-size costs:
[Gforth superinstructions](https://www.complang.tuwien.ac.at/anton/euroforth/ef03/ertl-gregg03.pdf).
Proebsting's superoperator work is a related source:
[Superoperators](https://web.stanford.edu/class/cs343/resources/superoperators.pdf).

For native Railshot, dispatch savings do not exist. Adopt only combinations
that reduce native work. Copy-and-patch is another fast-codegen design based on
precompiled stencils, but its main benefit is compiler throughput, which is not
Wago's current bottleneck: [Copy-and-Patch](https://sillycross.github.io/assets/copy-and-patch.pdf).

### 7. Trace scheduling and superblocks: poor initial fit

Trace scheduling selects a likely CFG path, schedules across block boundaries,
and needs compensation code for side entrances/exits:
[Fisher](https://doi.org/10.1109/TC.1981.1675827). It wants a CFG, profile or
heuristics, code motion, and often tail duplication. Those costs conflict with
direct bounded emission and can inflate native code.

A smaller Wago-compatible subset is branch layout: use loop/backedge and static
structured heuristics to keep likely paths as fallthrough, and keep cold trap or
slow paths out of line. Railshot already has shared cold trap stubs and aligned
hot entries. Do not add trace scheduling unless profiles show front-end or
branch-miss bottlenecks after local work is exhausted.

### 8. Lazy code motion / PRE: poor fit

Lazy code motion uses backward and forward data-flow analyses over a flow graph
to place partially redundant computations optimally and safely:
[Knoop, Rüthing, and Steffen](https://doi.org/10.1145/143103.143136). This is
not a bounded-lookahead algorithm and is not compatible with immediate native
emission. Local value numbering captures the inexpensive straight-line subset;
full PRE should stay out of Railshot.

### 9. Tiering and profile guidance: architecturally possible, strategically costly

V8 first compiles lazily with Liftoff, counts execution, and replaces hot
functions with TurboFan output in the background; it does not use Wasm OSR in
the described pipeline. WAMR likewise supports Fast JIT as a first tier and LLVM
JIT as a second: [WAMR build options](https://github.com/bytecodealliance/wasm-micro-runtime/blob/70f4cd383f1a474d6759e3185b4eca6f6ddde4d4/doc/build_wamr.md).

Wago could recompile only hot functions with the **same direct backend** but a
larger tree/window budget, a block-local pre-scan, and costlier selection. Each
individual compilation remains bounded and direct. This still adds counters,
code replacement, concurrency/lifetime rules, duplicate code memory, and no
benefit to an already-running call without OSR.

Prefer a compile-time “balanced mode” selected per function from existing body
size, loop, call, memory, and spill-pressure hints before dynamic tiering. Only
build runtime recompilation after measurements show that a small hot set merits
significantly more compile work and that code publication already has a safe
replacement seam.

## Ranked Wago work, with cost and risk

| Rank | Experiment | Expected execution leverage | Compile / memory cost | Main risk |
|---:|---|---|---|---|
| 1 | Costed tree covering on existing Valent blocks | Better evaluation order, fewer copies/spills, more target-specific fused forms | Linear in explicitly capped tree size; dense labels from worker scratch | Trap/effect reordering and a bad cost/size model |
| 2 | Call-aware next-use and region-pin decisions | Fewer call reloads/merge moves and less permanent pin pressure | O(locals + bounded region), reused per worker | Merge/call liveness mistakes |
| 3 | Extend loop prechecks to affine induction/index shapes | Large on explicit-bounds memory kernels | Bounded loop scan, but duplicates the selected loop body | Overflow, grow, multi-memory, trap order, and code growth |
| 4 | Alias-aware pending-load window | Keeps independent deferred loads alive across stores | Fixed-cap pending-load descriptors | Incorrect alias proof or stale memory value |
| 5 | Measured new native needles | Potentially large wins for a few hot idioms | Near-zero on misses with fixed matcher fuel | Rule proliferation and semantic corner cases |
| 6 | Scoped LVN after duplicate-expression instrumentation | Removes repeated pure computation and exposes DAGs | Fixed-cap open-addressed table per region | More register pressure; likely little opportunity in optimized Wasm |
| 7 | Budgeted per-function compilation policy | Spend more only where likely to amortize; remain small elsewhere | Explicit region, scratch, worker, and code-byte budgets | Tuning complexity and inconsistent code size |
| 8 | Runtime hot-function recompilation with richer Railshot | Attacks nonlocal hot cases without penalizing cold functions | Counters and duplicate code consume retained memory | Publication, concurrency, memory, and lifecycle complexity |

## Proposed experiment sequence

### Phase A: prove where local quality is lost

Extend explain output only if current counters cannot answer these questions:

- which sinks force the most deferred roots;
- per sink, how many copies, spills, reloads, and fixed-register shuffles result;
- allocator victim and next-use distance;
- repeated pure expression keys within a straight-line block;
- explicit bounds checks grouped by base and constant-offset range; and
- hot blocks where native instruction count differs most from wazero or
  Cranelift, separating runtime/ABI overhead from instruction selection.

Gate every later phase on a corpus count and a hot profile, not just a synthetic
case.

### Phase B: prototype costed Valent-tree covering

Start on ARM64 and only call-free, non-trapping trees with at most 16 nodes.
Use a scalar integer case to prove the machinery, then target one measured FP or
SIMD losing sequence. Support the existing rule families as alternative covers
rather than adding new optimizations initially. Compare:

- current native bytes vs. selected bytes;
- compile latency and allocation count;
- spills/reloads and maximum simultaneous temporaries;
- execution on kernels whose current disassembly shows avoidable copies; and
- byte-for-byte fallback equivalence when the feature switch is off.

If the selector merely reproduces current code at material compile cost, stop.
If it finds real wins, extend state-by-state to memory operands, flags, pinned
destinations, FP, and SIMD.

Run it under a deliberately tiny region budget as well as the balanced budget.
The tiny case must hit the fallback without changing generated semantics or
growing scratch beyond its configured cap.

### Phase C: bounded lifetime information

Add next-use metadata for the same simple regions and first use it to suppress
unneeded call-local reloads or shorten a permanent pin's region. Change allocator
victim choice only on a workload that actually spills. Then test region-scoped
pins for one call-free natural loop. Keep canonical frame slots and current merge
fallback. Compare execution, full compile latency, allocations, Wasm/native code
size, and RSS—not execution alone.

### Phase D: explicit-bounds facts

Retain the existing straight-line certificates and invariant-base loop
versioning as the oracle. Recognize one induction-loop form—prefer the
invariant-base-plus-index SHA-256 shape before running-pointer matmul—and emit a
trap-free precheck selecting the existing checked/unchecked loop bodies.
Differential test every boundary around `0`, `2^32`, `2^64`, maximum memory
size, negative wrapped indices, grow/call sites, zero-iteration loops, early
exits, and failing iterations.

### Phase E: decide whether LVN or tiering is justified

If duplicate-expression counts are high, add scoped LVN for non-trapping integer
values. If the remaining execution gap is concentrated in a few large/hot
functions and local techniques plateau, test a compile-time richer mode. Runtime
tiering is last, not the default answer.

### Phase F: resource-policy and scaling validation

Drive the same corpus through serial minimal, serial balanced, and bounded
parallel balanced policies. Plot full compile latency against peak RSS and
retained native-code bytes. Verify that adding cores improves throughput without
silently multiplying memory beyond the policy, and that the minimal policy stays
usable on a constrained-memory target rather than merely disabling random
optimizations.

## Specific non-recommendations

- Do not implement shunting yard. Wasm has already done the parsing and
  precedence work; tree scheduling/covering is the relevant algorithm.
- Do not call WAMR Fast JIT “single-pass.” Its checked-in pipeline and IR make
  the distinction unambiguous.
- Do not add whole-function linear scan under a single-pass label. Scanning
  already-built live intervals is one pass; constructing them still requires a
  prepass and CFG/liveness treatment.
- Do not add lazy code motion/PRE or trace scheduling to Railshot without an
  explicit architecture reversal. Both want global control/data-flow state.
- Do not expect interpreter superinstructions to help native code unless the
  combined rule reduces emitted machine work.
- Do not build an online e-graph or SMT optimizer. Cranelift's e-graph, ISLE,
  GVN, LICM, redundant-load elimination, and robust allocator are exactly the
  optimizing-compiler machinery that buys execution at a different compile-time
  and memory point: [Pulley RFC's Cranelift summary](https://github.com/bytecodealliance/rfcs/blob/main/accepted/pulley.md#proposal).

## Bottom line

Railshot is already substantially beyond a conventional baseline compiler. The
next balanced step is to make its existing deferred region **choose a whole
tree cover with a cost model**, then give that decision just enough future-use
and bounds information to avoid obvious spills and checks. That spends compile
time locally and predictably, preserves bounded memory and direct codegen, and
has a clean fallback when a region is too large or semantically difficult. The
cost model must price native bytes and scratch memory alongside instructions;
otherwise it will optimize the server benchmark by eroding the embedded product.

The strongest implementation order is:

1. instrument missed local opportunities;
2. costed tree covering;
3. next-use victim selection and simple region pins;
4. explicit-bounds range/loop facts;
5. more measured needles; and
6. LVN or richer per-function compilation only when corpus evidence supports it;
7. validate the whole ladder under explicit serial/parallel memory budgets.

## AMD64 implementation outcome (2026-08-05)

The first AMD64 campaign implemented and measured the bounded parts of this
plan on a Ryzen 7 7800X3D. The comparison baseline was exactly `d83cf0a`; runs
used `GOMAXPROCS=1`, `taskset -c 2`, alternating baseline/candidate order, and
the repository's full benchmark corpus. Execution medians below use ten
200 ms samples per engine and workload. Negative deltas are improvements.

### Retained changes

1. **Short displacement LEA encoding.** Scaled LEAs now select no displacement,
   signed 8-bit displacement, or signed 32-bit displacement as appropriate,
   including the RBP/R13 encoding special case. This is an encoder-only size
   win with no compiler data structure.
2. **Bounded affine scaled-index covering.** The existing deferred expression
   tree recognizes `base + ((index +/- constant) << k)` for `k=1..3` and folds
   the constant into the LEA displacement. The match is fixed-depth, O(1), and
   allocation-free. It fired 326 times in the real corpus and removed 2,286
   native bytes from modules containing hits. It is independently gated by
   `WAGO_AMD64_NO_AFFINE_LEA=1`.
3. **Eight float-local pins with lazy call preservation.** The float pin pool
   now extends from XMM12-15 through XMM8-11 even in call-making functions. The
   existing dirty-only pre-call store and lazy post-call reload preserve the
   pins. Taking XMM11 disables only float-result register merges; integer merges
   remain available. This keeps the resource cost to four fixed register-map
   entries and existing per-local state. It is gated by
   `WAGO_AMD64_NO_EXTFPPINS=1`.
4. **Bounded direct-call next-use scan.** Before a real direct call that spills
   pinned locals, a copied Wasm reader looks ahead at most 64 operations. If a
   dirty pinned local's next access is an overwrite, its canonical pre-call
   store is omitted. Structured-control boundaries, malformed immediates, and
   exhausted fuel fall back conservatively. State is two register bitmasks, so
   compile memory is O(1); there is no CFG, interval table, or heap allocation.
   Inlined calls and asynchronous host calls do not retain scan state. It is
   gated by `WAGO_AMD64_NO_CALL_NEXT_USE=1`.
5. **Remove spilled-local memory-destination RMW.** The baseline optimization
   emitted compact `op [slot], src/imm` instructions, but the extra memory
   dependency serialized hot update chains and consistently lost on the actual
   corpus. Restoring load/operate/store lets the CPU and surrounding register
   machinery overlap work. This is a deliberate removal, not unfinished work.

### End-to-end deltas versus `d83cf0a`

Across all 30 execution rows the geometric-mean time delta is **-3.11%**.
Every Wago execution benchmark remains at **0 B/op and 0 allocs/op**.

| Workload | Execution delta | Candidate wins |
|---|---:|---:|
| JSON serialize | -29.81% | 10/10 |
| JSON SIMD serialize | -17.07% | 10/10 |
| raytrace | -6.00% | 10/10 |
| UTF SIMD | -5.07% | 10/10 |
| spectral norm | -3.99% | 10/10 |
| n-body | -3.42% | 10/10 |
| BLAKE3 SIMD | -3.38% | 10/10 |
| matrix multiply | -2.95% | 10/10 |
| BLAKE3 scalar | -2.66% | 10/10 |
| memory tree | -2.40% | 10/10 |
| SHA-256 | -1.87% | 9/10 |
| JSON SIMD deserialize | -1.23% | 9/10 |
| fannkuch | -0.80% | 10/10 |
| JSON deserialize | -0.59% | 7/10 |

The other rows were neutral within 0.6%; the largest apparent regression was
`many_funcs` at +0.46%, which is not material at its tiny runtime.

Compilation remains close to the original single-pass point. Summed across all
34 corpus modules, compile-only time is **+0.47%**, compile allocation bytes are
**-0.99%**, and allocation count is **-0.002%**. Full compile time is **+0.06%**,
full allocation bytes are **+0.004%**, and allocation count is **+0.001%**.
Equal-module geometric means, which overweight tiny noisy modules, are +1.00%
for compile-only time and +0.94% for full compile time.

Total emitted native code falls from 92,003,395 to 91,572,574 bytes:
**-430,821 bytes (-0.468%)**. Esbuild accounts for -470,913 bytes (-1.526%);
raytrace is -576 bytes (-3.407%) and BLAKE3 SIMD is -1,071 bytes (-1.554%). Ruby
grows by 44,366 bytes (+0.087%) from the extra call-preserved float pins. Median
peak RSS while compiling Ruby is 252,032 KiB versus 252,196 KiB at the baseline
(-0.065%, effectively unchanged).

Against the current Wazero benchmark engine, the final Wago compiler is **3.88x
faster by summed full-corpus compile time** (2.05x equal-module geometric mean)
and uses **1.45x fewer summed compile allocation bytes**. Wago wins 25 of 30
execution rows. The remaining meaningful execution gaps are BLAKE3 scalar
(Wago/Wazero 1.860x), BLAKE3 SIMD (1.684x), and raytrace (1.061x); UTF SIMD
(1.012x) and the float microbenchmark (1.007x) are approximately tied.

### Measured rejections

- **Broader deferred-tree covering:** the current stack shapes had no useful
  both-deferred cover opportunities, while broader affine variants regressed
  fannkuch and spectral norm. Only the profitable index-affine form remains.
- **More permanent integer pins (RDI/RSI):** looked 3.8-4.0% faster on selected
  BLAKE/UTF runs, but full-corpus differential testing found an incorrect n-body
  result. Restricting by body size did not make the invariant safe. Removed.
- **Narrow i32 local loads/stores:** quicksort improved about 3.2%, but matrix
  multiply regressed 2.6-2.7%, spectral norm about 1.2%, and JSON deserialize up
  to 1.7%. Narrow stores without matching loads could retain stale high bits and
  trap. Removed.
- **Additional explicit-bounds/loop machinery:** guard pages versus explicit
  checks differed by only -0.12% to +0.11% on the relevant hot workloads. The
  current corpus does not justify a new loop-versioning form.
- **Generic LVN:** an opportunity scan found mostly immediate set/get patterns
  in tests and very large binaries, not in the BLAKE3 or JSON hot gaps. No table
  or per-region cache was added.
- **Shunting yard, PRE, trace scheduling, and runtime tiering:** still rejected
  for this compiler point. They either solve parsing already performed by Wasm
  or require global state and memory disproportionate to the measured gaps.

Hardware performance counters were unavailable on the benchmark host
(`kernel.perf_event_paranoid=4`), so the campaign did not change host policy.
Native-code dumps and disassembly show the remaining BLAKE3 gap is dominated by
local-value residency: its hot function has roughly 967 Wago stack references
versus 289 in Wazero. Closing that gap likely needs a bounded regional IR or
allocator-quality step, not another unmeasured peephole. That should be a
separate design with an explicit scratch/RSS cap and a fallback to today's
direct codegen.

## Post-merge AMD64 integration (2026-08-06)

The branch was rebased onto `daec329`, which brought in the generalized compact
AMD64 memory encoder, the WARP-sized eleven-float-local policy, and the bounded
SWAR/SIMD superoptimizer. The rebase deliberately kept the stronger mainline
forms instead of replaying older branch machinery:

- main's `addrMode`/`emitDisp` implementation now owns compact addressing; the
  branch only widens scaled-LEA displacement input to `int32`, which the affine
  cover requires;
- main's fixed float pin pool is XMM12-15 plus XMM4-10, leaving XMM0-3 and XMM11
  for scratch and result merges. This supersedes the branch's eight-pin/XMM11
  experiment, so the separate integer/float merge flags were dropped;
- affine index folding gets first chance only on its measured constant-bearing
  shape. Main's broader safe nested-LEA materialization remains the fallback;
- `call-next-use` and `affine-lea` are registered in the new runtime-local
  optimization catalog rather than restoring the old process-global registry.

All measurements below used the same Ryzen 7 7800X3D, `GOMAXPROCS=1`, core 2,
alternating order, and seven or more 200 ms execution samples.

### Does the merged superoptimizer help?

Yes. An exact `daec329^` versus `daec329` comparison across the 29 unchanged
execution rows improves geometric-mean execution by **0.50%**. The dominant
existing-corpus win is scalar UTF conversion, from 192,429 to 164,604 ns/op
(**-14.46%**, 7/7 wins). The changed UTF SIMD artifact and newly added focused
modules were excluded from that commit-to-parent comparison.

A stronger same-binary oracle compiled the complete current corpus with
`WAGO_NO_SWAR_MASK_TEST=1`, `WAGO_NO_SWAR_IDIOMS=1`, and
`WAGO_NO_SIMD_SUPEROPT=1`, then compared it with all three recognizers enabled.
That removes fixture and harness differences:

| Current-corpus workload | Disabled | Enabled | Delta |
|---|---:|---:|---:|
| SWAR pack/parse run | 1,729 ns | 1,365 ns | -21.05% |
| scalar UTF conversion | 191,510 ns | 164,679 ns | -14.01% |
| SIMD UTF validation | 153,095 ns | 145,984 ns | -4.65% |
| focused unsigned mulhi | 40.72 ns | 39.89 ns | -2.04% |
| BLAKE3 SIMD | 607,647 ns | 604,332 ns | -0.55% |

Across all 36 current execution rows the enabled recognizers improve the
geometric mean by **1.26%**. Their weighted full-compile cost is **+0.05%**;
weighted allocation bytes change by **+0.0003%** and allocation count by
**+0.0018%**. Total emitted native code falls by 11,160 bytes (-0.0145%). Ruby
compile peak RSS is 215,116 KiB enabled versus 215,052 KiB disabled, which is
noise-sized. This is a clear keep: bounded pattern matching buys double-digit
wins on its target kernels without a meaningful compile or memory tax.

### Does the earlier branch still add value on top?

Compared directly with `daec329` on identical current artifacts, the rebased
branch improves execution geometric mean by **0.45%** across 36 rows. The
strongest repeatable rows are:

| Workload | Main | Rebased branch | Delta |
|---|---:|---:|---:|
| SIMD UTF conversion | 58,110.5 ns | 56,595.5 ns | -2.61% |
| JSON serialize | 23,348.5 ns | 22,847.5 ns | -2.15% |
| JSON deserialize | 41,480.5 ns | 40,904.5 ns | -1.39% |
| SWAR pack/parse | 40.02 ns | 39.59 ns | -1.08% |

Feature-isolated 15-sample runs show affine LEA folding improves JSON serialize
by 0.54% and deserialize by 0.70%; its weighted full-compile cost is 0.07%.
Call next-use improves the same rows by 0.39% and 0.41%, respectively, for a
0.45% weighted full-compile cost. The total rebased branch is **+2.09%** weighted
full-compile time versus main, while weighted full-compile bytes are **+0.003%**
and Ruby peak RSS is 215,060 versus 215,116 KiB. It emits 310,625 fewer native
bytes (-0.402%). That remains inside the intended balanced point: a small,
bounded compilation-time spend, no retained-memory growth, and smaller code.

The first rebase placed two 64-bit dead-local masks directly in `fn`. Main's
larger compiler state made that cross a Go allocator size class, raising weighted
full-compile bytes by 2.58%. The final form stores the two 16-register banks as
`uint16` masks in the existing boolean-cluster padding. After packing, weighted
full-compile bytes returned to +0.003% and peak RSS to neutral. This is the kind
of scaling check that must accompany every future bounded analysis: O(1) state
can still be too expensive when it changes the allocation class of a per-function
object.

A broad pre-pull-to-rebased comparison also showed large net wins in BLAKE3 SIMD
(839,945 to 604,679 ns), globals (863.5 to 476.5 ns), CRC32 (19,480 to 16,346
ns), matrix multiply (166,138 to 139,967 ns), and memory tree (10,455 to 8,980
ns). Those numbers span many intervening main commits and therefore describe the
new combined compiler, not the isolated effect of `daec329`.

## Pressure-aware Valent tree ordering (2026-08-06)

The next AMD64 step makes the existing deferred tree choose its evaluation order
instead of merely making the tree deeper. Railshot's two-address integer path
condenses the right child before the left. For a commutative expression this is
needlessly expensive when the left child requires more registers: the cheap
right result stays live while the larger left subtree is emitted.

The retained selector computes a bounded Sethi-Ullman-style register need from
the existing Valent nodes and commutes the root when the left child needs more
registers. It is deliberately constrained:

- only commutative integer ALU operations are reordered;
- both subtrees must be non-trapping;
- deferred memory loads, `ref.func`, division/remainder, and every other
  trapping or fixed-register operation preserve Wasm evaluation order;
- the walk inherits Valent's existing height-six cap, so its work and native Go
  stack use are bounded;
- no annotation was added to `elem` or `fn`, and no slice, map, CFG, or IR is
  retained.

The full corpus contains **46,963** eligible roots. This is not just a source
pattern count: with the selector enabled, explicit-bounds allocator spills fall
from 7,422 to 6,002 (**-19.13%**) and guard-mode spills from 4,848 to 3,927
(**-19.00%**). Reload counts fall from 19,234 to 19,227 and 22,447 to 22,443,
respectively. Total emitted native code falls by 7,037 bytes in explicit mode
and 1,342 bytes in guard mode.

Execution was measured with the same Ryzen 7 7800X3D, `GOMAXPROCS=1`, and
`taskset -c 2` protocol as the preceding work. Two alternating disabled/enabled
passes contributed 14 samples per row at 200 ms each. Both passes independently
improved the 36-row geometric mean (-0.51% and -0.64%); combined medians improve
it by **0.70%**. Every execution row remains at 0 B/op and 0 allocs/op.

| Current-corpus workload | Disabled | Enabled | Delta |
|---|---:|---:|---:|
| SIMD UTF conversion | 58,301 ns | 53,069 ns | -8.97% |
| scalar BLAKE3 | 827,539 ns | 807,485 ns | -2.42% |
| spectral norm | 646,220 ns | 631,424 ns | -2.29% |
| quicksort | 68,640 ns | 67,075 ns | -2.28% |
| JSON serialize | 23,266 ns | 22,918 ns | -1.50% |
| SIMD BLAKE3 | 609,745 ns | 602,956 ns | -1.11% |

Repeated full-corpus `CompileFull` runs show no meaningful latency or memory
cost. Across combined per-module medians, weighted time changes by **-0.16%**
and the equal-module geometric mean by +0.24%. Summed allocation bytes change
from 272,328,001 to 272,267,136 (**-0.022%**) and allocation count from 942,122
to 941,555 (**-0.060%**). Median Ruby compile peak RSS is identical at
161,136 KiB in five alternating samples. The feature is exposed as the
runtime-local `tree-order` optimization and has the process-level A/B oracle
`WAGO_AMD64_NO_TREE_ORDER=1`.

### Bounded associative accumulator cover

A second retained cover handles balanced trees of the same trap-free integer
`add`, `and`, `or`, or `xor`. When such a tree needs at least three registers,
the cover collects at most eight leaves into a fixed stack array, starts with the
most expensive leaf, and consumes the others directly into one accumulator.
Internal binary results therefore never become simultaneously live. Trees with
a destination hint keep the established local-sink path, and variable shifts
are excluded because their fixed RCX role could evict the accumulator.

The corpus has 4,858 potential roots. On top of tree ordering, the cover reduces
explicit-mode spills from 6,002 to 5,421 (-9.68%) and guard-mode spills from
3,927 to 3,845 (-2.09%). Reloads remain effectively flat (19,227 to 19,225 and
22,443 to 22,444), while native code falls by 25,429 bytes in explicit mode and
9,090 bytes in guard mode. In two alternating
passes, both 36-row geometric means improve (-0.56% and -0.18%); the 14 samples
total per row improve the combined-median geometric mean by **0.33%**.

| Current-corpus workload | Disabled | Enabled | Delta |
|---|---:|---:|---:|
| SHA-256 | 43,113 ns | 41,485 ns | -3.78% |
| focused mulhi run | 2,566 ns | 2,512 ns | -2.12% |
| quicksort | 69,588 ns | 68,286 ns | -1.87% |
| JSON serialize | 23,626 ns | 23,484 ns | -0.60% |
| SIMD UTF validation | 152,001 ns | 151,234 ns | -0.50% |

Weighted `CompileFull` time changes by +0.24%, while summed allocation bytes
fall from 272,265,228 to 272,106,855 (-0.058%) and allocation count from 941,504
to 940,133 (-0.146%). Median Ruby peak RSS is again unchanged at 161,080 KiB.
The runtime-local knob is `assoc-tree`; `WAGO_AMD64_NO_ASSOC_TREE=1` is its
process-level A/B oracle. Like tree ordering, the cover adds no persistent
per-node or per-function state.

With both new tree features disabled versus both enabled in the final binary,
the exact combined 36-row execution geometric mean improves by **1.13%**; the
two alternating passes improve independently by 0.90% and 1.47%. Weighted
`CompileFull` time is effectively unchanged at -0.03%, summed allocation bytes
fall by 0.081%, and allocation count falls by 0.211%. Median Ruby RSS is 161,208
KiB disabled and 161,080 KiB enabled. Together they remove 32,466 native bytes
and 2,001 spills in explicit mode, and 10,432 native bytes and 1,003 spills in
guard mode.

### Rejected: raising the Valent height cap

A separate experiment allowed proven non-trapping trees to remain deferred from
height six through height twelve. It found 1,203 extensions, but made the
allocator worse: guard-mode spills rose from 3,927 to 4,013, reloads from 22,443
to 22,453, and native code grew by 1,927 bytes. The experiment was removed.
More deferred structure is useful only when the cover and scheduler can exploit
it; extending live ranges by itself is counterproductive.

## Private prepared entry and regional local residency (2026-08-06)

Two later changes spend a little more bounded compile work where the measurements
show a repeatable execution return.

### Private prepared entry

Prepared scalar calls already avoid generic slice marshaling, but every call still
repeated the instance lifecycle CAS and rebound a native context whose address is
stable for the lifetime of a private instance. The retained fast entry skips those
two operations only when the instance has no imported or shared memory, memory
directory, shared native-control state, or synchronization mode. It continues to
take the global native-execution lock, refreshes native control, and uses the normal
prepared engine entry, so host-visible global synchronization and trap behavior do
not change. `WAGO_PREPARED_PRIVATE_ENTRY=0` is the process-level A/B oracle.

An earlier prototype also bypassed global native-execution synchronization. It was
faster but unsound for host-visible globals and was removed. A second prototype
retained an extra instance resource root and finalizer in each handle; that lifetime
and memory complexity was unnecessary. The final handle instead documents the
existing rule that `Invoke` must not race `Instance.Close`, adds no retained root or
finalizer, and preserves deterministic instance teardown.

On the Ryzen 7 7800X3D, `BenchmarkPreparedInvokeAddOne` improves from 29.37 to
18.55 ns/op (**-36.84%**) with 0 B/op and 0 allocs/op.

### Reusable interval regions

The regional local cache uses the existing bytecode hint scan to record the final
`local.get` for integer locals, then reuses up to nine physical registers across
non-overlapping local lifetimes. A dirty local is written back when its register is
evicted; a later lifetime may reactivate the register, and a final get can transfer
it directly to the operand stack. This is intentionally not a CFG or whole-function
linear-scan allocator:

- admission is limited to call-free, control-free register-ABI functions from 128
  bytes through 16 KiB with 16 through 256 locals;
- the only variable state is O(number of locals): four-byte last-use hints plus a
  reusable one-byte marker per local;
- four fixed-role scratch GPRs remain reserved, and cached locals remain
  pressure-spillable;
- pending memory-reference borrows prevent eviction until materialized;
- existing floating-point pins remain active, while whole-function integer pins are
  disabled only for admitted regions; and
- `WAGO_AMD64_INTERVAL_REGIONS=0` is the process-level A/B oracle.

With six alternating samples per row, enabling the cache in the final binary makes
scalar BLAKE3 **10.30%** faster and SIMD BLAKE3 **7.72%** faster. The fixed 25-row
execution geometric mean improves by **0.79%**; all other rows are statistically
flat and every row remains at 0 B/op and 0 allocs/op. The scalar BLAKE3 hot function
shrinks from 9,376 to 8,728 native bytes (**-6.91%**). Its full-compile path costs
about 9.24% more, while SIMD BLAKE3 full compile costs 0.64% more. Across the four
representative compile rows used during tuning, weighted full-compile time increases
by 2.41%.

Nine cache registers beat eight by 2.22% geometric mean across the focused tuning
set. Ten registers changed that mean by only 0.14% and weighted time by 0.06%, so
the extra pressure was rejected. Lowering admission from 16 locals to eight admitted
additional Lua, Ruby, and script functions but no additional fixed-gate execution
rows; without measured execution value, the broader compile and code-size exposure
was rejected too. Broad admission also interfered with established bounds, folding,
store-forwarding, and SWAR peepholes in tiny functions, which is why the final size
and local-count thresholds are conservative.

### Cumulative branch result

The exact PR merge base (`daec329`) and final head (`e4b4eb17`) were compiled into
separate benchmark binaries and run in alternating order, pinned to one Ryzen core
with `GOMAXPROCS=1`. Six samples per row on the fixed 25-row general gate produce:

| Metric | Merge base | Final head | Delta |
|---|---:|---:|---:|
| execution geometric mean | 31.30 us | 27.81 us | **-11.15%** |
| summed mean execution time | 4.946 ms | 4.701 ms | **-4.95%** |
| full-compile geometric mean | 271.4 us | 278.2 us | **+2.51%** |
| summed mean full-compile time | 10.141 ms | 10.487 ms | **+3.41%** |
| full-compile allocation bytes | 4,860,123 | 4,945,131 | **+1.75%** |
| full-compile allocation count | 11,321.7 | 11,409.5 | **+0.78%** |
| execution allocation bytes/count | 0 / 0 | 0 / 0 | unchanged |

The largest execution wins are focused prepared calls and pack/parse operations
(about 54%), scalar BLAKE3 (18.84%), SIMD BLAKE3 (12.60%), SIMD UTF conversion
(9.50%), and SHA-256 (6.84%). SIMD UTF validation regresses 0.28%; the remaining
rows are flat or improve. The exploratory 20% broad target was not reached, but
the retained branch is a measured balance: an 11.15% equal-workload execution win
for a 2.51% full-compile geomean cost, bounded per-function state, smaller hot BLAKE3
code, and no runtime allocation increase.
