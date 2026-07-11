# Railshot AMD64 → ARM64 backend status

This is an implementation-status matrix, not a feature-support claim. It compares
the optimizations and compiler mechanisms present in `railshot/amd64` with the
current native implementation in `railshot/arm64`.

Status meanings:

- **Implemented** — an ARM64 implementation exists and has direct ARM64 execution,
  smoke, or focused codegen coverage.
- **Implemented, lower confidence** — the mechanism is present, but does not yet
  have the AMD64-equivalent focused regression net or full corpus acceptance.
- **Partial** — only part of the AMD64 surface is implemented, or a material
  architecture-specific gap remains.
- **Not applicable** — AMD64's exact technique cannot carry over; the ARM64
  replacement is listed instead.

The matrix deliberately distinguishes semantic completeness from performance
parity. ARM64 remains acceptance work, but the former json-as nontermination and
SQLite recursive-CTE miscompile are now covered by passing committed-golden tests
on Darwin/ARM64.

## WARP backend optimization inventory

The preceding matrix is organized around Wago's AMD64 implementation. This
inventory is the complementary source-oriented view: mechanisms present in
WARP's `x86_64` and `aarch64` backends, and the status of their Wago
counterparts. It groups closely related instruction templates rather than
claiming that every opcode spelling is a separate optimization.

| WARP mechanism | WARP implementation | Wago AMD64 | Wago ARM64 |
|---|---|---|---|
| Single-pass validation/codegen with a symbolic operand stack | `Frontend.cpp`; backend `emitDeferredAction` | **Implemented** | **Implemented** |
| Valent-block storage model: flush operands at Wasm block boundaries; keep locals in register/stack storage classes | `Frontend.cpp`, `Common.cpp`, backend storage helpers | **Implemented** | **Implemented**; ARM64 uses `localstate.go`'s lazy STACK_REG states for call-makers. |
| Whole-register-file allocation and spill-on-demand | `*_cc.*`, `reqScratchReg`, `spillAllVariables` | **Implemented** | **Implemented**; separate GP/NEON pools and AArch64-specific scratch exclusions. |
| Parallel-copy resolution for calls, branches, and entry adapters | `RegisterCopyResolver` and call dispatch | **Implemented** | **Implemented** for integer and mixed GP/FP register-ABI staging. |
| Direct internal register ABI plus wrapper/host adapters | `execDirectFncCall`, `emitFunctionEntryPoint`, call-dispatch files | **Implemented** | **Implemented**; loop-site tiny leaves avoid regressive inlining. |
| Indirect-call bounds, null, and signature checks | `execIndirectWasmCall` | **Implemented** | **Implemented**; local int-only funcrefs use guarded internal-entry dispatch, while host/cross-instance entries use the wrapper path. |
| Shared immutable module context in pinned registers | backend entry/call lowering | **Implemented** | **Implemented**: `linMemReg`, module-global pins, and explicit-mode memory-size pin. |
| Hot local and global register residency | WARP variable storage / register cache | **Implemented** | **Implemented, guarded**: loop-weighted local/global hints; call-making memory functions retain conservative pressure gates. |
| Constant propagation, constant folding, and same-operand identities | deferred-action simplification | **Implemented** | **Implemented** |
| Deferred load/address retention until a consumer requires materialization | WARP storage/deferred actions | **Implemented** | **Partial**: ARM64 retains `stMemRef` but cannot fold a memory operand into general ALU instructions. |
| Compare/branch fusion and condition inversion | `emitBranch`, `emitCmpResult` | **Implemented** | **Implemented** with NZCV, `B.cond`, `CBZ`/`CBNZ`, and `CSEL` substitutions. |
| Branch target patching and short structured-control lowering | branch patch objects and backend branch emitters | **Implemented** | **Implemented** |
| Wasm `select` lowered to a conditional move/select | `emitSelect` | **Implemented** | **Implemented** with `CSEL`/FP equivalents; performance remains a measured queue item. |
| Integer ALU instruction selection, including variable shifts/rotates | deferred-action templates | **Implemented** | **Implemented**; ARM64 sinks local-set ALU results with three-operand instructions (`ADD dst,left,right`) and has in-place shift/rotate sinking. |
| Constant-divisor magic-number division | backend deferred action templates | **Implemented** | **Implemented** with ARM64 execution tests. |
| Native divide/remainder lowering with trap checks | backend divide lowering | **Implemented** | **Implemented** using `SDIV`/`UDIV` and `MSUB`; active divide checks are retained where required. |
| Scalar FP arithmetic, conversions, rounding, min/max, and bit operations | deferred-action FP templates | **Implemented** | **Implemented**; ARM64 min/max preserves Wasm NaN and signed-zero semantics. |
| Linear-memory base/index/offset addressing and folded immediate offsets | `emitMemoryLoadStoreWithImmOffset`, load/store emitters | **Implemented** | **Implemented** for AArch64 load/store addressing; no general ALU-memory operand equivalent. |
| Explicit linear-memory bounds checks and passive/guard-page mode | `emitLinMemBoundsCheck`, config/runtime memory manager | **Implemented** | **Implemented**; Darwin/ARM64 uses signal-backed guard pages. |
| Bounds-check facts and loop prechecks | Wago extension beyond direct WARP source parity | **Implemented** | **Implemented**; ARM64 has the same A/B controls. |
| Bulk-memory specializations (`copy`, `fill`, `init`) | WARP copy/fill loops and builtins | **Implemented** | **Implemented**; ARM64 uses explicit loops and constant-size helpers where applicable. |
| Fast function-entry adapters and stack-fence checks | `emitFunctionEntryPoint`, stack checks | **Implemented** | **Implemented**; small call-free leaves may elide the fence. |
| Builtin/runtime helper calls | `execBuiltinFncCall`, call-dispatch | **Implemented** | **Implemented** through native helpers and no-cgo host-call trampolines. |
| SIMD/vector instruction selection | WARP backend vector support where enabled | **Implemented for Wago's documented x86 baseline** | **Partial**: broad NEON lowering exists, but not yet ARM64 feature/corpus parity. |
| ISA-specific dense instruction encodings | x86 REX/VEX and AArch64 assembler templates | **Implemented** | **Implemented**; ARM64 uses its own encoder rather than mechanically porting x86 templates. |

### Deliberate non-ports and Wago extensions

- WARP's x86 general-ALU memory operands and x86 condition-code idioms do not
  have an AArch64 equivalent. ARM64 uses a register load plus orthogonal ALU,
  NZCV flags, and conditional-select instructions instead.
- Wago's bounds facts, loop prechecks, explain/stats surface, and guarded local
  table-entry dispatch are Wago-specific optimization layers, not claims that
  WARP exposes the same switch or implementation shape.
- WARP's backend source is the reference for compiler structure and local
  lowering. Its runtime configuration (passive bounds, eager allocation,
  interruption, platform signal mechanics) must not be conflated with a
  per-instruction codegen optimization.

| AMD64 feature / optimization | AMD64 implementation | ARM64 status | ARM64 implementation or limitation |
|---|---|---|---|
| Single-pass railshot compiler and deferred operand trees | `compile.go`, `driver.go`, `emit.go`, `stack.go` | **Implemented** | Parallel `fn` compiler, driver, emitter, and symbolic stack are present. `compilesmoke_arm64_test.go` and `portexec_arm64_test.go` exercise the native path. |
| Valent-block register allocation, spills, and parallel register copies | `regalloc.go`, `regmask.go`, `regcopy.go`, `allocation_test.go` | **Implemented, lower confidence** | ARM64 has the same allocator family and separate integer/NEON register classes. It benefits from orthogonal ALU instructions and two scratch GPRs; its focused allocator regression coverage is smaller. |
| Register-resident block/if results (register merge) | `compile.go`, `regmerge_test.go` | **Implemented, lower confidence** | `WAGO_REG_MERGE` is enabled by default in ARM64 `compile.go`; the old slot path remains an A/B oracle. No ARM64-specific reg-merge test file exists yet. |
| Constant folding and same-operand identities | `fold.go`, `constfold_test.go`, `eqzfold_test.go` | **Implemented, lower confidence** | ARM64 has integer and comparison folding in `fold.go`, plus NZCV compare lowering. It lacks the AMD64-sized focused constant/`eqz` regression suite. |
| Deferred-tree condensation and instruction fusion | `emit.go`, `fuse.go` | **Implemented, lower confidence** | ARM64 uses `condenseToFlags` and has `WAGO_NO_STFLAGS` as an A/B kill switch. It replaces EFLAGS consumers with NZCV `CMP`/`CSET`/conditional-branch forms. |
| `eqz` / compare → branch or select fusion | AMD64 flags, `setcc`, `Jcc`, `cmov` | **Implemented** | ARM64 uses `CMP` + `CSET`, `B.cond`, and `CSEL`, with `CBZ`/`CBNZ` where suitable. This is an ISA replacement, not a direct port of EFLAGS logic. |
| Integer multiply, divide/remainder, shifts, rotates | Fixed-register `RAX`/`RDX` divide and `RCX` variable shifts | **Implemented** | ARM64 uses orthogonal `MUL`, `SDIV`/`UDIV` + `MSUB`, and variable-shift instructions. This eliminates AMD64 fixed-register staging; `magicdiv_arm64_test.go` covers constant-divisor lowering. |
| Magic-number constant division | `magicdiv.go`, exhaustive tests | **Implemented** | `tryDivByConst` is present and ARM64 has focused execution coverage. Maintain parity testing as the encoder and signed corner cases evolve. |
| Bounds facts / repeated-check elision | `memory.go`, `bounds_facts_test.go` | **Implemented, lower confidence** | ARM64 tracks certificates in `memory.go`; `WAGO_NO_BOUNDS_FACTS=1` is the A/B and kill switch. ARM64 stats tests cover bounds counters, but not the AMD64-focused facts suite. |
| Loop bounds-precheck hoisting | `boundshoist.go`, `boundshoist_test.go` | **Implemented, lower confidence** | ARM64 has the same precheck pass, enabled by default with `WAGO_LOOP_PRECHECK` and `WAGO_LP_MINCHECKS`. Add ARM64 execution tests for slow-trap ordering and benefit gating. |
| Guard-page bounds elision | Guard-page mode omits explicit checks | **Implemented** | On supported ARM64 targets, signal-backed guard pages provide the same no-explicit-check fast path. This is runtime/platform work, not an ISA-codegen optimization. |
| Memory operands folded into arithmetic / comparisons | x86 addressing modes preserve `stMemRef` through condensation | **Not applicable** | AArch64 load/store instructions cannot be general ALU memory operands. ARM64 materializes a load into a register before the operation, so this AMD64 code-size/performance win has no direct equivalent. |
| Bulk-memory fast paths | `memory.copy`/`fill` helpers and small-size paths | **Implemented** | ARM64 implements `memory.copy`, `memory.fill`, `memory.init`, and their constant-size helpers; `memexec_arm64_test.go` includes large bulk-memory execution coverage. |
| Hot local pinning | Loop-weighted local hints and pinned registers | **Partial** | ARM64 pins hot locals for call-free memory functions as well as compute-only functions, avoiding frame traffic in tight load/store loops. Memory-touching call-makers remain conservatively gated by register pressure; the SQLite regression now passes. |
| Module-global pinning | Aggregate global scores and pinned globals | **Implemented, lower confidence** | ARM64 has module-global selection and `WAGO_PIN_GLOBAL_K`; preserve A/B comparison with `WAGO_DEBUG_MODGLOBALS` while corpus issues remain. |
| Straight-line leaf inlining | Candidate analysis and bytecode splicing in `inline.go` | **Implemented, lower confidence** | ARM64 has matching candidate analysis and transform, enabled by default through `WAGO_INLINE`. It needs ARM64-specific execution and negative-case coverage comparable to `amd64/inline_test.go`. |
| Direct / indirect calls and register ABI | Register ABI, parallel moves, table signature checks | **Implemented** | ARM64 has native call lowering and a toggleable register ABI (`WAGO_ARM64_NOREGABI=1` for A/B). `callexec_arm64_test.go` exercises calls. |
| Scalar floating-point lowering | SSE/AVX scalar FP, conversions, rounding, traps | **Implemented** | ARM64 has native scalar FP lowering and focused ARM64 execution coverage in `floatexec_arm64_test.go`. Float comparison lowering uses NZCV, not the AMD64 parity flag. |
| SIMD / `v128` | Complete documented SSSE3/SSE4.1 + VEX.128 baseline, including relaxed SIMD | **Partial** | ARM64 has broad NEON lowering and direct tests for integer/vector FP operations, lane operations, shifts, compares, narrowing, extmul, dot product, shuffle/swizzle, reductions, and min/max. It is not yet declared feature-complete: some operations require synthesized NEON sequences and ARM64 does not yet have the AMD64 full-corpus acceptance claim. |
| SIMD movemask / reductions | `PMOVMSKB`-style efficient extraction | **Partial** | ARM64 implements tested bitmask/reduction behavior, but NEON lacks a single `PMOVMSKB` equivalent; it must synthesize the result and is a performance-sensitive gap. |
| Codegen statistics and explain mode | `CodegenStats`, `WAGO_EXPLAIN`, optimization counters | **Implemented** | ARM64 has the same opt-in statistics surface and focused `stats_test.go` coverage for peepholes, stores, bounds, and register allocation. |

## Evidence and maintenance rules

- The AMD64 baseline is the implementation in
  `src/core/compiler/backend/railshot/amd64`, not an abstract ISA wishlist.
- ARM64 direct coverage is in `*_arm64_test.go` files beside the backend. In
  particular, native smoke, integer/float, memory/bulk-memory, calls,
  constant-division, stats, and substantial NEON execution tests exist.
- `src/core/compiler/backend/railshot/arm64/_port/ENCODER_TODO.md` is historical
  porting inventory, not a live status source. Check the encoder and tests before
  marking an item incomplete solely because it appears there.
- A row marked **Implemented** should retain an ARM64 execution test. A row marked
  **Implemented, lower confidence** should gain a focused ARM64 test before being
  promoted.
- Do not mark ARM64 as performance-parity until native CI and the relevant corpus
  remain green with measured execution and footprint results.

## Highest-value follow-ups

1. Add ARM64-focused tests for bounds facts, loop-precheck trap ordering,
   reg-merge, constant/`eqz` folding, inlining, and pinning fallbacks.
2. Expand architecture-neutral Release 2 tests currently hidden behind legacy
   linux/amd64 build tags, preserving native Linux and Darwin ARM64 gates.
3. Measure synthesized NEON paths (especially movemask, shuffles, and scalar-count
   shifts) against the AMD64 baseline and document numbers before calling them
   optimized.
