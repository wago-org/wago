# Native Linux ARM64 RR3: receipt-backed closure at the exact source pin

[Run 41445401972](https://github.com/wago-org/wago/actions/runs/41445401972),
attempt 1, completed successfully November 2, 2026. Independent raw-receipt
validation supports **RR3 native Linux ARM64 closure only**, for:

- Execution source: `5847df30f1af15d10b743d90a24b1429b290d358`.
- Harness and workflow: `4af18b3a2484b901e11f7347adab8e547176dbc6`.
- Activation review: `ccfa39bd9604830cd10f73d5d764c2fd54687542`.

`dispatch.json` remains byte-for-byte unchanged; its pending status is historical.
The run's API head is the harness pin, not the production execution source. The
source was compiled from its separate pinned checkout. The activation review is
retained as `inputs/activation-review.md` from its exact Git object.

## Independently counted outcomes

| Gate | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Focused | 85/85 PASS | 85/85 PASS | 85/85 PASS |
| Original cancellation/GC guard | 1/1 PASS | 1/1 PASS | 1/1 PASS |
| Focused with race detector | 85/85 PASS | 85/85 PASS | 85/85 PASS |
| Original guard with race detector | 1/1 PASS | 1/1 PASS | 1/1 PASS |
| Runtime interrupt/token/signal/deadline | 11/11 PASS | 11/11 PASS | 11/11 PASS |
| Diagnostic backend | 9/9 PASS | 9/9 PASS | 9/9 PASS |
| Unarmed timer controls | 6/6 PASS | 6/6 PASS | 6/6 PASS |
| Driver exit | 0 | 0 | 0 |

Each mode has **192 top-level starts and passes, zero failures, zero skips**.
Across modes: 576 top-level passes. Including subtests, the six gate pass-event
counts per mode are 206, 1, 206, 1, 11 and 111 (536 total). Starts and passes
balance uniquely for every subtest. Every exact compiled test listing matches the
unchanged plan. All six test commands exit 0 with exactly one package pass each;
all 35 recorded subprocess commands per mode exit 0, with none pending. The
native execution step, all three matrix jobs and their aggregate succeed. The
driver exit 0 follows from its successful shell step and pinned return logic;
the 35 subprocess exits are explicitly recorded in `commands.json`.

The backend build commands retain the mode and supply exactly:

| Mode | Backend tag argument |
| --- | --- |
| ordinary | `-tags=wago_codegenstats` |
| wagodebug | `-tags=wagodebug,wago_codegenstats` |
| wago_regalloccheck | `-tags=wago_regalloccheck,wago_codegenstats` |

The audit compares complete compiled argv, source cwd, race flag, output path
and package. Wago, wago-race and runtime builds have only their original mode tag
(no tag for ordinary), and no `wago_codegenstats`. This is diagnostic backend
evidence; it does not silently replace ordinary production builds. All nine
backend names and their subtests are retained in raw JSON. Specifically,
`TestRegisterCallOverlappingArgumentSlotARM64/direct` and `/indirect` both pass
in every mode, with no prerequisite skip.

Original storage-only `none`/`table`, canceled admission, shared-memory handoff,
all five `TestRRRetirement*` families and the original shared guard pass normally
and under race in every mode. Exact subtest outcomes are in `audit.json`.
The six original probe hashes match the unchanged plan, exact source blobs and
both source manifests. No source overlay, deadline change or signal override.
Focused coverage is 85 ARM64-applicable names, not 100; the 15 AMD64-only names
and build constraints remain explicitly excluded in `inputs/prior-census-audit.json`.

## Timer discriminator and platform identity

All six combinations of notify 1/0/4 and signal 35/64 report errno 0 in every
mode, with `arch=arm64 clock=1 size=64`; timer build and execution exit 0.
Timer source SHA256 is
`f6e350bdf4efd8e2da007a81b9e98f9ce2bad2a8a535ed89245c8e497d470b4f`,
identical to the archived discriminator. These are unarmed create/delete
capability controls, not signal-delivery tests. The original deadline probes
supply separate execution evidence and do not identify the selected signal;
no claim of specific signal-64 delivery is made. Prior QEMU EINVAL and original
probe failures remain valid historical evidence, not overturned by native passes.

The raw job logs identify `ubuntu-24.04-arm`, image `20261025.165.1`. Host receipts
show Linux/aarch64, runner ARM64 and Python ELF machine 183. Go host/target are
linux/arm64, Go 1.27.1, CGO enabled. Archived test and timer executable ELF headers
are independently checked as ARM64 without executing them. Go and WABT identities
are recorded by the pinned native driver; those tool binaries themselves are not
included in the artifact. Native hardware attribution relies on the provider's
runner identity, not hypervisor attestation. Complete argv contain no emulator
or execution adapter.

- Go executable SHA256: `1675694ef690db0f18fbe7046a886170904bede1d9db6ec96ae27945c1705c64`.
- WABT 1.0.41 executable SHA256: `26e6fe02eeed144d0c91d8fcdf257d994b33d7e6885aa93e92a54d45027d5cee`.
- WABT ARM64 archive SHA256: `5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf`.

Per-mode binary hashes, complete commands, host/toolchain and WABT provenance
are preserved. Source manifests contain 18,364 blobs; harness manifests contain
18,930 blobs. Before and after are equal, and this audit independently computes
SHA256 from **every pinned Git blob** and compares every manifest entry. Raw Git
trees match the exact pins and all diff/status receipts are clean. Conformance
gitlinks are deliberately uninitialized by this lane and are not executed.
The workflow, probe/census plan and timer source are byte-identical to the prior
reviewed harness. Job-log summary JSON equals the uploaded summary in each mode.

## Raw evidence and preservation

All three original artifact ZIPs and the original complete `run-logs.zip` are
retained. ZIP lengths and SHA256s match the GitHub API, and upload log digests
match those same bytes. ZIP CRC checks pass.

| Mode | Artifact ID | ZIP SHA256 |
| --- | --- | --- |
| ordinary | 13142276997 | `446b6979c32bcc8ca3b5a7311ac52e7ec2b05a9b5d446335b9827cd70a373c21` |
| wagodebug | 13141677372 | `a106abbba43bbde7e7af13dfacfc024f424379c74ea263aa6b4ca2cacb60904c` |
| wago_regalloccheck | 13142352394 | `59e4bb04c613133066b0153aa05425d28ad702b2dbe4f658294f642ba3ae70a9` |

API metadata is in `run.json`, `jobs.json`, `artifacts.json`. Raw downloads used
`gh api`. Four `job-*.log` files are byte-exact entries from the full log ZIP,
with deterministic gzip copies. Human-readable mode receipts are exact artifact
bytes; full manifests and binaries remain in the ZIPs. `SHA256SUMS` covers every
retained file except itself. The log archive has a locally recorded digest;
GitHub does not provide an artifact-style API digest for that archive.

`python3 docs/rfwgc-evidence/native-run-41445401972/audit.py` validates receipts
offline using Git objects and archives only; exit 0, output `audit-validation.log`.
It derives the raw event counts rather than trusting GitHub success or uploaded
census assertions. Its baseline is the prior audit's parser, updated for the new
run and extended with full-blob, exact argv and log-summary checks. No runtime
rerun or new benchmark was performed.

Every file in prior failed `native-run-41442912023/` is hash-checked unchanged
against `preservation-before.json`. That run remains failed: eight backend
passes and one diagnostic-prerequisite skip per mode, even though its other
lanes passed. This new run supplies the missing backend evidence without
rewriting the failure. Original dispatch bytes are separately hash-checked.

## Scope and current limits

This is native Linux ARM64 correctness evidence for the **old pinned production
source** only. It is a baseline for later work, not validation of current HEAD.
The runtime 11/11 result covers the listed original probes at that pin; it does
not establish current production runtime correctness, all runtime behavior,
current baseline parity, or a performance comparison. Later Darwin work, router
repairs and follow-up production heads are **not covered**. Darwin, Windows,
normal full CI, broader conformance and later repair qualification remain outside
this run. `jobs.json` explicitly records 23 skipped jobs, including the normal
CI aggregate; a green special workflow is not full-CI success. This closure alone
does not establish overall release readiness.

No new production finding or artifact mismatch was identified, so no sibling
production-finding audit was triggered. No source/harness edit, dispatch, push,
PR, subagent or benchmark. Only this owned evidence directory is committed;
unrelated dirt, index entries and gitlinks are preserved. Documentation and
receipt storage cannot affect shipped runtime performance.
