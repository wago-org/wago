# Native Linux ARM64 evidence at 08f511fac — no final-port acceptance

[Run 41466556039](https://github.com/wago-org/wago/actions/runs/41466556039), attempt 1, provides **PASS native runtime coverage for all three original failing assertions in every mode**, normally and with race detection. The original three ARM64 QEMU repair cells remain **FAIL**. This audit does not waive them, identify every QEMU failure as a timer failure, or accept the final port.

Exact execution source: `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`.
Harness/workflow: `041f7ed4c376e2a33ce21b22075cd617ddca6597`.
Activation review: `57c04aa09c53321e86a8358ae7fc53688af82b2b`.
API head is the harness; the execution source is the independently pinned checkout. `dispatch.json` is preserved byte-exact, including its historical PENDING verdict.

## Independently verified native results

| Gate | ordinary | wagodebug | wago_regalloccheck |
|---|---|---|---|
| Focused | PASS 85/85 | PASS 85/85 | PASS 85/85 |
| Original cancellation/GC guard | PASS 1/1 | PASS 1/1 | PASS 1/1 |
| Focused with race detector | PASS 85/85 | PASS 85/85 | PASS 85/85 |
| Original guard with race detector | PASS 1/1 | PASS 1/1 | PASS 1/1 |
| Runtime interrupt/token/signal/deadline | PASS 11/11 | PASS 11/11 | PASS 11/11 |
| Diagnostic backend | PASS 9/9 | PASS 9/9 | PASS 9/9 |
| Unarmed timer controls | PASS 6/6 | PASS 6/6 | PASS 6/6 |
| Original 5b02 ARM64 QEMU repair cell | **FAIL** | **FAIL** | **FAIL** |
| QEMU rerun at 08f | NOT RUN | NOT RUN | NOT RUN |
| Full CI/full port at 08f in this run | NOT RUN | NOT RUN | NOT RUN |

Each mode has 192 top-level starts/passes, zero failures/skips/missing planned names; 576 top-level passes across modes. Including subtests, the six gates contain 206, 1, 206, 1, 11, 111 pass events (536 per mode). Every run event has exactly one matching pass. Package terminals are PASS, all six execution exits are 0, and all 35 recorded subprocess commands per mode exit 0. The driver exit is 0 from the successful native shell step and pinned driver return logic. Three matrix jobs and their summary succeed; **25 other jobs are skipped**. A green special workflow is not full-CI qualification.

`audit.py` derives these counts from original events and checks complete build/run/list argv, cwd, mode tags, race flags, package names, exact regex lists, `-test.v=test2json -test.count=1 -test.timeout=3m`, and summary/log agreement. Race binaries explicitly compile with `-race`. Backend tags are respectively `wago_codegenstats`, `wagodebug,wago_codegenstats`, and `wago_regalloccheck,wago_codegenstats`. Other binaries retain only their mode tag. Backend diagnostic evidence is separately labeled; both `TestRegisterCallOverlappingArgumentSlotARM64/direct` and `/indirect` pass without prerequisite skips in every mode.

## Original QEMU failures versus exact native assertions

Historical source: `5b02ec67afe0dce181a7f8d508e270c7ecb0ecfc`, `../final-combined-5b02/qemu-repairs/`. Each mode has 76 top-level starts and 181 total run events, 177 pass events, four fail events (including the storage parent), zero skips, and process exit 1. All three failing leaves are:

| Exact name | QEMU all three modes | Native all three modes | Native race all three modes |
|---|---|---|---|
| `TestRRRecheckStorageOnlyReturnBehindDynamicReader/none` | FAIL: timer EINVAL | PASS | PASS |
| `TestRRRecheckStorageOnlyReturnBehindDynamicReader/table` | FAIL: timer EINVAL | PASS | PASS |
| `TestSharedEntryGuardCancellationAndGC` | FAIL: native loop did not cancel | PASS | PASS |

Storage probe: original 5-second spin-entry limit, 2-second first-call wait and 1-second second-call context remain unchanged. Native logs explicitly report `storageReturnBlocked=false secondAdmissionErr=<nil>` for both leaves, normally and under race. The result-7 check, first-call return, second admission and cleanup all execute. QEMU logs instead report `create native deadline timer: invalid argument` for the second admission; first return was already unblocked.

Guard probe: unchanged source sets both ARM64 import variants to `1`, sets GOMAXPROCS to 1, requires native-scalar-leaf admission, enters the original loop with `0x7fffffff`, requires GC progress, rejects premature loop exit, cancels, and requires `context.Canceled` within the original 3 seconds. Native PASS with no skip covers all these sequential assertions, including the previously failing final cancellation assertion. **This guard uses cancellation without a deadline. Its QEMU failure is not proven to have the timer EINVAL cause.** Native success discriminates platform execution; it does not establish QEMU root cause.

`qemu-native-comparison.json` preserves every historical build/run argv and exit, exact failure output, native argv, leaf outcomes, and full name-set differences. Historical commands use Go 1.27.3, CGO disabled, `qemu-aarch64 -cpu max`, `-test.v -test.count=1 -test.timeout=12m`, and a broad repair regex. Native uses Go 1.27.1, CGO enabled, direct native execution, exact anchored lists, separate focused/guard processes and 3-minute outer timeout; race adds separate executions. Internal probe deadlines/oracles are unchanged and the native outer timeout is not relaxed.

**Assertion coverage: equivalent for every failing assertion, and native executes every substantive QEMU repair name/subtest. Whole-gate/environment equivalence: no.** Only `TestPlatformExecutionArchitecture`, an added QEMU export probe, is NOT RUN by native; runner/host/ELF receipts supply separate native architecture evidence. Native adds 11 wago top-level names and runtime/backend/race controls. The native run is neither an execution of the original QEMU command nor the entire 220-gate combined suite. Earlier AMD64-only census exclusions remain explicit in `inputs/prior-census-audit.json`.

## Independent source and Linux-selection proof

`compare.py` reads exact Git objects and the retained historical QEMU export, independently of worker-complete `25f68f2`. All **1,293 non-test entries under src/** are byte-identical between 5b02 and 08f, and each actual historical export production file matches its original Git blob identity. Complete-tree comparison finds exactly five non-doc changes: workflow, CI policy tests, TinyGo physical-stack test, multitable test, and RR flag helper formatting. `inputs/non-doc-source-delta.patch` retains the exact changes; `source-comparison.json` records object identities.

All six original native-plan probe hashes match exact current source and uploaded before/after manifests. Each historical QEMU export probe independently matches 5b02. Five probes are byte-identical across pins; `rr_recheck_flags_current_test.go` has identical non-whitespace bytes, the same returned field, and unchanged build constraint. This formatted helper adapter does not weaken an oracle or deadline.

`linux-selection-proof.json` and retained source blobs verify the relevant selection:

- Linux ARM64 selects `gc_conversion_store_host.go`; the opposite implementation is excluded. Both implementations are unchanged.
- The multitable fixture is unchanged; independently decoding its Wasm table section gives an externref first table. The selected host helper therefore returns true, so both old and new predicates take the same positive Linux route. Sum 545, host value `hello`, collection assertions and fixture bytes remain intact. The predicate change affects other targets, not this Linux assertion path.
- `engine_int_tinygo_test.go` requires TinyGo and is excluded from these normal Go binaries. Its changed oracle is not claimed native-executed here.
- RR probes and the shared guard are selected by their original Linux/ARM64 constraints. The historical extra stage7 portability fixture and architecture probe are separate QEMU export adapters; they do not change these original probes. Native source manifests prove no overlay.

No new production defect was found. There is therefore no new production finding to triage or trigger a sibling Slipstream/Starshine audit. The existing unresolved QEMU guard mechanism is not promoted into a new verified production finding.

## Identities, timers and bounded raw preservation

Runner logs identify `ubuntu-24.04-arm`, image `20261025.165.1`. Host receipts show Linux/aarch64, runner ARM64, Python ELF machine 183; Go host/target linux/arm64, Go 1.27.1, CGO enabled. Archived test and timer ELF headers independently identify ARM64 without executing them. Go and WABT paths/hashes and WABT 1.0.41 archive/bootstrap provenance are checked; tool binaries themselves are not in the artifacts.

All six timer combinations notify 1/0/4 × signal 35/64 report errno 0, `arch=arm64 clock=1 size=64`. Timer source SHA256 is `f6e350bdf4efd8e2da007a81b9e98f9ce2bad2a8a535ed89245c8e497d470b4f`, matching the pinned plan. These are **unarmed create/delete controls**, not signal-delivery tests. They do not establish signal-64 delivery or explain guard cancellation failure.

All three original artifact ZIPs (~195 MB total decimal) and complete original `run-logs.zip` are retained. ZIP sizes and SHA256 match GitHub API digests and upload log digests; CRC checks pass. Full binary bytes, source/harness before/after blob manifests and trees remain in original ZIPs. The auditor hashes **every pinned blob**, checks manifests before = after = exact pin, exact trees, clean tracked status/diffs, and all binary hashes. Human-readable receipts and four complete job logs are extracted byte-exact, with deterministic gzip log copies. `SHA256SUMS` covers retained files except itself.

| Mode | Artifact ID | ZIP SHA256 |
|---|---|---|
| ordinary | 13153236748 | `38c1f8fc67c00fb23b78f4aa6754a6b9faa019993bb6ff9dc09c2ab2eefbe8ea` |
| wagodebug | 13152011216 | `a6a7f544b36de4fb00b01caeaa479653faafa54c359512a9d2d3b283a666808c` |
| wago_regalloccheck | 13153815159 | `625c1c9a32f62e6abc7ba827d7f6a002b003b73681323928b8df4084bf3e3fdf` |

Artifact metadata/downloads used the connected GitHub tools; `gh api` supplied complete original run/job metadata and full ZIP logs. GitHub supplies no artifact-style digest for the full log archive, so its digest is locally recorded. No truncated tool-rendered log was used as primary evidence.

Offline validation: `python3 docs/rfwgc-evidence/native-run-41466556039/audit.py` and `compare.py`, both exit 0; outputs retained. These inspect receipts/Git objects only. The prior audit and original combined evidence are hash-checked unchanged against `preservation-before.json`. Dispatch SHA256 is separately checked. No build, runtime rerun, benchmark, dispatch, push, PR, implementation change or subagent. Evidence-only storage cannot affect shipped performance. Only this directory is committed; unrelated workspace dirt is preserved.

For the parent: bounded native coverage passes at these exact pins. The historical QEMU cells still fail; native gate coverage does not independently authorize their replacement or final acceptance. Full-CI, broader conformance, Darwin/Windows/other architectures and final-port acceptance are outside this run.
