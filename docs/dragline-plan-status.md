# Dragline master-plan implementation ledger

This ledger tracks the implementation against `dragline-final-master-plan.md`.
The master plan's performance figures are release gates, not current claims.

Status: ✅ implementation verified · 🚧 partial · ⬜ not implemented · ❌ gate failed

Implementation status is not completion status. All numbered phases currently
have their enumerated implementation, but the completion rule and several
performance/platform gates below still fail. See
`dragline-execution-optimization-arm64-2026-08-28.md` for the latest application
execution diagnosis.

| Plan phase | Status | Current evidence or missing work |
|---|:---:|---|
| 0 — sibling boundary | ✅ | `CompilerEngine`, the strict sibling router, shared input/output contract, runtime ABI revision, artifact engine identity, `--railshot`, `--dragline`, and dependency checks are implemented and covered. The shared input now carries target, speed/balanced/size objective, bounds policy, and an immutable original-Wasm profile. Dragline never delegates to Railshot. |
| 1 — measurement | ✅ | ISA execution and public compile benchmarks report latency, allocations, and native bytes. Dragline now exposes version-16 opt-in per-function lower/emit timing, native/frame/relocation counts, cache-hit identity, host-effect and guarded-indirect-call realization, capacity-based compiler-owned peak-live bytes, exact retained RailSSA/RailMach/native-planner attribution, and a RailSSA stage breakdown covering CFG, local SSA, value flow, semantic records, metadata, simplification, pressure, specialization, and emission. RailSSA and RailMach instruction identities are distinct, and exact quality-search rows include dependency edges, schedule candidates, selected producer combinations, live allocation segments, coalesced/physical/cycle/motion copies, edge rematerializations, spills, regional reloads/stores, post-RA rewrites actually emitted, bounds/obligation/address/memory elision, IPRA-refined calls, and shrink-wrapped saves. Each native bounds elision consumed by emission now performs and counts a bounded demand-proof query. Functions with realized post-RA rewrites get an exact signed byte-saving measurement from a diagnostic re-emission with only those rewrites disabled, outside primary emission timing and peak-live accounting. Failures can emit strict canonical replay artifacts containing the exact source module and function/stage identity. Railshot exports backend-neutral quality-debt rows, and stable target fingerprints plus a strict backend-neutral original-Wasm profile format are implemented. Version 3 of the strict alternating compiler report runs built-in Dragline and Railshot through the same isolated child-process seam as external engines and records executable hashes, tool versions, exact Wasm hashes, process wall and CPU time, peak RSS, artifact size, and exact generated native-code bytes. A paired execution worker measures prepared export calls for both Wago engines and Wasmtime without compiler or process startup in the timed region. Current ARM64 raw reports cover all 53 Dragline-admitted application/MVP-ISA modules for compilation and all 216 runnable manifest exports for execution; Cranelift uses Wasmtime 46.0.1. An actual LLVM-enabled command is not installed on this host. The paired ISA benchmark has an opt-in Linux `perf_event_open` group that locks the measured goroutine to its OS thread, excludes kernel/hypervisor work, scales multiplexed counts, requires cycles and instructions, and reports available branches, branch misses, L1I/L1D/LLC misses, and frontend/backend stalls per operation. Non-Linux hosts reject the option explicitly. Current-host PMU qualification remains because this development host is Darwin. |
| 2 — RailSSA | ✅ | The structured scan records dense nested regions, control boundaries, loop depth, approximate pressure, assigned locals, merge-live locals, and conservative loop-carried locals under a hard event budget. Bodies of at least 16 KiB use a bounded compact event form that propagates unique local read/write flags through ancestor regions, allowing the 16 MiB Ruby corpus to pass without the former nesting-multiplied event explosion. A reusable compact CFG planner emits verified blocks, normal-control edges with stack-transfer contracts, predecessor/successor CSR slabs, loop headers, and deterministic dumps. Type-indexed block signatures carry source-ordered parameter and result vectors; loop labels transfer parameters while block/if labels transfer results. Sparse edge refinements now give `br_on_cast` and `br_on_cast_fail` distinct taken/fallthrough typed block identities without widening common edge records or duplicating the native reference. Liveness-aware direct-local SSA and typed operand-stack value flow resolve administrative locals away and represent demanded joins as block parameters with explicit edge arguments. Dense local SSA now derives live-out from successor live-in sets rather than retaining a redundant matrix; its 14 MiB transient-byte guard admits Ruby's 1,082,172-cell largest function without increasing the old nominal ceiling. A 24-byte dense semantic instruction record and flat operand slab preserve source-ordered Wasm operations; uncommon multi-result types use sparse side metadata so the scalar record stays unchanged. Its verifier, deterministic dump, and bounded pure-integer/control evaluator cover the complete integer/control subset, including conditionals, loop-carried locals, indexed/default `br_table` exits, and multi-value control transfers. Compact side metadata classifies abstract-heap reads/writes, calls/re-entry, traps, semantic obligations, source offsets, and effect epochs. Demand liveness uses a precomputed instruction-to-block index and a visited-value worklist, removing the former quadratic scan/nonterminating cyclic-param behavior; the curated gate completes in about six seconds. A bounded fixed point eliminates trivial local and stack block arguments whose non-self inputs resolve to one value while preserving type-changing refinement parameters; independent verification replays every eliminated argument and checks the exact metric, with focused loop-carried-local and branch-cast tests covering both paths. The strict corpus now builds 3,184 functions, 9,662 blocks, 115 demanded local block parameters, 232 demanded stack block parameters, 8,324 semantic instructions, 5,228 semantic arguments, 1,981 effectful instructions, and 979 trapping instructions through these verifiers. A reusable production `EmissionPlanner` hides CFG/SSA/simplifier scratch behind source-indexed verified decisions consumed by both target emitters; its first production consumer removes proved bounds checks for directly constant structured loads and stores. General production emission still walks the compact 16-byte stack record; callee-refined effects and full RailSSA emission remain. |
| 3 — RailMach echo backend | ✅ | AMD64 and ARM64 scalar emitters execute the admitted corpus. A reusable machine-SSA builder emits verified 24-byte instructions, flat banked operands, target fixed-register constraints, private-call constraints, and weighted SSA edge affinities. Both compatibility and native modes send verifier-safe scalar functions and selected profitable loop operations through RailMach; an explicit opcode policy retains the established emitter for stronger loop shapes. Finalization covers numeric, memory, global, select, structured control, scalar sign extension, spills/rematerialization, and direct/imported/indirect calls. RailMach preserves the compact stack source index through scheduling and post-RA rewriting, and both finalizers consume verified source-indexed bounds certificates rather than merely reporting them. Architecture-specific tests prove reduced native bytes and removal of only the certified trap-table entry. Both ARM64 modes pass 180/180; backend and public execution tests pass under Rosetta AMD64. |
| 4 — late SSA exit | ✅ | RailMach retains all 500 demanded local/stack edge transfers as weighted affinities through allocation. A verifier-backed late exit resolves parallel edge and fixed-register bundles, coalesces equal locations, expands spill-to-spill moves, preserves cycle values in a dedicated bank temporary while using a distinct transfer temporary for memory-to-memory copies, and chooses predecessor-end, successor-entry, or split-edge placement from CFG degree. Both production finalizers realize each placement at its physical program point. A legal complete parallel bundle moves to a successor that is no hotter; independent verification rejects illegal or mixed placement and recounts the motion debt used by schedule scoring. Across the current strict corpus it emits 711 AMD64 and 695 ARM64 physical moves, coalesces 102 transfers per target, and exercises seven cycles per target. Its current natural edges require no profitable motion, while a focused weighted-edge test exercises two successor-entry moves. Both acyclic production finalizers realize register, spill, spill-to-spill, cycle-temporary, and constant-rematerialization moves, plus AMD64 fixed shift-count and divide-input repairs, including preservation of values live across the RCX repair and of a divisor already occupying RAX. Forced 21-value integer pressure, high-pressure spill cycles across conditional merge edges, and 28-value floating rematerialization execute through RailMach on native ARM64 and Rosetta AMD64. |
| 5 — optimizer and proofs | ✅ | `SparseSimplify` is a bounded non-mutating overlay for constants, known bits/ranges, aliases, GVN, reachability, liveness, branches, obligations, and bounds certificates. GVN independently replays equivalence and dominance; bit-identical float constants are legal, while `memory.size` CSE is restricted to one block and one verified memory epoch. Exact integer/float and reinterpret round trips alias only after independently replayed range/type proofs. Finalizers consume discharged divisor and finite-conversion obligations. Strict coverage now records 435 aliases, 3,183 constants, 151 simplified branches, 149 dead blocks, 1,042 dead instructions, 332 discharged obligations, and 294 verified bounds proofs. Broader dominator GVN and non-bitwise loop ranges remain bounded extensions. |
| 6 — pressure shaping | ✅ | A verified source-stable planner estimates separate GPR/FPR pressure, identifies safe cheap-operation sinks, emits constant/extension/affine rematerialization recipes, recognizes loop inductions, plans cold uses, and places bounded LICM. Sink planning requires canonical and direct use counts to agree, so GVN-created extra consumers cannot invalidate the later schedule. Allocation omits committed cold rematerialized uses from hot intervals and both finalizers reconstruct them locally. Strict coverage records 275 sink candidates and 470 commits across target candidates, 4,051 recipes including 81 affine recipes, 162 profitable target decisions, 16 inductions with eight commits, two LICM commits, 24 cold uses, and peaks of 28 GPR/10 FPR values. Seven pressure schedules win per target without allocation debt, and both ARM64 target modes retain 180/180 execution wins. |
| 7 — quality allocation | ✅ | `RAGreedyP` starts from a complete `RALinearQ` result and exposes verified stop points for nonconflicting promotion, weighted eviction, call-crossing placement in callee-saved regions, and regional splitting. It rebuilds fixed-use repairs and recolors abstract spill slots. Both allocators consume the verified schedule, including cross-block emission ownership. Regional fragment discovery now walks actual scheduled order, so block/control epochs agree with the logical positions consumed by allocation and verification; this removed false cross-control fragments and admits Lua, SQLite, and Ruby. The call position is the operand/use point, so a result is not misclassified as crossing its own call. Callee-saved promotion charges a target-configured save/restore cost only on first use of each physical register; a focused high-cost test proves an otherwise eligible range remains spilled. Every retained spill is grouped into an explicit verifier-checked spillset with a colored slot, bank, aggregate weight, and dense member slab. Cold-use splitting is realized per operand without retaining a cold interval. Stage 4 creates independently replayed register fragments for profitable hot-loop or call-separated uses of an otherwise spilled immutable SSA value, retains the spill slot as the boundary authority, and discounts explicit transition cost from weighted spill debt. A free physical register needs one entry reload. In a saturated region, an inactive register victim receives a dedicated bounded spill slot, is saved before the fragment, and is restored immediately after it by both finalizers. Focused tests exercise two fragments around a call and a saturated hot-loop victim fragment. Candidate allocation reuses function-bounded dense liveness and worklist scratch, while independent verification validates fixed moves without allocating another vreg-sized interval table. Version-16 metrics expose stage, promotions, evictions, call-crossing ranges, preservation cost, spill slots, fragments, reloads, and stores. The strict stage-4 corpus still promotes 408 ranges per target, 138 genuinely call-crossing, charges 274 preservation units per target, and leaves zero abstract spills/weighted debt; forced-pressure tests exercise spill retention, spillsets, callee-saved promotion, regional placement, and dependency-legal reordered allocation. |
| 8 — RailSpec and selection | ✅ | A versioned core RailSpec JSON contract declares target masks, operand forms, native-byte/latency/uop costs, and verification state. `go generate` validates that contract and reproducibly emits the checked-in dense rule table; a check mode rejects stale output, unknown fields/forms/targets, duplicate identities, and missing costs. The rules cover the admitted scalar opcode set through verified generic-register rules plus AMD64 signed-immediate/fixed shift/fixed divide forms, ARM64 12-bit add/sub immediates, folded memory addresses, and compare/branch flags. `SelectOrder` verifies target legality, retains per-operand/result forms and compact costs, searches bounded shallow producer combinations without a third IR, and consumes an explicit validated compatibility/native cost model. Producer-linked address selection commits when an unsigned range proves `i32.add` cannot wrap; the consumer uses the base plus absorbed offset and the dead add emits zero bytes. A wrapping near miss remains unfused. Post-allocation AMD64 selection also folds an adjacent, single-use full-width `i32.load` or `i64.load` into add/sub/and/or/xor while preserving the original explicit bounds trap; initialized-memory and out-of-bounds execution tests pass under Rosetta, and version-16 metrics expose realized folds. Adjacent single-use integer comparisons feed flags directly to conditional control. Near-miss tests reject out-of-range immediates, cross-target rules, mismatched models, register subtraction, displacement overflow, and non-single-use combinations. Current strict selection finds AMD64 177 immediate, 112 fixed, 531 address, 110 flags, and 818 combined rows; ARM64 selects 136 immediate, 531 address, 110 flags, and 777 combined rows, while the post-RA gate realizes two AMD64 memory folds. Measured CPU-family overrides and wider/floating memory folds remain later target-policy extensions, not missing Phase 8 contracts. |
| 9 — scheduling and feedback | ✅ | A flat CSR dependency builder records data, heap-domain effect, global-barrier/trap, fixed-register, and fusion edges. Same-domain accesses remain ordered; explicit narrow host contracts expose independent domains, while unknown calls retain the full barrier. Verifier-backed source-stable, latency/resource-priority, and pressure-priority topological candidates use reusable per-block scratch. The default/one-worker path builds them sequentially; explicitly parallel module compilation may evaluate all three concurrently for machine functions with at least 1,024 instructions. Sole-use integer comparisons are linked to their actual conditional consumer even across intervening instructions, held until the control tail, and independently replayed for exact adjacency; the strict winning schedules commit all 55 eligible pairs on each target. Every candidate runs through schedule-aware `RAGreedyP` and late SSA exit; a deterministic lexicographic score chooses actual weighted spill, copy-cycle, physical-copy, copy-motion, fixed-repair, and broken-fusion debt. Production rebuilds only the winner. Simplification-created zero-use machine results discard their now-irrelevant pressure sink rather than failing scheduling. More than four profile-weighted retained spill units or excessive physical/cycle debt requests exactly one second pass (`MaxBackendAttempts == 2`) with zero-cost preservation bias and the same bounded candidate policy; low debt and attempt two are hard stops. Version-16 metrics report the actual attempt count, and forced-pressure execution tests exercise two attempts on both targets. Across 2,684 strict-corpus functions, both targets choose source-stable 2,679 times and pressure five times; latency/resource wins none under current generic costs, and the spill-free strict corpus requests no retry. Deeper critical-path/resource occupancy, calibrated CPU-family costs, and reload-latency post-RA motion remain. |
| 10 — private ABI and IPRA | ✅ | A bounded reusable call-graph prepass computes deterministic SCCs and compiles acyclic callees before callers while keeping cold recursive SCC members source-stable and conservative. A profile count on any member selects a fully RailMach recursive SCC for exactly one refined pass: every conservative intrinsic/outgoing contract is collected before publication, the shared recursive caller mask is solved simultaneously and independently verified, and the refined allocation is retained only when it stays inside that complete contract and its measured schedule score is no worse. Mixed-emitter SCCs and failed prepasses publish nothing and remain conservative. Post-allocation ABI analysis records exact banked clobbers including baseline allocations, regional fragments, fixed-use repairs, and canonical-result writes; it propagates volatile clobbers transitively, records callee-save masks, scalar parameter/result counts, ABI classes, and direct-call contracts. The private ABI passes a source-ordered prefix of up to four results in result GPRs and assigns overflow results to a caller-owned vector. Floating values use their raw bits in those GPRs. Both finalizers stage overlapping allocated results before restoring callee saves, publish overflow through a preserved hidden pointer, and leave the scalar path unchanged. Direct, imported, indirect, public-wrapper, consumed-result, and cold/warm-cache execution with six mixed results passes on native ARM64 and Rosetta AMD64; type-indexed multi-value block, branch, parameter, and loop transfers pass on both targets. Production caller allocation consumes completed acyclic and selected recursive callee contracts and may retain call-live values in proved-unclobbered volatile registers; version-16 metrics report each realized refinement. Source-indexed function metrics and cache identity remain stable despite code-layout order. A canonical argument vector is valid at every production private call, preserving mixed Stack/RailMach ABI compatibility; RailMach emits direct relocations, imported wrapper transitions, and indirect wrapper dispatch. `FrameCompose` reserves the largest source-ordered argument/result vector once per function on both targets, and each finalizer reuses it for direct, imported, and indirect calls instead of adjusting the stack per call. ARM64 retains a separate 16-byte link-register boundary. The bounded FPR set live across imported/indirect calls follows the vector because platform callees may clobber the allocated volatile set. Production frames also save and restore allocator-selected callee GPR/FPR regions and own dense spill slots. Per-register shrink wrapping now admits an explicitly observed zero-count, non-loop single-entry/single-exit chain of at most eight blocks. Every baseline interval, fragment, and fixed-register write remains inside the chain before its verified restore; a side entry, side exit, or loop header rejects it. Initial-local writes remain at the function boundary, frame slots stay fixed, and a focused two-block pressure case executes both hot and cold paths on native ARM64 and Rosetta AMD64. Version-16 metrics count realized pairs. Direct, host-imported, table-indirect, and profile-hot mutual-recursion execution passes on native ARM64 and Rosetta AMD64; a dedicated recursive call-and-memory differential exercises depths zero through eight. Current strict-corpus coverage refines 808 calls per target and reaches maximum composed frames of 880 bytes AMD64/928 bytes ARM64. Collector-reference call results now retain exact types and participate in the narrow root plan. |
| 11 — target post-RA quality | ✅ | A verifier-backed bounded physical rewrite planner consumes selection, schedule, allocation, and late-exit debt. AMD64 realizes LEA repair, direct compare/branch flags, fixed-divide repairs, and adjacent full-width load-to-ALU folds while retaining explicit traps. Materialized conditions always use `setcc` plus a full-width `movzx` (including REX low-byte registers), while fused compare/branches avoid the partial-register value entirely. Target-neutral exact store/load forwarding is available. ARM64 realizes aligned legal integer and scalar-floating load pairs, direct compare/branch flags, compare-plus-conditional-increment, and independently replayed power-of-two repeated-add reduction. Floating `LDP` encoder goldens, forced finalization, and native execution prove that FPR allocation reaches the pair instruction. Each load retains an ordered source-specific bounds check: native boundary execution reports the first or second Wasm PC according to the actual failing access. `STP` cannot preserve the first Wasm store when only the second access traps, and byte/halfword pairs do not exist in AArch64; stores and narrow adjacent accesses therefore remain eligible for the post-index chain. A nonadjacent one-use integer comparison can now be physically renamed from its boolean GPR onto AMD64 EFLAGS or ARM64 NZCV across a bounded run of independently verified constant materializations; both finalizers omit boolean materialization and the later compare. Forced constrained schedules on each target prove exact byte savings, while current natural scheduler candidates keep all eligible corpus compare/branch pairs adjacent and therefore do not need this repair. Pair realization independently checks scaled-offset alignment. Nonzero scalar linear-memory offsets from 1 through 255 use verifier-gated signed-unscaled ARM64 pre-index loads/stores on the reserved native-address scratch. Adjacent same-base scalar accesses whose offsets differ by a signed imm9 use a real post-index first access and carry that scratch address into the second; the second retains its own bounds check on a distinct scratch, preserving trap order and first-store side effects. Neither form changes the Wasm-visible address. Encoder goldens, verifier range/base near misses, and focused native finalization prove positive exact byte savings. FEAT_MOPS targets now replace eligible dynamic copy/fill loops with the architecturally required `CPYP`/`CPYM`/`CPYE` or `SETP`/`SETM`/`SETE` triplet after the same explicit Wasm bounds checks. A source-keyed `MemOpSizes` histogram with at least ten observations keeps a site on the baseline loop unless at least 90% of observations may be 65 bytes or larger; absent or sparse profiles select the target-native path. Counts saturate and threshold-crossing buckets fail toward MOPS. Exact assembler goldens, compiler feature/profile gating, warm-cache ISA retention, `.wago` requirement persistence, and Linux `HWCAP2_MOPS` detection are tested; compatibility targets never emit MOPS. This Apple M4 Max traps on MOPS, so native instruction execution still requires qualifying ARMv8.8/ARMv9.3 hardware. Regional spill fragments replay selected-schedule control boundaries before commitment. The finalizer also uses bit-exact NEON copysign, signed-offset page loads, and zero-grow specialization. Near-miss and semantic edge tests reject unsafe rewrites, including NaN-payload and signed-zero copysign cases. Current ARM64 native metrics over the 15 admitted ISA modules report 807 realized rewrites and 900 bytes of exact diagnostic saving across 167 compiled functions; the prior Rosetta AMD64 count is 164 and must be refreshed on an AMD64 host after this batch. The enumerated Phase 11 deliverables are complete; broader physical renaming/folding and AVX/APX instruction expansion remain post-plan quality work. |
| 12 — runtime specialization | ✅ | A verifier-backed specialization plan consumes only explicit facts and records original-Wasm-tied same-instance calls, declared host effect contracts, preserved bounds certificates, and profile-dominant indirect targets. Public host contracts use exact module/name identity, are snapshotted and normalized into function-import order, survive replay, and enter only consuming functions' cache identities. Declared metadata is refined before simplification and scheduling; same-domain heap dependencies remain ordered, while independent domains can move around calls without global flags. Undeclared imports retain the conservative barrier. Backend-neutral compact GC reference facts and collector-reference classification now live at the shared codegen seam instead of under Railshot. Production RailSSA derives sparse, source/result-ordered exact-type and unpublished-fresh facts for struct/array allocation results, including fixed-array lengths, independently verifies reference result identity and freshness preconditions, records separate specialization certificates, and reports them in version-16 metrics. Reference-bearing signatures, locals, globals, block results, and calls retain their exact heap types through a sparse side slab without widening the 16-byte stack instruction. A bounded backwards typed-SSA dataflow plan independently verifies exact roots at every collecting call, colors disjoint values onto reusable slots, and marks only live-after values for reload. Both native finalizers realize the stores/reloads; version-3 artifacts and runtime output publish canonical root slabs, frame sizes, return PCs, helper safepoint identities, and wrapper stack adjustments. Wago validates and installs those sites in its existing native frame walker, and focused compiler plus product tests cover nested live collector roots while excluding `funcref`. Unused initialized/default struct results and pointer-free default/uniform/fixed/data array results select shared checked reservation helpers: native result materialization and unnecessary payload population disappear while real allocation, exact roots, collection, initializer validation, data-segment bounds, and bounded heap-exhaustion semantics remain. Reference-uniform arrays fail closed on the ordinary allocating helper. Stores of directly proved `null` and `i31` values select no-edge struct/array helpers; the collector independently repeats type, bounds, nullability, ownership, and non-object checks before mutation and rejects an object near miss. Initialized fresh-object constructors already publish payloads atomically and reconcile one whole-object remembered-set entry when required. The indirect gate requires at least 10 observations and a 90% target share without counter overflow, admits only in-module local targets, and emits a canonical-funcref-identity guard before the same-instance private direct-call relocation. Table mutation, profile misses, foreign identities, nulls, bounds failures, and signature failures retain the checked descriptor fallback. Native execution covers profile hits/misses, exact roots, checked allocation elimination, barrier-specialized stores, and trap preservation. Strict scalar coverage records 808 same-instance calls and preserves all 294 bounds certificates; allocation-bearing GC functions supply production facts. Snapshot guards beyond canonical identity, native code cloning, nested dead-constructor trees, and broader GC helper inlining remain optional later extensions rather than missing Phase 12 contracts. |
| 13 — native/profile mode | ✅ | Compatibility and native target modes plus speed/balanced/size objectives flow orthogonally through the API, CLI, replay, and cache identity. Both modes select the complete RailMach pipeline under one explicit source/opcode profitability policy; compatibility emits only baseline-portable instructions, while native identities record host feature bits plus a canonical CPU model and tuning family (for example, `apple-m4-max` / `apple-m4`). Unknown models remain exact identities and use architecture-generic tuning. Stable APX and MOPS feature identities join the existing BMI2/AVX/ERMS/FMA and AES/ATOMICS/CRC/SVE/SVE2 identities without renumbering prior bits; MOPS is selected only from the Linux architectural capability bit, while APX remains a fail-closed policy foundation without instruction selection. A reproducible native ARM64 calibration harness runs alternating 64-operation dependent ADD and cache-resident pointer-load chains through Wago's no-cgo trampoline. Three Apple M4 Max process runs measure the load at 2.9987x, 2.9988x, and 3.0024x ADD latency, so the first measured `apple-m4` table uses three latency units for folded memory instead of the generic four; byte/resource costs, unmeasured rules, and unknown families remain generic. The retained post-change August 28, 2026 exact six-round, 500 ms alternating gates pass 180/180 on ARM64 in each mode. Compatibility's narrowest raw/paired wins are 5.21%/5.19% at `isa_cmp_f32.eq`; native's are 5.35%/5.32% at `isa_cmp_f32.ne`. Complete per-round outputs are checked in for both target modes. The five sign-extension rows win by 82–88% after verifier-backed redundant-extension elimination. Immutable backend-neutral profiles are source-hash checked and included in replay/cache identity. The ExtTSP-like chain planner maps original-Wasm edge sites into actual target block order. Constant bulk-memory shapes use calibrated target loops. A bounded native-clone planner ranks only profile-hot roots with explicit native opportunities and projected gain, admits their complete local direct-call closures under hard function/body-byte budgets, and skips a closure atomically when it does not fit. `CompileNativeClone` emits only that sorted selection, keeps original indexes and exact GC metadata, rejects open direct-call closures, and produces a non-standalone/non-serializable compact image that can be installed only under its exact plan. One-time tier entry publication leaves cold functions on the Railshot compatibility body without per-call feature tests. The enumerated Phase 13 deliverables are complete; additional CPU-family calibration, APX/SVE2 instruction experiments, and single-call `TargetFatNative` packaging remain post-plan extensions. AMD64 emission and compact cloning execute under Rosetta and cross-build natively, while physical AMD64 hardware qualification remains an environment-specific release check. |
| 14 — production Dragline | ✅ | The independent compatibility backend, engine-tagged artifacts/cache identity, strict no-Railshot diagnostics, AMD64/ARM64 scalar emitters, public compiler/target selection, and expanded release benchmarks are implemented. Both modes finalize verifier-safe numeric, comparison, conversion, memory, global, select, structured-control, spill/rematerialization, late-move, fixed-register, call-live frame, direct/imported/indirect call, multi-result, and type-indexed multi-value control shapes. ARM64 additionally realizes compare-plus-conditional-increment, bit-exact NEON copysign, signed-offset page loads, zero-grow specialization, and verified power-of-two repeated-add reduction. All supported scalar loop families now enter RailMach; an exact 36-module ARM64 cutover moved 43 additional giant-module functions to the common path, reduced native output by 120,592 bytes, increased peak compiler-owned storage by at most 1,792 bytes, and left compile wall effectively unchanged. Mixed vector/collector functions use slot-accurate two-word `v128` helper arguments and results, and vector-live branch casts preserve refined edge identities through RailMach. Any parameterized or multi-result region also enters RailMach unconditionally because the older structured emitter has only a scalar control contract. The allocator extends loop-invariant liveness through every backedge and independently verifies that contract. Unreachable CFG blocks emit fail-closed rather than requiring a dead semantic terminator. The strict gate compiles all 782 emitted modules with zero MVP rejection and zero post-MVP exclusion. Compiler footprint regressed on eight expanded RailMach modules; reusable bounded pipeline scratch, exact slab sizing, value-count reservations, byte-bounded structured-prepass reservation, selection-verifier use-count reuse, function-bounded dependency-DAG slab reservation, rewrite-family-specific post-RA scratch, and a nonredundant 40-byte integer-fact record cut `isa_cmp_f32` from about 6.45 ms/419 KB/4,780 allocations to about 1.48 ms/224.6 KB/401 allocations without changing strict backend decisions or its 4,504-byte output. Source-disjoint local get/set identities now share one compact slab, and local-get flow identity uses the general instruction-value slab; block-parameter identity lives directly in the local entry matrix instead of a third dense phi matrix. Exact rematerialization-recipe reservation removes repeated growth without over-retaining scratch. Exact-on-growth slabs, dead pressure-scratch removal, saturating use counters, and type-indexed integer facts lower `isa_cmp_f32` again to about 208.6 KB/400 allocations and `isa_mem_narrow` to about 195.8 KB/620; `isa_cmp_f64` is now the largest measured row at about 214.4 KB/400. Consuming `isa_mem_narrow`'s already-verified RailMach bounds certificates reduced native output from 27,576 to 15,160 bytes, only 1.5% above Railshot's 14,932 bytes instead of 84.7% above. Replacing the ARM64 power-rotation package-init map computation with checked-in deterministic results removes about 31 MB of transient process-startup allocation. Bounded RailSSA local, region, nesting, structured-prepass, and pressure capacities, RailMach compact local/operand/spillset capacities, plus AMD64/ARM64 scalar frame encoding limits now retain required/limit values in a typed resource-limit error; frame sizing uses overflow-safe 64-bit arithmetic before narrowing; strict Dragline returns it directly, while explicit whole-module Railshot fallback may recover it. A pre-current-work six-round isolated-process gate over all 17 admitted ISA modules compared Dragline directly with Wasmtime 46.0.1 Cranelift: compatibility/native geometric-mean wall ratios are 0.917x/0.899x and RSS ratios are 0.437x/0.437x; worst module ratios are 1.144x/1.124x wall and 0.467x/0.471x RSS. These process-startup-inclusive results pass the current 3x latency and 1.5x Cranelift-memory ceilings on every ISA module. That run's corresponding six-round gate over 29 non-SIMD neutral optimized-Wasm modules uses the explicit eight-worker policy and also passes every module: compatibility/native geometric-mean wall ratios are 0.983x/0.966x, CPU ratios are 0.563x/0.556x, RSS ratios are 0.537x/0.538x, and worst wall/RSS ratios are 2.554x/0.855x and 2.554x/0.839x. Call-graph component scheduling preserves callee-first IPRA dependencies and deterministic layout; only explicitly parallel large functions evaluate their three verified schedule candidates concurrently. Serial and eight-worker artifacts are byte-identical. The enumerated Phase 14 deliverables are complete. Further footprint tuning, LLVM measurement, broader release qualification, and physical AMD64-host reruns are release follow-ups rather than missing compiler contracts. |
| 15 — cache, daemon, tiering | ✅ | A strict versioned function-artifact contract fingerprints original body and type dependencies, runtime/private ABI, target, objective, bounds/runtime configuration, profile, specialization, callee contracts, and compiler revision. Its flat relocations, traps, safepoints/root slab, source mappings, ISA requirements, entries, frame/ABI class, and clobber masks are independently validated and canonically encoded. Safepoint ranges must pack the root slab exactly in native-offset order; gaps, overlaps, out-of-bounds ranges, and trailing unreferenced roots reject before cache publication or restore. A concurrency-safe process-shared LRU stores immutable deep snapshots under a caller-set canonical-payload charge, rejects oversized entries, and reports hits/misses/evictions under a hard byte bound. A deterministic versioned stream snapshots entries for explicit cross-process persistence; restore validates the full archive and byte charge before one atomic replacement. Both production emitters populate relocatable code, entries, relocations, traps, safepoints, and source mappings. Cache identity separates non-code module structure, each function body, and the transitive direct-callee closure that can alter IPRA allocation. The optional `compilerdaemon` service exposes a bounded versioned digest-protected compile protocol, validates exact code-generating options and returned `.wago` artifacts, limits concurrent compilation, and shares the same immutable function cache across reusable connections. `draglined` provides an owner-only Unix listener, explicitly gated loopback TCP, strict startup restore, and atomic owner-only cache persistence across graceful restarts. Tests cover native invocation, recoverable malformed-Wasm requests, protocol corruption rejection, cross-connection and cross-restart cache hits, malformed/symlink snapshots, and race execution. Railshot profile collection, bounded tier and native-clone planners, compact clone compilation, and one-shot Railshot-to-Dragline installation are implemented. Exact native GC metadata switches with code: an immutable generation is published before entry slots, helper and host safepoints resolve it from return PCs, nested host activations retain per-generation maps, and frame walking crosses old/new generations while already-running Railshot frames finish. Native ARM64 tests install actual source-identical full and compact Dragline exact-root images under forced collection, execute selected Dragline and unselected Railshot allocating functions in the same instance, verify only the unselected entry remains profiled, prove unselected native entries and bodies are absent, and collect successfully. The current full 180-export ISA installation gate passes. |

## Latest runtime-specialization slice

- Dragline now executes `ref.null`, `ref.is_null`, `ref.func`, `ref.eq`,
  `ref.as_non_null`, `ref.i31`, `i31.get_s`, `i31.get_u`, `ref.test`,
  `ref.cast`, `any.convert_extern`, and `extern.convert_any`. `ref.func`
  retains its exact non-null indexed function type and resolves the canonical
  descriptor from per-instance runtime state; i31 values stay allocation-free
  tagged immediates. Runtime type tests and casts use one packed target encoding,
  admit collector, defined-function, abstract `func`, and bottom `nofunc` heap
  targets without confusing canonical function descriptors with collector
  references, and retain precise result types and explicit null/cast traps.
  Exact defined-function casts require bidirectional structural subtyping.
  Extern conversions round-trip both null and instance-owned references. A
  `br_on_cast` and `br_on_cast_fail` now use sparse verified RailSSA edge
  refinements and RailMach block identities for scalar machine values. Native
  execution covers taken and fallthrough paths, nullable targets, collecting
  functions with a live root, call-bearing functions, and multi-value branch
  targets. Mixed V128 functions remain on the structured SIMD emitter until
  RailMach gains a 128-bit machine value/spill contract, and execute through a
  focused native test.
- `struct.new_default` and initialized `struct.new` execute through the
  allocating-helper path in both native finalizers. Initialized fields are
  staged directly from allocated register, spill, or rematerialized locations
  into the bounded helper frame, including floating-point bit patterns and live
  collector-reference roots. Parked-helper dispatch encoding and synchronous
  control-frame offsets live at shared compiler/runtime seams rather than under
  Railshot.
- The non-collecting helper path also executes `struct.get`, `struct.get_s`,
  `struct.get_u`, and `struct.set` for scalar numeric, packed, floating-point,
  and reference fields. Recursive field types resolve to absolute module
  identities before typed SSA. Reference subtype assignments retain their more
  precise SSA type while the local/field boundary is independently checked.
  V128 field reads and writes use the structured two-slot helper ABI on both
  native targets.
- `array.new`, `array.new_default`, bounded `array.new_fixed`,
  `array.new_data`, and `array.new_elem` join the allocating-helper path;
  `array.get`, `array.get_s`, `array.get_u`, `array.set`, `array.len`,
  `array.fill`, `array.copy`, `array.init_data`, and `array.init_elem` join the
  non-collecting path. `data.drop` mutates the passive-data descriptor directly,
  while `elem.drop` uses the collector-aware helper so staged element roots are
  released before the segment becomes empty. Numeric, packed, floating-point,
  reference, overlapping
  copy, V128 get/set/fill/copy, and passive data/element-segment cases execute
  through the shared helper ABI, with reference write barriers and null/bounds
  traps. V128 `array.new`, `array.new_fixed`, and initialized `struct.new`
  stage canonical two-slot values through a deliberately root-free structured
  path and publish exact empty-root allocating safepoints on both native
  targets. Fixed-array initializer operands are typed individually and
  staged directly from their allocated locations; a collecting fixed allocation
  retains both live reference initializers with an exact root map. The
  deterministic module-global safepoint scan counts all implemented struct and
  array allocation sites, including both segment-backed constructors.
- Version-3 function artifacts and runtime output distinguish native-call
  return-PC roots from module-global allocating-helper safepoints. They also
  carry exported-wrapper return offsets for bounded cross-frame walking.
- Focused native ARM64 tests execute exact `ref.func`, signed and unsigned i31
  extraction, runtime reference tests/casts, branch casts on both taken and
  fallthrough edges, extern conversions,
  scalar/reference/packed struct reads and writes, default, uniform, fixed,
  data-segment, and element-segment array allocation, scalar/reference/packed
  array reads and writes, fill/copy, segment initialization, and segment-drop
  lifecycle, preserve live
  collector references across collecting helpers, and walk a root from an
  allocating callee through its caller and exported adapter. AMD64 compiler and
  Wago test binaries cross-compile successfully.
- A post-control-fragment-fix four-round, 100 ms native ARM64 smoke comparison
  still has all 180 admitted ISA exports faster than Railshot; the narrowest
  raw delta was -4.97% at `isa_cmp_f32.lt`, and the narrowest paired delta was
  -5.00% at `isa_cmp_f64.eq`. This does not replace the checked-in six-round, 500 ms
  release measurement.
- Allocating-helper safepoint bases are deterministic in final function-layout
  order and enter each function artifact identity. Warm helper modules reuse
  cached code and exact root metadata; changing an earlier allocation-site
  count invalidates every later artifact whose embedded module-global ID moves.

## Current compatibility boundary

The checked-in ISA gate admits 180 exports in 17 generated modules: all MVP
scalar numeric/comparison/conversion operations, full-width and narrow memory,
memory metadata, control, local/global, and call families, plus nine calibrated
post-MVP `memory.copy` and `memory.fill` rows and all five scalar sign-extension
instructions. A fixed inventory
test guards the count and structural compositions. It passes in compatibility
and native modes on ARM64; AMD64 lowering and execution tests pass under
Rosetta. Scalar and multi-result direct calls, function imports, and
table-dispatched indirect calls now execute through independent Dragline
lowering, including bounds/null/signature traps, host returns, cross-instance
returns, cross-instance trap propagation, arbitrary scalar parameter lists
through eight registers plus a canonical argument vector, and mixed six-result
register/vector returns. Type-indexed multi-value blocks, branches, parameters,
and loop labels also lower through verified RailSSA block arguments. Other
post-MVP module/runtime contracts remain rejected or constrained. A separate
post-MVP gate admits the exact 31-instruction core-SIMD subset used by the
checked-in JSON, Blake, and UTF corpora. V128 parameters, results, locals,
structured-control results, calls, selects, and frame homes use two canonical
64-bit slots without changing scalar frame accounting.

The opt-in pinned compile gate admits all 782 modules emitted by the 64-file
pre-reference-types corpus, with zero MVP rejections and zero post-MVP
exclusions. The emitted set includes eight multi-value function or type-index
block shapes because the pinned translator runs with `--enable-all`; those
shapes now exercise the explicit multi-result ABI and full block-signature
lowering. Run it with
`WAGO_DRAGLINE_MVP_COVERAGE=1 go test -run TestDraglineMVPCoverage` from `bench`.

The exact corpus folds are only legal when their recognizers prove their complete
shape. They cannot be used as evidence for general opcode semantics. In
particular, floating-point NaN/signed-zero behavior, saturating-conversion edges,
trap ordering, and general call/control shapes need semantic tests independent of
the performance corpus.

## Current performance gates

- Execution correctness: ✅ all 180 admitted exports match Railshot at the
  manifest trip counts, with zero timed allocations.
- ARM64 SIMD mask reduction: ✅ a general post-RA rewrite now recognizes an
  adjacent, sole-use `i8x16.bitmask` → `i32.popcnt` dataflow chain and counts
  the shifted sign bytes directly with `USHR.16B`, `ADDV.8B`, and `UMOV.B`.
  The legality proof is based only on typed machine values, exact use counts,
  schedule adjacency, target identity, and physical register locations; it has
  no module, export, corpus, or algorithm identity. On the September 5, 2026
  Apple M4 Max gate, ten alternating 500 ms rounds reduced
  `utf-as-simd.convertN` from a 83.945 us exact-head median to 46.076 us; the
  paired geometric-mean ratio was 0.548x and the candidate won all ten pairs.
  The same candidate measured 0.474x wazero across ten paired rounds. Module
  native code fell from 22,220 to 21,660 bytes; compiler-owned peak-live bytes
  moved only from 729,198 to 729,208, and compile-wall medians were effectively
  flat at 15.510 ms versus 15.569 ms. Full repository tests, Dragline vet, bench
  tests, corpus coverage, and the dedicated SIMD differential gate pass.
- Every admitted export faster than Railshot: ✅ all 180 compatibility-mode
  ARM64 balanced medians are lower in the post-change August 28, 2026
  six-round, 500 ms serialized alternating run. The narrowest raw and paired
  medians are -5.21% and -5.19% at `isa_cmp_f32.eq`. The narrowest bulk row,
  `copy_fwd_4096`, is -5.61% raw and -5.59% paired. The raw rounds are in
  `docs/dragline-isa-arm64-compat-2026-08-28.txt`. Run
  `go run ./cmd/draglinecompare -rounds=6 -benchtime=500ms` from `bench`.
- Native-target release gate: ✅ all 180 exports beat Railshot in the same
  post-change balanced six-round, 500 ms serialized alternating run. The
  narrowest raw and paired medians are -5.35% and -5.32% at
  `isa_cmp_f32.ne`. The narrowest bulk row, `copy_fwd_4096`, is -5.72% raw
  and -5.73% paired. `memory.size` is -86.78% and `memory.grow(0)` is
  -53.63%. The raw rounds are in
  `docs/dragline-isa-arm64-native-2026-08-28.txt`.
  Run `go run ./cmd/draglinecompare -target=native -rounds=6
  -benchtime=500ms` from `bench`.
- Compile latency: 🚧 seven of the original 15 scalar ISA modules compile faster than
  Railshot in the current three-sample run on Apple M4 Max. The eight expanded
  weak-loop modules routed through RailMach are slower. Start-ordered live
  intervals now let the independent allocation verifier stop its overlap scan
  once later overlap is impossible, while unordered external products retain
  the complete pairwise check. Pressure-candidate validation also reuses the
  scheduler's exact instruction-use count instead of rescanning the function
  for every sink. Together these lower `isa_cmp_f32` from about 1.55 ms to about
  1.28 ms without changing allocation or native output.
- Compile allocation: 🚧 seven of 15 allocate fewer bytes. Exact-on-growth
  reusable slabs, dead pressure-scratch removal, saturating use counters, and
  type-indexed integer facts lower `isa_cmp_f32` to about 208.6 KB and 400
  allocations versus Railshot's 113.7 KB. The current worst row is
  `isa_cmp_f64` at about 214.4 KB and 400 allocations,
  down from about 419 KB
  and 4,780 allocations after reusing bounded SSA, allocation, scheduling,
  verifier, and physical-occupancy scratch, sizing exact machine/semantic slabs,
  and reserving only guaranteed SSA values before construction. Bounded
  observation-driven native-buffer sizing plus bounded prepass reservation also
  cuts `isa_mem_narrow` from about 278.0 KB/670 allocations to 195.8 KB/620;
  verified bounds-certificate consumption also cuts it to 15,160 code bytes.
  This is active
  compiler-footprint debt; the execution gate does not waive it.
- Native size: ✅ 14 of the original 15 scalar ISA modules are smaller. The expanded
  `isa_mem_narrow` module is now 15,160 versus 14,932 bytes, a 1.5% increase
  and within the plan's 10–15% growth ceiling; it was 27,576 bytes before
  RailMach consumed the already-verified bounds certificates.
- Release binary footprint: the refreshed 2026-09-04 Linux/AMD64 release-profile
  run with Go 1.22.2 and TinyGo 0.41.1 measures 9,863,320 bytes for Standard,
  9,552,024 bytes for Minimal, and 3,178,096 bytes for TinyGo Minimal. The
  measured product ceilings are 9,900,000, 9,590,000, and 3,525,000 bytes,
  leaving 36,680, 37,976, and 346,904 bytes of headroom. The manager measures
  7,794,840 bytes and remains within its unchanged 9,000,000-byte ceiling.
- Native AMD64 execution performance: ✅ a Ryzen 7 7800X3D five-round,
  500 ms alternating run across 36 exports in 30 non-ISA modules measures
  Dragline at 95.58% of Wasmtime/Cranelift throughput by paired module-equal
  geometric mean. Every aggregate round is between 95.39% and 95.92%.
- Curated applications: ✅ 30/36 execute and match Railshot; `regexmatch`,
  `wasm3`, Lua, SQLite, Ruby, and esbuild additionally compile but need their
  host environments to run. All 36 available artifacts are admitted. The three
  former SIMD rejections now execute through a normal focused differential gate.
  The retained ARM64 operand/local caches are allocation-free and reduced the
  exact six-sample median range to 1.85x–8.35x slower than Railshot across the
  five manifest exports (Apple M4 Max, 500 ms/sample). SIMD-wide physical
  allocation, bounds-check elimination, and instruction combination remain
  explicit post-MVP debt.
- ARM64 emitter convergence: ✅ the exact 36-module corpus routes all 30 runnable
  applications and 27,384 of 27,390 total functions through RailMach. The six
  retained structured functions are scalar giants above 4,096 source
  instructions with exact trapping conversions; this is the measured internal
  fast path allowed by Phase 9, not module-identity routing. Moving trapping
  conversion scratch from allocatable V0/V1 to reserved V28/V29 removed the
  other 71 finalizer fallbacks. A five-round alternating comparison against the
  pre-cutover compiler lowered full-module compile time by 19% for Lua and 13%
  for wasm3. SQLite increased 4.6%, inside the 10% migration cap, while its
  native image shrank 0.4% and peak compiler-owned storage fell 11%. Native
  size changed by -1.8% for Lua and +0.01% for wasm3, and their peak storage
  also fell. Ruby's exact single run held compile wall effectively unchanged,
  reduced peak storage slightly, and shrank the complete native image 0.2%.
  Metrics schema 21 records the exact
  source-admission or finalizer-safety reason for every structured function.
- Giant-function workspace retention: ✅ serial and parallel compilation on
  both targets now retain ordinary planner slabs but release a planner after a
  completed function when its reusable capacity exceeds 16 MiB. This is a
  function-size policy only; emitted code, relocations, ABI contracts, and
  artifacts are consumed before release. On the exact Ruby module, compiler-
  owned peak live storage fell from 115,392,147 to 78,027,191 bytes (32.4%),
  compile wall fell from 95.330 to 94.603 seconds (0.8%), and the native image
  remained exactly 49,427,280 bytes. Fresh-process `/usr/bin/time` runs on the
  same Ruby artifact reduced maximum RSS from 472,416,256 to
  381,157,376 bytes (19.3%) and Darwin peak memory footprint from 394,756,960
  to 324,404,016 bytes (17.8%). This closes the measured Phase 11 debt in which
  one exceptional function's roughly 48 MiB workspace remained live while the
  module's native image continued to grow.
- Giant-function phase lifetimes: ✅ exceptional planners now snapshot their
  exact capacity high-water, release local SSA immediately after value-flow
  construction, release value-flow storage after its last address-folding
  consumer, and release the remaining planning-only slabs before native
  emission. Ordinary planners below 16 MiB keep all reusable storage. Any
  planner trimmed in one of these phases is discarded after emission rather
  than reused partially empty. Metrics schema 22 separates planning and
  emission high-water marks while retaining the function-wide peak. On the
  exact Ruby module, the largest function's compiler-owned peak fell from
  44,834,283 to 27,133,035 bytes (39.5%), the module peak fell from 78,025,015
  to 75,394,416 bytes (3.4%), and Darwin peak memory footprint fell from
  323,699,504 to 308,839,216 bytes (4.6%). Compile wall moved from 94.729 to
  93.978 seconds (0.8% lower), native output remained exactly 49,425,936
  bytes, and process maximum RSS was flat within 0.1%, confirming that Go
  runtime/unreclaimed heap dominates that process-level measure.
- Sparse bounds-cache scratch: ✅ the finalizer's reusable touched-address slab
  is now sized from the exact selected memory-descriptor count rather than all
  machine instructions. Only a selected memory operation can append to this
  scratch, and a focused planner test pins that bound. Against exact schema-29
  pre-change runs, compiler-owned peak live storage falls from 14,724,828 to
  14,690,496 bytes for SQLite and from 3,502,094 to 3,496,690 for Lua, with
  byte-identical native images. Esbuild falls from 82,838,709 to 82,688,737
  bytes (-149,972) with its 36,864,196-byte image unchanged; its single
  serialized wall run moved from 159.910 to 165.391 seconds (+3.43%), inside
  the footprint-migration ceiling. Ruby falls from 75,336,538 to 75,242,430
  bytes (-94,108), remains exactly 48,955,040 native bytes, and its paired wall
  run improves from 98.693 to 98.147 seconds (-0.55%).
- Operation-gated GC scratch: ✅ dead-constructor and no-write-barrier bitmaps
  are now allocated only when the machine function actually contains their GC
  operation family. The finalizers already treat absent maps conservatively;
  focused tests retain dead-constructor and reference-store proofs while a
  non-GC planner test requires zero bitmap capacity. Relative to the preceding
  sparse-bounds result, peak live storage falls another 21,556 bytes for SQLite
  and 3,910 for Lua. Esbuild falls another 105,278 bytes to 82,583,459 and Ruby
  another 56,250 bytes to 75,186,180; their largest-function planner reductions
  are exactly two bytes per machine instruction. Native output remains byte-
  identical for all four modules. Serialized wall time moves from 165.391 to
  159.406 seconds for esbuild (-3.62%) and 98.147 to 96.604 seconds for Ruby
  (-1.57%).
- Operation-gated bounds scratch: ✅ a function with no selected memory
  descriptor now presents empty bounds-cache endpoint and touched-address
  scratch to both finalizers. Reusable capacity remains available across
  functions, but a fresh memory-free planner allocates neither slab. On the
  exact ARM64 `many_funcs` corpus, the largest function falls from 5,793 to
  5,737 peak live bytes and the module peak from 71,007 to 70,951; `arith`
  falls from 15,234 to 15,106 function peak and from 15,563 to 15,435 module
  peak. Their native images remain byte-identical at 19,276 and 184 bytes.
- Native-planner category attribution: ✅ metrics schema 30 records the exact
  disjoint control-flow, bounds, post-RA, immediate, GC, and call/root capacity
  at each function's planner high-water; the canonical Markdown projection
  exposes the same totals. Exact ARM64 samples identify post-RA realization as
  the largest remaining native-planner category: 158,332 of 369,046 bytes for
  SQLite's module-peak function, 33,750 of 78,189 for Lua, and 158,312 of
  362,648 for regexmatch. Immediate and bounds state are the next largest
  categories, so subsequent footprint work can target measured retained state
  instead of aggregate planner size.
- Compact post-RA fusion relations: ✅ functions with at most 65,535 machine
  instructions retain fusion partners in an instruction-indexed `uint16` slab;
  larger functions preserve the existing `uint32` representation. Both remain
  direct O(1) finalizer lookups, and focused tests pin the boundary encoding on
  ARM64 and AMD64. Exact ARM64 native images remain byte-identical for SQLite,
  Lua, and regexmatch while peak compiler-owned storage falls by 21,556, 3,910,
  and 19,840 bytes respectively. Six alternating serialized SQLite public-
  compile runs are neutral at 1.6797 versus 1.6798 seconds median, while median
  allocation volume falls by about 65 KiB/op.
- Compact ARM64 pair relations: ✅ verified load/store partner identities use
  the same adaptive 16-bit representation below 65,536 machine instructions
  and retain the 32-bit fallback above it. SQLite, Lua, and regexmatch native
  images remain byte-identical while peak storage falls another 16,068, 3,910,
  and 19,840 bytes. Six alternating serialized SQLite public compiles are
  effectively neutral at 1.695 versus 1.693 seconds median, and median
  allocation volume falls by about 30 KiB/op.
- Compact load/store forwarding relations: ✅ the target-neutral forwarding
  map now uses 16-bit store identities below 65,536 machine instructions and a
  32-bit fallback above that boundary. SQLite, Lua, and regexmatch remain byte-
  identical while peak storage falls another 16,068, 3,190, and 9,716 bytes.
  Six alternating serialized SQLite public compiles improve slightly from
  1.6707 to 1.6686 seconds median, while median allocation volume falls by
  about 35 KiB/op.
- Unified compact instruction relations: ✅ pair, forwarding, fusion, AMD64
  memory-fold, ARM64 repeated-add, and ARM64 post-index identities now share
  one allocation-free adaptive representation. Go confirms every finalizer
  `get`/`has` call is inlined. Compact/wide boundary tests cover each operation
  family, while existing realization tests retain target behavior. Relative to
  the forwarding-only result, SQLite, Lua, and regexmatch remain byte-identical
  and lose another 16,068, 3,910, and 19,840 peak bytes. SQLite's measured
  post-RA category has fallen from 158,332 to 88,572 bytes across the compact
  relation series. Six alternating public compiles improve from 1.6566 to
  1.6533 seconds median, with about 37 KiB/op less allocation.
- Compact immediate-producer relations: ✅ immediate selection now reuses the
  same adaptive instruction-identity representation, including the selected
  ARM64 opcode seam and the AMD64 finalizer. Compact and wide selection tests
  cover both encodings. SQLite, Lua, and regexmatch remain byte-identical while
  peak compiler-owned storage falls by 21,556, 3,910, and 19,840 bytes; their
  immediate planner categories fall from 87,638 to 66,082, 16,295 to 12,385,
  and 88,800 to 68,960 bytes. Six alternating serialized SQLite public
  compiles are compile-wall neutral (+0.04% median) while median allocation
  volume falls by about 63 KiB/op.
- Unique-address bounds-cache slots: ✅ finalization retains a compact O(1)
  VReg-to-slot relation and one 64-bit bound only for each distinct selected
  memory address, using already-dead immediate scratch to assign slots. Two
  accesses through the same address are covered explicitly. SQLite, Lua, and
  regexmatch remain byte-identical while peak compiler-owned storage falls by
  52,106, 10,264, and 67,112 bytes; their bounds categories fall from 76,276
  to 24,170, 15,456 to 5,192, and 94,872 to 27,760 bytes. Six alternating
  serialized SQLite public compiles remain within +0.40% median wall noise,
  while median allocation volume falls by about 176 KiB/op.
- Compact post-RA membership bits: ✅ skip and ARM64 pre-index membership now
  use reusable 64-bit sets with inlined O(1) queries instead of one byte per
  instruction. Word-boundary and clearing tests cover the representation;
  target realization tests retain exact behavior. SQLite, Lua, and regexmatch
  stay byte-identical while peak compiler-owned storage falls by another
  16,452, 3,414, and 17,360 bytes. Six alternating SQLite public compiles
  improve by 0.13% median, with about 20 KiB/op less allocation.
- Scheduler and SSA-exit observability: ✅ metrics schema 24 retains every
  bounded initial schedule candidate's realized post-allocation spill debt,
  physical copies, copy cycles, copy motion, fixed repairs, broken fusions,
  and loop-invariant motion alongside the retained winner. Retry-policy
  candidates remain separate. The canonical Markdown projection exposes the
  same candidate frontier per function, so target scheduling changes can be
  investigated against the actual backend debt rather than an IR-only pressure
  estimate. Schema 23 also attributes physical copies to ordinary edges, loop
  backedges, fixed-register repairs, and the segmented-liveness baseline and
  candidate. These are observational additions and do not change codegen.
- Scheduler cost-frontier calibration: ✅ metrics schema 25 adds a bounded,
  schedule-aware issue/latency estimate, profile-weighted selected-resource
  cost, factual selected-rule bytes, and an explicit pre-postRA Pareto frontier
  for every initial candidate. An exact native ARM64 pass over all 36 non-ISA
  application modules found 24,805 multi-candidate functions; 4,749 retained
  schedules (19.15%) sit outside that preliminary frontier, with a mean modeled
  cycle gap of 2.285%. This is intentionally advisory: 4,621 of those retained
  schedules (97.30%) produce post-RA rewrites, and 4,612 (97.12%) realize
  positive native-byte savings. A trial that let the preliminary frontier steer
  production lost the verifier-gated ARM64 byte-swap plan and failed its focused
  codegen test, so it was reverted. The next scheduler cutover must add realized
  post-RA opportunity and native-byte costs before discarding candidates. Ten
  alternating 10-compile `blake-as` pairs measured 0.99788x compile latency
  versus exact `4f16f000` with 6/10 wins, unchanged B/op/allocs, and byte-for-byte
  identical native output (SHA-256
  `694f9159e1a2999ec94ca7618237911e8ac6b24a10e0cd9f967bf6178933771a`).
- Candidate post-RA calibration: ✅ metrics schema 26 opt-in planning records
  verifier-gated rewrite count, conservatively planned instruction elisions,
  vector wrap-spill opportunities, and already-eliminated moves for both the
  initial and bounded allocator-retry schedule candidates. Normal compilation
  does not perform this extra analysis. A refreshed native ARM64 pass over all
  36 non-ISA application modules found 24,805 multi-candidate functions and
  reduced the schedules outside the measured candidate frontier only from
  4,749 to 4,743 (19.12%). Of those retained schedules, 4,618 (97.36%) have
  first-pass post-RA rewrites and 2,635 (55.56%) have planned instruction
  elisions. This proves that rewrite counts alone do not resolve the selection
  question; exact candidate native bytes remain required before the frontier
  can steer production. Ten alternating metrics-enabled `blake-as` pairs put
  schema 26 at 1.01591x schema 25 compile latency (4/10 wins), with unchanged
  peak compiler-owned storage and identical native output. A separate ten-pair,
  500 ms exact-head run measured `blake-as.hashN` at 0.99429x wazero latency;
  it is at parity but remains the narrowest measured ARM64 application margin.
- Exact schedule-realization oracle: ✅ metrics schema 27 adds an opt-in
  `-schedule=source|latency|pressure` diagnostic to `draglinemetrics`. It forces
  only functions that normally have genuine schedule alternatives, bypasses
  cached function artifacts, and then uses the normal allocator, bounded retry,
  segmented-liveness trial, post-RA verifier, and target finalizer. Production
  selection and metrics-disabled compilation are unchanged. All three forced
  modes compiled all 36 non-ISA application modules on native ARM64: 27,384
  RailMach functions plus the six measured structured giant-function cases,
  with no verifier or finalizer failures. Exact aggregate native images were
  95,318,812 bytes for automatic mixed selection, 95,349,368 for source-stable,
  95,372,744 for latency/fusion, and 95,413,084 for pressure. The mixed
  production policy is therefore already smallest across this corpus, while
  individual functions still differ: `blake-as` is 10,176 bytes automatically,
  10,256 source-stable, 10,192 latency/fusion, and 10,144 pressure. This closes
  the missing exact-byte measurement seam without using estimated encoder
  output; execution calibration can now compare ordinary schedule policies via
  the same final machine-code pipeline before any production cutover.
- ARM64 schedule execution calibration: ✅ ten alternating 500 ms rounds on
  `blake-as.hashN` reject replacing the current mixed policy with any one
  scheduler. Automatic selection measured 371.7 us by geometric mean versus
  373.3 us for latency/fusion, 374.2 us for pressure, 381.9 us for
  source-stable, and 394.9 us for wazero. Automatic selection is 5.9% faster
  than wazero and at least 0.4% faster than every forced alternative. This
  measurement used temporary benchmark binaries only; no module identity or
  forced schedule enters production.
- Target-opcode convergence: ✅ metrics schema 28 counts explicit target and
  residual generic machine instructions after every late target-form refinement.
  The complete native ARM64 pass over all 36 non-ISA application modules found
  6,502,101 target-selected instructions and zero generic instructions across
  27,384 RailMach functions; the six retained structured giant functions are
  reported separately. The count runs only during opt-in metrics compilation,
  so normal compilation does not rescan the final machine program. This closes
  the measured ordinary-opcode convergence gate for the application corpus;
  future work should add genuinely new forms or loop transformations rather
  than recreate a second selection switch.
- Machine bounds-proof convergence: ✅ every independently verified explicit
  bounds decision is now rebound to the selected `MemoryAccess` that consumes
  it before instruction selection. Dense machine-local certificate identities
  survive scheduling and post-RA rewriting, and the RailMach verifier rejects
  missing, duplicate, out-of-order, or unknown proof references while retaining
  exact address, offset, semantic width, encoded width, and trap-source checks.
  Both ARM64 and AMD64 finalizers now consume this machine descriptor rather
  than consulting the source-indexed emission side plan; signal/guard-page mode
  remains an explicit target policy, not a forged semantic certificate. The
  focused constant, masked-range, and masked-induction elision tests pass, and
  native ARM64 `blake-as` output remains byte-identical at 10,176 bytes with
  SHA-256 `a9cf0dbc1c8664fdf076e0a5738eff210b8d3f72d4039b249aca03413801fc3c`.
- Bounds-check observability and common-predecessor reuse: ✅ metrics schema 29
  now reports each RailMach function's selected memory-access count separately
  from source-proved and dominating-check reuse. An exact native ARM64 pass over
  the non-ISA manifest shows that existing exact-address reuse already removes
  18,594 checks in `regexmatch`, 13,595 in SQLite, and 4,020 in Lua. The shared
  finalizer policy now also retains facts while layout moves from a memory-free
  branch arm to its sibling: both arms must have the same sole predecessor, and
  a binary search over the sorted memory descriptors independently rejects a
  first arm that could have established a path-local fact. This removes another
  119 checks and 2,816 native bytes from SQLite, 10 checks and 176 bytes from
  Lua, and 50 checks and 960 bytes from Ruby; every runnable application image
  and esbuild remain byte-identical. Three serialized alternating measurements
  put SQLite compile wall 0.77% lower and Lua 0.27% higher; an exact paired Ruby
  run was 0.71% higher, while its 75,336,538-byte peak-live value was identical.
  Esbuild remained at 36,864,196 native bytes, 159.910 seconds, and 82,838,709
  peak-live bytes. A broader unsigned-remainder/shift range-proof experiment
  reached zero current corpus accesses and was reverted rather than adding an
  unmeasured source-analysis path.
- Loop-segmented liveness experiment: ❌ enabling the existing one-location
  segmented allocator for loop CFGs reduced an exact native ARM64 `esbuild`
  image from 37,019,108 to 37,016,084 bytes and weighted spill debt from
  900,723,276,819 to 900,722,529,892, but serialized full-module compile wall
  increased from 192.878 to 496.860 seconds (2.576x). Peak compiler-owned live
  storage also rose from 82,578,401 to 82,720,285 bytes. A 1,024-unit spill-debt
  opportunity gate retained small code-size wins in Lua, `regexmatch`, and
  `wasm3`, but still launched too many duplicate allocation trials in giant
  modules. The experiment was reverted: Stage 7B must avoid a second full
  allocator pass (or use a genuinely bounded module budget) before loop ranges
  can enter production.
- Loop-segmented liveness, indexed retry: ✅ the allocator now keeps a sparse
  occupant list for each physical register, so a segmented conflict query
  visits only values assigned to the candidate register instead of rescanning
  every active interval. This removes the giant-function complexity cliff while
  preserving exact sparse overlap and one physical assignment per value. An
  independent allocation verifier now requires every ordinary use and outgoing
  edge transfer to lie within the value's computed segments. Against exact
  commit `f50cbce2`, serialized native ARM64 `esbuild` compile wall moved from
  160.921 to 162.733 seconds (+1.13%), peak compiler-owned live storage from
  82,578,401 to 82,840,501 bytes (+0.32%), and native code from 37,018,548 to
  37,015,524 bytes (-3,024). A minimum 64-unit weighted-debt payoff prevents
  marginal loop retries: all runnable application modules remain byte-identical,
  while SQLite is 768 bytes smaller at +0.16% compile wall, `regexmatch` is 304
  bytes smaller, Lua 48 bytes smaller, `wasm3` 32 bytes smaller, and Ruby 144
  bytes smaller at +0.07% compile wall and +0.13% peak live storage. This clears
  Stage 7B without introducing split children or hole-boundary copies.
- True-split allocation gate: ⏸ exact post-Stage-7B `esbuild` metrics attribute
  900,415,994,365 of 900,722,529,936 remaining weighted spill-debt units
  (99.966%) to 27 giant functions on the deliberately bounded Stage-0
  FastMachine allocator. The ordinary quality allocator accounts for only
  306,535,571 units (0.034%). Arbitrary split children in GreedyP are therefore
  deferred: the next allocation experiment must first compare a bounded
  FastMachine improvement against giant-function compile wall and peak-live
  gates, because enhancing only the ordinary allocator cannot materially move
  the measured total.
- FastMachine call-survivor allocation: ✅ the bounded giant-function allocator
  now performs one interval-start sweep that promotes call-live spills into a
  register only when that register survives every crossed call, does not
  overlap an existing assignment, and repays any new callee-save cost. Exact
  direct-call clobber refinements are shared with GreedyP; the path performs no
  priority sort, eviction, regional search, schedule alternative, or retry.
  Against exact commit `7de66424`, serialized native ARM64 `esbuild` compilation
  improves from 162.733 to 160.396 seconds (-1.44%), peak compiler-owned live
  storage is flat at -0.002%, native code falls by 151,328 bytes, spill slots
  across its 27 FastMachine functions fall from 1,062 to 979, and their weighted
  spill debt falls 0.77%. Ruby improves from 96.659 to 96.246 seconds (-0.43%),
  peak live storage falls 0.20%, native code falls by 469,184 bytes, and spill
  slots fall from 1,209 to 1,094. SQLite is 39,216 bytes smaller with flat
  compile wall, `regexmatch` is 3,920 bytes smaller, and Lua and `wasm3` remain
  byte-identical. This is the measured FastMachine policy requested by Phase 9,
  while true split children remain deferred until a distinct residual debt can
  justify their transfer machinery.
- External compiler and execution harness: 🚧 the current ARM64 report covers
  all 53 admitted compile modules and all 216 runnable exports. Across the 17
  MVP ISA modules, Dragline is 0.234x Railshot and 0.293x Cranelift execution
  latency while emitting 0.531x and 0.597x their native code; compile wall is
  1.058x Railshot and 0.917x Cranelift, with 0.585x Cranelift RSS. Across the 36
  applications. A subsequent three-round, 50 ms diagnostic pass after separating
  operand-stack/local register eligibility, caching immutable memory length, and
  reusing FP scratch across adjacent binary operations reduced general-code
  execution from 3.048x to 2.374x Railshot. This is still a failed product gate;
  the longer paired Cranelift run has not yet been refreshed. Ruby compile
  latency remains a 19.216-second outlier in the original report. The configured
  LLVM command remains unavailable, so the LLVM gate is unmeasured.
- Current ARM64 non-ISA execution: ✅ a three-round, 100 ms paired run on Apple
  M4 Max has all 36 runnable exports faster than wazero. The module-equal paired
  median geometric mean is 0.555x wazero latency, or 44.5% faster. The narrowest
  rows are `float.run` at 3.2% faster and `arith.run` at 3.9% faster. Restoring
  the quality allocator threshold after a measured FastMachine experiment was
  required: the 1,024-instruction threshold made the two BLAKE rows 36–67%
  slower and was rejected before commit.
- Current ARM64 execution floor refresh: ✅ three 300 ms samples per engine on
  Apple M4 Max keep every runnable non-ISA export faster than wazero. The
  narrowest row is now `arith.run`: median Dragline latency is 1,338 ns versus
  1,363 ns for wazero, or 1.8% faster. Its hot loop already consists of the
  minimal expected `SXTW`, `MADD`, shifted `EOR`, decrement, and conditional
  branch sequence, so no corpus-specific rewrite is justified. The next
  execution work should target a reusable operation family with measured
  headroom rather than forcing this already-minimal kernel toward an arbitrary
  percentage.
- ARM64 shifted-register logic: ✅ the post-RA verifier recognizes adjacent,
  single-use 32/64-bit constant shifts feeding matching AND, OR, or XOR forms
  and requires the unshifted base to remain live at the logical consumer. The
  finalizer emits one shifted-register `AND`/`ORR`/`EOR` only when the base and
  result are register-resident; all near misses retain ordinary lowering. The
  initial `i64.shr_u`/XOR slice reduces `arith.run` from 188 to 184 native bytes
  and its loop from six to five instructions. Sixteen forward/reverse paired
  one-second samples against exact commit `74ef2669` measured 0.994x median
  latency, about 0.65% faster. An exact audit of all 30 runnable modules found
  no native-size changes outside that reduction. Higher-value multi-instruction
  rewrites are planned first, preventing local logical folds from displacing
  byte-widen or byte-swap sequences.
- ARM64 integer multiply/subtract selection: ✅ the existing machine-SSA
  multiply/add contraction now also selects `MSUB` for a single-use integer
  product on the right side of Wasm subtraction. The target opcode retains
  subtraction semantics, the left-product near miss remains unfused, and native
  encoding tests cover the selected 64-bit form. An exact pre/post native-image
  audit across every manifest module except the six giant compile-only modules
  changed only `json-as` (-80 bytes), `json-as-simd` (-80 bytes), and
  `utf-as-simd` (-16 bytes). Six alternating 300 ms execution pairs ranged from
  0.989x to 1.008x baseline across their six exports; a longer ten-pair check of
  the narrowest row measured 1.0069x. This remains inside the migration floor
  but is recorded explicitly rather than claiming a broad execution win.

## Completion rule

Dragline is not complete until every applicable row above has implementation and
verification evidence, every required feature is admitted with strict semantics,
and the master plan's compatibility/native performance and memory gates pass on
the supported AMD64 and ARM64 targets. Corpus-specific wins alone do not satisfy
the plan.
