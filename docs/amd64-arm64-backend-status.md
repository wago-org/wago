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
parity. ARM64 is opt-in/acceptance work: `ROADMAP.md` still records a json-as
nontermination and a SQLite recursive-CTE miscompile on Darwin/ARM64.

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
| Hot local pinning | Loop-weighted local hints and pinned registers | **Partial** | ARM64 pins hot locals for call-free memory functions as well as compute-only functions, avoiding frame traffic in tight load/store loops. Memory-touching call-makers remain on stack locals because SQLite still exposes a pinned-local/control convergence hazard. |
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
- Do not mark ARM64 as performance-parity or corpus-complete until the known
  Darwin/ARM64 acceptance failures in `ROADMAP.md` are resolved and the relevant
  corpus is rerun.

## Highest-value follow-ups

1. Add ARM64-focused tests for bounds facts, loop-precheck trap ordering,
   reg-merge, constant/`eqz` folding, inlining, and pinning fallbacks.
2. Resolve the json-as and SQLite Darwin/ARM64 failures, then rerun the guarded,
   explicit, and wazero corpus matrix.
3. Measure synthesized NEON paths (especially movemask, shuffles, and scalar-count
   shifts) against the AMD64 baseline and document numbers before calling them
   optimized.
