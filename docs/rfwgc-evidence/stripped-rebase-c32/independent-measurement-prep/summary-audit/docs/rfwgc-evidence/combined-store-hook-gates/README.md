# Combined module-owned store-hook qualification — BLOCKED

Tested source: **501e9ab46c4f68090c7bbeb17a6ad14f4fadda6d**.
Starting evidence head: `35c36bc594d2a027317cfcd321379f2d40c6b219`.
The evidence commit is reported in the worker handoff. No future head is qualified.

Two safety gates failed: a race between lazy reference-store publication and the
lifetime-boundary hook, and a checked deferred-load register overwrite in the
explicit RFW catalog configuration. The latter also fails a compile-only
pre-region baseline control. Known default-route diagnostic failures and eight
distinct Staticcheck findings remain failures. See [FINDINGS.md](FINDINGS.md)
for exact commands, errors, owning files, baseline controls and sibling audit.

The failing drivers stopped. Remaining combined-source execution was terminated
at the unsafe qualification boundary; `safety-stop.json` records only this
worker's process trees and interrupted commands. Unfinished gates are not passes.
No production repair was authorized or attempted in this evidence-only lane.

## Fresh gate verdicts

All candidate rows below use the exact source above. Baseline controls use
`a3c08365f531eb3c9003e3acf3e0a822c12c31f4`. `qualification.json` provides every
command's source, exit, raw log, test failures and skip details. PASS means that
command completed successfully, not that its optional skips executed.

| Gate | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Full root `./...` | PASS | INTERRUPTED at safety stop | NOT_RUN |
| Full runtime-tagged CLI | PASS | NOT_RUN | NOT_RUN |
| Full installer nested module | PASS | NOT_RUN | NOT_RUN |
| Full bench nested module | PASS | NOT_RUN | NOT_RUN |
| Full collector package family | PASS | PASS via repeated hardening | NOT_RUN on host |
| Full collector/runtime/Wago/integration race command | FAIL: reference-store race | NOT_RUN | NOT_RUN |
| Hardening collector, three repetitions, explicit bounds | PASS | PASS | NOT_RUN |
| Hardening collector, three repetitions, guard pages | PASS | INTERRUPTED | NOT_RUN |
| Host-transition repetitions, explicit bounds | PASS | PASS | NOT_RUN |
| Host-transition repetitions, guard pages | PASS | NOT_RUN | NOT_RUN |
| Full runtime/Wago guard-page command | NOT_RUN | NOT_RUN | NOT_RUN |
| Canonical all-corpus/application/reference execution | PASS | PASS | INTERRUPTED |
| Fresh baseline all-corpus/application/reference control | PASS | INTERRUPTED | NOT_RUN |
| Explicit `RFW_CATALOG`, no inline + lifetime analysis | PASS | PASS | FAIL: checked clang-cpp-kernels compile |
| Separate generated guest/native graph controls | PASS | PASS | PASS before catalog failure |
| Explicit official spec v1, v2, v3 execution | PASS | PASS | PASS |
| Official spec v3 signals / guard-page corpus | NOT_RUN | NOT_RUN | NOT_RUN |
| QEMU Linux ARM64 full native collector | PASS | PASS | PASS |
| QEMU Linux RV64 full native collector | PASS | PASS | PASS |
| QEMU ARM64 combined graph/H6/H2 supported guest controls | PASS | PASS | INTERRUPTED (build passed) |
| QEMU RV64 combined supported guest controls | NOT_RUN | NOT_RUN | NOT_RUN |
| Actual TinyGo 0.42 serial evacuation/joint rerun | NOT_RUN | NOT_RUN | NOT_RUN |
| Default-route full codegenstats diagnostics | FAIL: 24 known top-level failures | INTERRUPTED | NOT_RUN |
| Focused default-route interval diagnostics, current + baseline | FAIL on both | FAIL on both | FAIL on both |
| Build and go vet root | PASS | PASS | PASS |

The 34-command TinyGo foreign-stack lane completed **2/34** commands: full
ordinary host and ordinary standalone gates. Guest-threads ordinary was
interrupted; 31 commands were not started. The actual binary is TinyGo **0.42.0**,
with a fresh archive/cache and verified existing patch integration. It is not a
completed combined TinyGo qualification. The reused driver's nonverbose output
does not enumerate per-test skips; no zero-skip claim is made for its two passes.

Other completed gates: generation with exact generated-file diff, formatting,
docs, all four Node script suites, release-assets/release-qualification/module-
download script contracts, RISC-V CI script contracts, actionlint, diagnostic
DCE, RISC-V diagnostics script, regalloc generated-code identity, native-compare
recipe, and portable 386 package execution all PASS. Staticcheck reviewed-baseline
verification FAILS with eight distinct locations repeated across three profiles.
The full CI established-path diagnostic configuration (`WAGO_SHARED_SCALAR=0`)
passes in ordinary codegenstats mode; it does not waive default-route failures.
Additional fuzz campaigns, explicit profiling/join/seeded-concurrency supplements
and remaining native execution lanes were not reached before the safety stop.
Normal root seed tests are not claimed as bounded fuzz campaigns.

## Corpus, graphs and conformance accounting

`corpus-identity.json` verifies **128 IDs, 121 distinct binary hashes and 47
input references** against the committed catalog. Candidate and baseline catalog
bytes match. `corpus-per-id.json` preserves all **768** expected ID/revision/mode
rows: 412 have all required terminal passes and 356 remain incomplete; these
include partial rows from interrupted suites. `corpus-joins.json` retains all
384 paired comparisons. Only ordinary completed both whole-suite controls.
No missing, skipped or matched-failing workload becomes a pass.

Canonical direct results, semantic outputs and application output/file contracts
are unchanged. Fresh wazero references are enabled where supported. Twelve
canonical captured V8/wasmtime references remain reference-only skips per
completed run; Wago still checks their pinned contracts. No fresh V8/wasmtime
execution is claimed. The historical corpus has no GC graph coverage.

`generated-graph-identities.json` records separately exported graph/scalar/noGC
binaries in each mode. The real guest graph is the 32-node chain with 32 static
constructor sites, expected return 7, 2,048 charges and 32 live objects after
64 calls and collection. Scalar and noGC controls remain distinct binaries.
Named guest/native graph tests passed all modes before the explicit catalog
failure; this does not make the entire failed command green. New baseline graph
execution was not reached. The earlier stage3 graph evidence remains unchanged.

All nine official execution commands explicitly set `WAGO_SPECTEST_DIR`,
`WAGO_SPEC_VERSION`, pinned interpreter/WABT and zero-gap requirements:

| Spec | Modules per mode | Assertions per mode | Fail / skip / gap |
| --- | ---: | ---: | --- |
| v1 | 629 | 16026 | 0 / 0 / 0 |
| v2 | 1600 | 48331 | 0 / 0 / 0 |
| v3 | 2226 | 58238 | 0 / 0 / 0 |

These are the repository's configured official execution selections. Raw
per-file totals remain in `spec-accounting.json`; root validation-harness skips
remain separately recorded and are not erased by these execution passes.

## Provenance, reproducibility and retained attempts

`source.json` pins the Git archive and exact spec-v1/v2/v3 Gitlinks. Main test
archives use Go/GOROOT **1.27.1**, explicit Node **24.22.2**, WABT **1.0.41**,
Release 3 interpreter **9d36019973201a19f9c9ebb0f10828b2fe2374aa**, and OCaml
**4.14.3** from `/home/dev/.local/share/wago-rfwgc-opam/rfwgc/bin`.
`tools.json` records absolute binary paths and SHA256; command JSON records the
actual environments. QEMU drivers use Go **1.27.3** and the exact Debian QEMU
package hash in their source records. Cross builds are never counted as execution.

The stage3/stage7/stage8/H2 drivers were references; current runs use the requested
pin. Recorded test driver hashes remain unchanged (`driver-integrity.json`).
New adapted drivers preserve source selection, oracles and test expectations;
the TinyGo adapter adds stop-on-command-failure only. The platform adapter retains
Stage 7 graph assertions and uses supported guest initialization on RV64, as in
the prior corrected platform evidence. Driver files are retained alongside logs.

Initial root attempt failed because GOWORK=off disabled the installer's local
module resolution; the complete ordinary root replay passed with automatic
workspace mode. Initial tooling was interrupted to isolate generation from the
source under test. Missing private Git metadata caused initial docs/release-
script/actionlint failures; a private archive index/HEAD with read-only object
alternates fixed them, and complete replays passed. Early Staticcheck discovery
attempts lacked its binary on PATH; after bootstrap and an explicit path, the
real lint failure above remained. All failed/interrupted attempts are retained.

Rebuild reports without executing tests:

```sh
python3 docs/rfwgc-evidence/combined-store-hook-gates/finalize-summary.py
```

Exact test commands and absolute archive paths are retained per gate. To replay
on a new archive, reconstruct the pinned archive and fixture inputs from
source.json and supply the recorded environment; do not redirect old commands
at a dirty worktree or overwrite prior logs. `run.py`, `extra.py`, `lanes.py` and
the specialized drivers show the selections. Do not resume failed-source runtime
execution to bypass the safety stop. A repaired coherent pin needs fresh gates.

## Integrity and boundaries

All original tracked blobs in candidate, tooling, baseline, TinyGo and both QEMU
archives verified unchanged after execution (`*-integrity-after.txt`). Adapter
files are test-only additions; no production overlays were used. The compile-only
baseline overlay is separately hashed. All original dirty paths and index entries
were preserved. Additional production dirt appeared concurrently in the shared
worktree and was neither staged nor changed by this worker.

Native Darwin has no available execution endpoint and remains an explicit gap.
Host-created converted containers and TinyGo/other-host opaque receipt exclusions
remain pending the parent's scope/support map. RV64 compact extern controls do
not establish host opaque receipt support. No future source head, final review,
seven-PR disposition, or performance bar closes from this evidence.

Only this evidence directory is committed. It is not consumed by shipped runtime
builds and cannot alter shipped performance. No benchmarks, intentional timings,
measurement lease, performance claims, subagents, PR, push, or closure occurred.
Test framework durations are incidental correctness output. Private archives,
binaries and caches are removed after integrity capture; cleanup.json records
exact owned paths. Shared tool installations and other workers' artifacts remain.
