# Immutable combined correctness qualification

Source: `5345bd365c13963f1bf60e26bb80789ab9ead4cc`. Evidence commit: supplied in the final handoff; no later source head is qualified.

This is a correctness checkpoint, not final review, inclusion, PR readiness or performance qualification. Darwin has no established execution runner and remains NOT_RUN. The November 4 deadline does not waive that gap.

| Gate | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Full root ./... | PASS | PASS | PASS |
| Runtime-tagged CLI | PASS | PASS | PASS |
| Installer nested module | PASS | PASS | PASS |
| Bench nested module | PASS | PASS | PASS |
| Full collector family | PASS | PASS | PASS |
| Full collector/runtime/Wago/integration race | PASS | PASS | PASS |
| Reduced parent reference-store race | PASS | PASS | PASS |
| Full runtime/Wago guard pages | PASS | PASS | PASS |
| Canonical candidate corpus | PASS | PASS | PASS |
| Fresh matched baseline corpus | PASS | PASS | PASS |
| Explicit RFW catalog, no inline + lifetime analysis | PASS | PASS | PASS |
| Actual candidate GC graphs and scalar/noGC controls | PASS | PASS | PASS |
| Baseline GC graphs and controls | PASS | PASS | PASS |
| Official spec v1 | PASS | PASS | PASS |
| Official spec v2 | PASS | PASS | PASS |
| Official spec v3 | PASS | PASS | PASS |
| Official spec v3 signals/guard pages | PASS | PASS | PASS |
| Guard-page corpus | PASS | PASS | PASS |
| Full backend | PASS | PASS | PASS |
| Full default backend diagnostics + Wago | PASS | PASS | PASS |
| Full WAGO_SHARED_SCALAR=0 diagnostics + Wago | PASS | PASS | PASS |
| Root build | PASS | PASS | PASS |
| Root vet | PASS | PASS | PASS |
| Runtime CLI build | PASS | PASS | PASS |
| Profile metadata | PASS | PASS | PASS |
| Profile runtime/unwind | PASS | PASS | PASS |
| Explicit shared-scalar controls | PASS | PASS | PASS |
| Native joins | PASS | PASS | PASS |
| Four seeded concurrency runs, 24 rounds each | PASS | PASS | PASS |
| Actual TinyGo serial evacuation | PASS | PASS | PASS |
| Actual TinyGo joint/foreign-stack supplement | PASS | PASS | PASS |
| Actual TinyGo supported conversion compatibility | PASS | PASS | PASS |
| Collector hardening, three repetitions / explicit bounds | PASS | PASS | PASS |
| Collector hardening, three repetitions / guard pages | PASS | PASS | PASS |
| Host transitions, three repetitions / explicit bounds | PASS | PASS | PASS |
| Host transitions, three repetitions / guard pages | PASS | PASS | PASS |
| Actual QEMU arm64 collector | PASS | PASS | PASS |
| Actual QEMU arm64 facade | PASS | PASS | PASS |
| Actual QEMU arm64 guest | PASS | PASS | PASS |
| Actual QEMU riscv64 collector | PASS | PASS | PASS |
| Actual QEMU riscv64 facade | PASS | PASS | PASS |
| Actual QEMU riscv64 guest | PASS | PASS | PASS |
| Actual ARM64 H6 ownership/wakeup adapter | PASS | PASS | PASS |
| Actual full RV64 managed-function controls | PASS | PASS | PASS |

| Repository / supplemental gate | Verdict |
| --- | --- |
| Generation | PASS |
| Generated-file diff | PASS |
| Formatting | PASS |
| Docs | PASS |
| Actionlint | PASS |
| Staticcheck, all four reviewed profiles | PASS |
| Native-compare recipe | PASS |
| Portable 386 package execution | PASS |
| RISC-V CI script contracts | PASS |
| Node update-website-bench | PASS |
| Node update-website-startup | PASS |
| Node engine-state-oracle | PASS |
| Node fuzz-engine-state | PASS |
| build-release-assets.sh | PASS |
| release-qualification.sh | PASS |
| ci-download-go-modules.sh | PASS |
| check-diagnostic-dce.sh | PASS |
| check-riscv64-diagnostics.sh | PASS |
| check-regalloc-code.sh | PASS |
| Actual patched TinyGo 0.42 foreign-stack lane, all 34 commands | PASS |
| Configured bounded fuzz runs | PASS |
| Extended 30-second bounded fuzz runs | PASS |
| Native Darwin execution | NOT_RUN |

Effective required command receipts (excluding Darwin): `{'PASS': 195}`. `qualification.json` indexes every attempt and skip reason; per-command result JSON and raw logs retain the full terminal events, including recovered harness failures. Build-only target receipts are separate from actual execution receipts.

## Corpus and target boundaries

`corpus-per-id.json` contains all 768 ID/revision/mode rows; `corpus-joins.json` contains all 384 comparisons. `corpus-identity.json` verifies 128 IDs, 121 distinct binaries and 47 input references. `catalog-per-id.json` separately records the 384 explicit-RFW compile rows. The baseline is immutable `a3c08365f531eb3c9003e3acf3e0a822c12c31f4`; its canonical checked compile-only preflight runs before baseline execution. The known unsafe baseline no-inline/lifetime-analysis clang-cpp configuration is never executed and is not waived by canonical passes.

`reference-only-skips.json` enumerates captured external-runtime references; these are not fresh V8/wasmtime execution claims. Fresh supported wazero references and all Wago semantic/application contracts are accounted for separately. The historical corpus is not GC graph coverage: `generated-graph-identities.json` and separate graph gates retain the real guest graph, scalar and noGC controls. The graph oracle requires 32 live objects and at least 2,048 allocations after 64 calls and collection.

`spec-accounting.json` retains per-file and total modules/assertions/gaps for each explicitly configured official spec lane. Suite PASS never promotes an optional skipped test into execution. ARM64 QEMU executes supported H6/H2 host-container/import tests. RV64 controls qualify its supported compact representation; build-excluded opaque-host receipts are not passes. TinyGo compatibility likewise does not establish excluded host opaque receipt support. B’s pending worker handoff is not assumed or polled; all results here concern the frozen combined source.

## Retained harness failures

The initial target census also detected that amd64-only H6 test tags excluded those bodies from the ARM64 command. Its failed census is retained. A new ARM64-only adapter preserves both H6 test bodies byte-for-byte and executes all nine ownership/wakeup families in all modes; the full RV64 managed-function family is separately executed. No excluded tests are promoted to passes. Three discovery failures were retained without changing source or expectations: the initial root run reached the mise PowerShell shim; initial native-compare reached the mise just shim; initial Staticcheck lacked its installed binary on PATH. Complete required replays use explicit PowerShell 7.6.8, just 1.58.0 and the verified Staticcheck directory. Their command/log receipts remain alongside the passing replays. Any additional failures or interruptions are authoritative in qualification.json, not silently omitted.

## Provenance and handoff

`source.json`, `source-ancestry.json`, source manifests, tool hashes and every command receipt pin provenance. Main tests use Go 1.27.1, Node 24.22.2, WABT 1.0.41 and the pinned Release 3 interpreter/OCaml environment. QEMU uses Go 1.27.3 and the recorded Debian emulator package SHA256. TinyGo is the actual patched 0.42 binary with a fresh cache. Shared submodules and expectations are untouched; private Git metadata supports repository scripts. Inherited orchestration metadata is omitted from committed command environments as recorded in environment-metadata-redactions.json; test/toolchain variables are retained.

`exports-integrity-after.json` compares original archived files after execution; declared baseline graph test overlays and target-only added adapters are separately documented. `drivers-integrity-after.json` checks the recorded driver hashes. `preservation-normalization.json` retains the initial status-comparison formatting mismatch and its normalized equality check. `cleanup.json` and `process-drain.json` record only owned export/cache cleanup and process readiness. Read those receipts before independent measurement scheduling; this report grants no measurement lease.

Only this evidence directory is committed. It is not consumed by shipped builds and cannot affect shipped performance. No production edits, benchmarks, performance timings, subagents, reviews, PRs, pushes or closures were performed. Test-framework durations in raw logs are incidental correctness output.

Rebuild terminal summaries without executing tests: `python3 docs/rfwgc-evidence/final-combined-5345/report.py`, then `accounting.py`, then `render-report.py` in the same directory.
