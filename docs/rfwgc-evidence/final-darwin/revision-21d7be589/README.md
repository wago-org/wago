# Pinned reservation and two-result native qualification preparation

Execution source: `21d7be58952b64435110246f65d44e11046c5083`. Harness SHA is the
commit containing this revision, supplied separately as both workflow execution
SHA and `harness_sha`; it is deliberately not embedded in its own commit.
**Prepared for parent activation review; no push, dispatch or native closure.**
Reservation repair `65df15685` has independent review `4f77ca9ce`; two-result
repair `21d7be589` has independent READY review
`367c026239607fc621f219b88a52929f3354de00`. Review commits do not change the
execution pin. Parent activation review of this harness remains required.

The complete audit in `native-run-41466555967` remains historical evidence:
Darwin ordinary CANCELLED, no artifact; debug and checked full race FAIL at raw
uintptr reconstruction; checked established diagnostic FAIL at the two-result
native bridge. Windows storage passed all three modes at the old pin. None of
these results transfers to the new source. Exact historical fault causation is
unproven. Ordinary is mandatory, not waived.

## Gates and receipts

Each Darwin mode runs the retained cancellation (one), Core 3 child (one), and
repaired contracts (ten) discriminators, followed by two new isolated gates,
before all 21 broad gates for that mode:

- `reservation`: exactly `TestGCARM64LiveStructDoesNotPrepareUnusedHandles` and
  `TestGCARM64WideArrayDoesNotPrepareUnconsumableState`, `-race`, `-count=1`,
  `-timeout=30s`, package `./src/wago`. Go race/checkptr is not disabled.
- `two-result`: exactly `TestTwoIntegerResultRegisterCall`, `wago_codegenstats`
  plus the mode tag, `WAGO_SHARED_SCALAR=0`, `-count=1`, `-timeout=30s`, package
  `./src/core/compiler/backend/railshot/arm64`.

No overlay changes either repaired test. The source preserves regabi count 1,
inputs 0/1/7/100 and results 1/3/15/201, raised-fence `TrapStackFenceBreached`,
and restored-fence reuse result 201. Exact top-level terminal census is required;
missing, duplicate, unexpected, failed or skipped tests fail a discriminator.
Raw JSON logs, effective argv/environment, package census, expected names and
terminal events are retained. Process or census failures remain failures even
if expected test events appear. A failing gate does not suppress later independent
gates; the driver returns 1 after all available gates. External cancellation
still stops execution and preserves partial artifacts; completion is not claimed.

All 63 broad command objects are unchanged, including full race, full backend,
default and established diagnostics, 60-minute suite deadlines, native
cancellation, and Core 3/spec exact-total checks. Focus remains 73 top-level
tests including 22 RR, positive multi-table sum 545/host hello/full collection,
make 42, initializer 55 and migrated positive children. Identical matched graph
fixtures and baseline `a3c08365f531eb3c9003e3acf3e0a822c12c31f4` remain:
candidate six false/true leaves, baseline three false leaves; no invented
baseline nursery counterpart. Core 3 child requires 10 modules/18 assertions,
broad Core 3 215/690, and spec v3 each route 2,226/58,238, with zero gaps.
Windows retains six storage/admission contracts per mode; this is not Windows
converted-GC JIT qualification.

Total: 63 broad + 15 discriminators + 3 focus + 3 baseline + 3 Windows = **87 test
commands with paired package censuses**. No failfast or continue-on-error waiver.
The existing cap is 3×360 Darwin + 3×30 Windows + 3 summary = **1,173 job-minutes**.
This is a ceiling, not measured usage or a runtime estimate. Runtime, queue time,
completion time and monetary cost are unknown. No benchmark or timing was taken.

## Validation and source integrity

`local-validation.json` records ordinary, wagodebug and wago_regalloccheck
policy/planner checks, actionlint, exact identities (including rejection of the
previous full source pin), dirty/mismatched harness refusal, native host refusal,
positive/negative exact census checks and aggregate summary states. The complete
workflow is compared byte-for-byte with `041f7ed4c376e2a33ce21b22075cd617ddca6597`,
without substituting source pins. No CI-policy or workflow edits were needed.

`continuation.json` executes the actual driver loop with synthetic subprocesses:
each of the two new gates fails independently in each mode, all 26 suite commands
still run, logs exist, and aggregate exit remains 1. This is a harness test,
not native execution evidence.

`target-checks.json` and logs record 15 Go 1.27.1 cross-builds and exact source
selection: all-three Darwin candidate/focus, Windows candidate/focus, matched
Darwin baseline, plus reservation and established two-result targets. Reservation
cross-builds use CGO=0 without race; native Darwin race remains required. Foreign
binaries were never executed. Complete source and baseline manifests were
attested byte-for-byte against immutable Git blobs before and after builds.
`harness-manifest.json` hashes the executable harness, selected contracts and
fixtures. Runtime checks continue to require separate clean source/harness
checkouts, exact HEADs and before/after manifests including initialized submodules.
Local archives do not attest submodule contents or native infrastructure.

`whole-head-delta-from-041f.txt` records the full committed history delta at audit;
`source-delta-from-041f.patch.gz` and `source-relation.json` distinguish it from the
individual repair commits. Across that whole interval, outside docs only the
two test files and the Call2 explanatory comment changed. Executable production
code is unchanged. Later review/ledger commits do not change execution source.
This preparation edits harness/evidence only and cannot affect shipped runtime
performance; it needs no measurement lease.

Native ARM stays pinned to `08f511fac41a77f4c0f9046f88ebc78ad05a5da6`; TinyGo stays
pinned to `7e41fafbbcd737ed1edce11b94b43b4eda134dcf`. Their dispatch contracts remain
bytewise unchanged. No equivalence across those pins is asserted. No source,
backend, arm64spike or test repair edit was made. Shared dirt/index/gitlinks and
failed-child `darwin-two-result-bridge` artifacts are outside this commit.

Readiness limitation: activation review, then an explicitly authorized native
run with all three complete Darwin receipts (especially ordinary), race and
backend diagnostics, all Windows receipts, identity/source-integrity and census
review are still required. A discriminator pass alone cannot close the previous
crashes or qualify a failing broad suite. No push or dispatch was performed.
