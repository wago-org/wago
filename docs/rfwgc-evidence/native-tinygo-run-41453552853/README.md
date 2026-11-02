# Darwin ARM64 TinyGo native run 41453552853: FAIL

Run: https://github.com/wago-org/wago/actions/runs/41453552853

Execution source: `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`.
Harness/workflow: `8801252952255edf6ab6196250bd544873785328`.
No credit applies to later `5b02` or `ca164` changes. `dispatch.json` is preserved byte-for-byte, including its historical PENDING verdict; this report supersedes that verdict without rewriting the dispatch record.

All three setup/compiler-release steps succeeded. All nine TinyGo package compilations exited zero and produced native ARM64 Mach-O executables. All nine executables ran. Failures occurred inside test execution, including guest configuration rejection; this was neither a runner provisioning failure nor a TinyGo compile/link failure. The summary correctly failed; ordinary CI and unrelated qualification jobs skipped. No platform approval is granted.

## First exact failure and minimal repair scope

The first test failure in the ordinary lane is `TestTinyGoNativeResumeUsesPhysicalStackTop` in `0-run.log`:

```
resume stack top = 0x10497c000, want physical top 0x10497c000 distinct from entry 0x10497c000
```

At the exact source pin, `engine_int_tinygo_test.go:29` fails when `got != want || got == e.stackTop`. Its first clause is false in this receipt: the resume pointer is correct. The second clause demands numerical inequality between the exclusive stack end and the separate generated-entry mapping base. `engine.go:143` derives the physical top from stack base plus length; `invocation_other.go:10` calls that method; `trampoline_tinygo_arm64.go:94-110` preserves the physical top and replaces `e.stackTop` with the entry mapping base. Adjacent mappings may share that boundary address without overlapping. The receipt establishes the invalid inequality oracle, not a wrong resume pointer. Exact VM mapping adjacency is an explanation consistent with the receipt, not an independently captured VM-map measurement.

Reproduction is retained, not rerun: ordinary `0-build.command.json`, `0-run.command.json`, both exit receipts, and the hashed `0.test` reproduce the original command/environment; its exact selector is retained. The reported equality itself deterministically satisfies the failing second clause. Debug and regalloc modes pass this same test, so do not propagate the ordinary failure to them.

Minimal proposed repair: make the test distinguish the physical-stack source deterministically (for example a controlled entry sentinel or explicit mapping identity), rather than require two unrelated addresses to differ. No runtime repair is established or made. An allocator-dependent re-execution might not reproduce the equality.

## Independent remaining failures

1. **Cancellation adapter oracle, all three modes.** `TestDarwinTinyGoCancellationAndGC` fails with `error = context canceled, want interrupted trap`. The exact harness fixture calls `InvokeContext`, cancels its context, then uses `requireInterruptedTrap`. The pinned API documentation at `api.go:4232` explicitly promises `ctx.Err()`; `instance_call.go:136-155` converts interrupted traps to the context error. Minimal repair is `errors.Is(err, context.Canceled)` for the cancellation branch, retaining the appropriate interrupted-trap assertion for Close and guaranteed cleanup on assertion failure. The original fixture reaches its entered handshake and eight GCs and receives cancellation completion, but `t.Fatal` prevents the subsequent post-cancel GC/Close and the second (`closeInstead=true`) iteration. Those gates are **NOT_RUN**, not passed. The import handshake proves import entry, not independently that execution reached the infinite guest loop after returning from the import; a repaired qualification should establish that boundary if claiming pure-loop cancellation. No unsafe failure follow-through was run in this audit.
2. **ARM64 guest fixture configuration, all three modes.** Both `TestGCStage8TinyGoGuestRelocatedRead` and `TestGCStage8TinyGoControls` fail with `wago: unknown arm64 optimization "dead-gc-new"`. The adapter changed only the AMD64 test's filename/build constraint; lines 27 and 81 retain `.WithOptimization("dead-gc-new", false)`. The pinned optimization catalog registers this option for AMD64, and `config.go:889` rejects unknown architecture options. This is an execution-time fixture setup rejection before guest compilation, not a native compiler failure or failed relocation. Minimal repair is an ARM64-aware test configuration preserving all guest/movement/control assertions and explicitly verifying equivalent backend behavior. Guest relocation, result 42, native-view generation and the associated controls are **NOT_RUN beyond configuration**.

No failure here establishes a production runtime defect. Therefore the conditional read-only Slipstream/Starshine production-finding cross-check was not triggered; no sibling code was changed or used to waive a gate. The prior supplement's platform audit remains historical context only.

Repair duration is unknown. No deadline was supplied. The existing workflow ceiling for any separately authorized full rerun is 333 job-minutes (3 × 110 plus 3); harness steps cap at 95 minutes, individual package builds at 1200 seconds and executions at 600 seconds. These are configured limits, not benchmark estimates. No rerun, dispatch, workflow change, production change, push, PR, benchmark, or subagent was performed.

## Native suite verdicts

Counts are top-level passed/expected; required children are additional.

| Mode | Runtime | Serial evacuation | Public/guest suite | Total | Native verdict |
| --- | --- | --- | --- | --- | --- |
| ordinary | FAIL, exit 1, 6/7 | PASS, exit 0, 11/11 | FAIL, exit 1, 12/15 | 29/33 | FAIL |
| wagodebug | PASS, exit 0, 7/7 | PASS, exit 0, 14/14 | FAIL, exit 1, 12/15 | 33/36 | FAIL |
| wago_regalloccheck | PASS, exit 0, 7/7 | PASS, exit 0, 11/11 | FAIL, exit 1, 12/15 | 30/33 | FAIL |

All three runtime suites pass all three required foreign-stack children (`return`, `trap-unwind`, `interrupt`). All public suites pass ten required local/owned token-classification children and eight validation-ordering children; two intermediate `local`/`owned` group passes are also recorded, not extra top-level tests. No test SKIP, duplicate pass, unexpected top-level pass, or recognized silent TinyGo early-return message occurs in any of the nine logs. The harness's `missing` list means missing **passes**, not necessarily tests absent from execution: every listed missing top-level test actually ran and failed. Total: 92/102 top-level passes and 10 failed top-level executions.

The foreign-stack fixture results support the specified concurrent-GC return/trap/interrupt, prepared-entry 42, nested host-reentry park/resume, and forced thunk-unmap failure/GC/retry paths on this native toolchain. They do not qualify every signal-frame behavior or unrelated ordinary-Go Mach interruption. Cleanup evidence is specifically the tested thunk-failure path; it does not close the aborted cancellation cleanup or later cleanup-fix gates.

Serial evacuation's Workers 0/1 and policy/refusal, admission, generation, cards/LOS/young and pressure assertions pass, with the three additional debug fault/rollback/maintenance tests. Public conversion identity/ownership, foreign/forged/closed rejection, replacement/publication rollback, ordinary/null storage, authenticated-wrapper classification, unsupported opaque conversion and validation ordering pass. This does not enable helper pools, parallel evacuation, native object-edge/reclaim callbacks, or host receipt ownership that the suite excludes.

## Provenance and independently checked receipts

`audit.py` performs only offline hash/manifest/census validation. `audit-attempt-1.log` records success; `audit.json` contains every suite's exact build/run argv, environment, timeout, exit, Mach-O hash, expected census, children and missing passes. No archived executable is launched. `_inspected/` retains exact source and harness bytes used for diagnosis, stored as inert text where appropriate.

The three original, unmodified ZIP downloads are committed and SHA-256 matches both GitHub's artifact metadata and the complete upload logs:

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| ordinary / 13147191072 | 7119021 | `5774cda250e54b8fd553c775f249dbc4892bb3a48fa3f971d4979f13ec43eeee` |
| wagodebug / 13147012458 | 7152171 | `9cb0f755d31a91ec5933a30ff9f8e18ce4974d9e9b6a0e13e533844fc74cb468` |
| wago_regalloccheck / 13147114243 | 7163281 | `27786ac4c2340190e18dc8d2c2e380c6de3864efff64d62416731e190ae76fab` |

Downloads used a 180-second and 30 MB per-file bound. All download logs remain. ZIP CRCs, member bytes, binary hashes, command-log hashes and reported sizes were checked. All 41 files per artifact were retained locally under `_artifacts/`; text receipts are committed, while duplicated `.test` files remain available in the committed original ZIPs and local extraction. Complete logs for all four jobs that actually ran (three native cells and aggregate) are retained both as tool response JSON and readable text; skipped jobs have no execution logs. Job metadata and artifact metadata are retained.

The exact source `git archive` SHA-256 is `9ca226384ac27874ae0ffd36030931f805b232e963dcb1475f5c87b84660a3f5`. The audit regenerated its hash from the immutable Git object without checking out or executing it, compared every original-source manifest entry, and reconstructed every effective-source adapter hash. Original source files remain unchanged; the four test-only additions exactly match the pinned harness. All four driver hashes match the harness Git objects, and suite plans independently match 33/36/33 and required children.

Compiler archive SHA-256 is `493da3585e66c5da677a4be39402faf2041b4e45a61eb456e4547eca753a544e`; compiler executable SHA-256 is `1b74ee7ef9841f01364823be1254833a96832e74076957c956d89872f705b7d2`. The retained original release archive in the sibling supplement directory was rehashed read-only, including its compiler and pristine patched-input files. Each native job independently recorded the same archive hash. The exact task-exit renames and foreign-stack patches, all four patched hashes and patch-script source hashes match `toolchain.json`; patch application and negative controls exited zero in each mode. Full effective tool manifests are retained, and each runner's final integrity receipt attests unchanged source/tool/driver trees. The entire remote post-patch tool tree is not uploaded, so whole-tree post-run equality is runner-attested rather than independently reconstructable from downloaded files.

Host receipts record Darwin ARM64 (no Rosetta accepted by the gate), macOS 15.8.2, Xcode 16.4, Go 1.27.1 darwin/arm64, and TinyGo 0.42.0 using LLVM 22.1.4. All nine downloaded binaries independently have ARM64 Mach-O headers and hashes matching the directly executed artifacts. OS/SDK are attested infrastructure, not hermetic source pins.

Historical preparation/cross-build success and activation review were read for context; neither is substituted for native results. Initial read-only discovery attempted the user-shorthand `docs/darwin-tinygo-*` paths, found them absent, and used their actual `docs/rfwgc-evidence/` locations. A later read-only glob for backend optimization files was absent; the actual catalog was located under `src/core/compiler/optimization/`. These lookup failures did not launch tests or alter evidence. No failed download or audit execution was discarded.

## Missing gates and handoff

Native all-three-mode qualification remains failed. Required unresolved gates are the ordinary resume-oracle test, all-three cancellation/Close and cleanup completion, and all-three guest relocation/control execution after valid ARM64 configuration. Full CI, other platform qualifications, excluded capabilities, and later `5b02`/`ca164` revisions receive no credit. Production triage requires new evidence if any failure remains after independently reviewed fixture repairs; this report authorizes no repairs or reruns.

Only this owned evidence directory is committed with `git commit --only`. This evidence-only change cannot affect shipped runtime performance; no measurement is needed. The final handoff names the resulting commit. Other workers' dirty files, index entries and gitlinks remain outside this commit.
