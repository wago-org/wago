# Integer join-pressure measurement archive

This archive records the 2026-10-19 native AMD64 comparison described in
[the profile](../../integer-join-pressure.md#current-main-comparison-and-arm64-checks-2026-10-19).
It belongs to the pinned builds in `identity.json`, not later test-binary builds.

- `measure-original.py` is the exact pairing and summary script used for those
  samples. Its absolute path records the original evidence location.
- `measure.py` is a portable replay of the same pairing, timing, environment,
  parsing, and summary procedure. It accepts the evidence directory as an
  argument and checks that both test binaries exist before it starts.
- `identity.json` records source revisions, tool versions, selected environment,
  fixture/native hashes, and test-binary hashes. It does not record CPU governor,
  frequency, or continuous host load; those were not isolated.
- `raw-process-records.json` preserves all 16 stdout, stderr, and exit records.
- `summary.json` contains the computed paired medians, ranges, CV, and resource data.
- [The CSV](../../integer-join-pressure-20261019.csv) contains all 192 rows.

Build each pinned source with Go 1.27.2 and the identical architecture test file.
In the baseline checkout, first copy the measured fixture:

```sh
git show 7e8829b9bab3d5d9a9e29f8dfb59acdeebf3e46b:src/core/compiler/backend/railshot/amd64/integer_join_pressure_test.go > src/core/compiler/backend/railshot/amd64/integer_join_pressure_test.go
```

Then build the matching binaries in their respective pinned checkouts:

```sh
go test -c -o /chosen/evidence/main-amd64.test ./src/core/compiler/backend/railshot/amd64
go test -c -o /chosen/evidence/pr-amd64.test ./src/core/compiler/backend/railshot/amd64
```

Run the first command in the baseline checkout and the second in the measured
candidate checkout. Then run the portable replay script from this directory:

```sh
python3 measure.py /chosen/evidence
```

Keep both binary names unchanged and run with no concurrent builds or tests.
The script removes inherited `WAGO_*`
variables, sets one Go thread, pins CPU 4, alternates eight pairs, and checks for
12 rows per process. Its explicit bounds environment matches the session;
the backend compile options set bounds directly, so that variable does not
select the strategy for this benchmark.

The two test binaries were built in separate worktrees without `-trimpath`.
Their hashes differ. Worktree paths and binary layout can differ; these samples
do not identify the cause of timing variation. No performance or memory gain
is claimed. No native ARM64 timing was measured.
