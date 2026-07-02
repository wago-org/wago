# WARP x64 Port Plan

## Summary

Port the remaining high-value WARP architecture into wago's x64 backend in
ordered, atomic stages. The goal is to close the json-as and call+memory gap by
moving from the current partial pinned-local/register model toward WARP's unified
storage model, then layering target hints, richer call ABI, and explicit-bounds
memory lowering on top.

Current baselines on this branch:

| Workload | wago x64 explicit | wago x64 guard | wazero | WARP |
|---|---:|---:|---:|---:|
| `memory_tree.run(8,24)` | ~17.5 us/op | ~11.95 us/op | ~21.1 us/op | ~8.55 us/op |
| json-as serialize | 256.2 ns/op | 239.7 ns/op | 145.7 ns/op | not in Go harness |
| json-as deserialize | 434.0 ns/op | 388.3 ns/op | 299.9 ns/op | not in Go harness |

## Key Changes

- No public wago API changes from the perf work itself (the cutover separately
  added one debug/profiling method, `Instance.CodeBase()`, for the perf-map tool).
- Internal x64 backend changes:
  - Replace side-band `localReg`/`localFReg`/`localState` behavior with WARP-like
    local storage states: `constantZero`, `register`, `stackReg`, `stack`.
  - Add occurrence/reference tracking for local refs, scratch regs, and spill
    slots so storage replacement does not require broad stack scans.
  - Add target-hinted materialization/condense APIs so values can be produced
    directly in desired registers.
  - Expand internal call ABI to WARP shape: mixed GP/FPR params, up to 4
    register params per class, up to 2 GP and 2 FPR register returns, stack
    fallback for the rest.
  - Revisit explicit-bounds lowering only after the allocator/target-hint work
    lands.

## Implementation Order

1. Persist plan and baseline.
   - Write this plan to `docs/warp-x64-port-plan.md`.
   - Record current benchmarks in commit notes: json-as explicit/guard,
     `memory_tree`, `fib_iter`, `fib_rec`, wazero, WARP.
   - Do not change codegen in this commit.
2. Add x64 occurrence tracking without behavior change.
   - Add internal reference keys for local refs, owned scratch regs, and spill
     slots.
   - Maintain reference heads on stack push, erase, storage replacement, spill,
     materialize, and deferred-load materialization.
   - Keep existing codegen output equivalent.
   - Add focused tests for replacing all local/reg occurrences and preserving
     deferred-tree semantics.
3. Replace pinned-local side band with WARP-style local defs.
   - Introduce `localDef` per wasm local: type, assigned register, storage state,
     stack slot.
   - Params start initialized in their assigned storage; declared locals start as
     `constantZero`.
   - `local.get` calls `recoverLocalToReg` semantics when the local has a
     register and is stack-only.
   - `local.set`/`local.tee` calls `prepareLocalForSetValue`, realizes
     outstanding local refs through occurrence tracking, then writes directly to
     the local's current storage.
   - Branch merge/reconcile converges assigned-register locals to `stackReg`.
   - Preserve the existing guard-mode call+memory heuristic unless new
     benchmarks prove it unnecessary.
4. Add target-hinted materialization and condense.
   - Add `materializeInto(e, target, writable)` and `condense(e, target)`
     behavior equivalent to WARP's `liftToRegInPlaceProt`.
   - Reuse an existing register when safe; otherwise prefer the target hint
     before allocating a fresh scratch.
   - Apply target hints to local.set, returns, select, comparisons, memory load
     result registers, and call params.
   - Keep existing non-hinted paths as fallback during the transition.
5. Port WARP call parameter/result resolver.
   - Replace direct-call argument setup with a copy resolver for GP and FPR
     params.
   - Support mixed int/float register params and stack fallback.
   - Support 2 GP and 2 FPR register results, with stack fallback for additional
     results.
   - Keep wrapper ABI for exports/host boundary; use richer ABI only for
     wasm-to-wasm internal calls.
   - Extend differential coverage with mixed int/float, multi-result,
     stack-param, and indirect-call cases.
6. Port explicit-bounds memory lowering.
   - After steps 3-5 are benchmarked, reintroduce WARP-style explicit bounds:
     cache `curBytes - 8` only if benchmarks show the allocator can absorb the
     reserved register; fold static offsets into the address before checking;
     compare address against cached limit for object sizes up to 8; keep the
     current explicit path as fallback behind an A/B env toggle during
     validation.
   - Add constant-address and large-offset tests.
7. Bulk memory and cleanup.
   - Port WARP const-size `memory.copy`/`memory.fill` specializations where they
     apply.
   - Remove obsolete A/B toggles only after replacement paths are stable.
   - Update comments/docs to describe the final x64 storage and call ABI model.

## Test Plan

- Correctness after every stage:
  - `go test ./...`
  - `cd bench && go test . -count=1`
  - x64 differential corpus, including traps and guard-page tests.
- Required focused tests:
  - local storage state transitions;
  - reference replacement for local aliases and register aliases;
  - branch reconcile states;
  - mixed GP/FPR direct calls;
  - multi-result direct calls;
  - explicit-bounds OOB traps after memory lowering.
- Benchmarks after stages 1, 3, 4, 5, and 6:
  - json-as explicit and guard;
  - `memory_tree.run(8,24)` explicit and guard;
  - `fib_iter`, `fib_rec`, `call_overhead`;
  - wazero and WARP comparison for json-as and memory_tree.
- Acceptance:
  - No correctness regressions.
  - No sustained >3% regression on json-as guard, memory_tree guard, or fib_rec
    unless the same commit has a larger targeted win and the regression is
    documented.
  - Do not keep cached `memSize` if it repeats the earlier measured regression.

## Assumptions

- Work happens on the current `port/warp-x64` branch/worktree.
- Commits stay atomic and include proof per `skills/commit/SKILL.md`.
- Big refactors are allowed, but each stage must leave the repo buildable and
  testable.
- Guard-page mode remains opt-in via build tag/env; no public API change is
  planned.
