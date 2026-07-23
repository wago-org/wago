# Wago → WARP-Topology Redesign Plan

Status: superseded; retained as design history. The current direction shares only
measured architecture-neutral orchestration and keeps direct per-architecture
lowering rather than adopting this wholesale topology.

## Context

Wago's railshot backend is already a port of WARP's valent-block codegen core, but its *pipeline topology* is not: Wago materializes a whole decoded `wasm.Module` AST, then runs ~4 separate body walks (validate → support pass → hints → codegen), duplicated across two ~17k-line per-arch driver mirrors. WARP instead has **one shared frontend** (streamed sections, decode-each-instruction-once, validation fused into the same walk) driving **thin per-arch backends**, with **no module AST** (compact offset tables) and **two bounded memory pools** (scratch slabs + output).

Measured consequence (compile-only peak RSS, darwin/arm64, min of 3): Wago is 3.4–4.8× WARP on real modules (ruby 168.5 vs 50.2 MB, esbuild 150.3 vs 36.4 MB) but ~1.2× on synthetic deep-stack modules — proving the gap is module materialization + walk duplication + GC headroom, **not** the codegen algorithm. The user directive: adopt WARP's architecture wholesale ("HUGE REFACTOR"), fresh start from `main`, using branch `jairus/streaming-everything` (CompileReader spool, CodeArena, CompileLimits, compact link artifact — all already written) as a parts bin.

**Key session facts the plan builds on** (verified, with citations in session):
- `bodyLoop` and `pushBinOp` are byte-identical between `railshot/amd64` and `railshot/arm64`; control/driver files are line-for-line structural twins (~17,251 duplicated arm64 lines / 28 mirrored files). Arch-dependent surface = `f.a.*` encodings, regalloc, `cc.go` ABI, condition mapping only.
- At the time, `hostLinkCache` was the only post-compile `wasm.Module` consumer. The production path now removes that cache and link-time recompilation entirely in favor of per-instance dispatch cells.
- WARP fuses validation via a `ValidationStack` running beside the operand stack in one decode loop (warp `Frontend.cpp:1290+`); holds output in ONE buffer with forward-call chains threaded through it; scratch is slab-reset per function.
- Each railshot backend is `//go:build <arch>`-gated (one arch per binary), so a concrete type alias — not a Go interface — can bind frontend→backend with zero dispatch cost.

## Target architecture

```
io.Reader / []byte
  → strict streamed section decoder (from streaming branch spool)
      → modmeta: compact tables (types/imports/funcs/tables/mems/globals/
        exports/segments/names) — NO wasm.Module AST on the compile path
  → railshot shared frontend (ONE copy of driver/control/stack/dispatch):
      Walk 1 (fused): decode once → validation stack + feature gating
                      + BodySummary (hints/inline/pin facts)
      Walk 2 (replay): same driver, validation off, full codegen
                      (keeps inlining, module-global pins, loop versioning)
  → per-arch backend (emit/regalloc/ABI only), bound via build-tagged
    `type backend = amd64.Backend` alias
  → CodeArena output pool (RW mmap → Seal RX; W^X kept, unlike WARP)
  → scratch pool: slab arenas reset per function (streaming-branch slabs)
  → CompileLimits budgets enforced at every seam
```

Adopt from WARP: shared frontend, no AST, fused validation, decode-once, two pools, bounded scratch. Keep from Wago (reject WARP's version): W^X sealed code, full feature set (SIMD/GC/reftypes/bulk/imported mems+globals), real linking (no trap stubs), serialization, guard-page bounds, inlining + module-global pinning (via the 2-walk replay profile — pure 1-walk would forfeit them).

## Phases (each lands green; ~10–13 engineer-weeks total)

### Phase 0 — Gates & harness (0.5 wk)
- Corpus-wide **byte-diff golden gate**: SHA-256 of native code + metadata per arch, per corpus module (spec1/spec2 sets + ruby/esbuild/lua/markdown/regexmatch), baselines checked in.
- **RSS gate** (productionize this session's compile-only peak-RSS harness) + **alloc gate** (`-benchmem` budgets) with per-module budget tables in CI; initial budgets = current +5%.
- Existing spec1/spec2 + corpus differential wired as required checks.
- Exit: all gates green on unmodified main.

### Phase 1 — Frontend/backend carve (3–4 wks; biggest phase)
Extract arch-independent driver into a shared frontend; delete the arm64 mirror.
- New leaf pkg `railshot/rscore`: shared types (`elem`, operand `stack`, control frames, `machineType`, `Reg`/`regMask`, localDef/pins, trap kinds, BodySummary).
- `railshot` root = the **frontend**: `bodyLoop`, control ops, condense/stack policy, the ~150-arm `emitPlain` + `emitFC` + 286-arm `emitFD` dispatch (exactly once), hints/inline/peephole *decisions*, `CompileModuleWith` driver. Backend bound per-GOARCH via `arch_{amd64,arm64}.go` `type backend = amd64.Backend` (concrete alias → direct inlinable calls; no interface on the per-instruction path).
- `railshot/{amd64,arm64}` gutted to **backends**: `emit.go`, `regalloc/regcopy/regmask`, `cc.go` ABI, cond mapping, fp/simd lowering leaves, peephole *implementations*, frame layout, thunks, `table.go`.
- `fn` splits: frontend fn (operand/ctrl stacks, meta, locals/hints) embeds pointer to backend struct (encoder, reg users, pin masks, const pools, frame patch sites).
- Procedure = family-at-a-time steps (driver → control → stack → emitPlain clusters → FC → FD → hints/inline), **amd64 golden hashes unchanged after every step**; then port arm64 onto the frontend and delete its ~17k mirror lines, **arm64 hashes also unchanged** (divergences triaged + explicitly re-baselined, never silently absorbed).
- Exit: byte-identical output both arches; spec zero-fail; compile throughput within 3%; railshot net −15k LOC.

### Phase 2 — modmeta + streamed input: kill the compile-path AST (2–3 wks)
- New pkg `src/core/compiler/modmeta`: compact tables (canonical type table feeding existing GC-desc machinery; `funcs []FuncMeta{typeIdx, bodyOff, bodyLen}`; import/table/mem/global summaries; export/segment/name tables; feature bits). Bodies never become `Expr` trees.
- Harvest from streaming branch: `compile_stream_unix.go` section spool (32KiB window, per-section short-lived mappings, custom-section drain), CompileLimits plumbing, product-owned data copies.
- Rewire `src/wago/api.go compileDecodedModule` (~line 155): stream → modmeta → header validation during section decode → `railshot.CompileModuleWith(meta, opts)`. Body validation temporarily stays as the existing byte-backed walk (`validate_bytebacked.go`) until Phase 3. Walks: 4 → 3.
- `wasm.DecodeModule`/AST **kept** for tools/tests only (cli meta cmd, bench harnesses, fuzzers). The 12 programmatic-AST test files get a `wasmtest.EncodeModule(m) []byte` adapter (existing `encode.go` machinery is ~80% there) instead of rewrites.
- Exit: spec/differential green; golden hashes unchanged; RSS ratchet: esbuild ≤ 100 MB, ruby ≤ 120 MB; chunk-boundary/short-read fuzz ported and passing.

### Phase 3 — Fused validation + 2-pass replay (2–3 wks)
- **Walk 1 (fused, per instruction, decode ONCE):** validation-stack transition (types, control typing, feature gating — errors at decode point, spec error-precedence preserved via table-driven tests) + BodySummary accumulation (hotness, loop eligibility, inline candidacy + bounded replay copies, pin facts). Subsumes `ValidateModule` body pass, `RejectUnsupported*`, and `computeModuleHints` entirely.
- **Walk 2 (replay):** same frontend driver, validation off, full-quality codegen (inlining/module pins/loop versioning intact). Structure: walk-1 all bodies → module aggregation → walk-2 all bodies.
- Legacy byte-backed validator becomes the differential reference (fused-vs-legacy verdict fuzzing) before removal from the product path.
- **Decision (user-confirmed):** 2-pass replay is the default *and only* profile in this redesign — a pure 1-walk would forfeit auto-inlining, module-global register pinning, and immutable-table monomorphization (they need whole-module body knowledge). The 1-walk "one-way" profile is a non-goal; the seam is left open (walk-1/walk-2 share the driver behind a `mode` flag) for a future config knob.
- Exit: spec zero-fail incl. error positions; validator differential fuzz clean; **golden hashes unchanged** (proves BodySummary hint-parity); ≥10% compile-throughput win on esbuild.

### Phase 4 — Memory model: two pools + budgets (1.5–2 wks; overlaps P3)
- Scratch pool: per-compile slab arena reset per function; harvest streaming-branch slabs (operand nodes, control-frame scratch, pin-only local snapshots, fixed trap-site lists, top-k pin selection, reloc prealloc — already measured: ruby 594→235 MiB alloc traffic).
- Output pool: CodeArena default via `WithSealedCode` (RW mmap → Seal RX); heap path kept for non-Unix; native-code budget checked before every growth.
- `RuntimeConfig.WithCompileLimits` public (input/body/native-code/retained-data) + `codegen.LimitError`. Library does **not** mutate GOGC/GOMEMLIMIT (embedder-hostile); document `SetMemoryLimit` guidance + `Compiled.Footprint`.
- Exit RSS budgets: markdown ≤ 6, lua ≤ 6.5, regexmatch ≤ 10, esbuild ≤ 75, **ruby ≤ 90 required / 70 stretch** (vs WARP 50.2; residual = Go runtime + feature tables, accepted). Golden unchanged; exec benchmarks within noise.

### Phase 5 — Linking without raw source (1 wk)
- Port streaming branch's compact link artifact + file-backed `linkBodyStore`; link-time recompile = walk-2 replay with bindings (walk-1 results retained, validation skipped). Port shared-image dynamic binding for instance-export imports. Trap-stub linking rejected.
- Exit: hostlink/cross-instance suites green; retained-data budget covers body store.

### Phase 6 — Cleanup & reconciliation (0.5–1 wk)
- Delete AST fallbacks in validation/hints/inline; document `wasm` pkg as tools/tests layer.
- **PR #269**: close with per-optimization mapping to superseding phases; cherry-pick orthogonal encoder wins separately.
- **jairus/streaming-everything**: close once its parts re-land as clean PRs (P2/P4/P5); keep for archaeology. `docs/streaming-singlepass-plan.md` → superseded-by pointer; write `docs/architecture-frontend-backend.md`.
- Final CI budget ratchet to achieved numbers.

## Delivery strategy (user decision: 5 sequential PRs)

Five sequential PRs to `main`, one per phase group, each on its own branch off the then-current `main` and merged before the next starts:

| PR | Contents | Approx size |
|---|---|---|
| **PR 1** | Phase 0: gates (golden byte-diff harness, RSS/alloc budget tables, CI wiring) | small |
| **PR 2** | Phase 1: frontend/backend carve + arm64 dedup | very large (~15–20k lines moved/deleted) |
| **PR 3** | Phase 2: modmeta + streamed input (AST off the compile path) | large |
| **PR 4** | Phases 3+4: fused validation walk + two-pool memory model & budgets | large |
| **PR 5** | Phases 5+6: link artifact + cleanup/reconciliation (#269, streaming branch, docs) | medium |

To keep the big PRs reviewable and bisectable:
- Within each PR, every extraction cluster / logical step stays **one commit**, and the golden gate + spec suites are run per commit locally — a regression is pinpointed by `git bisect` against the gate even inside PR 2.
- Each PR's description records its gate results (golden hashes unchanged, spec pass counts, RSS/alloc numbers) so review is against measurable claims.
- If `main` moves while a PR is open, rebase and re-run the full gate before merge.

## Top risks & mitigations
1. **Byte-identity fails mid-carve** (hidden amd64/arm64 policy drift) → family-at-a-time steps + per-step golden gate; divergences explicitly re-baselined with justification.
2. **Fused-validation error precedence/spec regressions** → table-driven precedence tests + fused-vs-legacy verdict fuzzing before deleting legacy path.
3. **BodySummary hint parity** (silent exec-perf loss) → golden hashes double as proof: walk-2 output must be byte-identical to Phase-2 output.
4. **RSS targets missed on Go GC floor** → alloc-gate ratchets per phase; accept documented ~1.4–1.8× WARP floor rather than GOGC hacks.
5. **Backend surface balloons** → concrete alias makes 300+ methods cheap; per-family doc-assertion interfaces; the arm64 port is the forcing function proving surface completeness.

## Non-goals
One-way 1-walk profile (seam left open behind a future config knob); WARP's non-W^X mutable output; trap-stub linking; dropping any feature; new arches; rewriting encoders/runtime/instantiate; public API breaks (additive only).

## Verification
- Per-PR: corpus byte-diff golden gate (both arches), spec1 (629 modules) + spec2 (1600) zero-fail, corpus differential execution, RSS + alloc budget tables in CI.
- Per-phase exits as listed; Phase 3 adds 24h validator-differential fuzz; Phase 4 re-runs the WARP head-to-head RSS harness (this session's methodology) and updates the comparison table.
- End state: ruby compile ≤ 90 MB peak RSS (from 168.5), walks 4 → 2, one shared frontend (−~15k duplicated LOC), all features and exec performance preserved.

## Critical files
- `src/core/compiler/backend/railshot/amd64/driver.go` (bodyLoop:25, emitPlain:67) — extraction source
- `src/core/compiler/backend/railshot/amd64/compile.go` (`fn` struct :119) — the carve line
- `src/wago/api.go` (`compileDecodedModule` :155; link recompiles :810/:875) — pipeline rewiring
- `src/core/compiler/wasm/validate_bytebacked.go` — fused-validator reference implementation
- Branch `jairus/streaming-everything`: `src/core/compiler/wasm/compile_stream_unix.go`, `src/core/runtime/code_arena_unix.go`, CompileLimits, compact link artifact — the parts bin
