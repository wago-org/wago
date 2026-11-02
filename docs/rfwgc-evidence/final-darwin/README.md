# Next exact-pin native Darwin qualification — proposed only

Execution source: **`da19f1a8d29fec7305d937a42622a198b8e79717`**. This is the
latest committed head requested after the ARM64 handoff. Initial preparation at
`76cdc5cb77f505176f74abb7c17e4bf454da8650` is retained in `revision-76cdc5cb7/`.
The new source contains c786 token authentication, d8dc/a915 join work, bc624
helper fixtures and 02eaf/b943 worker comparison repairs. ARM64's tested source
was b9433aa2050c8069baa3edbaa50bd4e8e9c95b26; its QEMU passes are not native
Darwin qualification. Later source changes are **unqualified** by this pin.

**No push or dispatch performed. Native Darwin remains NOT_RUN at this pin.**
Parent's independent ARM64 review and review of this harness must precede
activation. There are no waived gates, deadline changes, production edits, test
source edits, benchmarks, timing measurements, subagents or PRs in this work.
Evidence/harness changes cannot affect shipped runtime performance.

## Exact execution and diagnostic gates

All three modes (ordinary, wagodebug, wago_regalloccheck) retain the original
Darwin conversion-rejection contracts and all six generated graph leaves.
The graph option repair from harness 1fe8 remains; fixture hashes and assertions
are unchanged, and duplicate graph leaf passes are now rejected. Windows keeps
its four storage/rejection/admission contracts, not a GC JIT qualification claim.

Each Darwin broad driver now starts with three independent narrow lanes:

1. **Unchanged TestSharedEntryGuardCancellationAndGC**, exact single-test pass,
   no skips. Its intrinsic three-second cancellation deadline and admission/GC
   assertions remain untouched. QEMU failed cancellation in all modes while
   preceding admission/GC passed; the earlier native run did not flag this test.
   The ARM64 cancellation audit finds no source-level link to the join repair.
   Native host interruption, delivery, matching and unwind remain discriminators.
2. **Exact Core 3 child**, selecting TestWasmtimeCore3Corpus with
   WAGO_WASMTIME_CORE3_FIXTURE=gc/const-expr-gc.wast, protocol 2, an explicit nonce,
   WAGO_BOUNDS=explicit, verbose JSON events and a 30-second test timeout (the
   existing parent child budget). No enclosing corpus parent suppresses stdout.
   Require exactly one authenticated marker with **10 modules/18 assertions,
   zero failures/skips**, plus exactly one top-level pass and process exit zero.
   Zero exit with 9/17 passed plus 1/1 skipped is a **failed gate**.
3. **Ten repaired contracts**, including original converted-global/table
   rejection behavior and shared import, fallback, result/argument and native-pin
   tests. Cancellation is separate so it cannot obscure this census.

These add nine commands across modes to the unchanged **63 broad commands**.
Broad Core 3 still requires **215 modules/690 assertions**. The prior parent
205/672 excludes the entire 10/18 fixture because its skip check precedes total
accumulation; totals and expectations are not adjusted.

The child receipt retains complete raw output, all marker payloads, exit code,
exact census, observed rejection reason/phase/source line, mapped module-definition
filename, the entire commands.json and all fixture hashes. Thus a rejection at
module_instance line 96 can be tied to commands.5.wasm, with assertion line 97
preserved in the command file. Converted-global initialization is **suspected,
not confirmed** until native output establishes it. Unemitted assertion reasons
are not fabricated; a missing marker, malformed marker or crash fails qualification.
The native artifact, not a source inference, must establish the exact reason.

A narrow diagnostic failure does not stop subsequent independent lanes. A
focused-contract step failure no longer suppresses the broad driver after
successful setup; there is no continue-on-error. Setup failure/cancellation still
blocks unsafe execution. Original failed step/job status and driver aggregate
failure remain. Every attempt gets a new output directory and artifact attempt
suffix; source and driver manifests are checked before/after, and running drivers
must never be edited. All historical failed attempts and prior native archives
remain intact.

## Identity and CI isolation

Source and harness are independent checkouts. `identity.py` requires the full
source pin, exact workflow execution HEAD and a clean harness. The native driver
requires native Darwin ARM64 without Rosetta, exact Go 1.27.1, clean initialized
source/submodules and rejects inherited target/test overrides (allowing only the
three setup-provided interpreter path/identity variables). `contracts.json`
binds source and fixture hashes. Each mode records complete source and driver
manifests. The new immutable export was verified against all **17,895 Git blobs**;
its compressed SHA256 manifest is `revision-da19f1a8d/source-manifest.json.gz`.

Only `.github/workflows/ci.yml`'s special Darwin setup identifier/continuation
condition and native artifact pin labels changed. Normal CI bodies, planner and
summary contracts remain unchanged. No special dispatch produces a success or
skipped check named CI. Normal ci-ok still depends on every job; special summary
is acyclic and requires both native matrices successful and all normal jobs
skipped. Neither special-summary success nor local checks mean full CI success.

`revision-da19f1a8d/local-validation.json` records syntax, actionlint 1.7.7,
identity/refusal and exact/negative census checks, 64 summary state combinations,
and policy/planner/full-profile verification in all three modes. Tests used the
explicit Go 1.27.1 install and matching GOROOT, GOTOOLCHAIN=local, and PATH without
mise shims. `immutable-check.json` separately records all six policy/planner
runs against an immutable archive, with only the proposed workflow overlaid;
all pass, no unexpected source changes. These are harness checks, not native
runtime execution. The initial-pin validation receipt is historical; the fresh
repin validation supersedes it. No failed attempt was deleted.

## Activation review and unresolved gates

After parent review and separately authorized publication, the **proposed only**
dispatch is:

```sh
gh workflow run ci.yml --repo wago-org/wago --ref experiment/rfwgc-port \
  -f darwin_qualification=true \
  -f source_sha=da19f1a8d29fec7305d937a42622a198b8e79717
```

Parent must verify the remote branch equals the reviewed harness SHA and confirm
source reachability before activating. Any later harness change needs refreshed
identity/review. Artifact labels are darwin-da19f1a8d and windows-da19f1a8d plus
mode and attempt. Retain job/setup logs as well as all six artifacts, including
failed/partial runs. Missing events or artifacts cannot qualify a gate.

Open gates and blockers:

- Native cancellation, Core 3 zero-gap census, repaired contracts and broad
  Darwin correctness: NOT_RUN at this source. If the suspected converted-global
  initializer rejection is confirmed, harness work cannot close it; parent needs
  a production ownership/lifetime implementation. No scope waiver is assumed.
- Native Linux ARM64 cancellation remains a separate parent gate.
- TinyGo: NOT_RUN. A reviewed native Darwin compiler archive/hash and exact
  patched runtime identity are still missing. `tinygo_setup.py` is preparation
  only; no TinyGo execution or qualification is invented.
- Matched baseline graphs at a3c08365f531eb3c9003e3acf3e0a822c12c31f4 remain
  NOT_RUN and outside this dispatch. Adding an independently checked baseline
  export/overlay/census is a separate supplement; candidate success cannot
  substitute. All six candidate fixtures retain their identical historical bytes.
- H6 ownership remains intentionally unsupported on Darwin, not implemented by
  this harness; platform census and skipped broad cases require parent review.

Nov 4 deadline feasibility and repair cost remain **unknown**, pending native
execution and the Core 3 reason. Existing ceilings remain 1,080 Mac plus 90
Windows job-minutes per attempt, plus summary job and seven-day artifact retention;
these are configured bounds, not measured runtime or cost estimates.
