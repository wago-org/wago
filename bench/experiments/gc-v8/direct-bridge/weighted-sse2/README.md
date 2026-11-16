# weighted-sse2 matched protocol

Measured qualified runtime: `b92d0e24f03e032c30700cea25badfd60b5c48fe`.
Builds, audits, bounded/full measurements and separate compile checks completed.
See `summary.md` and retained raw `evidence/` for results and limitations.

Build two binaries sequentially from the SAME qualified source, with the SAME
probe: `baseline.test` uses `wago_gc_compact_weighted_sum,wago_gc_compact_refstruct`; `candidate.test` uses `wago_gc_compact_weighted_sse2,wago_gc_compact_refstruct`.
Runtime age2 is OFF in both. Shared checks, stride, cohort, shadow and metrics are OFF.
Allocation elision is OFF. Exact six artifacts and checksum/allocation assertions
are unchanged. Young budget is 2 MiB; old budget is two 8 MiB semispaces.
Promote-all active nursery is 2 MiB. The only compiler difference is the
weighted-reduction SSE2 experiment. This is not a collector-only comparison.

After explicit qualification and CPU release:
1. Build with Go 1.27.1, `go test -p 1 -c`, exact tags above; save source and binary hashes.
2. `run.py --binary-dir DIR --output-dir AUDIT --audit`: 128 warmups, no timed calls; require real collection for every case.
3. `run.py --binary-dir DIR --output-dir BOUNDED --matched`: 3 warmups + 5 calls, three rotating fresh-process rounds.
4. Report bounded results before any long gate. `full-v8.py` retains 100 warmups + 9 batches of 32 calls, three rotating rounds, contemporaneous Node `--no-liftoff`.
5. Separately run compile/close measurements with identical cases: 256 iterations, three rotating fresh processes, Go B/op and allocs/op, native code size, compile time. Do not mix this into execution timings.

Capture actual allocation/collection counters, checksums, Go heap and proc VmHWM.
Do not infer live/promoted bytes from uninstrumented generic counters. Keep all
controls, regressions and outliers. Require each case <=1.15x V8, never aggregate
away misses. Shared-host variation and opaque V8 allocation optimizations limit
causal interpretation. No historical cross-host baseline. Diagnostic binaries
must not enter timing results. Preserve bounded and full protocols separately.
