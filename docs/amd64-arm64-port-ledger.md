# AMD64 ↔ ARM64 port ledger

This is the actionable bidirectional parity ledger for the railshot backends.
It complements `docs/amd64-arm64-backend-status.md`: that document records the
current implementation state, while this one identifies concrete work to port
between architectures.

Treat semantic support, optimization parity, and test confidence separately.
Equivalent behavior and performance matter more than mechanically matching
instruction sequences.

## ARM64 → AMD64

| Priority | ARM64 capability | AMD64 work |
|---:|---|---|
| 1 | Prepared-call control refresh tests | Already covered on AMD64: `cross_instance_test.go`'s `TestCrossInstanceCallNoArgs` prepares `inA.trap`, invokes it after a cross-instance call, and asserts it still traps `Unreachable` in A's context — the trap-cell/fence refresh regression. (linux&&amd64; would widen to arm64 once `cross_instance_test.go`'s helper web is untangled, per the arch-neutral-tests item.) |

**ISA-blocked (no clean x86 equivalent):** *Leaf scratch-register pinning.* ARM64
dedicates fixed-role-free scratch registers (X12–X14) that a call-free leaf can
repurpose for pins; AMD64's scratch registers (RAX/RCX/RDX for mul/div/shift, R8
for bulk memory) all carry hard fixed roles, and RSI/RDI are consumed by the trap
path, interrupt poll, and `rep movs`. There is no register AMD64 can free up only
in a leaf, so this optimization is specific to AArch64's register file.

**Landed:**

- Three-operand / local-result sinking, incl. unary and `local.tee` (were
  priorities 1–2). AMD64 already sank a binop self-update into the local's pinned
  register; this extends the in-place `skipFrom` to shifts, unary (clz/ctz/popcnt),
  width conversions, and the `local.tee` form, so `local.set/tee $x (op (local.get
  $x) …)` computes straight into x's register with no pre-copy. The genuinely
  three-operand distinct case (`a = b + c`) has no x86 equivalent — its selective
  destructive form (dest aliases an operand) was already handled. Gated by
  `WAGO_AMD64_NOUNARYSINK` / `WAGO_AMD64_NOTEESINK`; covered by
  `amd64/localsink_test.go` (exec across shift/unary/convert/tee + kill-switch
  equivalence).
- Specialized 33–64-byte constant bulk memory (was priority 6). Constant
  `memory.copy`/`memory.fill` now unroll through 64 bytes: copy uses an SSE
  16-byte load-all-then-store-all (≤4 XMM registers, overlap-safe) for 33–64,
  fill extends its 8-byte pattern stores (one pattern register, no pressure).
  Both skip the dynamic dispatch (runtime length check + `rep movs/stos`
  startup) for these fixed sizes. Covered by `amd64/bulkmem_const_test.go`
  (exact-byte coverage, neighbour non-disturbance, and forward-overlap memmove
  semantics).
- Entry-argument pinning, x86-adapted (was priority 1). A call-free reg-ABI leaf
  never clobbers its caller-saved argument registers, so AMD64 now adds the *free*
  incoming-arg registers (R9–R11 past the parameter count) to the pin pool for
  5–7-param leaves — closing the gap where AMD64 previously added them only for
  ≤4-param functions. Only R9–R11 qualify: RAX/RCX/RDX carry fixed x86 roles
  (mul/div/shift) and R8 doubles as bulk-memory scratch. The full arm64 form
  (every param stays in its arrival register via a parallel-move prologue) does
  not port — x86's fixed-role arg registers and sequential arg-homing make the
  free-register subset the profitable equivalent. Gated by
  `WAGO_AMD64_NO_ENTRY_ARG_PINS`, stat `entry-arg-local-pin`, covered by
  `amd64/entryargpin_test.go`.
- Call-free hint propagation through inlining (was priority 3–4). When every
  direct call in a function is spliced away by inlining — and inline targets are
  call-free leaves (`inlineClass`), so they add no call of their own — the caller
  makes no native call after inlining. AMD64 now recognizes this
  (`allCallsWillInline`) and plans the function as call-free: aggressive pins
  (including the entry-arg registers) and the STACK_REG spill model off, instead
  of the conservative call-making model. Gated by `WAGO_AMD64_NO_INLINE_CALLFREE`,
  stat `all-calls-inlined`; covered by `amd64/inline_callfree_test.go` (hint
  fires + is gated) and `src/wago/inline_callfree_test.go` (cross-arch execution).
- Deeper FP local pinning, x86-adapted (was priority 1). A call-free function now
  extends the float pin pool past the base XMM12-15 into XMM8-10 (never XMM11 =
  `mergeFReg`), holding more hot float locals when no call can clobber those
  operand registers. Deliberately shallow and call-free-only: amd64 has 16 XMM
  (vs arm64's 32 V), so each extra pin shrinks the float operand pool — matching
  arm64's residency is impossible, but the call-free bump is safe. Gated by
  `WAGO_AMD64_NO_EXTFPPINS`, stat `deep-fp-local-pin`; covered by
  `amd64/deepfppin_test.go` (fires + gated) and `src/wago/deepfppin_test.go`
  (cross-arch execution).
- Small-frame elision (was priority 1–4). A register-homed call-free
  single-result reg-ABI leaf never touches its frame — the register-returning
  internal entry doesn't use the results-ptr header, all locals live permanently
  in registers, and there are no operand spills. AMD64 now drops such a frame to
  size 0 (being call-free, the 16-byte-alignment it provided is moot), so
  `sub/add rsp` adjust nothing and the leaf uses no stack. The x86 single-
  instruction `sub rsp,imm32` was already the small-frame *adjust* (no MOVZ+MOVK
  to replace); this adds the *elision*. Gated by `WAGO_AMD64_NO_FRAME_ELIDE`,
  stat `frame-adjust-elide`; covered by `amd64/frameelide_test.go`.

- Branch folding pass (was priority 1). amd64 now folds the empty-edge
  `Jcc over; JMP target; over:` br_if idiom (loop back-edges, block exits) into a
  single inverted `Jcc target` + a size-preserving 5-byte NOP, as a post-assembly
  pass over recorded sites (`amd64/peephole.go`) — the same rewrite arm64 applies
  to its `B.cond`/`B` pair. Gated by `WAGO_AMD64_NOBRFOLD`, stat `br-pair-fold`,
  covered by `amd64/branchfold_test.go` (exec, fires, kill-switch equivalence).

- Proven monomorphic `call_indirect` + immutable local-table specialization (were
  priorities 1–2). With no function imports, a single private (non-exported,
  non-imported) never-mutated table, amd64 now skips the home/tag fork, elides the
  type check on a uniformly-typed table, and direct-calls a monomorphic table —
  preserving the OOB/null/signature checks. The shared monomorphic analysis was
  also corrected to fold the table's `Init` default target (an active element
  overriding one slot no longer makes an initialized table look monomorphic),
  fixing a latent arm64 bug. `amd64/immutable_table_test.go` (compile-time
  specialization stats) and `src/wago/monomorphic_call_indirect_test.go`
  (cross-arch execution, incl. the `Init`-override regression).
- Linear store-to-load forwarding (was priority 3, then 1). amd64 now keeps an
  owned full-width store value in a register across a bounded window (local.get
  leaves + the exact matching load) and forwards it to an immediately re-read
  linear-memory slot instead of reloading — via reader look-ahead
  (`nextLoadMatchesStore`) with per-opcode invalidation (`prepareStoreForward`),
  gated by `WAGO_AMD64_NOMEMFWD`. New backend tests `amd64/storefwd_test.go` and
  `arm64/storefwd_arm64_test.go` (the arm64 implementation previously had no
  focused test) cover execution, that the peephole fires, and kill-switch
  equivalence.

- Native cooperative cancellation polls (was priority 1). The amd64 backend emits
  function-entry and loop-header trap-cell polls under `Interruptible`, and the
  context-aware `Call` arms the cancellation watcher on amd64 as well as arm64.
  Covered by `src/wago/cancellation_test.go`.
- Two-integer-result register ABI (was priority 1). amd64 returns a two-integer
  result in `RAX:RDX` (mirroring arm64's `X0/X1`) across the register-ABI
  definition, direct call, and tagged indirect fast path; the shared
  `funcSigIntRegABI` now tags two-int funcrefs on both backends. Covered by
  `src/wago/regabi_two_result_test.go`.
- Mixed GP/FP parallel argument staging (was priority 2). amd64's mixed call now
  stages GP and FP arguments as independent parallel moves instead of flushing
  every argument through a canonical slot, and correctly captures a two-integer
  result — which also fixed a latent arm64 mixed-call bug that dropped the second
  result of a float-param/`(i64,i64)`-result call. Covered by
  `src/wago/regabi_mixed_call_test.go`.

See the corresponding entries under "Already broadly equivalent".

## AMD64 → ARM64

| Priority | AMD64 capability | ARM64 work |
|---:|---|---|
| 1 | Module-global pinning tests | Arch-specific: `pickModuleGlobals` demands a higher first-pin threshold on ARM64 (a pin displaces a hot local), so ARM64 keeps its own `TestModuleGlobalPinRequiresABIWideReuse`; the score-scan is shared but the selection tests do not port verbatim. |
| 2 | Golden disassembly harness | Low-value/brittle and deprioritized: amd64's goldens are exact `objdump` Intel-syntax strings; an ARM64 equivalent needs AArch64 `objdump` on the test host and re-baselines on every instruction-selection change, while behaviour is already covered by the exec suites + corpus differential. Add only if a specific instruction-selection regression needs locking down. |
| 4 | Stack and frame-layout tests | Operand-stack arena sizing ported (`arm64/stack_arm64_test.go`); the remaining register-layout/pinned-local coverage is arch-specific. |
| 5 | SIMD benchmark suite | Add equivalent ARM64 backend-local microbenchmarks after the SIMD branch lands. |
| 6 | Full SIMD acceptance claim | **Achieved on ARM64.** With the NEON perf branch merged (`#235`), the official SIMD spec suite runs fully green on darwin/arm64: **470 modules / 24 325 assertions, 0 failed, 0 skipped, 0 gaps** (`WAGO_SPECTEST_DIR=tests/spec WAGO_SPEC_VERSION=simd go test ./src/wago -run TestSpecSuiteExec`). This covers exactly the previously-flagged gaps — bitmask/movemask, `all_true`/`any_true` reductions, `dot`, `extadd_pairwise`, shuffles, lane load/store, and all conversions. Remaining: confirm the identical assertion counts on linux/amd64 (hub) so the differential is locked in CI — amd64 is the mature reference side, so this is a confirmation run, not new work. |
| 7 | Architecture-neutral spec tests | In progress. Shared helpers consolidated into broadly-tagged files (`exec_shared_test.go` = `runv`/`run1`/`runImports`; `testdata_shared_test.go` = `testdata`/`memprogWasm`; `dataseg_shared_test.go` = `passiveDataModule`), and `global_test.go` (40 tests), `defer_bounds_checks_test.go`, `memory_access_test.go`, `bulk_memory_test.go` widened to arm64 (green on both). Remaining files (`segment_state`, `table_ops`, `trap`, `cross_instance`, …) follow the same mechanical per-file dedup — repeat as coverage is wanted. |

**Landed** (regression suites now on ARM64):

- Allocator-pressure net (was priority 1). `arm64/allocation_arm64_test.go` +
  a general wrapper executor (`arm64/exec_helpers_arm64_test.go`): deep
  deferred-tree / register-pressure shift chains, deferred-load (`stMemRef`)
  spill correctness, `br_table` jump-table dispatch on a computed index, and the
  operand-stack arena overflow invariant. The AMD64 `UnpinnedRetry` assertion is
  deliberately not ported — ARM64's larger orthogonal file + deferred-tree cap
  never need the pinning-off retry for those shapes, so compile-success at
  extreme depth is the regression instead.
- Constant-folding suite (was priority 3). `arm64/constfold_arm64_test.go`:
  `foldCompare`/`foldUnaryConst` unit tables, folded-body execution, same-local
  compare identities, and the `const-fold`/`same-operand`/`compare-setcc`
  peephole assertions.
- `eqz`/flags fusion (was priority 4) — expanded the pre-existing
  `eqzfold_arm64_test.go` with the `br_if` fused-consumer path and the
  nested-`eqz` fold-count assertion (`arm64/eqzfold_brif_arm64_test.go`).
- Backend self-update checks (was priority 3). `arm64/selfupdate_arm64_test.go`
  ports the in-place self-update aliasing cases (`x = 100 - x` with the self-local
  as a non-commutative RHS; `x = x - x`), confirming ARM64's condense handles the
  operand-aliases-dest sink correctly.
- Inlining positive/negative suite (was priority 1). `arm64/inline_exec_arm64_test.go`
  extends the two pre-existing ARM64 inline tests to the full amd64 set: memory-
  touching leaves, if/else and early-`return` control flow, bare return, nested
  splices, declared-local zeroing, the loop-callee opt-in, and eligibility
  (oversized/recursive/unused). Callers run through `runArm64u` (a call-free entry
  after splicing, which also wires linear memory for the memory case).
- Extension-elimination coverage (was priority 5). `arm64/extelim_arm64_test.go`:
  redundant `i64.extend_i32_u` of a clean 32-bit result is elided (`ext-elim`),
  and an `extend`→`wrap` round trip collapses (`extend-wrap-elim`), each checked
  for both correct execution and that the peephole fired.

Register-merge (was priority 2), bounds-facts (was priority 5), and loop-precheck
(was priority 6) already had focused ARM64 coverage
(`regmerge_arm64_test.go`, `bounds_facts_arm64_test.go`,
`boundshoist_arm64_test.go`) and were removed from the work list.

## Already broadly equivalent

Both backends currently have:

- single-pass validation and code generation;
- a symbolic operand stack and deferred values;
- GP/FP register allocation and spilling;
- register merging;
- direct and indirect calls, including the immutable private-table `call_indirect`
  specializations (home/tag elision, uniform-type-check elision, and monomorphic
  direct call);
- internal register and wrapper ABIs, including one- and two-integer register
  results and mixed GP/FP parallel argument staging;
- host calls and cross-instance execution;
- native cooperative cancellation polls at function entries and loop headers,
  with identical `TrapInterrupted` → `context` error semantics;
- scalar integer and floating-point lowering;
- reference globals and indexed tables;
- bulk memory, including fixed-size constant `copy`/`fill` unrolling through 64
  bytes;
- linear store-to-load forwarding of an owned full-width value into an
  immediately re-read slot;
- explicit and guard-page bounds modes;
- bounds facts and loop prechecks;
- constant division and constant folding;
- compare/branch fusion and empty-edge double-branch folding;
- hot local and module-global pinning;
- straight-line leaf inlining;
- codegen statistics and explain mode; and
- core SIMD lowering, with the official SIMD spec suite fully green on ARM64
  (470 modules / 24 325 assertions, 0 skipped) as well as AMD64; performance is
  tracked separately via the NEON perf work (branch merged in `#235`).

## ISA-specific equivalents

Do not port these mechanisms literally:

| AMD64/ARM64 mechanism | Equivalent approach |
|---|---|
| AMD64 folded ALU memory operands | ARM64 must load into a register before a general ALU operation. |
| AMD64 `EFLAGS`, `SETcc`, and `CMOV` | ARM64 uses `NZCV`, `CSET`, and `CSEL`. |
| AMD64 `PMOVMSKB` | ARM64 needs a synthesized NEON movemask. |
| ARM64 three-operand ALU sinking | AMD64 needs selective destructive/two-operand forms. |
| ARM64 `UXTW` addressing | AMD64 uses scaled-index/addressing modes. |
| NEON bulk-memory loops | AMD64 should use SSE/AVX equivalents appropriate for the documented CPU baseline. |

## Recommended sequence

1. ~~Port ARM64 cancellation to AMD64.~~ **Done** — entry/loop-header polls +
   amd64 watcher, `src/wago/cancellation_test.go`.
2. ~~Port ARM64 call ABI improvements to AMD64.~~ **Done** — two-integer
   `RAX:RDX` register results and mixed GP/FP parallel argument staging;
   `src/wago/regabi_two_result_test.go`, `src/wago/regabi_mixed_call_test.go`.
3. ~~Port AMD64 allocator, control, and bounds regression suites to ARM64.~~
   **Done** — allocator-pressure and constant-folding suites ported, `eqz`/flags
   coverage expanded; register-merge/bounds-facts/loop-precheck already had ARM64
   coverage. `arm64/{allocation,constfold,eqzfold_brif,exec_helpers}_arm64_test.go`.
4. ~~Port ARM64 monomorphic indirect calls and store forwarding to AMD64.~~
   **Done** — monomorphic/immutable-table `call_indirect` and linear
   store-to-load forwarding both landed (`amd64/immutable_table_test.go`,
   `amd64/storefwd_test.go`, `src/wago/monomorphic_call_indirect_test.go`).
5. ~~Close remaining SIMD parity after the dedicated SIMD branch lands.~~
   **Done** — NEON perf branch merged (`#235`); official SIMD spec suite fully
   green on ARM64 (470 modules / 24 325 assertions, 0 skipped/failed). Confirm the
   matching amd64 counts on hub to wire the differential into CI.
6. Add shared golden-disassembly and cross-architecture differential gates.

## WebAssembly 2.0 feature parity

Optimization parity is not enough. The end state is feature equivalence: a valid
WebAssembly 2.0 module accepted on one supported architecture must be accepted on
the other, and it must have the same observable behavior.

| Feature family | AMD64 | ARM64 parity requirement |
|---|:---:|---|
| MVP scalar, control, memory, tables, globals, imports, and exports | Implemented | Match results, side effects, validation failures, and traps. |
| Sign-extension operators | Implemented | Execute every width/sign combination and match boundary cases. |
| Non-trapping float-to-int conversions | Implemented | Match NaN, infinity, signed-zero, and saturation boundaries. |
| Multi-value control and functions | Implemented | Match blocks, branches, direct/indirect calls, host calls, cross-instance calls, public APIs, and artifact reload. |
| Reference types | Implemented | Match nullable/non-null `funcref`, `externref`, typed select, globals, ownership, identity, and lifetime rules. |
| Multiple tables and typed elements | Implemented | Match all indexed table operations, element modes, aliases, imports, limits, and nonzero-table `call_indirect`. |
| Bulk memory | Implemented | Match passive/dropped state, overlap behavior, failure atomicity, traps, and imported-memory behavior. |
| Core SIMD | Implemented | Match opcode admission, lane behavior, NaNs, signed zero, saturation, shuffles, conversions, and reductions. |
| Deterministic relaxed SIMD policy | Implemented | Make the same permitted deterministic choice on both architectures, or document and test an allowed architecture-specific result. |
| Synchronous host imports | Implemented | Match parameter/result layouts, traps, re-entry, nested calls, and maximum supported arity. |
| Reference-safe public APIs | Implemented | Match tokens, store ownership, rejection behavior, close order, and producer retention. |
| Compiled artifact codec | Implemented | Persist and reload identical required-feature, table, global, element, signature, and limit metadata. |
| Pooling and snapshots | Implemented | Enforce identical eligibility, reset, rejection, and shared-state rules. |
| Explicit and guard-page bounds | Implemented | Produce identical wasm-visible results and traps in both modes on both architectures. |

### Shared-memory context gap resolved (July 22, 2026)

The former two-failure `linking.wast:452-453` gap is closed. Each instance now
captures a private pointer `InstanceContext` and rebinds it before serialized
native entry, so imported/shared linear memory no longer implies shared globals,
tables, host state, or segment descriptors. Imported calls compile once and bind
entry/home/context tuples through per-instance dispatch cells.

Indirect calls no longer use `homeLinMem == callerLinMem` as an instance-identity
test. Every canonical funcref descriptor carries an owning-context pointer after
its 32-byte table payload; `call_indirect` compares that pointer with the caller
context and performs the same context copy/restore used by direct imports. This
correctly distinguishes two instances sharing one linear-memory base. Focused
regressions alternate private globals across shared-memory instances and call an
imported function indirectly through a local table while verifying both producer
and consumer globals remain independent.

### Parity gates

WebAssembly 2.0 parity is complete only when all of these are true:

- `SupportedFeatures()` and compile-time admission are equivalent on AMD64 and
  ARM64, except for explicitly documented CPU-feature requirements.
- The official Release 2 suite reports identical applicable module, assertion,
  skip, failure, timeout, and crash counts on both architectures.
- The SIMD, relaxed-SIMD, bulk-memory, reference-types, multi-value, and
  multi-table focused suites are green on both architectures.
- The same generated modules produce matching results, traps, memories, globals,
  tables, and reference identities on AMD64 and ARM64.
- Public `Invoke`, typed `Call`, host calls, cross-instance calls, pooling,
  snapshots, and codec reload expose the same behavior.
- Explicit-bounds and guard-page execution are differential-tested on each
  architecture.
- Architecture-specific skips name a real platform or CPU limitation; no skip
  may hide a feature advertised by that target.
- CI fails if either architecture's conformance counts decrease, even when the
  remaining assertions pass.

The desired final state is one WebAssembly 2.0 feature matrix shared by AMD64 and
ARM64. Backend-specific documentation should then describe instruction selection
and performance differences, not different language support.

## Maintenance rule

When a row lands, add or link focused tests for both architectures, record any
intentional ISA-specific replacement, and move the capability into the broadly
equivalent section only after semantic and fallback behavior match. Performance
parity claims require measurements on representative native hardware.
