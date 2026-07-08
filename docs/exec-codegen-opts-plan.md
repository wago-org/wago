# Exec-codegen optimization pass (2026-07-07)

Worktree: `../wago-exec-codegen`, branch `perf/exec-codegen`. Scope chosen by the
user: **generated machine-code quality** in `src/core/compiler/backend/railshot`
(not compile speed, not runtime/infra). Ranked by ROI = expected corpus exec win ÷
effort, discounted by miscompile risk (the storage-model / deferred-load areas have
a history of subtle desync bugs — #68, inflate-condense, br_table-RAX, sqlite-pin).

## Baseline (AMD Ryzen 7 7800X3D, explicit bounds, `BenchmarkExec`, 200ms)

| program | ns/op | program | ns/op | program | ns/op |
|---|--:|---|--:|---|--:|
| tiny.add | 16.0 | memory.sum | 235 | sha256.hashN | 42 937 |
| fib_iter | 27.1 | memory_tree | 11 557 | crc32.hashN | 19 426 |
| fib_rec | 1 426 929 | globals.acc | 852 | quicksort | 63 267 |
| dispatch | 19.0 | linked_list | 9 374 | matmul | 162 467 |
| branches | 15.3 | sieve.count | 83 939 | json-as ser | 22 293 |
| many_funcs | 16.1 | mandelbrot | 267 341 | json-as deser | 39 980 |
| arith | 1 675 | nbody | 323 487 | blake-as | 750 904 |
| float.run | 10 067 | spectralnorm | 2 221 418 | utf-as | 198 396 |
|  |  | fannkuch | 1 072 877 | raytrace | 415 523 |

Float-heavy programs dominate the slow tail (spectralnorm, raytrace, nbody,
mandelbrot). Loop/branch-heavy: sieve, quicksort, fannkuch. Rotate/shift-heavy:
sha256, blake, crc32.

## What's already landed (reconciled vs OPTIMIZATIONS.md, which was stale)

DONE since the doc: R2a in-place XMM accumulation (`fbinInto`/`float-local-sink`),
R2b lazy float pinned locals (uniform STACK_REG), R1's *select*-onto-flags fusion
(#192 `trySelectOnFlags`), leaf inlining (default-ON), loop-precheck bounds hoisting
(default-ON). Don't re-do these.

## Correctness gate (run after every change)

1. `go test ./src/core/compiler/backend/railshot/` — unit + golden disasm + exec.
2. `cd bench && go test -run 'Differential|Corpus|Sqlite|Jsonas|Wasi' ./...` — corpus
   differential + real-program exec (this is what catches miscompiles).
3. Spec suite `TestSpecSuiteExec` (needs `WAGO_SPECTEST_DIR`) before finalizing.

Any change lands only with (a) its CodegenStats counter moving, (b) gate green, (c)
a measured benchmark delta.

## ROI-ranked optimizations (implement top-down, keep winners, revert losers)

### 1. R3 — store-narrowing peephole  · S · low risk
`setcc r8; movzx r,r8; i32.store8 [mem],r` keeps a dead `movzx`. Store the setcc
byte directly. Self-contained in the store8 lowering; the `SETcc` byte is already
8-bit. Targets boolean-array writes (sieve mark, predicate tables). Counter:
`store-narrow`.

### 2. Const-fold pack + same-operand compare identities (P2.3 + P2.4)  · S · low risk
`fold.go:foldable` folds only arithmetic binops. Add const folding for compares,
`eqz`, `clz/ctz/popcnt`, and integer extensions; add same-operand compare
identities (`x==x→1`, `x<x→0`, `x<=x→1`, …) beside `simplifySameOperand`. Pure
compile-time; near-zero exec risk, shrinks code and removes a few live ops where
constants/aliases flow in. Bundled because they touch the same two functions.

### 3. R2c — float `call; local.set` result fusion  · S–M · low–med risk
The call-result→pinned-local fusion (`call.go`) is `sigIsIntOnly`-gated. Extend to a
single float result landing directly in the pinned XMM local, mirroring the int
path. Helps float programs that call helpers (raytrace, nbody, spectralnorm).
Counter: `float-call-set`.

### 4. R7 P2.1 — alias-aware pending loads  · M · med risk
`materializePendingLoads` flushes ALL deferred `stMemRef` on every store. Keep loads
that are provably disjoint from the store (same base register, non-overlapping
[disp,disp+size) static ranges; conservative — bail to full-flush otherwise). Helps
load-heavy code between stores (json, sha256, blake, memory_tree). Higher risk:
deferred-load aliasing is where past desync bugs lived — extra differential runs.
Counter: `alias-load-kept`.

### 5. R1 — `stFlags`, first slice: one-deep deferred-set fusion window  · M–L · higher risk
Compare→branch fusion breaks when a `local.tee`/`local.set` sits between the compare
and its `br_if`/`if` (`cmp; local.tee $c; br_if`, `eqz; local.set $c; if`) — common
in TinyGo/AssemblyScript loop conditions. Allow the fusion to survive a single
deferred local-set of the compared boolean, materializing the flag only if a
flag-clobbering op intervenes. The full flags-resident storage kind is deferred;
this is the high-value, lower-scope slice. Do LAST, gate behind an env flag first,
spec-suite before default-on. Counter: `cmp-fuse-window`.

### 6. (stretch) BMI2 `rorx`/`shlx`/`shrx` behind a CPUID probe  · M · niche
Non-destructive 3-operand shifts/rotates skip the value-copy + `CL` staging that
2-operand shifts need. Helps rotate-heavy sha256/blake3/crc32. Needs a one-time
JIT'd CPUID stub. Only if 1–5 leave time; measure on sha256/blake.

## Rejected / out of scope
- Same-operand compare identities on their own program impact is ~nil (compilers
  don't emit `x==x`) — folded into #2 only because it's free there.
- call_indirect inline caches, immutable-global folding, mixed-call parallel staging
  (R2d): higher effort, lower/uncertain corpus ROI — deferred.
- Anything in the SSA/IR direction: forbidden by the no-IR decision.

## Results log — MEASURED (2026-07-07/08)

The measurement pass **overturned the ranking above**. Every "cheap win" turned out
exec-dead or neutral on the corpus; the one real lever needs a substantial feature.
Method: `bench/cmd/explain` counters (static) + `BenchmarkExec` explicit-vs-guard
(the guard-vs-explicit delta is the *entire* ceiling of ALL bounds-check work at once).

### The decisive measurement: guard (all bounds checks removed) vs explicit baseline

| program | explicit ns | guard ns | bounds ceiling |
|---|--:|--:|--:|
| **matmul** | 162 467 | 107 966 | **−33.5%** |
| **memory_tree** | 11 557 | 9 100 | **−21%** |
| **sha256** | 42 937 | 36 969 | **−14%** |
| json deser | 39 980 | 37 264 | −7% |
| json ser | 22 293 | 21 063 | −5.5% |
| blake-as | 750 904 | 723 706 | −3.6% |
| crc32 | 19 426 | 18 833 | −3% |
| fannkuch | 1 072 877 | 1 043 652 | −2.7% |
| sieve / quicksort / spectralnorm | — | — | <2% |

**Bounds-check elision is the only lever with real dynamic ROI, and it's concentrated
in matmul / memory_tree / sha256.** Everything else is ≤7%.

### What was TRIED and the verdict

| item | verdict | evidence |
|---|---|---|
| R3 store-narrowing (`setcc;movzx;store8`) | **DEAD** | `compare-setcc` counts are ~0–3 static across the corpus; the slow (float) programs are 0 (compares already fuse); a lone `movzx` is OOO-hidden. |
| R1 `stFlags` / fusion-past-adjacency | **DEAD** | `cmp-branch-fuse` already fires nearly everywhere (fannkuch 66 fused vs 3 not; matmul 12 vs 1). The adjacency limit almost never triggers in real compiled wasm. |
| const-fold pack + same-operand compares (P2.3/4) | **near-zero exec** | compile-time only; folds constants computed once. Not worth the surface. |
| R2c float `call;local.set` fusion | **DEAD for the hot programs** | the slow float loops (spectralnorm/nbody/mandelbrot/raytrace) are call-free; `f64.sqrt` is an intrinsic, not a call. |
| **loop-param versioning** (lifted `len(paramTypes)!=0` bail) | **IMPLEMENTED, then REVERTED — exec-neutral** | Correct + safe (railshot units + full spec suite + corpus differential all green; void-loop codegen byte-identical). But it captured **0 new versioned loops** anywhere in the corpus: the blocker was never params — `scanLoopHoistable` only proposes a *direct invariant-base* `local.get;load` as a candidate, and no param loop had one. Reverted per "no exec win → don't keep the complexity." |

### Why the big-ceiling programs are NOT captured today

- **matmul (−33%)**: hot loop `c[i*n+j] += aik*b[k*n+j]` compiles to **running pointer
  locals** (`local 1`/`local 2`) incremented by a constant stride (16) each iteration
  and used as direct memop bases. They're `local.set` in the loop → not invariant →
  correctly skipped by the invariant-base precheck. Capturing them needs
  induction-variable max-extent analysis.
- **sha256 (−14%)**: `hoistable=16` checks are **indexed** accesses (`invariantBase +
  idx`, idx a loop counter), not a bare `local.get;load` — `scanLoopHoistable` never
  proposes them. Same IV/max-index need.
- **memory_tree (−21%)**: pointer-chasing tree traversal — the base is **data-dependent**
  (not monotone), so it is *genuinely unhoistable* by any static analysis. Unreachable.

### Conclusion / the one real next step

The railshot exec codegen is already highly tuned (measured: spills≈0, compares fuse,
in-place XMM accumulation, lazy float pins all present). The single remaining lever
with real ROI is **monotonic-induction-variable loop-bounds-check elision** — extend
`scanLoopHoistable` + the existing fast/slow versioning to counted loops whose base is
either (a) a running pointer advanced by a constant stride, or (b) an invariant base +
a bounded loop-counter index. Precheck the **conservatively over-approximated** max
extent (over-approx = safe: a too-strict precheck only diverts to the checked slow
body; a too-lax one silently skips a real trap — the security-critical direction).
Ceiling: matmul −33%, sha256 ~−7–14%. This is a real, trap-sensitive feature (~150–250
LOC + IV edge-case tests), **not** a quick peephole — it warrants its own spec-gated
session, which is why it was scoped here rather than rushed. The versioning machinery
(`boundshoist.go`), the fast/slow trap-exact safety proof, and the param-loop support
(trivially re-addable, shown safe above) are all already in place for it.

