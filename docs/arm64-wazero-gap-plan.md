# ARM64 wazero-gap closure plan

## Goal

Close the measured Darwin/arm64 execution gaps against wazero while retaining
Railshot's one-pass compiler model, predictable memory use, and guard-page
correctness. Every change needs a focused benchmark, root tests, and the
guard-page corpus differential.

## Baseline (Apple M4 Max, Darwin/arm64, guard-page Wago)

These are single 1-second runs, useful for ordering work but not a final claim.
Re-run the touched rows at least five times before accepting a delta.

| Corpus export | Wago | wazero | Gap / interpretation |
|---|---:|---:|---|
| `tiny.add` | 32.08 ns | 20.59 ns | Wago 56% slower; host→wasm fixed entry overhead. |
| `memory_tree.run` | 18.64 us | 12.18 us | Wago 53% slower; recursive calls plus memory. |
| `globals.accumulate` | 1.035 us | 678.5 ns | Wago 53% slower; mutable global residency/coherence. |
| `dispatch.apply` | 37.30 ns | 22.02 ns | Wago 69% slower; `call_indirect` fast path. |
| `branches.classify` | 30.41 ns | 19.15 ns | Wago 59% slower; `br_table` / structured-control fixed cost. |
| `many_funcs.run` | 31.22 ns | 21.59 ns | Wago 45% slower; exported leaf and repeated direct-call overhead. |

Other rows from the same sweep: Wago is faster on recursive fib, linked-list,
and Mandelbrot; memory sum and sieve are effectively tied. Do not use a change
to improve one of the listed gaps if it materially regresses those control rows.

## Current worktree state and recent work

The worktree intentionally has uncommitted ARM64 work. Preserve it; in
particular, `warp/` is intentionally dirty and must not be reset or cleaned.

Recent ARM64 changes include:

- three-operand integer local-set sink (`ADD dst,left,right`), which improved
  `isa_var.local_getset` from roughly 29.3 us at `9415112` to roughly 17.3 us;
- `compare; local.tee; br_if` fusion (`CMP; CSET; B.cond`);
- expanded explain counters for flush roots, deferred flush roots, call flushes,
  and deferred local sets;
- experimental call-free loop local promotion in `control.go`, gated by
  `WAGO_ARM64_LOOP_PINS=1`. It is deliberately opt-in: its first A/B was mixed
  (local microbench +6.9%, memory_tree about -2%).

## Hard constraints

- Keep a **single pass**. No SSA tier, global IR allocator, recompile tier, or
  unbounded profiling cache.
- A bounded body/control scan is acceptable. The user permits up to about a 30%
  compile-time cost for better analysis, but measure it.
- Keep conservative fallback paths and temporary A/B environment switches.
- Do not broaden pinning globally merely to use more registers. Values should
  get a register for a measured live region.

## Priority order

### 1. Direct/indirect call entry and dispatch

Target: `dispatch.apply`, `many_funcs.run`, and part of `memory_tree.run`.

Relevant code:

- `src/core/compiler/backend/railshot/arm64/call.go`
  - `emitRegisterCallVia`
  - `callIndirect`
  - `emitIndirectCallHomeAware`
- `src/core/compiler/backend/railshot/arm64/compile.go`
  - `emitRegABI` wrapper adapter and internal entry
- `src/wago/instantiate.go` table descriptor setup.

Facts already established:

- int-only same-instance funcrefs have an internal-entry tag and use a guarded
  `BLR` register-ABI fast path; host/cross-instance entries retain the wrapper
  path.
- Direct simple leaves can preserve caller pins; this was previously measured as
  a substantial direct-call win.

Next experiments:

1. Disassemble `dispatch.wasm` Wago and WARP code. Count the table checks,
   stack stores, moves, and branches on the local-int-only path.
2. Ensure the internal path does not flush or spill values unnecessarily when
   the table index and arguments are already register-resident.
3. For `many_funcs`, inspect whether `run`'s direct calls use the preserve-pins
   leaf ABI and whether its final result avoids slot round trips.
4. Consider table-entry inline cache only after disassembly proves repeated
   identical targets and a table epoch can make it correct. It must be guarded
   by table mutation/versioning.

Validation:

```sh
cd bench
WAGO_BOUNDS=signals go test -tags wago_guardpage -run '^$' \
  -bench 'BenchmarkExec/(dispatch\.apply|many_funcs\.run|memory_tree\.run)' \
  -benchmem -benchtime=500ms -count=5 .
go test -run '^$' -bench '^BenchmarkWazeroExec/(dispatch\.apply|many_funcs\.run|memory_tree\.run)$' \
  -benchmem -benchtime=500ms -count=5 .
```

### 2. Branch and `br_table` lowering

Target: `branches.classify`.

Relevant code: `src/core/compiler/backend/railshot/arm64/control.go`, especially
`opBrTable`, `flush`, branch edge reconciliation, and `brTableJumpMin`.

The corpus module is deliberately a nested-block `br_table` classifier
(`bench/corpus/src/branches.wat`). It is likely dominated by control-edge flush,
slot moves, and table dispatch rather than loop-local allocation.

Work in this order:

1. Disassemble the classifier and WARP equivalent; count per-case instructions.
2. Check whether the small-table chain vs ADR/LDR/BR jump table threshold is
   optimal on Apple M4.
3. Avoid flushing already-dead/no-result operand stacks at branches.
4. Extend register merge only if the classifier actually carries a result through
   a merge; preserve canonical-slot fallback for multi-value/mixed cases.

### 3. Mutable globals

Target: `globals.accumulate`.

Relevant code:

- `src/core/compiler/backend/railshot/arm64/globals.go`
- `compile.go`: `assignPinnedLocals`, module-global/value-global pinning
- `localstate.go`: call spill/reload state.

The corpus is a call-free loop over one mutable `i64` global. Inspect the
generated code first: it should load the global once, keep it in a register for
the loop, and write back once at exit. If it instead repeatedly derives a cell
pointer or loads/stores inside the loop, fix that directly. Measure with
`WAGO_PIN_GLOBAL_K` where relevant, but do not trade away operand registers in
unrelated memory-heavy functions.

### 4. `memory_tree` after calls and globals

Target: recursive direct calls with load/store churn.

Do this only after the call ABI and global work above. Use explain counters and
disassembly to distinguish:

- call-adjacent local spills/reloads;
- wrapper vs internal entry selection;
- repeated bounds/memory-base setup;
- register pressure caused by pins.

The experimental loop pinning does not target this recursive workload and should
remain opt-in unless a dedicated A/B proves otherwise.

### 5. Tiny host→wasm entry

Target: `tiny.add`.

This is mostly runtime/adapter cost, not JIT ALU lowering. Relevant locations:

- `compile.go:emitRegABI` host adapter;
- `src/core/runtime/guardmem_darwin_arm64.go` / `sigtrap_*arm64.go`;
- `enterNative` trampoline and public `Invoke` marshalling.

First separate public API overhead from the emitted adapter with a direct runtime
benchmark. Do not weaken trap-cell installation, stack-fence safety, or Go ABI
preservation just to improve a 20 ns benchmark.

## Required verification

For every code change:

```sh
go test ./...
make test-guard
git diff --check
```

For every performance claim, use at least five 500 ms samples of the touched
Wago corpus row and matching wazero row. Record medians and noise. Finish with:

```sh
cd bench
WAGO_BOUNDS=signals go test -tags wago_guardpage -run '^$' \
  -bench '^BenchmarkExec/' -benchmem -benchtime=1s -count=1 .
go test -run '^$' -bench '^BenchmarkWazeroExec/' -benchmem -benchtime=1s -count=1 .
```

If a gap remains after disassembly and a focused experiment, record the concrete
reason in this file rather than claiming parity from an unrelated benchmark.

## Results — iteration 1 (empty-edge branch fold)

Disassembly of the target rows showed the dominant waste in the tight ALU/branch
loops was the two-branch lowering of a value-less `br_if` (`CMP; B.cond skip; B
target; skip:`), which executes **two taken branches per loop iteration** — the
throughput ceiling on M4 for a 5–6 instruction loop.

Landed, all gated for A/B and on by default:

- **Empty-edge branch fold (the win).** `opBr` and `brIfFused` now detect an edge
  that emits no code (no value moves / loop-pin stores / local converge) and emit
  a *single* conditional branch straight to the target — no skip branch, no
  padding. Non-empty edges relocate their (branch-free, flag-neutral) bytes one
  word to keep the guarded form byte-identical to before. Gated on
  `WAGO_ARM64_NOBRFOLD=1` (also disables the post-assembly `foldBranchPairs`
  peephole, which now only catches `br_table` chains + residual pairs).
- **UXTW extended-register add.** `i64.add(x, i64.extend_i32_u(y))` →
  `ADD Xd,Xn,Wm,UXTW`. Correct but ~neutral on M4 (the two removed instructions
  aren't on the critical path); kept as a code-quality win. `WAGO_ARM64_NOUXTW=1`.
- **Adjacent store→load forwarding.** `STR Xs,[SP,#k]; LDR Xd,[SP,#k]` →
  `STR…; MOV Xd,Xs` (NOP if Xd==Xs). Neutral on M4; removes the redundant reloads
  inlined-call arg staging emits. `WAGO_ARM64_NOSTLDFWD=1`.

Benchmarks (Apple M4 Max, guard-page, 500 ms × 5, medians; baseline = all three
gated off, i.e. the prior two-branch lowering):

| Row | Baseline | New | Δ |
|---|---:|---:|---:|
| `sieve.count` | 98 549 ns | 49 540 ns | **−49.7%** |
| `globals.accumulate` | 1 045 ns | 545 ns | **−47.8%** (now < wazero's 678 ns) |
| `memory.sum` | 294 ns | 170 ns | **−42.3%** |
| `linked_list.sum` (control) | 5 464 ns | 4 960 ns | **−9.2%** |
| `mandelbrot.render` (control) | 255 µs | 240 µs | −5.8% |
| `memory_tree.run` | 18 596 ns | 18 002 ns | −3.2% |
| `dispatch.apply` | 37.6 ns | 36.9 ns | −2.0% |
| `fib_rec` / `nbody` / `tiny` / `branches` / `many_funcs` | — | — | neutral (±0.5%) |

No control row regressed (an earlier size-preserving NOP-based peephole regressed
`linked_list` ~10%; the emission-side fold — no NOP, one fewer taken branch —
turned it into a win). `dispatch`, `many_funcs`, `branches`, and `tiny` are
call/frame-boundary bound, not branch bound, so the fold leaves them ~flat; their
gaps need the call-ABI / frame-elision work in sections 1 and 5, still open.

### Pre-existing worktree breakage (NOT from this change)

The guard-page corpus differential does not pass on the current worktree,
independent of this iteration's changes (they reproduce with all three flags off,
which makes the new code byte-inert; and `git stash` → HEAD passes sha256/
spectralnorm). The uncommitted WIP regressed, versus commit `9415112`:

- `fannkuch.wasm.run` — hangs (infinite loop) in guard mode.
- `sha256.wasm.hashN` — traps "linear memory access out of bounds".
- `spectralnorm.wasm.run` — miscompiles in *explicit* mode (`=1000000000`, golden
  `1274222120`); no existing feature flag (`WAGO_NO_STFLAGS`, `WAGO_REG_MERGE=0`,
  `WAGO_NO_BOUNDS_FACTS`, `WAGO_ARM64_NOREGABI`) restores it.

These block `make test-guard` and must be root-caused before shipping the WIP.
The 11 other differential modules (blake, utf, json×2, memory_tree, sieve, nbody,
matmul, quicksort, crc32, raytrace, linked_list) pass with this iteration on.
