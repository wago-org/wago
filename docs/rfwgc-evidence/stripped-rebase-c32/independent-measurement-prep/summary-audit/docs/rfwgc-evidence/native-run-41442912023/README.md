# Native Linux ARM64 run 41442912023: failed backend census, RR3 passes

[GitHub run](https://github.com/wago-org/wago/actions/runs/41442912023), attempt 1,
completed November 2, 2026. **All three matrix jobs and the native aggregate FAIL.**
This is not a setup failure or a production test assertion failure. Native tests
executed; the backend execution census rejects one diagnostic-prerequisite skip
per mode. No qualification downgrade or successful full-CI claim is warranted.

Exact immutable inputs:

- Source: `5847df30f1af15d10b743d90a24b1429b290d358`.
- Harness/workflow: `6c71c4d6747456578f55928e900e36bb2d1449a8`.
- Activation review: `9f8e4cad8d2b6d5ce262967e48f275a8bb9ae872`.
- `dispatch.json` is preserved byte-for-byte as the original dispatch record;
  its pending status is historical, superseded by this audit.

## First blocking event and minimal scope

In **every mode**, `backend-backend.log` line 4 records this exact diagnostic:

```text
register_call_slot_arm64_test.go:51: compiler diagnostics require -tags=wago_codegenstats (also enabled by wago_profile)
```

Line 5 is `--- SKIP: TestRegisterCallOverlappingArgumentSlotARM64 (0.00s)`;
line 6 is its test2json `skip` event. The package exits zero, with eight top-level
passes and one skip. Harness `census.ok` is false and the driver exits 1.
All 35 recorded subprocess commands per mode exit zero; no pending command,
compiler failure, malformed census, failed assertion, or setup exception is
recorded. The aggregate then rejects the failed matrix, as designed.

Pinned `register_call_slot_arm64_test.go:51` calls `requireCompilerDiagnostics`.
Pinned `diagnostics_test.go` skips unless `diagnosticsEnabled`; the enabled file
requires `wago_codegenstats` or `wago_profile`. The reviewed harness's build loop
supplies only the matrix mode tag (no tag for ordinary), so none of these three
backend binaries enables diagnostics. This is a harness build-configuration
omission, not evidence of an ARM64 register-call runtime defect.

Minimal repair scope for the owner: separately review and pin a harness change
that supplies `wago_codegenstats` for the diagnostic backend build in each mode,
retaining the mode tag, all nine required names, strict zero-skip census, source
pin, original probes, and existing ordinary runtime coverage. Preserve the eight
uninstrumented backend passes as such; diagnostic-build evidence must be labeled
as diagnostic. No need demonstrated here for a production change, deadline
change, signal override, test removal, or skip allowance. **No fix or rerun was
performed in this audit.** A fresh reviewed harness pin and authorized native run
are needed to obtain the missing backend assertions; this run stays failed.

## All three modes

Counts below are top-level test passes, independently recomputed from raw JSON
and compared with the pinned plan and the uploaded summaries.

| Gate | ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Setup / native build | PASS | PASS | PASS |
| Focused | 85/85 PASS | 85/85 PASS | 85/85 PASS |
| Original shared-entry guard | 1/1 PASS | 1/1 PASS | 1/1 PASS |
| Focused with race detector | 85/85 PASS | 85/85 PASS | 85/85 PASS |
| Original guard with race detector | 1/1 PASS | 1/1 PASS | 1/1 PASS |
| Runtime interrupt/token/signal/deadline | 11/11 PASS | 11/11 PASS | 11/11 PASS |
| Backend | 8 PASS, 1 SKIP | 8 PASS, 1 SKIP | 8 PASS, 1 SKIP |
| Unarmed timer capability control | 6/6 PASS | 6/6 PASS | 6/6 PASS |
| Source and harness integrity | PASS | PASS | PASS |
| Qualification / driver exit | FAIL / 1 | FAIL / 1 | FAIL / 1 |

Per mode: 192 top-level starts, **191 passes, zero fail events, one skip** across
the six test commands. All six package commands exit zero and report package
pass; the stricter harness rejects the skip. No planned top-level test is absent
or unstarted. **NOT_RUN:** the skipped register-call test's `direct` and
`indirect` execution/assertion bodies. This must not be counted as nine backend
passes. All passing gates have balanced unique start/pass events for every
subtest and exactly one package pass, with no skips or failures.

The focused census is **85 ARM64-applicable tests, not 100**. The 15 AMD64-only
families remain NOT_RUN on ARM64, with names/build constraints retained in
`inputs/prior-census-audit.json`; these exclusions are not runtime skip events.
Darwin, Windows, broader normal CI and later router repairs are NOT_RUN by this
lane. The 23 skipped GitHub jobs are explicit in `jobs.json`; these include the
normal aggregate, whose qualification-dispatch name states full CI was not run.

## Exact original outcomes and timer discriminator

Every mode passes both the ordinary and race versions of:

- `TestRRRecheckStorageOnlyReturnBehindDynamicReader`, including `none` and `table`.
- `TestRRRecheckCanceledAdmissionBehindDynamicReader`.
- `TestRRRecheckSharedMemoryHandoff`.
- All five `TestRRRetirement*` lifecycle/borrowed-root families, including all
  four last-pin/revival subcases.
- `TestSharedEntryGuardCancellationAndGC` with its unchanged original deadline
  and admission/GC assertions.

`audit.json` enumerates every original outcome and the six original probe
SHA256s. Each hash matches the source Git blob and both uploaded integrity
manifests. No source overlay or probe adaptation was applied.

Each mode's `timer-control.log` records native `arch=arm64`, CLOCK_MONOTONIC
(`clock=1`), a 64-byte event, and **errno 0 for all six** combinations:
notify 1 / 0 / 4 crossed with signal 35 / 64. Timer source SHA256 is
`f6e350bdf4efd8e2da007a81b9e98f9ce2bad2a8a535ed89245c8e497d470b4f`, identical
to the archived QEMU discriminator. The control creates/deletes unarmed timers;
it does not itself prove signal delivery. Original unmodified deadline probes
provide separate execution evidence and do not log which runtime signal was
selected, so this audit does not claim they specifically delivered signal 64.

The retained `docs/rr3-retirement-repair/TIMER-DIAGNOSIS.md` records QEMU ARM64
EINVAL for signal 64 with notify 0/4, and failure of the original storage-only
probe at this same source pin. This native run has errno zero for those controls
and passes both original storage-only subcases in all modes, including race.
That is a narrow, receipt-backed platform discriminator; the earlier QEMU
failures stand. No blanket native/QEMU correctness or compatibility inference.

## Identity, integrity and retained evidence

GitHub job logs identify `ubuntu-24.04-arm` (image `20261025.165.1`). Each
`host.json` reports Linux/aarch64, runner ARM64 and Python ELF machine 183.
Go host and target are linux/arm64, Go 1.27.1, CGO enabled. Go, WABT and all four
test binaries are recorded as ELF machine 183; this audit independently checks
the archived test binaries and timer binary without executing them. Native
hardware attribution relies on the provider runner identity, not hypervisor
attestation. Commands contain no emulator or execution adapter.

- Go executable SHA256: `1675694ef690db0f18fbe7046a886170904bede1d9db6ec96ae27945c1705c64`.
- WABT 1.0.41 executable SHA256: `26e6fe02eeed144d0c91d8fcdf257d994b33d7e6885aa93e92a54d45027d5cee`.
- WABT ARM64 archive provenance SHA256: `5e35416ee8725dc7cc0572e4392a8117cbf008b0e34c0db65c75506b0299cdbf`.
- Per-mode binary hashes, probe hashes, command argv/cwd/timeout/exit, host,
  toolchain and WABT provenance are retained in `audit.json` and mode directories.
- Each mode's 18,364 source and 18,634 harness blob SHA256 manifests match before
  and after; clean diff/status receipts and raw Git trees match exact pins.
  The driver verified every checked-out blob against Git; this offline audit
  additionally verifies the trees, manifest equality and original probe hashes.

All three **original artifact ZIPs** are retained, including binaries, complete
manifests and logs. Their SHA256s match the GitHub artifact API digests:

| Mode | Artifact ID | ZIP SHA256 |
| --- | --- | --- |
| ordinary | 13140298947 | `e5ebc35071616845720dfdb747b3dbd06db3a58ba985d4157eac47714d4dc0d2` |
| wagodebug | 13139564738 | `860ecddcd9ca3b5c2d4ca85b6b67d37b12c82ecdeb7da8d9eede0f20eb635e26` |
| wago_regalloccheck | 13141047733 | `1dfe9177b06c12a1f04e33dc235f921fc80f308feadf301de635fe1515a38d63` |

`run-logs.zip` is the original full GitHub log download, including per-step logs
and the failing aggregate. Four `job-*.log` files are byte-exact aggregate entries
from that ZIP; `.gz` copies preserve them compressed. `SHA256SUMS` covers all
retained files except itself. `run.json`, `jobs.json`, and `artifacts.json` retain
API metadata. The connected GitHub tools supplied jobs/artifacts and artifact
downloads; `gh api` retrieved the full compressed log archive and run metadata.
An attempted individual-job `gh api` download failed; the complete run archive
provided the exact raw logs instead. No log is inferred from a truncated view.

`python3 docs/rfwgc-evidence/native-run-41442912023/audit.py` is an offline-only
receipt validator (exit 0); output is `audit-validation.log`. It reads immutable
Git objects and ZIPs and checks hashes, ELF headers, commands, exact listings,
raw event counts, original outcomes and integrity across all three modes. No
runtime rerun or benchmark is involved. During authoring, two parser checks
needed corrections (sort compiled listings; ignore timer prose tokens); final
validation succeeds without altering any archived receipt.

No newly verified production finding was identified, so the conditional sibling
production audit was not triggered. No edits outside this owned evidence area,
source changes, dispatch, push, PR, runtime rerun, benchmark, or subagents.
Documentation and receipt storage cannot affect shipped runtime performance.
The November 4 target remains blocked on the missing diagnostic backend evidence
and separately pinned remaining platform/router work; this audit does not close
those obligations.

Authored README and audit script pass `git diff --check`. The full evidence diff
reports trailing whitespace in verbatim GitHub logs; those bytes are deliberately
preserved. All retained-file checksums pass. Original dispatch SHA256:
`587ee2ef99405f67e7a4d4e52a8d42327558ce465fe24cbb69e45ebb9c2ae4b6`.
