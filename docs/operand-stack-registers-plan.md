# Operand-stack-in-registers refactor (WARP RegisterCopyResolver)

## Goal

Replace railshot's **canonical-slot** reconciliation at control boundaries with
**register** reconciliation — WARP's model. Today, at every block / if / loop /
branch / end, `flush()` writes the whole operand stack to position-indexed frame
slots (`spillOff(i)`) and `setDepth` rebuilds it as slot entries; merges are
trivial because all edges agree on slots. This costs a store+reload per operand
per boundary. WARP instead keeps operands in registers across merges and shuffles
them into a canonical register assignment at each edge via a parallel-move
resolver. Target win: call/branch-heavy code (`memory_tree`), not json-as
(memory-bound; leave it alone).

Infra already present: `regcopy.go` `resolveRegMoves` (parallel move with cycle
handling), used today for call args. Reuse it for edge reconciliation.

## Current model (facts)

- `flush()` (control.go): each operand i → `spillOff(i)`, deferred nodes
  condensed, all registers freed, stack rebuilt as `stSlot` entries. Idempotent
  for already-canonical slots.
- `moveSlots(from,to,n)`: copy n slots (used to place branch values at the target
  frame's `height`).
- `ctrlFrame{height, paramN, resultN, branchN, ...}`: `branchN` = values
  transferred to this label (results for block/if, params for loop).
- Branch (`opBr`/`brIfFused`/`br_table`): `flush(); moveSlots(d-branchN, fr.height, branchN); jmp`.
- Merge (`opEnd`): fallthrough `reconcileLocals(); flush()`; result slots at
  `[height, height+resultN)`.
- Pinned locals already live in registers (R12-R15) with the STACK_REG/eager
  state machine (`localstate.go`), reconciled at merges by `reconcileLocals`.

## Design

**Canonical branch registers.** Assign branch/result values (up to K) to a fixed
ordered register set `branchRegs` (GP) + `branchFRegs` (XMM), disjoint from
`pinnedLocalRegs`, RBX (linMem), and RSP. Value at branch-position j → `branchRegs[j]`
(by class). Overflow beyond K, and mixed layouts that don't fit, spill the
remainder to slots `[height+K, ...)` (hybrid — never worse than today).

**Edge reconciliation (`reconcileToBranchRegs(fr)`).** Replaces `flush+moveSlots`
at a branch and the result-placement at a merge:
1. Materialize the top `branchN` operands, assigning each a target = its canonical
   branch reg (or slot for overflow).
2. Feed (dst,src) pairs to `resolveRegMoves` (regcopy.go) — handles cross-target
   cycles with xchg.
3. Operands **below** `fr.height` keep the existing rule: they must already be in
   canonical slots (they were flushed when the frame was entered and untouched
   since), so nothing to do. (Phase 3 lifts below-operands into registers too.)

**Merge landing.** At the block/if end and loop header, the branchN values are
known to be in `branchRegs`/slots; push them as `stReg`/`stSlot` elems so post-merge
code consumes them from registers. Mark those registers occupied.

**Register pressure.** `branchRegs` are normal allocatable regs. Between the header
and an edge they may hold other values; `reconcileToBranchRegs` runs immediately
before the jmp / at the merge, so nothing executes between reconciliation and the
join. The allocator must treat branchRegs as occupied by the pushed result elems
after the merge (so subsequent ops spill them normally).

## Phases (each: `go test ./...` + spec 1.0/2.0/3.0 green, bench memory_tree, no
>3% regression on fib_rec/memory_tree/json-as guard)

1. **Infra, no behavior change.** Add `branchRegs`/`branchFRegs`, a
   `reconcileToBranchRegs` that currently just calls flush+moveSlots (identity),
   and unit tests for the reg-assignment mapping. Land as a no-op scaffold.
2. **Single-value merges (branchN==1) in a register.** if/else and block with one
   result: reconcile the result to `branchRegs[0]` (or FReg) at every edge
   (fallthrough, else, br/br_if/br_table); land it in a register at the merge.
   Everything else keeps slots. Highest-frequency case, smallest blast radius.
3. **Multi-value merges up to K** (K≈4 GP + 4 FPR), slot fallback for overflow.
4. **Below-operands in registers** across the block (full WARP): stop flushing the
   sub-`height` stack to slots on entry; reconcile the *entire* live operand
   stack at edges. Biggest change; needs a spill policy when live operands exceed
   the register file.
5. **Loops.** Loop-carried operand values (non-locals) stay in `branchRegs` across
   iterations; back-edge reconciles to the header's assignment. (Locals already
   register-resident, so gate on real loop-carried operand stack depth.)

## Risks / invariants

- Every edge into a join MUST agree on the register assignment — derive it purely
  from `(branchN, per-value class)` so all edges compute the same mapping.
- Unreachable edges must not emit reconciliation.
- Interaction with pinned-local reconcile (`reconcileLocals`) — keep it; it runs
  on the same edges. branchRegs must exclude pinnedLocalRegs.
- Traps/`return` already handled by the register-return hint (RAX/XMM0); keep.
- Fallback path (slots) stays for overflow and until a phase covers a case, so the
  refactor is never a regression relative to today.
