# Focused non-backend repairs

Source commit: `30b0813f7881a6fbb938a21535a205803bd27a0c`.
Only the five parent-authorized `src/wago` test files changed in that commit.
It descends from backend worker commit `d8dc1a6dd`; the immutable qualification
therefore includes that dynamic import join repair. No backend file was edited
by this worker, and no mutable backend file was copied into qualification.

## Source policy and assertions

- `externref_global_conversion_test.go`: Linux retains the converted initializer,
  GetValue/roundtrip(7), SetValue/readg(42), and null publication checks. Darwin
  requires the specific unsupported-publication error and no returned instance
  for the original initializer. A null-initialized version of the same module
  then executes make/roundtrip for 7 and 42, rejects converted SetValue, checks
  unchanged public global value, and accepts null. No skip was added.
- `gc_extern_multi_table_test.go`: Linux retains sum 545, host identity, and forced
  collection. Darwin executes the same store, requires the specific unsupported
  publication error, compares all three table descriptors byte for byte with
  their pre-call snapshots, and checks host identity and collection. The first
  converted table store precedes all other table mutations, so rejection must
  leave all three tables unchanged. No sum call is valid after rejected setup.
- The three shared ARM64 test files retain their execution, value, callback-count,
  Caller lifetime, native-admission, foreign fallback, cancellation and GC checks.
  They assert native unwind maps exist and expect no direct shared Go transition
  and identical selector-off/on code. This is a **Linux and Darwin** policy, not
  an OS skip: `api.go` installs collector-free `nativeUnwind` plans for imported
  ARM64 functions; `prepareGCFrameCallsite` records Candidate return maps;
  `callHostSync` requires `!recordRoots` for `directSharedGo`;
  `nativeImportVariantEligible` rejects Candidate frame maps. Shared native pins
  require that native candidate. Callback admission remains a separate contract.

## Qualification

`qualify.py` exports the pinned Git tree (not a worktree), uses private home
GOCACHE/GOTMPDIR/GOMODCACHE, Go 1.27.1 with explicit matching GOROOT and
GOTOOLCHAIN=local, CGO disabled, and GOMAXPROCS=4. Its exact selector, commands,
exit codes, QEMU executable hash and binary hashes are in `qualification.json`.
Raw build/test logs are retained. No submodule checkout or gitlink was changed.
ARM64 execution uses actual qemu-aarch64 10.0.13; Darwin is compile-only.

Final verdicts are recorded per test in `verdicts.json` (regenerate with
`summarize.py`). In **each** of ordinary, wagodebug and wago_regalloccheck:

| Target | Result |
| --- | --- |
| Linux/amd64 native | 2 PASS, 0 FAIL, 0 SKIP |
| Linux/arm64 QEMU | 10 PASS, 1 FAIL, 0 SKIP after same-binary WABT replay |
| Darwin/arm64 | build PASS; execution NOT_RUN |

The initial ARM64 executions each had 9 PASS / 2 FAIL. The foreign fallback
failed before compilation of its WAT fixture because wat2wasm resolved through
mise. `replay-foreign.py` puts the installed WABT 1.0.42 binary first on PATH and
replays only that test with the identical hashed binary; all three pass. These
are combined per-test verdicts, **not three clean aggregate runs**. Both original
failures and separate replay logs/commands are retained.

`TestSharedEntryGuardCancellationAndGC` fails in all three QEMU modes with
`native loop did not cancel` at its unchanged three-second deadline. Native
admission and GC progress checks preceding cancellation passed. The native-run
archive did not identify this cancellation test as failing. QEMU reproduction
alone does not establish a new native production defect; do not waive it or
increase the deadline to claim qualification. Parent must run this unchanged test
on native ARM64/Linux and Darwin at the same pin, then investigate the runtime
cancellation/signal path if it reproduces. Fix cost is unknown pending that
native discriminator; this remains an explicit qualification gate.

The foreign fallback and shared import tests depend on the dynamic import join
compiler path. They ran against the already-committed `d8dc1a6dd` repair; later
backend fixture/comparator commits are outside the qualification source pin. No
waiting for or importing later sibling work occurred.
The initial attempts are preserved in `initial-mise-failure/` and
`initial-goroot-failure/`: untrusted export mise config, then inherited Go 1.27.3
GOROOT with a Go 1.27.1 executable. These were harness failures before test
execution, repaired by using the real executable and explicitly matching GOROOT.

## Separate Core 3 diagnosis (no expectations changed)

`core3-native-excerpt.json` preserves raw events from native run 41396042416,
ordinary full-root replay, with archive identity. The failing
`gc/const-expr-gc` child reports **9 modules/17 assertions passed and 1 module/1
assertion skipped**, not ten independent module failures. The fixture contains
10 modules and 18 assertions. `TestWasmtimeCore3Corpus` calls `t.Fatalf` on any
fixture skip **before** `total.add(stats)`. Consequently the entire fixture is
excluded: 215-10=205 and 690-18=672. The global deficit is secondary accounting
from a failed fixture; changing either totals or expectations would conceal it.

The strongest source-supported explanation is `commands.5.wasm`: the non-null
converted externref global initialized from a struct (value 55), followed by
`extern-convert-any-const`. On Darwin, `initializeConversionReceipts` in
`gc_conversion_store_other.go` rejects that global via `unsupportedConversionWord`.
The spec executor then skips the module and its unavailable assertion. This is
an inference from source plus the matching census, **not a captured child error**:
`regressionFixtureOutcome` transports six totals but drops gap reasons/sites,
and successful child-process output is not printed by the parent. Thus the
zero gap array in the parent diagnostic does not establish absence of a gap.
No new production defect is claimed; the rejection is explicitly documented
unsupported H6 behavior already identified in the native-run evidence.

Exact next step outside these five tests: on the parent's pinned native Darwin
runner, execute the child directly, retaining verbose output, in each mode:

```sh
WAGO_WASMTIME_CORE3_FIXTURE=gc/const-expr-gc.wast \
WAGO_REGRESSION_CHILD_PROTOCOL=2 \
WAGO_REGRESSION_CHILD_NONCE=00112233445566778899aabbccddeeff \
WAGO_BOUNDS=explicit ./wago.test \
  -test.run='^TestWasmtimeCore3Corpus$' -test.v -test.count=1
```

The explicit child marker must report 10/18 with zero gaps for conformance
qualification; a process exit of zero alone is insufficient. Confirm the
instantiation rejection at commands.json module_instance line 96 and assertion
line 97 (commands.5.wasm). If confirmed, closing the conformance gate requires
an explicitly owned Darwin converted-publication/initialization implementation
and its ownership/lifetime proof, or an explicit parent scope decision retaining
this unmet gate. It cannot be repaired inside these five test files. Do not
change 215/690 or count a rejected module as passing. Full-corpus native rerun
belongs to the parent, after the production/target gate is resolved.

## Handoff limits

Native Darwin execution of these repaired tests remains NOT_RUN locally.
Parent coordinates review and exact-source native rerun in all three modes.
No broad full qualification, push, PR, subagent, benchmark or measurement was
performed. These test/evidence-only changes cannot affect shipped runtime
performance; no measurement lease is needed. Existing user dirt, index entries
and shared submodules are preserved. Existing archived failures remain intact.
