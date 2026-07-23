# ARM64 parity roadmap

Status date: 2026-07-10
Primary target: Linux/arm64
Secondary target: Darwin/arm64
Implementation branch: `jairus/arm64-runtime-perf`

## Goal

Make ARM64 a first-class Wago target with the same correctness, WebAssembly feature admission, public API, product behavior, operational safety, and release confidence as Linux/amd64. Performance parity means matching or beating the fastest relevant reference on representative workloads where the APIs and semantics are comparable, while preserving Wago's compile-time and memory-footprint priorities. It does not mean mechanically reproducing x86 instruction sequences.

Parity is reached only when all of these are true:

- Linux/arm64 passes the same applicable WebAssembly 1.0, WebAssembly 2.0, SIMD, runtime, codec, snapshot, host-call, cross-instance, and lifecycle suites as Linux/amd64.
- Darwin/arm64 passes the same architecture-neutral runtime/API suites and its platform-specific memory, signal, and MAP_JIT tests.
- `SupportedFeatures()` and compile-time rejection behavior are honest per target; ARM64 does not advertise a feature whose backend or runtime is incomplete.
- Explicit-bounds and guard-page modes have the same observable wasm semantics.
- Standard Go builds are release-ready on both ARM64 operating systems. TinyGo support is either green and documented or explicitly excluded with a clean build-time error.
- ARM64 has permanent CI, corpus, conformance, fuzz/differential, performance, code-size, allocation, and compile-memory gates.
- Public documentation no longer describes ARM64 as planned or unsupported.

## Starting point

This is an integration and closeout project, not a fresh port. The ARM64 branch already contains approximately 28,000 added lines across the encoder, compiler, runtime, tests, corpus tooling, benchmarks, and documentation. Its proven work includes:

- a native AArch64 instruction encoder with golden tests;
- a full railshot ARM64 backend organized alongside an architecture-specific amd64 backend;
- symbolic-stack compilation, register allocation, spills, register merging, constant folding, compare/branch fusion, calls, memory, globals, tables, bulk memory, scalar floating point, and broad NEON lowering;
- no-cgo native entry, return, trap, and synchronous host-call re-entry;
- Linux and Darwin executable mappings;
- Linux and Darwin guard-page implementations with native signal handling;
- architecture selectors in `src/wago`;
- explicit and guard-page corpus runners;
- Darwin/ARM64 CI coverage;
- correctness fixes for the earlier fannkuch, sha256, and spectralnorm regressions;
- measured host-entry, indirect-call, branch, global, and memory-tree optimizations.

At the branch's execution-gap closeout, the root suite and guarded corpus passed on Apple Silicon. Representative guard-page medians versus wazero included approximately equal tiny/dispatch costs, a 39% memory-tree win, a 14% globals win, and a small remaining branch-classification gap dominated by Wago's host-entry floor.

The remaining risk comes from integrating that work with the newer WebAssembly 2.0 mainline. A trial merge exposed concrete ARM64 gaps in reference-global lowering and nonzero-table `call_indirect`, plus amd64-only test-helper/build-tag assumptions. Those are the first semantic closeout items after the branch is made reviewable.

## Engineering constraints

- Correct wasm semantics come before performance.
- Malformed modules and unsupported features must be rejected explicitly.
- Keep the backend single-pass and analysis memory bounded.
- Do not introduce cgo.
- Do not use X28 for wasm state; Go owns it as `g` on ARM64.
- Keep platform signal, mmap/MAP_JIT, stack, trap, and host re-entry boundaries small and auditable.
- New optimizations need an A/B kill switch while being evaluated, focused regression tests, disassembly evidence, and before/after measurements.
- Avoid permanent duplication where a small neutral core is genuinely shared, but do not force ISA-specific behavior through abstractions that obscure code generation.
- Preserve Linux/amd64 behavior byte-for-byte where architecture separation is intended to be mechanical.
- Preserve Wago's zero-allocation hot-call target and bounded instance/runtime memory.

## Workstream 0: freeze the baseline and define the scorecard

Before restructuring or merging code, record one reproducible baseline for `main` and `origin/jairus/arm64`.

Deliverables:

- Pin the exact Go, TinyGo, WABT, Linux kernel/QEMU, Apple hardware, and benchmark tool versions.
- Record explicit and guard-page results for the root suite, `src/wago`, `src/core/runtime`, encoder/backend tests, corpus runner, MVP suite, Release 2 suite, and SIMD suite.
- Save compile-time, code-size, allocation, instantiate-memory, and execution medians for the standard benchmark matrix.
- Record all ARM64 kill switches and whether each is intended as temporary evaluation scaffolding or a permanent diagnostic control.
- Create a parity dashboard with one row per gate in this document and links to logs/artifacts.

Exit gate:

- Every later regression can be attributed to a specific phase rather than to an unknown branch baseline.

## Workstream 1: turn the branch into a reviewable integration stack

The current branch changes nearly 200 files and mixes mechanical package separation, new architecture code, runtime ports, correctness fixes, test infrastructure, and optimizations. Do not land it as one opaque backend replacement.

Recommended PR sequence:

1. Mechanical backend namespace split: move current amd64 railshot sources into `railshot/amd64`, add architecture-neutral shared types only where necessary, and prove identical amd64 output/tests.
2. ARM64 encoder and encoder golden tests.
3. ARM64 compiler baseline: integer/control/calls/memory/scalar-float with explicit bounds.
4. Linux/arm64 runtime: mappings, foreign stack, trampoline, traps, host re-entry, memory, and lifecycle.
5. `src/wago` architecture selectors and architecture-neutral API wiring.
6. ARM64 tables, globals, references, bulk memory, and NEON.
7. Linux guard pages.
8. Darwin explicit execution, MAP_JIT, and guard pages.
9. Corpus/conformance/benchmark infrastructure and CI.
10. Correctness fixes and performance changes, each independently measurable.

Each PR must remain buildable on Linux/amd64 and the ARM64 target it introduces. Mechanical amd64 moves should contain no semantic changes.

Exit gate:

- The ARM64 integration can be reviewed, bisected, reverted, and backported by subsystem.
- Linux/amd64 generated-code and benchmark baselines show no unexplained regression.

## Workstream 2: establish a stable architecture contract

Promote the ARM64 branch's port contract into maintained developer documentation and encode its invariants as tests.

Required contract:

- X26: linear-memory/basedata base.
- X27: explicit-bounds memory-size cache when applicable.
- X25/X24/X23: module-pinned globals subject to allocator policy.
- X19-X23 and V8-V11: callee-saved pinned local pools.
- X15/V15: merge scratch/value locations.
- X16/X17: fixed backend scratch registers, never long-lived wasm values.
- X18: platform-reserved.
- X28: Go `g`, never allocatable.
- X29/X30/SP: frame pointer, link register, and stack.
- Wrapper entry: X0 arguments, X1 linear-memory base, X2 trap cell, X3 results.
- Internal register ABI: X0-X7 and V0-V7 arguments, X0/V0 result, with an explicit rule for multi-result fallback.
- One canonical basedata layout shared by amd64 and ARM64, with compile-time and runtime assertions for every offset used by assembly.
- One canonical trap continuation and host-control-frame layout per supported ABI.

Add tests that fail if assembly constants, Go constants, backend register masks, trampoline save areas, stack alignment, or basedata offsets drift.

Exit gate:

- No ABI rule exists only in comments or an ignored porting directory.
- Async preemption, GC pressure, recursion, traps, and returning host calls pass stress tests without corrupting `g`, SP, LR, callee-saved GP registers, or NEON state.

## Workstream 3: close WebAssembly 2.0 semantic gaps

This is the critical path after rebasing onto current `main`.

### Reference globals

Implement and test ARM64 `global.get`/`global.set` for `funcref` and `externref`, including:

- local, imported, exported, mutable, and immutable forms;
- imported immutable global initializers;
- exact type and mutability checks;
- token/descriptor translation at public and cross-instance boundaries;
- explicit same-store ownership requirements;
- close-order and producer-retention behavior;
- codec-v21 load/round-trip execution.

### Multiple tables and typed references

Complete indexed table lowering for every admitted table index and reference element type:

- `table.get`, `table.set`, `table.size`, `table.grow`, `table.fill`, `table.copy`, `table.init`, and `elem.drop`;
- nonzero-table `call_indirect` with bounds, null, and exact signature checks;
- local, imported, exported, re-exported, aliased, and mixed funcref/externref tables;
- active, passive, declarative, expression-based, and non-null table initializers;
- exact min/max and declared-maximum import compatibility;
- atomic failure/no-partial-mutation guarantees;
- shared-table producer lifetime and store compatibility.

Retain the internal-entry fast path only when immutability, locality, signature, and ownership are proven. All other cases must use the safe wrapper/context-switch path.

### Owned host funcrefs

Finish and prove the ARM64 active-caller control-frame thunk for explicitly owned host funcrefs. Cover same-store cross-instance invocation, returning results, traps, owner teardown, descriptor retention, and rejection across stores.

### Multi-value and reference combinations

Run mixed scalar, SIMD, funcref, and externref parameters/results through direct calls, indirect calls, host calls, cross-instance calls, public `Invoke`, typed `Call`, and compiled-artifact reload. Use the wrapper ABI for shapes not admitted by the internal register ABI.

### Feature reporting

Split frontend feature completeness from target admission. `CoreFeaturesV2` may describe the release family, but `SupportedFeatures()` must expose only what the current GOOS/GOARCH and CPU can execute. Add tests comparing admission, rejection messages, codec required-feature bits, and actual execution.

Exit gate:

- The official Release 2 suite passes on Linux/arm64 with the same applicable module/assertion count as amd64.
- Darwin/arm64 passes the architecture-neutral Release 2 API/codec/reference tests.
- No ARM64-specific skip hides a feature advertised by `SupportedFeatures()`.

## Workstream 4: finish SIMD/NEON parity

Treat correctness and performance as separate gates.

Correctness tasks:

- Remove the remaining ARM64 SIMD corpus skips, including packed rounding modules.
- Verify every core SIMD opcode admitted on amd64 has either an ARM64 lowering or a target-specific compile rejection.
- Verify deterministic relaxed-SIMD choices match Wago's documented policy.
- Exhaustively cover NaN payload handling where observable, signed zero, saturation boundaries, unsigned conversions, lane ordering, high-half selection, shuffle indices, and narrowing/extension corners.
- Test SIMD through locals, globals, spills, control merges, tables where applicable, host calls, cross-instance calls, public APIs, and codec reload.

Performance tasks:

- Evaluate vector f64x2 saturating conversions only if a compact sequence preserves wasm edge behavior.
- Replace scalar packed min/max fixups only with a proven NaN/signed-zero-correct NEON sequence.
- Tune load-extend, narrow, shuffle/swizzle, bitmask, dot, q15, and demote/promote sequences against disassembly and real hardware.
- Measure NEON register pressure and spill behavior under mixed scalar/vector functions.

Exit gate:

- The ARM64 SIMD corpus has zero unexplained skips and zero assertion failures.
- Opcode admission/lowering parity is machine-checked against the amd64 matrix.
- Optimized forms never replace a known-correct scalar fallback without focused equivalence tests.

## Workstream 5: runtime and operating-system hardening

### Linux/arm64

- Test executable mappings, W^X transitions, arena alignment, foreign-stack switching, signal masks, async preemption, GC stress, and memory reuse on native hardware and QEMU.
- Verify guard-page ucontext offsets against supported libc/kernel combinations.
- Verify lazy page commit, memory growth, OOB traps, unrelated fault forwarding, nested runtime use, and concurrent instances.
- Run under race-enabled host tests where possible; native code itself remains outside race instrumentation.

### Darwin/arm64

- Keep MAP_JIT usage and write/execute transitions compatible with hardened runtime expectations.
- Verify SIGSEGV/SIGBUS handler installation, chaining, restoration, parallel faults, `GOMAXPROCS=1`, pointer-auth state, and unrelated Go faults.
- Test repeated runtime construction/destruction and interaction with applications that install their own signal handlers.
- Document supported macOS versions and Apple Silicon generations.

### Host calls and cross-instance execution

- Stress returning host calls at maximum supported arity, nested wasm→host→wasm re-entry, traps during re-entry, and instance closure races.
- Keep the synchronous dispatcher as the correctness baseline. Port or retire the legacy async host-log path deliberately; do not maintain an accidental semantic split.
- Verify imported memory refreshes stack fences and basedata context correctly on every entry.

Exit gate:

- Runtime stress tests pass for hours under GC, preemption, concurrency, memory growth, traps, and host re-entry.
- Signal-handler behavior is documented as an operational constraint and has restoration/chaining tests.

## Workstream 6: remove build-tag and test-infrastructure blind spots

Many current tests and helpers are tagged `linux && amd64`, sometimes because they execute x86 code and sometimes only because they were colocated with x86 helpers. Audit every such constraint.

Classify each test as:

- architecture-neutral and should run everywhere;
- backend-contract and should have amd64 and ARM64 versions;
- operating-system-specific;
- amd64 instruction-selection-specific;
- unsupported on ARM64 with a documented reason.

Move shared wasm fixture builders out of architecture-tagged files. Do not duplicate helpers merely to make Darwin compile. Give ARM64 focused equivalents for allocator pressure, register merge, constant folding, bounds facts, loop prechecks, inline decisions, magic division, calls, tables, globals, SIMD, and golden disassembly.

Exit gate:

- `go test ./...` builds and passes natively on Linux/amd64, Linux/arm64, and Darwin/arm64.
- Cross-compilation compile checks cover all supported GOOS/GOARCH/tag combinations.
- A skipped test always reports a capability/tool reason, never just an architecture name.

## Workstream 7: conformance, differential, and fuzz gates

Required permanent gates:

- WebAssembly 1.0 official suite on Linux/arm64.
- WebAssembly 2.0 official suite on Linux/arm64.
- SIMD and relaxed-SIMD proposal suites on Linux/arm64.
- Explicit-versus-guard corpus differential on Linux/arm64 and Darwin/arm64.
- Result/trap differential against wazero for generated modules.
- Bytecode validation differential independent of the backend.
- Architecture differential: run the same generated module/inputs on amd64 and ARM64 and compare results, traps, memory, globals, and tables.
- Fuzz seeds for every ARM64 bug fixed during the port.
- Watchdog-isolated corpus processes so a native-code hang cannot stall the whole job.

Track module counts, assertion counts, skips, timeouts, crashes, and mismatches as structured artifacts. A count decrease fails CI even if remaining tests pass.

Exit gate:

- ARM64 has no unexplained conformance-count delta from amd64.
- Corpus hangs, crashes, and wrong results are isolated and reproducible from saved inputs.

## Workstream 8: performance and footprint parity

Optimize only after semantic and runtime gates are green.

### Scorecard

Measure at least:

- public cached `Invoke` floor;
- raw wrapper and internal-entry calls;
- direct, recursive, host, cross-instance, and indirect calls;
- branch-heavy and `br_table` workloads;
- globals and pinned locals;
- sequential, dependent, random, and growing memory workloads;
- bulk copy/fill/init;
- scalar float and conversions;
- SIMD kernels;
- instantiate, snapshot reset, class reuse, compile, validate, and codec load;
- code bytes/function, compile allocations, peak compile memory, instance arena bytes, executable mappings, and guarded reservation size.

Compare Wago ARM64 with Wago amd64 by normalized mechanism and with wazero/WARP on the same ARM64 hardware where possible. Keep end-to-end API cost separate from generated body cost.

### Remaining likely opportunities

- Reduce the host-entry floor without allocating or weakening trap/lifetime checks.
- Compact safe same-instance `call_indirect` dispatch further.
- Improve branch classification only after subtracting entry overhead.
- Extend bounded store-to-load forwarding without recreating register-exhaustion failures.
- Improve recursive call live-state preservation.
- Tune bulk-memory NEON chunking on real cores.
- Expand destination propagation and conditional-select sinking where one-pass cost gates prove a win.
- Share neutral magic-number, stats, and selected analysis code only where it reduces drift without introducing interface overhead.

Acceptance rules:

- No optimization lands on a red correctness baseline.
- Five-sample medians with min/max are the minimum for noisy rows.
- Protect existing wins in memory-tree, sieve, globals, linked lists, calls, and local access.
- Compile-time regression budget is at most 10% per change and 30% cumulative for the ARM64 backend unless a larger cost is explicitly justified with runtime data.
- Hot invocation remains zero-allocation.
- Instance and compiler memory deltas are reported in bytes, not adjectives.

Exit gate:

- ARM64 is within 10% of the fastest relevant reference on entry-dominated rows or has a documented safety/API reason.
- Wago matches or beats the fastest reference on the representative body-dominated rows selected for release.
- No material regression exists in compile time, code size, allocations, or instance footprint versus the frozen baseline.

## Workstream 9: CI and release qualification

Required CI matrix:

| Target | Standard tests | Guard pages | Spec 1 | Spec 2 | SIMD | Corpus | TinyGo | Bench smoke |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Linux/amd64 | yes | yes | yes | yes | yes | yes | yes | yes |
| Linux/arm64 native | yes | yes | yes | yes | yes | yes | decide/declare | yes |
| Darwin/arm64 native | yes | yes | targeted | targeted | targeted | yes | decide/declare | yes |
| Linux/arm64 QEMU | compile/smoke | smoke | no | no | no | small | no | no |

Use native Linux ARM64 runners for release gates; QEMU is useful for compiler/ABI smoke tests but is not a performance oracle and should not be the only correctness environment.

Release checklist:

- clean checkout builds with Go 1.22 minimum and current supported Go;
- all matrix gates green twice from clean caches;
- no unexplained skips or conformance-count deltas;
- race-free host-side lifecycle tests;
- 24-hour runtime/corpus stress soak;
- benchmark and footprint report checked in;
- supported OS/CPU baseline and signal-handler caveats documented;
- installation and release artifacts produced for ARM64;
- rollback/disable path documented if target-specific regressions appear.

Exit gate:

- ARM64 is a required branch-protection check, not an optional informational job.

## Workstream 10: documentation and maintenance

Update `README.md`, `FEATURES.md`, `ROADMAP.md`, `ARCHITECTURE.md`, benchmark docs, TinyGo docs, and release notes together when ARM64 support changes.

Documentation must state:

- exact supported GOOS/GOARCH pairs;
- minimum CPU/ISA assumptions (AArch64 + NEON baseline and any later optional extensions);
- explicit and guard-page availability;
- signal-handler operational impact;
- TinyGo status;
- per-target supported WebAssembly features;
- benchmark hardware/tool versions;
- known non-goals and target-specific limitations.

After the port stabilizes, delete superseded beachhead/spike code and fold useful design records into maintained docs. Keep a short ARM64 backend-status matrix generated or reviewed alongside amd64 optimizer changes so parity does not decay.

Exit gate:

- A new backend, runtime, or optimizer change cannot silently land for amd64 only without updating or explicitly waiving the ARM64 parity row.

## Milestones

### M0 — reproducible baseline

Scorecard, tools, logs, and branch state are frozen.

### M1 — reviewable backend/runtime stack

Mechanical amd64 separation, encoder, compiler, runtime, and selectors are split into bisectable PRs; amd64 remains unchanged.

### M2 — ARM64 MVP

Linux/arm64 passes all WebAssembly 1.0 semantics, runtime/API tests, explicit bounds, host calls, cross-instance calls, and corpus gates.

### M3 — ARM64 WebAssembly 2.0

Reference globals, multiple typed tables, owned host funcrefs, multi-value/reference combinations, bulk memory, codec v21, snapshots, and Release 2 conformance match amd64.

### M4 — SIMD and guard-page closeout

NEON has zero unexplained corpus skips; Linux and Darwin guard-page modes pass stress and differential gates.

### M5 — performance and footprint parity

Release scorecard meets the stated runtime, compile-time, allocation, code-size, and memory thresholds on native hardware.

### M6 — first-class release target

Linux/arm64 and Darwin/arm64 are documented, packaged, protected by required CI, and included in release qualification.

## Recommended immediate next actions

1. Preserve `origin/jairus/arm64` as an immutable reference tag before rewriting integration history.
2. Generate the M0 scorecard from clean `main` and ARM64 branch checkouts.
3. Extract the mechanical amd64 package split as the first PR and prove generated-code identity.
4. Rebase the ARM64 stack onto current WebAssembly 2.0 `main` in the planned PR sequence.
5. Add failing ARM64 tests for funcref/externref globals and nonzero-table `call_indirect` before implementing those gaps.
6. Move architecture-neutral wasm fixture builders out of `linux && amd64` test files.
7. Stand up a native Linux/arm64 CI runner before claiming Linux support.
8. Keep Darwin/arm64 native CI as a required early-warning gate throughout the integration.
