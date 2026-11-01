# Current Snap port full-suite qualification

Snapshot under test: `d546a1af4513c63e4bda6fae7d19cf9cd86f332a`.
Production runtime commit: `5b31c81ec0c74695a90bc8d36754b92845b59f14`.
The source trees under src, cli, bench, tests and scripts are identical between
these two commits. Concurrent working-tree additions are excluded.

**Full-root gate: PASS in all three modes after reproducible host-tool fixes.**
**Aggregate qualification: BLOCKED by one unresolved intermittent TinyGo CLI
abort; this is not an unconditional green report.** See TINYGO-FINDING.md.
Both baseline and current targeted probes subsequently passed 3/3; that does
not erase the original failure or establish its cause. Raw Go JSON
streams retain built-in timestamps/durations as emitted, without interpreting
them as performance measurements. No benchmark flags or intentional timing
commands are used. Evidence-only files cannot affect shipped runtime performance.

## Reproduce

From the assigned branch worktree, with the stage3-toolchain user-local OCaml
installation prepared (see its README):

```sh
E=docs/rfwgc-evidence/stage3-full-suite
bash "$E/prepare.sh" .tools/stage3-full-suite/current-new
bash "$E/run.sh" .tools/stage3-full-suite/current-new "$E"
bash "$E/independent.sh" .tools/stage3-full-suite/current-new "$E"
python3 "$E/verify-export.py" d546a1af4513c63e4bda6fae7d19cf9cd86f332a .tools/stage3-full-suite/current-new
python3 "$E/summarize.py"
```

Use a fresh output directory/copy of these scripts to preserve existing logs.
`run.sh` intentionally preserves the initial environment used for the first
matrix. It lacks the .NET globalization workaround discovered during that run.
For a fresh fully provisioned run, export
`DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1` before invoking it.
`independent.sh` sets this explicitly and repeats every complete root mode,
then runs the additional gates. The driver records every exit and continues
through failures. There are no expectation updates or failed-test exclusions.

Go/GOROOT are explicitly 1.27.1, GOTOOLCHAIN=local, GOMAXPROCS=2,
GOFLAGS=-p=2, and package timeout 30 minutes. Release 3 interpreter and WABT
1.0.41 are rebuilt/verified by the existing pinned preparation script.
Pinned OCaml 4.14.3, dune 3.22.1 and menhir 20260209 are user-local.
`prepare.log`, `provenance.txt`, command files and fixture logs retain provenance.

Exact fixtures are spec-v1 a8bcbafe6d2fb191ce0188de0e18fdc107fa2598,
spec-v2 05ca4182176763112561ae20153975c12bd689e4, and
spec-v3 9d36019973201a19f9c9ebb0f10828b2fe2374aa. They are cloned and detached
only inside the private archive. The initial fixture-source lookup for v2 used
an absent path in the main checkout; fixtures.sh now uses the assigned
worktree's repository objects. Both the failed attempt and successful retry
are retained. No dirty source fixture is copied as test input.

The first ordinary root attempt failed two PowerShell bootstrap tests because
mise did not trust the new export. After trusting its inspected mise.toml,
the next mode exposed missing system ICU in the installed PowerShell 7.6.8.
Supported .NET invariant globalization fixes that dependency without production
edits or test exclusions. Failed first runs are retained alongside complete
reruns. These are host-tool failures, not Wago engine findings.

## Scope

See INVENTORY.md for independent CI gates and module boundaries. These results
are Linux x86-64 Go execution evidence, not full multi-platform CI qualification.
Default-policy conformance is not region-enabled conformance. Actual region
test outcomes are separately extracted in summary.json. New runtime changes or
host integration tests from concurrent workers require fresh qualification.

## Final results

| Final full-root run | Packages passed | Top-level passed / failed / skipped | Subtests passed / failed / skipped |
| --- | ---: | --- | --- |
| ordinary | 137 | 6308 / 0 / 287 | 26024 / 0 / 440 |
| wagodebug | 137 | 6320 / 0 / 298 | 26030 / 0 / 440 |
| wago_regalloccheck | 137 | 6637 / 0 / 288 | 31971 / 0 / 440 |

Each root run also reports 50 packages without tests as package skips. All six
native region tests and the guest caller-root test pass in all three modes;
the latter includes four lifetime/region configurations. These outcomes are
separately listed in summary.json. No failed test was skipped or excluded.

Runtime-tagged CLI passes ordinary and checked modes (740 top-level passes
and four skips each). Debug CLI records 739 passes, four skips and the one
TinyGo interruption failure. Both nested modules pass in all modes: installer
1 top-level test and nine subtests, bench 73 top-level tests and 432 subtests
with eight top-level/two subtest skips. Those bench runs use the default quick
corpus, followed by a separate all-corpus run.

All supplemental commands exit zero: focused race (three packages), debug GC
hardening with explicit/guard-page bounds (three repetitions each), matching
host-transition stress, checked guard-page runtime/API suites, and the complete
corpus/application selection (13 top-level tests, 867 passing subtests and 12
built-in subtest skips). See accounting.txt and summary.json for exact counts.
Repetition counts in GC-hardening accounting are test executions, not unique tests.

Spec v1 executes 629 modules and 16,026 assertions; spec v3 explicit and signals
each execute 2,226 modules and 58,238 assertions. All have zero failure/skip/gap
accounting. Exact Core v2 execution in each root mode executes 1,600 modules and
48,331 assertions with zero failures/skips/gaps. The separate Core v2 validation
harness reports 2,880 passing and 1,077 skipped assertions; these validation
skips are not erased by execution coverage. See spec-accounting.txt.

Raw logs are losslessly compressed as *.jsonl.gz. `gzip -dc` reads the original
stream; summarize.py accepts compressed and uncompressed streams. Command files,
stderr, failed attempts, result exits and both targeted reproduction runs remain.

## Integrity and remaining gates

Before/after identity checks verify all 8,007 current and 7,374 baseline tracked
blobs. Fixture identities also match their pins after testing, without tracked
fixture changes. Original dirty corpus scripts, port log and spec-v1/v2 checkouts
were not edited. Only this evidence directory is committed by this worker.

The immutable baseline is the reused stage3-toolchain export at 9713878ab; the
same harness/tool binaries are used for its interruption probe. Current and
baseline each pass all three targeted repetitions. The observed abort remains
unresolved and is not attributed to region mode or dismissed as pre-existing.

Not run here: the full multi-platform/TinyGo CI matrix, portable 386 lane,
bounded fuzz campaigns beyond normal seed tests, native-compare diagnostics,
diagnostic DCE/generated-code identity, codegen diagnostic variants, complete
lint/docs/release/script CI, corpus shard aggregation/rebuild and release/size
qualification. INVENTORY.md identifies their independent status. TinyGo 0.43.0
was available locally; CI pins 0.42.0, so these runs do not qualify that pinned
TinyGo lane. Other architectures/OSes and new concurrent test additions remain
outside the evidence snapshot. No PR, push, production edit, expectation update,
subdelegation, benchmark, or intentional measurement occurred.
