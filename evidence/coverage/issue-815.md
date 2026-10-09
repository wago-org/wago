## Action
Extend Wago's existing diagnostic output with a comparison that links changed native instruction sequences to Wasm source locations and preserves performance-relevant operands.

This is diagnostic tooling, not a claim that a particular Wago lowering is defective.

## Paper and transferable idea
Zeng et al., **Debugging Performance Issues in WebAssembly Runtimes via Mutation-based Inference** (WarpL), ICSE 2026, Sections 3–5: https://arxiv.org/html/2604.13693v1 ; https://doi.org/10.1145/3744916.3773141 . WarpL compares original and mutated machine code to narrow performance causes. Its opcode-based alignment is a useful starting point, not a sufficient model of operand dependencies or semantic equivalence.

## Repository fit
Review the final contracts of profiling/artifact work in PR #719, the shared compiler introduced by #802, and checked-build emission observers in #807. Reuse existing source maps, artifact identities, and disassembly support. Do not create a second profiler or change those active PRs. The production compiler remains Railshot; `src/core/compiler/ir` stays outside the load path.

## Possible implementation
1. Define a test/diagnostic-only normalized record containing function identity, Wasm byte offset where known, instruction bytes, opcode, typed operand roles, immediate bits, memory addressing mode, and explicit/implicit register reads and writes.
2. Normalize relocation addresses and unstable symbol names. Do not normalize away constants, operand widths, partial-register writes, zero-extension, memory displacements, or live dependency relationships.
3. Align corresponding functions and basic regions before comparing instructions. Use bounded sequence alignment or anchored hashes; do not apply an unrestricted quadratic longest-common-subsequence algorithm to entire large modules.
4. Report changes in spills/reloads, copies, branches, addressing modes, constant materialization, and dependency chains. Keep static observations distinct from sampled hotness and measured timings.
5. Record the actual shared/established compiler path, target architecture, CPU-feature mask, bounds mode, profiling build, and binary hash. Instrumented profiling builds can select a different compiler path and must not be silently compared as ordinary builds.

## Acceptance criteria
- [ ] Controlled examples distinguish identical opcodes with different constants, false destination dependencies, changed spill widths, and harmless relocation changes.
- [ ] Both AMD64 and ARM64 records are supported for an explicit initial instruction subset; unknown semantics remain unknown.
- [ ] Compare at least one current performance fixture with the existing human-readable disassembly and verify source-location attribution.
- [ ] Bound comparison time and memory, and report incomplete mappings or truncated regions.
- [ ] No additional state, code generation, allocation, or dispatch in ordinary builds.

This tool provides evidence for diagnosis, not a correctness proof or automatic root-cause verdict. It must not upload program artifacts. The deferred private cross-project research harness is expressly excluded.
