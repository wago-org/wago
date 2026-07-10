# ARM64 branch-fold findings (2026-07-09)

Session notes from iteration 1 of `docs/arm64-wazero-gap-plan.md`. Records what was
tried, what worked, what didn't, and the microarchitectural reasoning — so the next
session doesn't re-derive it.

## TL;DR

- The single biggest lever for tight ALU/branch loops on Apple M4 is folding a
  **value-less `br_if`** from a two-branch lowering into one conditional branch.
  It roughly **halves** such loops (`sieve` −50%, `globals` −48%, `memory.sum`
  −42%) and beats wazero on `globals`.
- Do the fold at **emission time** (emit one `B.cond target`), *not* as a
  size-preserving post-assembly peephole that leaves a `NOP`. The NOP executes
  every iteration and regressed `linked_list` ~10%; the emission-side form (one
  fewer taken branch, no NOP) makes `linked_list` ~9% *faster*.
- `uxtw-add` fusion and adjacent store→load forwarding are correct but
  **~neutral on M4** — the removed instructions aren't on the critical path.
  Kept as code-quality wins; the branch fold is what moves the needle.

## The waste (disassembly)

A value-less `br_if L` (loop-exit test carrying no operands) lowered to:

```
CMP  Wn, #0
B.cond skip        ; skip the (empty) edge
B    L             ; the real branch
skip: ...          ; loop body
```

Two **taken** branches per iteration (the `B.cond` that jumps over the `B`, plus
the loop back-edge `B`). On M4 a 5–6-instruction loop is throughput-bound, and
taken-branch throughput is the ceiling — so this doubles the effective loop cost.

Fused form:

```
CMP  Wn, #0
B.cond(inverted) L   ; one branch; fall through into the body
... body
B loop_top           ; the only taken branch
```

## What was tried, in order

1. **Post-assembly peephole** `B.cond +8; B target → B.cond(inv) target; NOP`
   (size-preserving, provably safe via a branch-target set + BR guard). Correct,
   and it captured the wins on branch-bound loops — but the residual NOP is on the
   hot fall-through path and **regressed `linked_list` ~9–10%** (a memory-latency
   pointer chase, where the branch saving doesn't help but the extra front-end
   slot does hurt). Kept only for `br_table` chains + residual pairs.

2. **Emission-side fold** (shipped). `opBr` and `brIfFused` measure the edge
   (`storeLoopPinsLeaving` + `moveBranchValues`/`branchEdgeToMerge1`); if it
   emitted nothing, emit a single `condBranchJump` to the target. Non-empty edges
   relocate their bytes one word to insert the skip guard — safe because every
   edge helper emits only position-independent, NZCV-neutral `LDR/STR/MOV`.
   `ctrlFrame.condEnds` holds forward `B.cond` sites patched imm19 at block end;
   loop targets patch immediately (imm19 range-checked, falls back to the guarded
   form when out of ±1 MiB); function-frame targets keep the guarded form (the
   branch carries the single-result load). Gated `WAGO_ARM64_NOBRFOLD=1`.

## Results (M4 Max, guard-page, 500 ms × 5, medians; baseline = all flags off)

| Row | Baseline | New | Δ | notes |
|---|---:|---:|---:|---|
| `sieve.count` | 98 549 ns | 49 540 ns | −49.7% | |
| `globals.accumulate` | 1 045 ns | 545 ns | −47.8% | beats wazero (678 ns) |
| `memory.sum` | 294 ns | 170 ns | −42.3% | |
| `linked_list.sum` | 5 464 ns | 4 960 ns | −9.2% | control row; was +10% under the NOP peephole |
| `mandelbrot.render` | 255 µs | 240 µs | −5.8% | control row |
| `memory_tree.run` | 18 596 ns | 18 002 ns | −3.2% | |
| `dispatch.apply` | 37.6 ns | 36.9 ns | −2.0% | |
| `fib_rec`,`nbody`,`tiny`,`branches`,`many_funcs` | — | — | ±0.5% | neutral |

Bisection confirmed the attribution: only-uxtw ≈ baseline everywhere; only-brfold
≈ the full win. (Caution: `linked_list` has a ~10% run-to-run noise floor — a
peephole that fires 0 times there still measured ±13% at `count=2`; trust
interleaved `count≥5`.)

## Why the flat rows stay flat

`dispatch`, `many_funcs`, `branches`, and `tiny` are **call- and
frame-boundary bound**, not branch bound:

- `tiny.add` internal entry is 1 useful instruction wrapped in an unused 32-byte
  frame (`MOVZ/MOVK/SUB SP` … `ADD SP`) — the frame reserves slots for locals that
  are all register-pinned. Frame elision for frameless leaves (plan §5) is the
  lever; deferred (correctness-sensitive: stack fence, StpPre).
- `dispatch.apply` is dominated by the `call_indirect` table-check + `BLR`
  dispatch machinery (plan §1), not the bounds-check branches the fold touched.
- `many_funcs.run` (inlined leaves) still emits `MOV x,x19; STR;` per arg; the
  store→load-fwd NOPs the reloads but the value is off the critical path.

Next levers, in plan priority order: §1 call/indirect dispatch, §5 frame elision
for frameless leaves, §3 already largely closed by this pass (`globals`).

## Tooling

Disassembly via a standalone `armdump` (`golang.org/x/arch/arm64/arm64asm` +
`arm64.CompileModuleWith`, guard mode). **Do not** add `x/arch` to the repo
`go.mod` — it bumps the `go` directive and makes the `bench/` submodule
inconsistent (`go mod tidy` demand). Build it as a separate throwaway module with
a `replace` back to the repo.

## Pre-existing worktree breakage (NOT from this work)

See the warning block in `docs/arm64-isa-regressions.md`. `fannkuch` (hang),
`sha256` (OOB), `spectralnorm` (explicit miscompile) fail on the current WIP,
proven independent of this pass. They block `make test-guard` and need
root-causing; `fannkuch` and `spectralnorm` first.
