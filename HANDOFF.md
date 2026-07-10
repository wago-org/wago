# HANDOFF — ARM64 railshot backend (branch `jairus/arm64`)

Written 2026-07-09 for a fresh agent with no conversation context. Read this top
to bottom before touching the backend.

Working directory: `/Users/work/Code/Wago/wago`. Host: Apple M4 Max, Darwin/arm64.
Base commit: `9415112` (`arm64: fast-path local indirect calls`).

---

## 0. READ FIRST — critical state

1. **Nothing is committed.** All work below is uncommitted in the worktree. There
   is pre-existing uncommitted WIP that is **not mine** (see §4). Do not `git
   reset`/`git checkout`/`git stash drop` without care — you will lose work.
2. **`warp/` is intentionally dirty and untracked. Never reset or clean it.**
3. **The guard-page test baseline is RED — and it was red before this session.**
   `make test-guard` fails/times out because the pre-existing WIP miscompiles three
   corpus modules. This is **not** caused by the optimization work in §1. Details
   and proof in §5. **Do not treat a green `make test-guard` as your acceptance
   gate — it cannot currently pass.** Use the substitute gate in §3.

---

## 1. What this session did

Implemented iteration 1 of `docs/arm64-wazero-gap-plan.md` — closing measured
Darwin/arm64 execution gaps vs wazero. Three changes, all gated by env var, all
on by default:

| Change | Where | Effect on M4 | Off switch |
|---|---|---|---|
| **Empty-edge branch fold** (the win) | `control.go` `opBr`, `fuse.go` `brIfFused`, `condBranchJump`+`ctrlFrame.condEnds` in `control.go` | A value-less `br_if` emits **one** `B.cond target` instead of `B.cond skip; B target; skip:` — one fewer taken branch per loop iteration | `WAGO_ARM64_NOBRFOLD=1` |
| **UXTW extend-add fusion** | `emit.go` `tryUxtwAdd`, `asm2.go` `AddExtUXTW` | `i64.add(x, extend_i32_u(y))` → `ADD Xd,Xn,Wm,UXTW`. Correct, ~neutral | `WAGO_ARM64_NOUXTW=1` |
| **Store→load forwarding** | `peephole.go` `forwardStoreLoads` | Adjacent `STR Xs,[SP,#k]; LDR Xd,[SP,#k]` → `MOV`/NOP. Correct, ~neutral | `WAGO_ARM64_NOSTLDFWD=1` |

`peephole.go` also has `foldBranchPairs`, a size-preserving post-assembly peephole
(`B.cond +8; B → B.cond target; NOP`) that now only mops up `br_table` chains and
residual pairs; it shares `WAGO_ARM64_NOBRFOLD`.

### Results (guard-page, `-benchtime=500ms -count=5`, medians; baseline = all flags off)

| Row | Baseline | New | Δ |
|---|---:|---:|---:|
| `sieve.count` | 98 549 ns | 49 540 ns | **−49.7%** |
| `globals.accumulate` | 1 045 ns | 545 ns | **−47.8%** (now < wazero 678 ns) |
| `memory.sum` | 294 ns | 170 ns | **−42.3%** |
| `linked_list.sum` (control) | 5 464 ns | 4 960 ns | **−9.2%** |
| `mandelbrot.render` (control) | 255 µs | 240 µs | −5.8% |
| `memory_tree` / `dispatch` | — | — | −2–3% |
| `fib_rec`, `nbody`, `tiny`, `branches`, `many_funcs` | — | — | neutral (±0.5%) |

**No control-row regression.** Full reasoning, the failed first approach, and the
"why the flat rows stay flat" analysis are in **`docs/arm64-branch-fold-findings.md`**
— read it, it's the real design log.

---

## 2. Build / test / bench commands

```sh
# build everything
go build ./...

# unit + arm64 exec tests (fast, MUST stay green)
go test ./...

# encoder golden (AddExtUXTW added here)
go test ./src/core/encoder/arm64/ -run TestPortIntEncodings

# disassemble generated code — see §6 for armdump setup
```

Benchmark one row, A/B (all my flags off = the pre-fold baseline):

```sh
cd bench
# baseline
WAGO_ARM64_NOBRFOLD=1 WAGO_ARM64_NOSTLDFWD=1 WAGO_ARM64_NOUXTW=1 WAGO_BOUNDS=signals \
  go test -tags wago_guardpage -run '^$' -bench 'BenchmarkExec/globals\.accumulate$' \
  -benchtime=500ms -count=5 .
# new (default-on)
WAGO_BOUNDS=signals go test -tags wago_guardpage -run '^$' \
  -bench 'BenchmarkExec/globals\.accumulate$' -benchtime=500ms -count=5 .
```

`linked_list.sum` has a ~10% run-to-run noise floor — use `count>=5` and interleave
runs; don't trust `count=2` deltas there (a peephole that fires *zero* times still
measured ±13% at `count=2`).

---

## 3. Substitute acceptance gate (because `make test-guard` is red)

Since the WIP baseline can't pass the full guard suite, verify a change is
regression-free by:

1. `go test ./...` stays green.
2. The **11 corpus differential modules that DO pass** stay green (bit-exact
   explicit == guard == golden), run each individually so one hang doesn't block
   the rest:
   ```sh
   cd bench
   for m in blake-as utf-as json-as memory_tree sieve nbody matmul quicksort crc32 raytrace linked_list; do
     WAGO_BOUNDS=signals go test -tags wago_guardpage -run "TestCorpusDifferential/$m\.wasm" -timeout 40s .
   done
   ```
   (Do **not** run `TestCorpusDifferential` bare — `fannkuch` hangs it. Do not use a
   `-run` pattern that also matches `TestCorpus/` — those are separately broken.)
3. All-flags-off vs on produce the same pass/fail set (my code is byte-inert with
   all three flags off, so any diff is a real regression).

---

## 4. Files touched

**Mine (this session):**
- `src/core/compiler/backend/railshot/arm64/peephole.go` — NEW (foldBranchPairs + forwardStoreLoads)
- `src/core/compiler/backend/railshot/arm64/control.go` — `condEnds`, `condBranchJump`, `opBr` empty-edge restructure, condEnds patch site
- `src/core/compiler/backend/railshot/arm64/fuse.go` — `brIfFused` empty-edge restructure
- `src/core/compiler/backend/railshot/arm64/emit.go` — `tryUxtwAdd`, `isZExt32Deferred`, call site
- `src/core/compiler/backend/railshot/arm64/compile.go` — `finalizePeepholes` calls, `uxtwAddEnabled`, `edgeScratch` field
- `src/core/encoder/arm64/asm2.go` + `asm2_test.go` — `AddExtUXTW` + golden
- docs: `arm64-wazero-gap-plan.md` (new "Results" section), `arm64-optimizations.md`,
  `arm64-isa-regressions.md`, `arm64-branch-fold-findings.md` (NEW)

**Pre-existing WIP — NOT mine, do not attribute to this pass:** `call.go`, `cc.go`,
`driver.go`, `localstate.go`, `stats.go`, `stats_test.go`, and the doc edits to
`amd64-arm64-backend-status.md`, `valent-blocks-expansion-plan.md`.

---

## 5. Pre-existing breakage (root-cause candidates)

Versus commit `9415112`, the uncommitted WIP regressed three modules in guard mode:

| Module | Symptom | Mode |
|---|---|---|
| `fannkuch.wasm.run` | **hangs** (infinite loop) — makes `make test-guard` time out ~11 min | guard |
| `sha256.wasm.hashN` | traps "linear memory access out of bounds" | guard |
| `spectralnorm.wasm.run` | returns `1000000000`, golden `1274222120` | **explicit** |

**Proof these are pre-existing, not from §1:** (a) they reproduce with all three
new flags off, which makes the new code byte-inert; (b) `git stash` → clean HEAD
passes `sha256` and `spectralnorm`. No feature flag toggles `spectralnorm` back
(`WAGO_NO_STFLAGS`, `WAGO_REG_MERGE=0`, `WAGO_NO_BOUNDS_FACTS`,
`WAGO_ARM64_NOREGABI` all still fail), so it's a WIP change with no A/B switch.

Repro one quickly:
```sh
cd bench
WAGO_BOUNDS=signals go test -tags wago_guardpage -run 'TestCorpusDifferential/spectralnorm' -timeout 30s .
WAGO_BOUNDS=signals go test -tags wago_guardpage -run 'TestCorpusDifferential/sha256'      -timeout 30s .
WAGO_BOUNDS=signals go test -tags wago_guardpage -run 'TestCorpusDifferential/fannkuch'    -timeout 30s .   # hangs → 30s timeout
```

**Recommended next work, in priority order:**
1. Root-cause the three WIP regressions above (they block acceptance).
   `fannkuch` (hang) and `spectralnorm` (explicit miscompile) first — bisect the
   dirty tracked files (`call.go`/`control.go`/`compile.go`/`emit.go`/`fuse.go`/
   `localstate.go`) against HEAD hunk by hunk.
2. Then resume the wazero-gap plan §1 (call/`call_indirect` dispatch — closes
   `dispatch`, `many_funcs`) and §5 (frame elision for frameless leaves — closes
   `tiny`; those rows stayed flat because they're call/frame-boundary bound, not
   branch bound).

---

## 6. Tooling gotchas

- **Disassembler.** Use a standalone `armdump` (`golang.org/x/arch/arm64/arm64asm`
  + `arm64.CompileModuleWith(m, CompileOptions{ElideBoundsChecks:true})`, iterate
  `cm.Entry`/`cm.InternalEntry`). **Do NOT add `x/arch` to the repo `go.mod`** — it
  bumps the `go` directive to 1.25 and breaks the `bench/` submodule (which
  `replace`s `../`), forcing `go mod tidy`. Build `armdump` as a separate throwaway
  module with `replace github.com/wago-org/wago => <repo>`. (A stray `go.sum` from
  a `go get` was removed this session; the repo has no tracked `go.sum` — its only
  dep is the local `wasi` replace.)
- The corpus internal entry is at `cm.InternalEntry[i]`; offset 0 is the host/
  wrapper adapter. The hot code is the internal entry.
- Background `go test` with output redirected buffers until the package finishes —
  a hang shows as an empty log, not a partial one. Prefer per-module runs with
  `-timeout`.

---

## 7. Pointers

- `docs/arm64-wazero-gap-plan.md` — the master plan + this pass's results.
- `docs/arm64-branch-fold-findings.md` — the design log for §1 (read this).
- `docs/arm64-optimizations.md` — optimization matrix (3 new rows).
- `docs/arm64-isa-regressions.md` — ISA queue + the ⚠️ baseline-red warning.
- `docs/no-ir-plan.md` — the `WAGO_EXPLAIN=1` / `cmd/explain` codegen-stats model.
