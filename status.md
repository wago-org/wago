# Repository Status

Status date: 2026-07-10
Branch: `jairus/arm64-runtime-perf`
Commit: `9ac253b` (`docs: refresh arm64 acceptance status`)
Upstream: local branch is 14 commits ahead of `origin/jairus/simd-perf` before publication under its new name.
Scope: the primary repository only. Submodule contents and submodule working-tree state are excluded.

## Executive summary

Wago is a pure-Go, no-cgo, single-pass WebAssembly JIT focused on low compile latency, low host-call overhead, predictable memory use, and small deployments. The supported production target is currently `linux/amd64` on a modern x86-64 baseline (SSSE3/SSE4.1 plus AVX/VEX.128). It is JIT-only: there is no interpreter or optimizing execution tier.

The repository is at a strong semantic milestone:

- WebAssembly 1.0 support is complete against the pinned MVP suite.
- WebAssembly 2.0 runtime/product support is recorded as complete on the supported target, including reference types, multiple tables, bulk memory, multi-value, SIMD, reference-safe host APIs, compiled-artifact metadata, inspection, pooling restrictions, and snapshot restrictions.
- The official Release 2 execution corpus is reported green at 1,600 modules and 48,248 assertions with zero gaps.
- The core project remains dependency-light: the root module is standard-library-only at runtime, aside from a local development replacement for the separately maintained WASI package.
- Near-term work has shifted from semantic completion toward measurement-driven railshot optimization, interruption, stack traces, artifact productization, and additional targets.

The most important qualification is platform scope. Linux/amd64 remains the mature baseline, while this branch adds native Linux/arm64 and Darwin/arm64 execution, guard-page support, runtime/API coverage, corpus gates, reference globals, indexed tables, optimized calls, and cooperative cancellation. Some legacy amd64-only test tags still need to be made architecture-neutral before `go test ./...` is a universal ARM64 gate.

## Repository state

- Git state before publishing this report, excluding submodules: implementation commits clean; this report and the ARM64 roadmap staged.
- Tracked primary-repository files, excluding submodule trees: 584.
- Tracked Go files, excluding submodule trees: 377.
- Root Go packages reported by `go list ./...`: 32.
- Go language version: 1.22.
- Root module: `github.com/wago-org/wago`.
- Source TODO/FIXME/XXX markers outside submodules, graph output, and the planning ledger: 5.
- No release tag was shown in the current checkout; installation documentation still describes `v0.1.0` as the future public-prebuilt transition.

Recent development has concentrated on WebAssembly 2.0 closeout, package-manager commands, funcref table initialization, plugin installation/publishing, bulk memory, table operations, and railshot constant/flags optimizations.

## Knowledge graph

The graphify knowledge graph was incrementally refreshed for this status pass.

- Nodes: 10,792.
- Edges: 28,564.
- Communities: 493.
- Updated outputs: `graphify-out/graph.json` and `graphify-out/GRAPH_REPORT.md`.
- HTML visualization was skipped by graphify because the graph exceeds its 5,000-node visualization safety limit.
- Nine JSON source files produced no graph nodes; graphify reported that they will be retried on a later update.

The graph was used to orient architecture and cross-file relationships. This report excludes conclusions drawn from submodule contents and treats the repository's own docs, source, tests, and Git metadata as authoritative.

## Architecture

The runtime pipeline is:

1. Decode wasm bytes in `src/core/compiler/wasm`.
2. Validate module structure, types, control flow, immediates, limits, and enabled feature families.
3. Compile directly with railshot in `src/core/compiler/backend/railshot`.
4. Produce a `Compiled` artifact containing native code plus instantiate-time metadata.
5. Instantiate through `src/wago`, allocating code, linear memory, tables, globals, reference stores, host-call state, and traps.
6. Execute through `src/core/runtime` on a dedicated off-heap foreign stack.

Important architectural properties:

- No cgo and no C toolchain are required.
- Railshot combines code generation and register allocation in one forward pass using a symbolic operand stack and deferred materialization.
- Native-facing state is off-heap and stable; native code does not receive movable Go heap pointers.
- Code mappings follow W^X: write while populating, then read/execute.
- Linear memory and negative-offset basedata form the native runtime context.
- Export invocation uses fixed off-heap argument, result, and trap buffers.
- Explicit and guard-page bounds modes are supported on the production target.
- Cross-instance functions may trigger link-time recompilation and native context switching.
- `src/core/compiler/ir` is an off-path verification/oracle package, not an execution tier. The roadmap explicitly rejects adding an SSA/IR runtime tier.
- Compiled `.wago` artifacts are versioned and validated before mapping or execution.

## WebAssembly support

### Complete on the supported target

- WebAssembly 1.0 scalar numeric, control-flow, call, memory, table, global, import/export, segment, and start semantics.
- Sign-extension and non-trapping float-to-int conversions.
- Multi-value functions and structured control flow.
- Bulk memory and table operations, including passive/dropped segment state.
- Reference types: nullable/non-null funcref, externref, typed select, reference globals, typed elements, host-owned references, public reference tokens, and multiple local/imported tables.
- Exact table/global import compatibility, duplicate aliases, shared mutable identity, and store ownership rules.
- Synchronous returning host imports and `v128` host parameters/results.
- Core SIMD and deterministic relaxed-SIMD lowering through the documented opcode range on the modern amd64 baseline.
- Compile-time feature admission and required-feature persistence in compiled artifacts.

### Partial

- WASI Preview 1 is intentionally minimal. Implemented areas include basic fd I/O, seek/fdstat, process exit, args/environment, clock, and random. Filesystem, sockets, and polling remain stubbed or incomplete.

### Planned

- Tail calls.
- Threads and atomics.
- Cooperative cancellation is implemented on ARM64 at function entries and loop headers; amd64 polling remains planned.
- Wasm-level stack traces.
- `call_indirect` inline caches using table epochs.
- Further target closeout, including remaining ARM64 test-tag parity and Windows ABI work.
- A wazero-compatible migration shim.

### Explicit non-goals

- Multi-memory.
- Wasm exception handling.
- Wasm GC.
- An interpreter tier.
- A separate SSA/IR execution tier.

Unsupported or disabled behavior is expected to fail clearly during decode, validation, or compilation rather than being accepted on a best-effort basis.

## Runtime and public API

The low-level API exposes raw eight-byte call slots through `Compile`, `Instantiate`, and `Invoke`. The higher-level `Runtime` API adds typed `Value` calls, contexts, plugins, hooks, policy, metadata, and explicit ownership for reference values.

Notable runtime/product behavior:

- Compile once and instantiate many times.
- Numeric, SIMD, funcref, and externref values cross supported API boundaries.
- Explicit runtime stores bound reference identities and reject incompatible cross-store sharing.
- Host funcrefs, reference globals, and externref tables have bounded owner APIs.
- Shared memories, tables, globals, and cross-instance functions are supported subject to exact type/store compatibility.
- Class pooling reinstantiates local reference state and rejects imported shared reference state that cannot be reset safely.
- Snapshot products reject table/reference-global modules; eligible small numeric-memory instances can use an in-place reset fast path.
- Module inspection returns deterministic index-ordered function/global/table metadata and exact limits/types.
- Compiled artifact codec v20 persists structural WebAssembly 2.0 metadata without serializing live runtime identity.

## CLI and packaging

Implemented CLI/product surfaces include:

- Run and validate wasm modules.
- Typed CLI arguments and export selection.
- Plugin listing and inspection.
- Module import/capability inspection.
- Environment/version reporting.
- Package commands with force, verbose, update, info, and aliases.
- Custom plugin declaration/build flows through `wago.json`.
- Registry publish metadata including unpacked size.
- Source-backed installation for plugin builds.

Known product gap: `wago build` remains reserved for the future `.wago` product path and reports not implemented. Artifact productization still needs durable cache keys, compiler/CPU/bounds/ABI compatibility policy, and compile/run/inspect CLI integration.

## Testing and conformance

The test strategy spans:

- Unit tests for decoder, validator, frontend, compiler, runtime, API, CLI, codec, stores, tables, globals, snapshots, pooling, and policies.
- Strict malformed-module and malformed-artifact tests.
- Generated/fixture-based opcode tests.
- Native execution and trap tests.
- Cross-instance and close-order/lifetime tests.
- WABT-driven spec execution for WebAssembly 1.0, official Release 2.0, and proposal-oriented 3.0 inputs.
- Guard-page tagged tests.
- TinyGo build and test coverage.
- Corpus differential tests and benchmark workloads.
- Coverage and PR-card generation in CI.

Documented conformance snapshots:

- MVP: 57/57 applicable files, zero failing assertions.
- Release 2: 1,600 modules and 48,248 assertions, zero gaps.
- SIMD proposal corpus: 24,325 assertions with zero skipped modules/assertions.

CI jobs represented by this branch:

- Lint and formatting.
- Build and unit tests plus guard-page tests.
- Official WebAssembly 2.0 spec suite.
- TinyGo build/test.
- Coverage.
- Binary-size checks.
- PR status-card generation/commenting.
- Native Linux/arm64 encoder, backend, runtime/API, bounds-mode, and corpus gates.
- Darwin/arm64 encoder, backend, runtime/API, explicit-bounds, and guard-page gates.

Local verification for the ARM64 closeout passed the ARM64 encoder/backend,
runtime/API, explicit-bounds corpus, and guard-page corpus suites. Focused tests also
cover json-as committed goldens, SQLite recursive CTEs, nested host re-entry,
cancellation and instance reuse, indexed tables, reference globals, nonzero-table
indirect calls, and limited multi-result register returns.

## Performance and footprint

Latest checked-in benchmark snapshot: `nightly-96-g6e73d12`, measured 2026-07-05 on an AMD Ryzen 7 7800X3D running Linux amd64.

Representative documented results versus wazero:

- Compile: approximately 5.4x to 20x faster across listed tiny, recursive, memory-tree, and json-as modules.
- Execution: approximately 1.2x to 2.3x faster across listed micro, recursive, memory, JSON, and SQLite workloads.
- Full-process startup snapshot: 5.4 ms for Wago, between wasm3's 5.0 ms and wasmtime's 8.0 ms in that experiment.
- Default Go CLI: about 3.1 MB.
- Stripped Go CLI: about 2.1 MB.
- TinyGo size build: about 0.43 MB; about 0.16 MB with UPX in the recorded experiment.
- Project stats snapshot reports zero cgo lines and 79% generated test coverage.

These are point-in-time machine-specific measurements. Future hot-path or footprint claims should continue to include measured before/after data.

## Near-term roadmap

The roadmap prioritizes measured improvements to the sole railshot backend:

1. Add `CodegenStats`, explain mode, and golden disassembly infrastructure.
2. Land cheap deferred-tree, alias, constant-folding, and mask-elision wins.
3. Expand flags-resident compare fusion.
4. Extend the landed mixed call staging and two-integer-result register ABI to additional profitable result shapes.
5. Improve explicit bounds facts, loop prechecks, store combining, load forwarding, and feature-gated BMI2 paths.
6. Evaluate restricted pending local writes using instrumentation.
7. Re-measure fused validation/compilation.
8. In parallel, extend landed ARM64 interruption and monomorphic table calls with stack traces, mutable-table epoch caching, amd64 polling, and `.wago` productization.

The current history shows that parts of the earlier optimization plan are already landing (constant folding and `stFlags` work). The roadmap should be reconciled periodically so completed sub-items do not remain presented as wholly pending.

## Risks, constraints, and documentation drift

- Platform concentration is the largest operational constraint: production support and CI are centered on Linux amd64.
- The modern amd64 instruction baseline is intentional and currently lacks runtime CPUID fallback for older CPUs.
- Guard-page mode installs process-wide signal handlers and requires deliberate build/runtime selection.
- Reference values introduce explicit store and lifetime constraints; fail-closed ownership checks are part of the correctness model.
- Snapshot and pooling eligibility is deliberately narrower for modules with tables or reference globals.
- WASI breadth is limited relative to general command workloads.
- The architecture document contains stale language describing SIMD as partial even though `FEATURES.md`, `ROADMAP.md`, and the current release claim it complete for the documented baseline.
- The architecture document also describes CLI compile/profile/validate surfaces in terms that no longer fully match the current CLI. `FEATURES.md` is the feature-status source of truth; README/architecture prose should be refreshed after major closeout work.
- The graph currently exceeds the default interactive visualization limit, so graph queries should be scoped by subsystem or symbol.

## Overall assessment

The repository is semantically mature for its stated Linux amd64 target and has completed the hardest WebAssembly 2.0 reference/table/product integration work. Its strongest characteristics are strict validation, direct single-pass native code generation, explicit ownership at unsafe/reference boundaries, broad conformance testing, and measured performance/size goals.

The next phase should focus on keeping documentation aligned with the completed feature set, maintaining a consistently green supported-target CI signal, instrumenting optimization work before further hot-path changes, and deciding whether additional platform support is a product priority or remains a longer-term engineering track.
