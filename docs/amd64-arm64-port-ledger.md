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
| 1 | Three-operand/local-result sinking | Use x86 forms where profitable; destructive instructions require selective equivalents. |
| 2 | Unary and `local.tee` result sinking | Keep results in their eventual local destination. |
| 3 | Entry-argument pinning | Keep hot incoming arguments resident when pressure allows. |
| 4 | Leaf scratch-register pinning | Use otherwise-free registers in call-free leaf functions. |
| 5 | Deeper FP local pinning | Expand XMM residency based on pressure and call behavior. |
| 6 | Small-frame adjustment and elision | Specialize tiny stack frames and register-homed locals. |
| 7 | Call-free hint propagation through inlining | Preserve pinning decisions when leaf bodies are spliced. |
| 8 | Specialized 33–64-byte bulk memory | Add fixed-size load-all/store-all copy and fill shapes. |
| 9 | Prepared-call control refresh tests | Ensure AMD64 has equivalent cross-instance trap-cell and fence regression coverage. |

**Landed:**

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
| 1 | Inlining positive/negative suite | Port eligibility, recursion, growth limits, calls, traps, and result-shape cases. |
| 2 | Module-global pinning tests | Arch-specific: `pickModuleGlobals` demands a higher first-pin threshold on ARM64 (a pin displaces a hot local), so ARM64 keeps its own `TestModuleGlobalPinRequiresABIWideReuse`; the score-scan is shared but the selection tests do not port verbatim. |
| 3 | Golden disassembly harness | Add stable ARM64 instruction-selection goldens. |
| 4 | Backend self-update checks | Add ARM64 equivalents where generated-code patching applies. |
| 5 | Stack and frame-layout tests | Operand-stack arena sizing ported (`arm64/stack_arm64_test.go`); the remaining register-layout/pinned-local coverage is arch-specific. |
| 6 | SIMD benchmark suite | Add equivalent ARM64 backend-local microbenchmarks after the SIMD branch lands. |
| 7 | Full SIMD acceptance claim | Close remaining NEON corpus and performance gaps, especially movemask and reductions. |
| 8 | Architecture-neutral spec tests | Remove legacy `linux && amd64` tags where tests are not actually ISA-specific. |

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
- bulk memory;
- linear store-to-load forwarding of an owned full-width value into an
  immediately re-read slot;
- explicit and guard-page bounds modes;
- bounds facts and loop prechecks;
- constant division and constant folding;
- compare/branch fusion and empty-edge double-branch folding;
- hot local and module-global pinning;
- straight-line leaf inlining;
- codegen statistics and explain mode; and
- core SIMD lowering, although ARM64's acceptance and performance confidence is
  currently lower.

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
5. Close remaining SIMD parity after the dedicated SIMD branch lands.
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
