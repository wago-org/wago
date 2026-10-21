# PR #992 ARM64 signal-log test race

The Linux/arm64 job for source `f2a9a040705ea1c172b1bb10e9118e00b8cd270e`
failed in `TestWatchSignalSendsOnceToIsolatedGroup`: the leaf signal count was
an empty string instead of `1`. Job:
https://github.com/wago-org/wago/actions/runs/39606582329/job/119248458290

The signal recorder uses `os.WriteFile`. A reader can see the newly created
file before its data is written. The test helper previously waited only for
file existence, then immediately read the count. This is a test synchronization
race, unrelated to native null extern conversions.

Wait for nonempty data in both readiness and signal files. Keep the five-second
timeout and the exact `1` count assertion, so a missing signal, write failure,
or duplicate signal still causes a visible failure. All helper callers expect
nonempty files. A regression test holds a file empty for 50 ms before writing
`1`: the original helper fails with empty data, and the fixed helper waits.

Validation and review results are recorded below. Native ARM64 execution is
left to the hosted ARM64 check; a cross-build is not an ARM64 execution result.

The regression failed before the fix (7.141 seconds including build) and passes
after it (0.605 seconds). The regression and original signal test each pass
100 repeats (17.467 seconds). The complete run package passes (2.774 seconds),
and the Linux/arm64 test executable cross-build passes (7.648 seconds).
Daybreak Blue's read-only review found no concrete issue and confirmed the
exact count check and writer cleanup are retained. Raw red/green/stress/build
results, the original job log, and review metadata are in
`docs/research/data/pr992-arm64-signal-log-race-2026-10-21/`.
