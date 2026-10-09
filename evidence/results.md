# Issue 815: bounded operand comparison result

Recommendation: **keep this local diagnostic prototype; revise/extend before any full #815 claim**. The declared operand-sensitivity hypothesis passed. This is not a runtime optimization, performance improvement, complete disassembler, correctness proof, or completed issue.

- Issue: https://github.com/wago-org/wago/issues/815
- Base: `5cd443965a09e46076cce5e3dd4bf3ae69500348`, verified against live main and fetched into a private repository.
- Branch: `experiment/operand-comparison-815`
- Worktree: `/home/jtenner/Documents/Codex/2026-10-09/task-3/wago-815`
- Private Git repository: `/home/jtenner/Documents/Codex/2026-10-09/task-3/isolated.git`
- Baseline/hypothesis commit: `00b9645e`.
- Implementation commit: `236b6b7a` (full IDs in `commits.txt`). Evidence is retained in a subsequent local commit.
- Untouched baseline worktree: `../baseline-815` at the base above.

Current issue/branch/PR snapshots are retained. #815 had no assignee/comments or matching local/remote branch. The selected tooling slice avoids active #719's profiler/sidecar implementation, #909/#910/#895, RISC-V, paused experiments/SQLite, and owned #810/#812/#813/#819/#826 work. No shared checkout or settings were edited; no network write, PR, issue comment, push, merge or installation occurred. Wago `.agents`/`.codex` were empty; no applicable Wago AGENTS/skill/agent-todo file was found. Repository CONTRIBUTING/CONTEXT guidance was read; local Codex memory supplied historical installer context only.

## Hypothesis and controls

Declared before implementation in `plan.md`: preserve operands and dependency/width facts to distinguish same-opcode changes, while normalizing explicitly identified JMP/B relocations. Reject/mark incomplete unknown, ambiguous, unqualified or over-budget comparisons. No guest speedup is hypothesized.

The final baseline projects opcodes from the same distinct raw byte pairs used by candidate controls. It misses all four AMD64 constant, zero-idiom dependency, spill-width and stack-displacement changes; candidate detects each. Matching-target relocation changes compare equal in both. Different/missing target identities and non-admitted relocation opcodes do not receive a waiver. ARM64 synthetic controls detect constants, ADD operands and GP load/store widths, and accept matching B relocations. Raw bytes remain intact alongside the separate normalized encoding.

Unknown/partial-write/call instructions, missing provenance/source anchors, changed region or instruction counts, incompatible configuration and regions missing on one side never establish complete comparison; image regions omitted on both sides are outside the qualification. Malformed hex, overlapping/noncontiguous regions, alignment and instruction/region/input limits reject; change-budget exhaustion reports incomplete. CLI replay passes for the mapped scalar control and exits 3 for incomplete fib self-comparison.

Limits are 1 MiB JSON, 64 regions, 4096 instructions and 512 changes. Comparison/alignment is linear and positional within matching anchored regions; it does not guess insertions or apply quadratic LCS. Fixed register/address fields avoid per-instruction register-list and address-format allocations. Metadata identities and instruction boundaries remain producer assertions, not artifact authentication.

## Real native and source evidence

`TestRealScalarSourceRegion` uses an independently validated bounded integer XOR module. Direct established-backend source-map compilation uses an explicit configured feature mask of zero, explicit bounds, one worker and `wago_profile`. Observed and ordinary direct-backend code bytes match. Existing objdump agrees with `xor eax,r10d` at native offset 0x25, mapping to function 0 / Wasm offset 5. The image has 48 bytes; the selected source region has two supported instructions. Its report is complete **for that supplied region**, not the entire image.

The same unchanged valid Wasm passes six independent integer-XOR result checks through the supported public API on native Linux AMD64. This is separate execution evidence: it is not proof that the offline direct-backend artifact or every source-map record is the executed public-API artifact. The changed diagnostic bytes are never mapped or executed.

`TestExistingFibSourceRegions` checks the current `tests/fixtures/wasm/fib.wasm` performance fixture: 88 Wasm bytes, 105 native bytes, eight supplied source regions / twelve instructions, six supported and six unknown. Self-comparison correctly remains incomplete. Its emitted ADD attribution is independently checked against the Wasm i32.add byte after accounting for local-declaration bytes; saved objdump/source maps permit manual inspection. No omitted byte gaps are counted as qualified coverage.

ARM64 word controls and Linux ARM64 cross-compilation pass. **No native ARM64 execution was available or claimed.** No FP, SIMD, host calls, trap-producing changes or production instruction emission were added.

## Short paired workload measurements

Host: Linux AMD64, Ryzen 7 8845HS, Go 1.27.1, GNU objdump 2.44. All Go work was capped at GOMAXPROCS=2 / build -p=2. Host load was sampled; it declined from about 2.4 to 0.3. No long saturated benchmark or full CI/release-profile campaign was started. No exclusive resource reservation was obtained. Active CI metadata is retained.

Three alternating baseline/candidate pairs; 100 compile iterations and 10,000 execution iterations per sample. Stock benchmark setup/calibration precedes each measured batch; no separate long warmup. Raw samples are retained. Workloads are existing bounded scalar/control/ALU and memory-read shapes, not an end-to-end application campaign.

| Measurement | Baseline median (range) | Candidate median (range) | B/op, allocs/op (both) |
| --- | --- | --- | --- |
| Small scalar compile | 6.693 µs (6.281–10.718) | 10.803 µs (6.337–11.427) | 8698, 22 |
| Medium control compile | 10.086 µs (9.895–15.044) | 10.209 µs (9.979–10.789) | 10639, 21 |
| ALU-heavy compile | 25.673 µs (25.522–26.118) | 25.765 µs (24.858–26.036) | 9640, 11 |
| Eight-source explicit-bounds execution | 43.02 ns (35.43–48.36) | 35.24 ns (34.93–63.39) | 0, 0 |

Adverse small-scalar median: **+61.4%**; execution median: **−18.1%**. These samples are noisy and inconclusive, not demonstrated regressions or gains. Compiler/runtime source is unchanged. The ordinary runtime builds, made with matching `-trimpath -buildvcs=false -tags=wago_runtime`, are **byte-identical**, each 14,753,697 bytes, SHA-256 `b4525ea2eaa62f2e78eeef8aa3e73e993b492ddd3e93d9c3c362671eab269f53`. The execution benchmark reports unchanged 811 native bytes. No break-even estimate is meaningful.

Offline equal lowercase register-only comparison, one region, three 100-iteration samples:

| Records | Median | Range | Allocation traffic |
| --- | --- | --- | --- |
| 16 | 1.516 µs | 1.483–1.517 | 0 B/op, 0 allocs/op |
| 256 | 28.730 µs | 21.134–34.905 | 0 B/op, 0 allocs/op |
| 4096 | 322.411 µs | 322.360–340.863 | 0 B/op, 0 allocs/op |
| 32 changed memory records | 8.482 µs | 4.398–12.421 | 26,656 B/op, 6 allocs/op |

The initial string-address implementation's changed-memory report used 26,916 B/op / 70 allocs/op; compact typed addresses lower allocation traffic/count in that control. Timing variability prevents a speed claim. JSON parsing, uppercase normalization, explicit relocation normalization and multiple-region maps were not benchmarked; zero allocation is not a whole-tool claim. Report records borrow input strings and are capped; RSS, process peak memory and exact retained heap were **not measured**. Allocation traffic is not retained memory.

## Verification, review and limitations

Passed ordinary package tests; profiled and checked+profiled package tests; checked/profiled vet; Linux ARM64 cross-build; standalone CLI replay. Whole-repository suites and full release size profiles were not run because the patch is offline tooling only; matched runtime binary identity directly checks the production build effect. The runtime source is untouched, so production Wasm semantics and trap ordering are unchanged. Scope uses trusted small scalar inputs; no malformed native artifacts or faulty emitted code execute.

Independent agent review requested explicit incomplete coverage, preservation of raw relocation bytes, a real current performance fixture, accurate configured feature masks and honest allocation scope. Those changes are implemented. The reviewer independently reproduced ordinary and profiled real-fixture tests and inspected the fib ADD source attribution. The exact review report is retained separately.

Development failures were retained as conclusions: the first baseline was too tautological and was replaced with actual paired byte controls; a source-offset check initially forgot local-declaration bytes and was corrected; an attempted direct low-level execution harness lacked complete runtime setup and failed, so it was removed in favor of supported public-API checks. None is evidence of a new compiler/runtime defect. The failed harness supplies no native-artifact execution qualification. The first standalone frozen-binary replay ran from the repository root and could not resolve the relative fib fixture path; rerunning from the package directory passed, and the incorrect-working-directory log is retained. Captured text has only trailing whitespace stripped for Git diff hygiene; measurement values are unchanged.

Keep the bounded operand-sensitivity prototype for local review. Before full #815 acceptance, revise the producer/authentication contract against settled #719 interfaces, extend operand/implicit-effect coverage conservatively, obtain native ARM64 evidence, and qualify any richer bounded alignment. Do not publish or close the issue from this result.
